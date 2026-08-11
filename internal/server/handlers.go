package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"fakemodelapi/internal/conversation"
	"fakemodelapi/internal/errs"
	"fakemodelapi/internal/openai"
	"fakemodelapi/internal/provider"
	"fakemodelapi/internal/telemetry"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErrorKind menulis error dengan format OpenAI-compatible yang konsisten
// dan pesan actionable.
func writeErrorKind(w http.ResponseWriter, kind errs.Kind, msg, action string, cause error) {
	full := msg
	if cause != nil && !strings.Contains(full, cause.Error()) {
		full += " (" + cause.Error() + ")"
	}
	status := errs.HTTPStatus(kind)
	body := map[string]any{
		"error": map[string]any{
			"message": full,
			"type":    errs.OpenAIType(kind),
			"code":    status,
		},
	}
	if action != "" {
		body["error"].(map[string]any)["action"] = action
	}
	writeJSON(w, status, body)
}

// writeProviderError memetakan error provider ke kategori errs dan status
// HTTP OpenAI-compatible, lengkap dengan saran aksi untuk user.
func writeProviderError(w http.ResponseWriter, err error) {
	kind := errs.KindInternal
	msg := "error internal server"
	action := ""
	switch {
	case errors.Is(err, provider.ErrNotAuthenticated):
		kind = errs.KindSessionExpired
		msg = "session tidak valid atau kedaluwarsa"
		action = "Jalankan /login di TUI untuk memperbarui session, lalu /start lagi."
	case errors.Is(err, context.DeadlineExceeded) || errs.Is(err, errs.KindTimeout):
		kind = errs.KindTimeout
		msg = "request ke provider melebihi batas waktu"
		action = "Coba lagi; jika terus terjadi, periksa koneksi internet."
	case errs.Is(err, errs.KindRateLimited):
		kind = errs.KindRateLimited
		msg = "provider membatasi jumlah request"
		action = "Tunggu beberapa saat lalu coba lagi."
	case errs.Is(err, errs.KindProviderUnavailable) || errs.Is(err, errs.KindInvalidResponse):
		kind = errs.KindProviderUnavailable
		msg = "provider tidak tersedia atau respons tidak valid"
		action = "Cek /doctor untuk diagnosis; jika session bermasalah, jalankan /login ulang."
	case errs.Is(err, errs.KindUnauthorized):
		kind = errs.KindUnauthorized
		msg = "tidak diizinkan mengakses provider"
		action = "Periksa kembali session login dan token lokal."
	}
	writeErrorKind(w, kind, msg, action, err)
}

// handleHealthz melayani GET /healthz — dipakai `fakeapi doctor` dan
// opencode-style health check.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorKind(w, errs.KindUnsupportedFeature, "method not allowed", "Gunakan GET untuk /healthz.", nil)
		return
	}
	p, err := s.currentProvider()
	if err != nil {
		writeErrorKind(w, errs.KindInternal, "provider tidak tersedia", "Jalankan /doctor untuk diagnosis.", err)
		return
	}
	ai := p.AuthStatus()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"provider": s.providerName,
		"logged_in": ai.LoggedIn,
		"uptime_s": int(time.Since(s.startedAt).Seconds()),
	})
}

