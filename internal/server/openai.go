package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Tipe-tipe minimal ala OpenAI API. Tidak butuh SDK eksternal —
// hanya subset yang dipakai OpenCode.

// openAIContent adalah isi pesan yang bisa datang sebagai string biasa
// atau array content parts (format OpenAI modern). Semua bentuk diubah
// menjadi teks polos.
type openAIContent string

// UnmarshalJSON menerima `"teks"`, `[{"type":"text","text":"..."}]`, atau null.
func (c *openAIContent) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*c = ""
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*c = openAIContent(s)
		return nil
	}
	if data[0] == '[' {
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(data, &parts); err != nil {
			return err
		}
		var b strings.Builder
		for _, p := range parts {
			if p.Text != "" {
				b.WriteString(p.Text)
			}
		}
		*c = openAIContent(b.String())
		return nil
	}
	return fmt.Errorf("content tidak valid: %s", string(data))
}

// MarshalJSON selalu mengirim content sebagai string.
func (c openAIContent) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(c))
}

// openaiMessage adalah satu pesan dalam percakapan. Mendukung pesan tool
// (role "tool" dengan tool_call_id) dan pesan assistant berisi tool_calls.
type openaiMessage struct {
	Role       string           `json:"role"`
	Content    openAIContent    `json:"content,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

// openAIToolCall adalah satu tool call dalam pesan assistant (native OpenAI).
type openAIToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function openAIToolFn `json:"function"`
}

type openAIToolFn struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"` // JSON string berisi objek argumen
}

// openAITool adalah definisi satu tool yang dikirim OpenCode.
// Hanya nama, deskripsi, dan schema parameter yang kita butuhkan untuk
// mengarahkan model DeepSeek web memanggil tool dalam format JSON.
type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// chatCompletionRequest adalah body POST /v1/chat/completions.
// Field model diterima apa adanya (tidak divalidasi); request selalu
// diproses oleh provider yang sedang aktif di TUI.
type chatCompletionRequest struct {
	Model      string          `json:"model"`
	Messages   []openaiMessage `json:"messages"`
	Stream     bool            `json:"stream"`
	Tools      []openAITool    `json:"tools"`
	ToolChoice json.RawMessage `json:"tool_choice"`
}

// chatCompletionResponse adalah respons non-streaming.
type chatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []chatCompletionChoice `json:"choices"`
	Usage   usage                  `json:"usage"`
}

type chatCompletionChoice struct {
	Index        int           `json:"index"`
	Message      openaiMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// chatCompletionChunk adalah satu event SSE untuk streaming.
type chatCompletionChunk struct {
	ID      string                      `json:"id"`
	Object  string                      `json:"object"`
	Created int64                       `json:"created"`
	Model   string                      `json:"model"`
	Choices []chatCompletionChunkChoice `json:"choices"`
}

type chatCompletionChunkChoice struct {
	Index        int            `json:"index"`
	Delta        map[string]any `json:"delta"`
	FinishReason any            `json:"finish_reason"`
}

// newID membuat id unik untuk satu completion (untuk client cukup unik).
func newID() string {
	return fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
}
