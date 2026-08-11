// Package openai berisi tipe-tipe minimal ala OpenAI API yang dipakai
// gateway (request/response/streaming/error). Dipisah dari server supaya
// format OpenAI tidak menempel ke lapisan handler, provider, atau tool.
package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Content adalah isi pesan yang bisa datang sebagai string biasa atau array
// content parts (format OpenAI modern). Semua bentuk diubah menjadi teks polos.
type Content string

// UnmarshalJSON menerima `"teks"`, `[{"type":"text","text":"..."}]`, atau null.
func (c *Content) UnmarshalJSON(data []byte) error {
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
		*c = Content(s)
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
		*c = Content(b.String())
		return nil
	}
	return fmt.Errorf("content tidak valid: %s", string(data))
}

// MarshalJSON selalu mengirim content sebagai string.
func (c Content) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(c))
}

// Message adalah satu pesan dalam percakapan. Mendukung pesan tool
// (role "tool" dengan tool_call_id) dan pesan assistant berisi tool_calls.
type Message struct {
	Role       string     `json:"role"`
	Content    Content    `json:"content,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall adalah satu tool call dalam pesan assistant (native OpenAI).
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function ToolFn `json:"function"`
}

// ToolFn membawa nama tool dan argumen (JSON string berisi objek argumen).
type ToolFn struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"` // JSON string berisi objek argumen
}

// Tool adalah definisi satu tool yang dikirim client (mis. OpenCode).
type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

// Function adalah definisi fungsi dalam sebuah tool.
type Function struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ChatCompletionRequest adalah body POST /v1/chat/completions.
type ChatCompletionRequest struct {
	Model      string          `json:"model"`
	Messages   []Message       `json:"messages"`
	Stream     bool            `json:"stream"`
	Tools      []Tool          `json:"tools"`
	ToolChoice json.RawMessage `json:"tool_choice"`
}

// ChatCompletionResponse adalah respons non-streaming.
type ChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   Usage                  `json:"usage"`
}

// ChatCompletionChoice adalah satu pilihan jawaban dalam respons.
type ChatCompletionChoice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage adalah estimasi token (dipakai 0 oleh gateway karena provider web
// tidak melaporkan hitungan token).
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletionChunk adalah satu event SSE untuk streaming.
type ChatCompletionChunk struct {
	ID      string                       `json:"id"`
	Object  string                       `json:"object"`
	Created int64                        `json:"created"`
	Model   string                       `json:"model"`
	Choices []ChatCompletionChunkChoice  `json:"choices"`
}

// ChatCompletionChunkChoice adalah satu pilihan dalam chunk streaming.
type ChatCompletionChunkChoice struct {
	Index        int            `json:"index"`
	Delta        map[string]any `json:"delta"`
	FinishReason any            `json:"finish_reason"`
}

// NewID membuat id unik untuk satu completion (untuk client cukup unik).
func NewID() string {
	return fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
}