// handleModels melayani GET /v1/models.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorKind(w, errs.KindUnsupportedFeature, "method not allowed", "Gunakan GET untuk /v1/models.", nil)
		return
	}
	p, err := s.currentProvider()
	if err != nil {
		writeErrorKind(w, errs.KindInternal, "provider tidak tersedia", "Jalankan /doctor untuk diagnosis.", err)
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
		writeErrorKind(w, errs.KindUnsupportedFeature, "method not allowed", "Gunakan POST untuk /v1/chat/completions.", nil)
		return
	}

	var req openai.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeErrorKind(w, errs.KindRequestTooLarge, "body request terlalu besar", "Kurangi jumlah pesan yang dikirim dalam satu request.", err)
		} else {
			writeErrorKind(w, errs.KindInvalidRequest, "body tidak valid", "Periksa format JSON request.", err)
		}
		return
	}
	if len(req.Messages) == 0 {
		writeErrorKind(w, errs.KindInvalidRequest, "messages tidak boleh kosong", "Kirim minimal satu pesan berisi instruksi.", nil)
		return
	}

	p, err := s.currentProvider()
	if err != nil {
		writeErrorKind(w, errs.KindInternal, "provider tidak tersedia", "Jalankan /doctor untuk diagnosis.", err)
		return
	}
	if !p.AuthStatus().LoggedIn {
		writeErrorKind(w, errs.KindSessionExpired, "belum login", "Jalankan /login di TUI lalu /start lagi.", nil)
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

	// Request dengan tools: system prompt asli dipertahankan dan instruksi
	// protokol ditambahkan; tool call diemulasi sebagai tool_calls native
	// agar OpenCode yang mengeksekusi (dengan sistem permission-nya).
	if len(req.Tools) > 0 && p.Capabilities().SupportsTools {
		msgs = conversation.Compile(msgs, req.Tools)
		s.activity.Add("request", "chat request dengan tools (model: "+req.Model+")", nil)
		if req.Stream {
			streamChatWithTools(w, ctx, p, msgs, req.Model, req.Tools)
			return
		}
		completeChatWithTools(w, ctx, p, msgs, req.Model, req.Tools)
		return
	}

	s.activity.Add("request", "chat request masuk (model: "+req.Model+")", nil)

	if req.Stream {
		s.streamChat(w, ctx, p, msgs, req.Model)
		return
	}
	s.completeChat(w, ctx, p, msgs, req.Model)
}

// completeChat melayani request non-streaming.
func (s *Server) completeChat(w http.ResponseWriter, ctx context.Context, p provider.Provider, msgs []provider.Message, model string) {
	text, err := p.Chat(ctx, model, msgs)
	if err != nil {
		s.activity.Add("error", "chat gagal: "+err.Error(), nil)
		writeProviderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, openai.ChatCompletionResponse{
		ID:      openai.NewID(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []openai.ChatCompletionChoice{{
			Index:        0,
			Message:      openai.Message{Role: "assistant", Content: openai.Content(text)},
			FinishReason: "stop",
		}},
		Usage: openai.Usage{PromptTokens: 0, CompletionTokens: 0, TotalTokens: 0},
	})
}

// streamChat melayani request streaming (SSE).
func (s *Server) streamChat(w http.ResponseWriter, ctx context.Context, p provider.Provider, msgs []provider.Message, model string) {
	// Minta stream dulu sebelum menulis header SSE, supaya error masih bisa
	// dikirim sebagai JSON biasa.
	ch, err := p.ChatStream(ctx, model, msgs)
	if err != nil {
		s.activity.Add("error", "chat stream gagal: "+err.Error(), nil)
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
		writeErrorKind(w, errs.KindUnsupportedFeature, "streaming tidak didukung", "Gunakan request non-streaming.", nil)
		return
	}

	id := openai.NewID()
	created := time.Now().Unix()
	first := true
	start := time.Now()

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
					s.logger.Request(telemetry.Request{
						ID:         requestIDFromContext(ctx),
						Method:     "POST",
						Path:       "/v1/chat/completions",
						Status:     200,
						Latency:    time.Since(start),
						FirstToken: time.Since(start),
						Provider:   s.providerName,
						Model:      model,
					})
				}
				writeChunk(w, flusher, id, created, model, delta, nil)
			}
		}
	}
}

func requestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyRequestID).(string); ok {
		return v
	}
	return ""
}

func writeSSE(w http.ResponseWriter, s string) {
	fmt.Fprint(w, s)
}

// writeChunk menulis satu event SSE dalam format OpenAI.
func writeChunk(w http.ResponseWriter, f http.Flusher, id string, created int64, model string, delta map[string]any, finish any) {
	chunk := openai.ChatCompletionChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []openai.ChatCompletionChunkChoice{{Index: 0, Delta: delta, FinishReason: finish}},
	}
	data, err := json.Marshal(chunk)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	f.Flush()
}
