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
		pm := provider.Message{Role: m.Role, Content: string(m.Content), ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			var args any
			if len(tc.Function.Arguments) > 0 {
				if err := json.Unmarshal(tc.Function.Arguments, &args); err != nil {
					args = string(tc.Function.Arguments)
				}
			}
			pm.ToolCalls = append(pm.ToolCalls, provider.MessageToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: args,
			})
		}
		msgs = append(msgs, pm)
	}
	ctx := r.Context()

	// Request dengan tools: system prompt asli OpenCode dipertahankan dan
	// instruksi protokol ditambahkan; tool call diemulasi sebagai tool_calls
	// native agar OpenCode yang mengeksekusi (dengan sistem permission-nya).
	if len(req.Tools) > 0 {
		sys := buildToolSystemPrompt(joinSystemPrompts(msgs), req.Tools)
		clean := make([]provider.Message, 0, len(msgs))
		for _, m := range msgs {
			if m.Role != "system" {
				clean = append(clean, m)
			}
		}
		msgs = append([]provider.Message{{Role: "system", Content: sys}}, clean...)

		if req.Stream {
			streamChatWithTools(w, ctx, p, msgs, req.Model, req.Tools)
		} else {
			completeChatWithTools(w, ctx, p, msgs, req.Model, req.Tools)
		}
		return
	}

	if req.Stream {
		s.streamChat(w, ctx, p, msgs, req.Model)
		return
	}
	s.completeChat(w, ctx, p, msgs, req.Model)
}

// completeChat melayani request non-streaming.
func (s *Server) completeChat(w http.ResponseWriter, ctx context.Context, p provider.Provider, msgs []provider.Message, model string) {
	text, err := p.Chat(ctx, msgs)
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
func (s *Server) streamChat(w http.ResponseWriter, ctx context.Context, p provider.Provider, msgs []provider.Message, model string) {
	// Minta stream dulu sebelum menulis header SSE, supaya error masih bisa
	// dikirim sebagai JSON biasa.
	ch, err := p.ChatStream(ctx, msgs)
	if err != nil {
		writeProviderError(w, err)
		return
	}
	s.streamProvider(w, ctx, model, ch)
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
