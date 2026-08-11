package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"fakemodelapi/internal/errs"
	"fakemodelapi/internal/openai"
	"fakemodelapi/internal/provider"
	"fakemodelapi/internal/toolcall"
)

// toolBufferThreshold: jumlah karakter awal yang ditampung sebelum stream
// diputuskan sebagai teks biasa. Blok ```tool dalam jendela ini (di mana pun
// posisinya — model sering menulis prosa dulu) dianggap sebagai tool call.
const toolBufferThreshold = 2048

// completeChatWithTools melayani request non-streaming yang membawa tools:
// respons model diterjemahkan menjadi tool_calls OpenAI native. Eksekusi
// dilakukan oleh client (OpenCode) — proxy tidak pernah menjalankan tool.
func completeChatWithTools(w http.ResponseWriter, ctx context.Context, p provider.Provider, msgs []provider.Message, model string, tools []openai.Tool) {
	text, err := p.Chat(ctx, model, msgs)
	if err != nil {
		writeProviderError(w, err)
		return
	}

	if calls, ok := toolcall.Detect(text); ok {
		if known := toolcall.Validate(calls, toolcall.Allowed(tools)); len(known) > 0 {
			writeJSON(w, http.StatusOK, openai.ChatCompletionResponse{
				ID:      openai.NewID(),
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   model,
				Choices: []openai.ChatCompletionChoice{{
					Index:        0,
					Message:      openai.Message{Role: "assistant", ToolCalls: toolcall.MapToOpenAI(known)},
					FinishReason: "tool_calls",
				}},
				Usage: openai.Usage{PromptTokens: 0, CompletionTokens: 0, TotalTokens: 0},
			})
			return
		}
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

// streamChatWithTools melayani request streaming yang membawa tools.
// Strategi: tampung awal stream sampai (a) terlihat blok ```tool/```json di
// mana pun dalam jendela buffer → seluruh respons ditampung lalu dikirim
// sebagai delta tool_calls native; atau (b) buffer melewati threshold tanpa
// blok → stream sebagai delta content biasa. Jika blok muncul di tengah
// stream teks yang sudah berjalan, sisa stream ditampung dan dikirim sebagai
// tool_calls (pesan jadi berisi teks + tool_calls).
func streamChatWithTools(w http.ResponseWriter, ctx context.Context, p provider.Provider, msgs []provider.Message, model string, tools []openai.Tool) {
	flusher, ok := writeSSEHeaders(w)
	if !ok {
		writeErrorKind(w, errs.KindUnsupportedFeature, "streaming tidak didukung", "Gunakan request non-streaming.", nil)
		return
	}

	ch, err := p.ChatStream(ctx, model, msgs)
	if err != nil {
		writeProviderError(w, err)
		return
	}

	id := openai.NewID()
	created := time.Now().Unix()

	var buf strings.Builder
	toolMode := false
	decided := false
	first := true
	tail := ""

	for {
		select {
		case <-ctx.Done():
			return
		case c, ok := <-ch:
			if !ok {
				if !toolMode {
					// Stream selesai: putuskan berdasarkan seluruh isi buffer.
					toolMode = toolcall.HasFence(buf.String())
				}
				if toolMode {
					emitToolCallsSSE(w, flusher, id, created, model, buf.String(), tools)
				} else {
					flushTextBuffer(w, flusher, id, created, model, &buf, &first)
					writeChunk(w, flusher, id, created, model, map[string]any{}, "stop")
					writeSSE(w, "data: [DONE]\n\n")
				}
				return
			}

			if toolMode {
				buf.WriteString(c.Delta)
				continue
			}

			if !decided {
				buf.WriteString(c.Delta)
				if toolcall.HasFence(buf.String()) {
					toolMode, decided = true, true
				} else if buf.Len() >= toolBufferThreshold {
					decided = true
					flushTextBuffer(w, flusher, id, created, model, &buf, &first)
				}
				continue
			}

			// Mode teks: cek fence di tail stream.
			probe := tail + c.Delta
			if toolcall.HasFence(probe) {
				toolMode = true
				buf.WriteString(probe)
				tail = ""
				continue
			}
			tail = trimTail(probe)
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

// trimTail menyimpan beberapa karakter terakhir dari teks yang sudah dikirim
// untuk mendeteksi fence yang membentang antar-delta.
func trimTail(s string) string {
	r := []rune(s)
	if len(r) > 8 {
		return string(r[len(r)-8:])
	}
	return s
}

// flushTextBuffer mengirim teks yang sempat tertahan di buffer sebagai satu
// delta content.
func flushTextBuffer(w http.ResponseWriter, f http.Flusher, id string, created int64, model string, buf *strings.Builder, first *bool) {
	if buf.Len() == 0 {
		return
	}
	delta := map[string]any{"content": buf.String()}
	if *first {
		delta["role"] = "assistant"
		*first = false
	}
	writeChunk(w, f, id, created, model, delta, nil)
	buf.Reset()
}

// emitToolCallsSSE mengirim blok ```tool yang sudah dikumpulkan sebagai delta
// tool_calls native, lalu menutup stream dengan finish_reason "tool_calls".
func emitToolCallsSSE(w http.ResponseWriter, f http.Flusher, id string, created int64, model, text string, tools []openai.Tool) {
	writeRawText := func() {
		writeChunk(w, f, id, created, model, map[string]any{"role": "assistant", "content": text}, nil)
		writeChunk(w, f, id, created, model, map[string]any{}, "stop")
		writeSSE(w, "data: [DONE]\n\n")
	}

	calls, ok := toolcall.Detect(text)
	if !ok {
		// Model memakai blok ```tool tapi isinya tidak valid — kirim sebagai
		// teks mentah, jangan pernah diterjemahkan menjadi tool_calls.
		writeRawText()
		return
	}
	known := toolcall.Validate(calls, toolcall.Allowed(tools))
	if len(known) == 0 {
		// Semua tool tak dikenal — kirim sebagai teks mentah.
		writeRawText()
		return
	}

	for i, call := range known {
		argsJSON, err := json.Marshal(call.Args)
		if err != nil {
			argsJSON = []byte("{}")
		}
		delta := map[string]any{
			"tool_calls": []map[string]any{{
				"index":    i,
				"id":       toolCallID(),
				"type":     "function",
				"function": map[string]any{"name": call.Name, "arguments": string(argsJSON)},
			}},
		}
		if i == 0 {
			delta["role"] = "assistant"
		}
		writeChunk(w, f, id, created, model, delta, nil)
	}
	writeChunk(w, f, id, created, model, map[string]any{}, "tool_calls")
	writeSSE(w, "data: [DONE]\n\n")
}

func toolCallID() string {
	return fmt.Sprintf("call_%d", time.Now().UnixNano())
}
