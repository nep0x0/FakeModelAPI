package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"fakemodelapi/internal/provider"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    "invalid_request_error",
			"code":    status,
		},
	})
}

// handleModels melayani GET /v1/models.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	p, err := s.currentProvider()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	models := p.Models()
	data := make([]map[string]any, 0, len(models))
	for _, m := range models {
		data = append(data, map[string]any{
			"id":       m.ID,
			"object":   "model",
			"created":  0,
			"owned_by": s.providerName,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

// handleChatCompletions melayani POST /v1/chat/completions.
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req chatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "body tidak valid: "+err.Error())
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages tidak boleh kosong")
		return
	}

	p, err := s.currentProvider()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !p.AuthStatus().LoggedIn {
		writeError(w, http.StatusUnauthorized, "belum login: jalankan /login di TUI lalu /start lagi")
		return
	}

	msgs := make([]provider.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, provider.Message{Role: m.Role, Content: string(m.Content)})
	}
	ctx := r.Context()

	var ag *agent
	if len(req.Tools) > 0 {
		ag = newAgent(ctx, p, req.Tools, msgs)
	}

	if req.Stream {
		s.streamChat(w, ctx, p, msgs, req.Model, ag)
		return
	}
	s.completeChat(w, ctx, p, msgs, req.Model, ag)
}

// completeChat melayani request non-streaming.
func (s *Server) completeChat(w http.ResponseWriter, ctx context.Context, p provider.Provider, msgs []provider.Message, model string, ag *agent) {
	var text string
	var err error
	if ag != nil {
		text, err = ag.run(nil)
	} else {
		text, err = p.Chat(ctx, msgs)
	}
	if err != nil {
		writeProviderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, chatCompletionResponse{
		ID:      newID(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []chatCompletionChoice{{
			Index:        0,
			Message:      openaiMessage{Role: "assistant", Content: openAIContent(text)},
			FinishReason: "stop",
		}},
		Usage: usage{PromptTokens: 0, CompletionTokens: 0, TotalTokens: 0},
	})
}

// streamChat melayani request streaming (SSE).
func (s *Server) streamChat(w http.ResponseWriter, ctx context.Context, p provider.Provider, msgs []provider.Message, model string, ag *agent) {
	if ag == nil {
		// Minta stream dulu sebelum menulis header SSE, supaya error masih bisa
		// dikirim sebagai JSON biasa.
		ch, err := p.ChatStream(ctx, msgs)
		if err != nil {
			writeProviderError(w, err)
			return
		}
		s.streamProvider(w, ctx, model, ch)
		return
	}

	// Path agent: tulis header SSE SEGERA sebelum loop berjalan, supaya client
	// tidak menunggu terlalu lama dan koneksi tidak dianggap timeout. Selama
	// loop, kirim komentar progress sebagai keep-alive.
	flusher, ok := writeSSEHeaders(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming tidak didukung")
		return
	}

	progress := func(step int, tool string) {
		writeSSE(w, fmt.Sprintf(": agent step %d/%d tool=%s\n\n", step, maxAgentSteps, tool))
		flusher.Flush()
	}

	text, err := ag.run(progress)
	if err != nil {
		// Stream sudah terkirim; error dikirim sebagai delta teks.
		writeChunk(w, flusher, newID(), time.Now().Unix(), model,
			map[string]any{"content": "\n[error] " + err.Error()}, nil)
		writeChunk(w, flusher, newID(), time.Now().Unix(), model, map[string]any{}, "stop")
		writeSSE(w, "data: [DONE]\n\n")
		return
	}
	s.streamText(w, flusher, model, text)
}

// writeSSEHeaders menulis header streaming dan mengembalikan flusher.
// Return ok=false jika writer tidak mendukung streaming.
func writeSSEHeaders(w http.ResponseWriter) (http.Flusher, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	w.Header().Set("content-type", "text/event-stream")
	w.Header().Set("cache-control", "no-cache")
	w.Header().Set("connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return flusher, true
}

// streamText menulis teks final (hasil agent loop) sebagai SSE chunks.
// Header SSE sudah harus ditulis oleh pemanggil.
func (s *Server) streamText(w http.ResponseWriter, flusher http.Flusher, model, text string) {
	id := newID()
	created := time.Now().Unix()

	runes := []rune(text)
	const chunkLen = 100
	for i := 0; i < len(runes); i += chunkLen {
		end := i + chunkLen
		if end > len(runes) {
			end = len(runes)
		}
		delta := map[string]any{"content": string(runes[i:end])}
		if i == 0 {
			delta["role"] = "assistant"
		}
		writeChunk(w, flusher, id, created, model, delta, nil)
	}
	writeChunk(w, flusher, id, created, model, map[string]any{}, "stop")
	writeSSE(w, "data: [DONE]\n\n")
}

// streamProvider meneruskan stream asli dari provider sebagai SSE OpenAI.
func (s *Server) streamProvider(w http.ResponseWriter, ctx context.Context, model string, ch <-chan provider.Chunk) {
	flusher, ok := writeSSEHeaders(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming tidak didukung")
		return
	}

	id := newID()
	created := time.Now().Unix()
	first := true

	for {
		select {
		case <-ctx.Done():
			return
		case c, ok := <-ch:
			if !ok {
				writeChunk(w, flusher, id, created, model, map[string]any{}, "stop")
				writeSSE(w, "data: [DONE]\n\n")
				return
			}
			if c.Delta != "" {
				delta := map[string]any{"content": c.Delta}
				if first {
					delta["role"] = "assistant"
					first = false
				}
				writeChunk(w, flusher, id, created, model, delta, nil)
			}
		}
	}
}

func writeSSE(w http.ResponseWriter, s string) {
	fmt.Fprint(w, s)
}

// writeChunk menulis satu event SSE dalam format OpenAI.
func writeChunk(w http.ResponseWriter, f http.Flusher, id string, created int64, model string, delta map[string]any, finish any) {
	chunk := chatCompletionChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []chatCompletionChunkChoice{{Index: 0, Delta: delta, FinishReason: finish}},
	}
	data, err := json.Marshal(chunk)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	f.Flush()
}

// writeProviderError memetakan error provider ke status HTTP.
func writeProviderError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, provider.ErrNotAuthenticated):
		writeError(w, http.StatusUnauthorized, "session tidak valid, jalankan /login lalu /start lagi di TUI: "+err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "request ke provider timeout: "+err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "provider error: "+err.Error())
	}
}
