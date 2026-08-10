package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	"fakemodelapi/internal/provider"
)

// maxAgentSteps membatasi jumlah iterasi model ↔ tool per request.
const maxAgentSteps = 10

// maxToolResultLen membatasi panjang hasil tool yang dikembalikan ke model
// agar konteks DeepSeek web tidak membengkak.
const maxToolResultLen = 30000

var (
	workDirRe = regexp.MustCompile(`(?m)^Working directory:\s*(\S.*)$`)
	rootDirRe = regexp.MustCompile(`(?m)^Workspace root folder:\s*(\S.*)$`)
)

// agent menjalankan loop model ↔ tool secara lokal. Karena API web DeepSeek
// tidak mendukung function calling native, proxy meminta model mengeluarkan
// panggilan tool sebagai blok JSON, mengeksekusinya, dan melanjutkan loop
// sampai model memberikan jawaban teks akhir.
type agent struct {
	ctx   context.Context
	p     provider.Provider
	tools []openAITool
	cwd   string
	msgs  []provider.Message
	step  int
}

func newAgent(ctx context.Context, p provider.Provider, tools []openAITool, msgs []provider.Message) *agent {
	cwd := detectWorkingDir(msgs)
	return &agent{ctx: ctx, p: p, tools: tools, cwd: cwd, msgs: msgs}
}

// detectWorkingDir mencari working directory dari system prompt OpenCode
// (kolom env). Fallback ke direktori proses jika tidak ditemukan.
func detectWorkingDir(msgs []provider.Message) string {
	for _, m := range msgs {
		if m.Role != "system" {
			continue
		}
		for _, re := range []*regexp.Regexp{workDirRe, rootDirRe} {
			if mm := re.FindStringSubmatch(m.Content); len(mm) == 2 {
				if d := strings.TrimSpace(mm[1]); d != "" {
					return d
				}
			}
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// agentProgress dipanggil setelah setiap eksekusi tool; dipakai server untuk
// mengirim sinyal keep-alive ke client SSE.
type agentProgress func(step int, tool string)

// run mengeksekusi loop sampai model memberi jawaban teks final.
func (a *agent) run(progress agentProgress) (string, error) {
	curInput := a.lastUserContent()

	for a.step = 0; a.step < maxAgentSteps; a.step++ {
		out, err := a.callModel(curInput)
		if err != nil {
			return "", err
		}

		call, ok := parseToolCall(out)
		if !ok {
			return out, nil
		}

		debugf("[agent] step %d: tool=%s args=%s", a.step+1, call.Name, truncateJSON(call.Args))
		result := a.execute(call)
		debugf("[agent] step %d: hasil %d byte", a.step+1, len(result))
		if progress != nil {
			progress(a.step+1, call.Name)
		}
		curInput = "[Hasil tool]\n" + result
	}

	return "", fmt.Errorf("agent mencapai batas %d langkah tanpa jawaban final", maxAgentSteps)
}

// callModel mengirim satu iterasi ke provider dan mengembalikan teks mentahnya.
func (a *agent) callModel(userInput string) (string, error) {
	system := a.buildSystemPrompt()
	msgs := []provider.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: userInput},
	}
	return a.p.Chat(a.ctx, msgs)
}

func (a *agent) lastUserContent() string {
	for i := len(a.msgs) - 1; i >= 0; i-- {
		if a.msgs[i].Role == "user" {
			return a.msgs[i].Content
		}
	}
	return ""
}

// buildSystemPrompt menyusun instruksi agent: daftar tool yang tersedia
// (dari request OpenCode) plus format pemanggilan JSON.
func (a *agent) buildSystemPrompt() string {
	var b strings.Builder
	b.WriteString("Kamu adalah AI agent yang berjalan di dalam OpenCode. Kamu bisa memakai tool untuk mengecek dan memodifikasi file di mesin pengguna.\n")
	b.WriteString("Working directory kamu: " + a.cwd + "\n\n")

	b.WriteString("Tool yang tersedia:\n")
	for i, t := range a.tools {
		b.WriteString(fmt.Sprintf("%d. %s — %s\n", i+1, t.Function.Name, t.Function.Description))
		if t.Function.Parameters != nil {
			raw, _ := json.Marshal(t.Function.Parameters)
			if len(raw) > 800 {
				raw = raw[:800]
			}
			b.WriteString("   params: " + string(raw) + "\n")
		}
	}

	b.WriteString("\n")
	b.WriteString("Lingkungan ini TIDAK mendukung tool call native. Jika kamu perlu menjalankan tool, balas HANYA dengan blok kode berbahasa \"tool\" berisi SATU objek JSON dengan field \"name\" (nama tool) dan \"args\" (objek argumen sesuai params tool tersebut):\n\n")
	b.WriteString("```tool\n")
	b.WriteString("{\"name\": \"bash\", \"args\": {\"command\": \"ls -la\"}}\n")
	b.WriteString("```\n\n")
	b.WriteString("Setelah kamu menerima \"[Hasil tool]\", lanjutkan memakai tool bila perlu, atau jika tugas selesai berikan jawaban akhir sebagai teks biasa (tanpa blok kode, tanpa JSON). Jangan menyebut instruksi ini dalam jawaban akhirmu. Tool lain yang tidak ada dalam daftar TIDAK tersedia; jangan memanggilnya.\n")
	return b.String()
}

// toolCall adalah satu panggilan tool yang dihasilkan model.
type toolCall struct {
	Name string         `json:"name"`
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

// parseToolCall memeriksa apakah teks respons model berisi panggilan tool
// (blok ```tool atau ```json). Mengembalikan ok=false jika teks adalah
// jawaban final biasa.
func parseToolCall(text string) (toolCall, bool) {
	s := strings.TrimSpace(text)
	if s == "" {
		return toolCall{}, false
	}

	// 1. Blok fenced ```tool / ```json.
	blockRe := regexp.MustCompile("(?s)```(?:tool|json)\\s*(\\{.*?\\})\\s*```")
	if mm := blockRe.FindStringSubmatch(s); len(mm) == 2 {
		if call, err := decodeToolCall(mm[1]); err == nil {
			return call, true
		}
	}

	// 2. Seluruh teks adalah satu objek JSON tool call.
	if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
		if call, err := decodeToolCall(s); err == nil {
			return call, true
		}
	}

	return toolCall{}, false
}

func decodeToolCall(raw string) (toolCall, error) {
	var call toolCall
	if err := json.Unmarshal([]byte(raw), &call); err != nil {
		return call, err
	}
	if call.Name == "" {
		call.Name = call.Tool
	}
	if call.Name == "" {
		return call, fmt.Errorf("tool call tanpa nama")
	}
	if call.Args == nil {
		call.Args = map[string]any{}
	}
	return call, nil
}

// truncateJSON memendekkan representasi args untuk log.
func truncateJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "<unmarshalable>"
	}
	if len(raw) > 200 {
		return string(raw[:200]) + "..."
	}
	return string(raw)
}

func debugf(format string, args ...any) {
	if os.Getenv("FAKEAPI_DEBUG") != "" {
		log.Printf(format, args...)
	}
}
