package toolcall

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"fakemodelapi/internal/openai"
)

// MaxCallsPerResponse membatasi jumlah tool call yang diterjemahkan per
// respons (mencegah model mengirim blok tool berlebihan).
const MaxCallsPerResponse = 10

// toolBlockRe mendeteksi blok fenced ```tool / ```json.
var toolBlockRe = regexp.MustCompile("(?s)```(?:tool|json)\\s*(\\{.*?\\})\\s*```")

// fenceOpenerRe mendeteksi awal blok ```tool / ```json di dalam teks.
var fenceOpenerRe = regexp.MustCompile("```(?:tool|json)")

// Call adalah satu panggilan tool yang dihasilkan model dalam blok ```tool.
type Call struct {
	Name string         `json:"name"`
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

// decodeCall adalah bentuk mentah blok tool — juga menerima key "arguments"
// (gaya OpenAI) selain "args", supaya model yang memakai format native tetap
// terbaca.
type decodeCall struct {
	Name      string          `json:"name"`
	Tool      string          `json:"tool"`
	Args      map[string]any  `json:"args"`
	Arguments json.RawMessage `json:"arguments"`
}

// Detect memeriksa apakah teks respons model berisi panggilan tool
// (satu atau lebih blok ```tool / ```json, atau seluruh teks berupa satu
// objek JSON). Mengembalikan ok=false jika teks adalah jawaban biasa.
func Detect(text string) ([]Call, bool) {
	s := strings.TrimSpace(text)
	if s == "" {
		return nil, false
	}

	// 1. Blok fenced ```tool / ```json — semua blok yang valid.
	matches := toolBlockRe.FindAllStringSubmatch(s, -1)
	if len(matches) > 0 {
		calls := make([]Call, 0, len(matches))
		for _, mm := range matches {
			if call, err := decode(mm[1]); err == nil {
				calls = append(calls, call)
			}
		}
		if len(calls) > 0 {
			return calls, true
		}
	}

	// 2. Seluruh teks adalah satu objek JSON tool call.
	if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
		if call, err := decode(s); err == nil {
			return []Call{call}, true
		}
	}

	return nil, false
}

func decode(raw string) (Call, error) {
	var d decodeCall
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return Call{}, err
	}
	call := Call{Name: d.Name, Tool: d.Tool, Args: d.Args}
	if call.Name == "" {
		call.Name = call.Tool
	}
	if call.Name == "" {
		return call, fmt.Errorf("tool call tanpa nama")
	}
	if call.Args == nil && len(d.Arguments) > 0 {
		call.Args = decodeArguments(d.Arguments)
	}
	if call.Args == nil {
		call.Args = map[string]any{}
	}
	return call, nil
}

// decodeArguments menafsirkan field "arguments": bisa berupa objek JSON
// langsung, atau string berisi JSON (format OpenAI mengirim string).
func decodeArguments(raw json.RawMessage) map[string]any {
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err == nil {
		return args
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if err := json.Unmarshal([]byte(s), &args); err == nil {
			return args
		}
	}
	return nil
}

// HasFence mengembalikan true jika teks mengandung awal blok ```tool/```json.
// Dipakai state machine streaming untuk memutuskan mode tool.
func HasFence(text string) bool {
	return fenceOpenerRe.MatchString(text)
}

// Allowed memetakan nama tool yang diizinkan dari daftar tool request.
func Allowed(tools []openai.Tool) map[string]bool {
	known := make(map[string]bool, len(tools))
	for _, t := range tools {
		known[t.Function.Name] = true
	}
	return known
}

// Validate menyaring tool call yang namanya terdaftar, menormalisasi args,
// dan membatasi jumlah maksimal per respons.
func Validate(calls []Call, allowed map[string]bool) []Call {
	out := make([]Call, 0, len(calls))
	for _, c := range calls {
		if len(out) >= MaxCallsPerResponse {
			break
		}
		if !allowed[c.Name] {
			continue
		}
		if c.Args == nil {
			c.Args = map[string]any{}
		}
		out = append(out, c)
	}
	return out
}

// MapToOpenAI mengubah tool call menjadi bentuk tool_calls native OpenAI.
func MapToOpenAI(calls []Call) []openai.ToolCall {
	out := make([]openai.ToolCall, 0, len(calls))
	for _, c := range calls {
		argsJSON, err := json.Marshal(c.Args)
		if err != nil {
			argsJSON = []byte("{}")
		}
		out = append(out, openai.ToolCall{
			ID:   callID(),
			Type: "function",
			Function: openai.ToolFn{
				Name:      c.Name,
				Arguments: argsJSON,
			},
		})
	}
	return out
}

func callID() string {
	return fmt.Sprintf("call_%d", time.Now().UnixNano())
}
