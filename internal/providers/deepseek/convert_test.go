package deepseek

import (
	"strings"
	"testing"

	"fakemodelapi/internal/provider"
)

func TestBuildChatRequestFlattensFullHistory(t *testing.T) {
	msgs := []provider.Message{
		{Role: "system", Content: "You are an agent."},
		{Role: "user", Content: "Buat file hello.txt"},
		{Role: "assistant", ToolCalls: []provider.MessageToolCall{
			{ID: "call_1", Name: "bash", Arguments: map[string]any{"command": "echo halo > hello.txt"}},
		}},
		{Role: "tool", ToolCallID: "call_1", Content: "file ditulis"},
		{Role: "user", Content: "Baca isinya"},
	}

	req, err := BuildChatRequest(msgs, "deepseek-chat", nil)
	if err != nil {
		t.Fatalf("BuildChatRequest error: %v", err)
	}
	p := req.Prompt

	for _, want := range []string{
		"[System]",
		"You are an agent.",
		"[User]",
		"Buat file hello.txt",
		"memanggil tool bash dengan argumen",
		`"command":"echo halo \u003e hello.txt"`,
		"[Hasil tool call_1]",
		"file ditulis",
		"Baca isinya",
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt tidak memuat %q:\n%s", want, p)
		}
	}
}

func TestBuildChatRequestClipsOversizedHistory(t *testing.T) {
	long := strings.Repeat("x", 61000)
	msgs := []provider.Message{
		{Role: "user", Content: "awal: " + long},
		{Role: "user", Content: "akhir pesan"},
	}
	req, err := BuildChatRequest(msgs, "deepseek-chat", nil)
	if err != nil {
		t.Fatalf("BuildChatRequest error: %v", err)
	}
	if len(req.Prompt) > maxPromptLen+len("[Bagian awal percakapan terpotong]\n\n") {
		t.Fatalf("prompt %d byte melebihi budget", len(req.Prompt))
	}
	if !strings.Contains(req.Prompt, "akhir pesan") {
		t.Fatalf("pesan terakhir hilang setelah clip")
	}
	if !strings.Contains(req.Prompt, "[Bagian awal percakapan terpotong]") {
		t.Fatalf("penanda potongan tidak ada")
	}
}

func TestBuildChatRequestEmptyMessages(t *testing.T) {
	req, err := BuildChatRequest(nil, "deepseek-chat", nil)
	if err != nil {
		t.Fatalf("BuildChatRequest(nil) error: %v", err)
	}
	if req.Prompt != "" {
		t.Fatalf("prompt harus kosong, dapat %q", req.Prompt)
	}

	req, err = BuildChatRequest([]provider.Message{{Role: "user", Content: "   "}}, "deepseek-chat", nil)
	if err != nil {
		t.Fatalf("BuildChatRequest(blank) error: %v", err)
	}
	if req.Prompt != "" {
		t.Fatalf("prompt blank harus kosong, dapat %q", req.Prompt)
	}
}
