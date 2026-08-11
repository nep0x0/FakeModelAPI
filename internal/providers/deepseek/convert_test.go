package deepseek

import (
	"fakemodelapi/internal/provider"
	"strings"
	"testing"
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
	long := strings.Repeat("x", maxPromptLen+1000)
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

// TestBuildChatRequestModelMapping memastikan model publik dipetakan ke
// model_type wire DeepSeek yang benar: deepseek-chat → "default" (Instant),
// deepseek-reasoner → "expert" (Expert-Think). DeepThink aktif di keduanya.
func TestBuildChatRequestModelMapping(t *testing.T) {
	msgs := []provider.Message{{Role: "user", Content: "halo"}}

	cases := []struct {
		model     string
		wantType  string
		wantThink bool
	}{
		{"deepseek-chat", "default", true},
		{"deepseek-reasoner", "expert", true},
		{"", "default", true},
	}
	for _, c := range cases {
		req, err := BuildChatRequest(msgs, c.model, nil)
		if err != nil {
			t.Fatalf("BuildChatRequest(%q) error: %v", c.model, err)
		}
		if req.ModelType != c.wantType {
			t.Errorf("model %q: model_type = %q, want %q", c.model, req.ModelType, c.wantType)
		}
		if req.ThinkingEnabled != c.wantThink {
			t.Errorf("model %q: thinking_enabled = %v, want %v", c.model, req.ThinkingEnabled, c.wantThink)
		}
		if req.SearchEnabled {
			t.Errorf("model %q: search_enabled harus false", c.model)
		}
	}
}

func TestWantsFreshThread(t *testing.T) {
	cases := []struct {
		name string
		msgs []provider.Message
		want bool
	}{
		{"tui single user msg keeps thread", []provider.Message{{Role: "user", Content: "hai"}}, false},
		{"opencode system+user fresh", []provider.Message{
			{Role: "system", Content: "you are an agent"},
			{Role: "user", Content: "hello"},
		}, true},
		{"multi turn no system fresh", []provider.Message{
			{Role: "user", Content: "q1"},
			{Role: "assistant", Content: "a1"},
			{Role: "user", Content: "q2"},
		}, true},
		{"single system msg fresh", []provider.Message{{Role: "system", Content: "x"}}, true},
		{"empty fresh", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := wantsFreshThread(tc.msgs); got != tc.want {
				t.Fatalf("wantsFreshThread() = %v, want %v", got, tc.want)
			}
		})
	}
}
