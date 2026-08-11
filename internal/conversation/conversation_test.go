package conversation

import (
	"strings"
	"testing"

	"fakemodelapi/internal/openai"
	"fakemodelapi/internal/provider"
)

func TestFlattenRoleMarkers(t *testing.T) {
	msgs := []provider.Message{
		{Role: "system", Content: "Kamu asisten."},
		{Role: "user", Content: "halo"},
		{Role: "assistant", Content: "hai"},
		{Role: "user", Content: ""},
	}
	out := Flatten(msgs, 0)
	for _, want := range []string{"[System]", "[User]", "[Assistant]", "Kamu asisten.", "halo", "hai"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Flatten kehilangan %q: %q", want, out)
		}
	}
	if strings.Count(out, "[User]") != 1 {
		t.Fatalf("pesan user kosong tidak boleh di-flatten: %q", out)
	}
}

func TestFlattenToolCalls(t *testing.T) {
	msgs := []provider.Message{
		{Role: "assistant", ToolCalls: []provider.MessageToolCall{{Name: "bash", Arguments: map[string]any{"command": "ls"}}}},
		{Role: "tool", ToolCallID: "call_1", Content: "hasil"},
	}
	out := Flatten(msgs, 0)
	if !strings.Contains(out, "memanggil tool bash") || !strings.Contains(out, "[Hasil tool call_1]") {
		t.Fatalf("Flatten tool calls tidak lengkap: %q", out)
	}
}

func TestFlattenTruncation(t *testing.T) {
	long := strings.Repeat("x", 100)
	msgs := []provider.Message{{Role: "user", Content: long}}
	out := Flatten(msgs, 50)
	if !strings.Contains(out, "terpotong") {
		t.Fatalf("Flatten tidak menandai pemotongan: %q", out)
	}
	if strings.Count(out, "x") > 50 {
		t.Fatalf("Flatten tidak memotong ke batas: %d x di output", strings.Count(out, "x"))
	}
	if len(out) > 60+len("[Bagian awal percakapan terpotong]\n\n") {
		t.Fatalf("output melebihi batas: len=%d", len(out))
	}
}

func TestBuildToolSystemPromptKeepsOriginalAndFullSchema(t *testing.T) {
	original := "Working directory: /tmp/xyz\nYou are opencode.\nEnvironment: linux"
	tools := []openai.Tool{{
		Type: "function",
		Function: openai.Function{
			Name:        "edit",
			Description: strings.Repeat("Edit a file. ", 20),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"filePath":   map[string]any{"type": "string"},
					"oldString":  map[string]any{"type": "string"},
					"newString":  map[string]any{"type": "string"},
					"replaceAll": map[string]any{"type": "boolean"},
				},
				"required": []any{"filePath", "oldString", "newString"},
			},
		},
	}}
	p := BuildToolSystemPrompt(original, tools)

	if !strings.Contains(p, original) {
		t.Fatalf("system prompt asli hilang dari BuildToolSystemPrompt")
	}
	if !strings.Contains(p, `"required":["filePath","oldString","newString"]`) {
		t.Fatalf("schema tool tidak lengkap: %s", p)
	}
	if strings.Contains(p, "schema too large") {
		t.Fatalf("schema tool dipotong padahal di bawah budget")
	}
	if !strings.Contains(p, "does NOT support native tool calls") {
		t.Fatalf("instruksi protokol tool tidak ada")
	}
	if !strings.Contains(p, "plan") || !strings.Contains(p, "read-only") {
		t.Fatalf("instruksi kepatuhan plan mode tidak ada: %s", p)
	}
}

func TestCompile(t *testing.T) {
	msgs := []provider.Message{
		{Role: "system", Content: "system A"},
		{Role: "system", Content: "system B"},
		{Role: "user", Content: "halo"},
	}
	tools := []openai.Tool{{Function: openai.Function{Name: "bash", Description: "Run bash"}}}
	out := Compile(msgs, tools)

	if len(out) != 2 {
		t.Fatalf("Compile = %d pesan, want 2 (system gabung + user)", len(out))
	}
	if out[0].Role != "system" || !strings.Contains(out[0].Content, "system A") || !strings.Contains(out[0].Content, "system B") {
		t.Fatalf("system prompt tidak digabung: %q", out[0].Content)
	}
	if !strings.Contains(out[0].Content, "bash") {
		t.Fatalf("instruksi tool tidak ditambahkan")
	}
	if out[1].Role != "user" || out[1].Content != "halo" {
		t.Fatalf("pesan user tidak dipertahankan: %+v", out[1])
	}
}

// TestFlattenKeepsSystemPromptIntact memastikan system prompt TIDAK ikut
// terpotong saat total melebihi batas: hanya riwayat non-system yang
// dipangkas (dari sisi lama) dan penanda pemotongan muncul.
func TestFlattenKeepsSystemPromptIntact(t *testing.T) {
	sys := "You are opencode, a coding agent. " + strings.Repeat("INSTRUKSI-", 10)
	msgs := []provider.Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: "pesan lama " + strings.Repeat("LAMA", 200)},
		{Role: "user", Content: "pesan terbaru " + strings.Repeat("BARU", 10)},
	}

	out := Flatten(msgs, 300)

	if !strings.Contains(out, sys) {
		t.Fatal("system prompt harus dipertahankan utuh")
	}
	if !strings.Contains(out, "terpotong") {
		t.Fatal("harus ada penanda pemotongan")
	}
	// Pesan terbaru tetap ada; pesan lama bisa hilang.
	if !strings.Contains(out, "pesan terbaru") {
		t.Fatal("pesan terbaru harus dipertahankan")
	}
	if strings.Contains(out, "pesan lama") {
		t.Fatal("riwayat lama harus dipangkas")
	}
}

// TestFlattenSystemOnlyNoLimit memastikan output tanpa batas sama dengan
// gabungan system + riwayat.
func TestFlattenSystemOnlyNoLimit(t *testing.T) {
	msgs := []provider.Message{
		{Role: "system", Content: "S1"},
		{Role: "user", Content: "U1"},
	}
	out := Flatten(msgs, 0)
	for _, want := range []string{"[System]", "S1", "[User]", "U1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output tidak memuat %q:\n%s", want, out)
		}
	}
}
