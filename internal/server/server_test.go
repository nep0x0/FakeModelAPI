package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"fakemodelapi/internal/provider"
	"fakemodelapi/internal/providers/dummy"
)

func startTestServer(t *testing.T, port int) *Server {
	t.Helper()
	dp := dummy.New()
	dp.SetLoggedIn(true)
	provider.Register("test-dummy", dp)
	srv := New("test-dummy", port)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	return srv
}

func postJSON(t *testing.T, url string, body string) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s error: %v", url, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

func TestHandleModels(t *testing.T) {
	startTestServer(t, 8123)

	resp, err := http.Get("http://localhost:8123/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		Object string `json:"object"`
		Data   []struct {
			ID     string `json:"id"`
			Object string `json:"object"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if out.Object != "list" || len(out.Data) == 0 || out.Data[0].ID == "" {
		t.Fatalf("respon models tidak valid: %+v", out)
	}
}

func TestCompleteChatNonStream(t *testing.T) {
	startTestServer(t, 8124)

	resp, data := postJSON(t, "http://localhost:8124/v1/chat/completions",
		`{"model":"dummy-model","messages":[{"role":"user","content":"halo"}],"stream":false}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, string(data))
	}
	var out struct {
		Object  string `json:"object"`
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if out.Object != "chat.completion" || len(out.Choices) != 1 {
		t.Fatalf("respon tidak valid: %s", string(data))
	}
	c := out.Choices[0]
	if c.Message.Role != "assistant" || c.Message.Content == "" || c.FinishReason != "stop" {
		t.Fatalf("choice tidak valid: %+v", c)
	}
}

func TestCompleteChatStream(t *testing.T) {
	startTestServer(t, 8125)

	body := `{"model":"dummy-model","messages":[{"role":"user","content":"halo"}],"stream":true}`
	resp, err := http.Post("http://localhost:8125/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("content-type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q, want text/event-stream", ct)
	}

	var sawContent, sawDone bool
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		if line == "data: [DONE]" {
			sawDone = true
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		var chunk chatCompletionChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			t.Fatalf("chunk decode error: %v (line=%q)", err, line)
		}
		if chunk.Object != "chat.completion.chunk" || len(chunk.Choices) != 1 {
			t.Fatalf("chunk tidak valid: %s", line)
		}
		if c := chunk.Choices[0].Delta["content"]; c != nil && c != "" {
			sawContent = true
		}
	}
	if !sawContent || !sawDone {
		t.Fatalf("stream tidak lengkap: sawContent=%v sawDone=%v", sawContent, sawDone)
	}
}

func TestCompleteChatArrayContent(t *testing.T) {
	startTestServer(t, 8127)

	body := `{"model":"dummy-model","messages":[
		{"role":"user","content":[{"type":"text","text":"halo"},{"type":"text","text":" dunia"}]},
		{"role":"assistant","content":null}
	],"stream":false}`
	resp, data := postJSON(t, "http://localhost:8127/v1/chat/completions", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, string(data))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(out.Choices) != 1 || out.Choices[0].Message.Content == "" {
		t.Fatalf("respon tidak valid: %s", string(data))
	}
}

func TestCompleteChatEmptyMessages(t *testing.T) {
	startTestServer(t, 8126)

	resp, data := postJSON(t, "http://localhost:8126/v1/chat/completions",
		`{"model":"dummy-model","messages":[],"stream":false}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", resp.StatusCode, string(data))
	}
}

// scriptedProvider adalah provider tiruan yang mengembalikan respons
// berurutan (untuk menguji jalur chat & emulasi tool call).
type scriptedProvider struct {
	responses []string
	loggedIn  bool
}

func (p *scriptedProvider) Chat(ctx context.Context, msgs []provider.Message) (string, error) {
	if len(p.responses) == 0 {
		return "", fmt.Errorf("tidak ada respons tersisa")
	}
	r := p.responses[0]
	p.responses = p.responses[1:]
	return r, nil
}

func (p *scriptedProvider) ChatStream(ctx context.Context, msgs []provider.Message) (<-chan provider.Chunk, error) {
	if len(p.responses) == 0 {
		return nil, fmt.Errorf("tidak ada respons tersisa")
	}
	r := p.responses[0]
	p.responses = p.responses[1:]
	ch := make(chan provider.Chunk, 16)
	go func() {
		defer close(ch)
		// Pecah per rune supaya klasifikasi prefiks pada path streaming teruji.
		for _, rn := range r {
			ch <- provider.Chunk{Delta: string(rn)}
		}
	}()
	return ch, nil
}

func (p *scriptedProvider) AuthStatus() provider.AuthInfo {
	return provider.AuthInfo{LoggedIn: p.loggedIn}
}

func (p *scriptedProvider) Models() []provider.ModelInfo {
	return nil
}

func (p *scriptedProvider) SetModel(modelID string) {}
func (p *scriptedProvider) Reset()                  {}

func TestParseToolCalls(t *testing.T) {
	cases := []struct {
		in    string
		ok    bool
		names []string
	}{
		{"```tool\n{\"name\":\"bash\",\"args\":{\"command\":\"ls\"}}\n```", true, []string{"bash"}},
		{"```json\n{\"tool\":\"read\",\"args\":{\"filePath\":\"x.go\"}}\n```", true, []string{"read"}},
		{"{\"name\":\"glob\",\"args\":{\"pattern\":\"**/*.go\"}}", true, []string{"glob"}},
		{"```tool\n{\"name\":\"bash\",\"args\":{\"command\":\"ls\"}}\n```\n```tool\n{\"name\":\"grep\",\"args\":{\"pattern\":\"func\",\"include\":\"*.go\"}}\n```", true, []string{"bash", "grep"}},
		{"jawaban biasa saja", false, nil},
		{"", false, nil},
		{"```tool\n{\"args\":{\"command\":\"ls\"}}\n```", false, nil},
		{"blabla ```tool\n{\"name\":\"bash\",\"args\":{}}\n``` blabla", true, []string{"bash"}},
	}
	for _, c := range cases {
		calls, ok := parseToolCalls(c.in)
		if ok != c.ok {
			t.Fatalf("parseToolCalls(%q) = (%+v, %v), want ok=%v", c.in, calls, ok, c.ok)
		}
		if !ok {
			continue
		}
		if len(calls) != len(c.names) {
			t.Fatalf("parseToolCalls(%q) = %d call, want %d", c.in, len(calls), len(c.names))
		}
		for i, want := range c.names {
			if calls[i].Name != want {
				t.Fatalf("parseToolCalls(%q)[%d].Name = %q, want %q", c.in, i, calls[i].Name, want)
			}
		}
	}
}

func TestBuildToolSystemPromptKeepsOriginalAndFullSchema(t *testing.T) {
	original := "Working directory: /tmp/xyz\nYou are opencode.\nEnvironment: linux"
	tools := []openAITool{{
		Type: "function",
		Function: openAIFunction{
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
	p := buildToolSystemPrompt(original, tools)

	if !strings.Contains(p, original) {
		t.Fatalf("system prompt asli hilang dari buildToolSystemPrompt")
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

// toolBodyRequest membuat body request chat dengan satu tool "bash".
const toolBodyRequest = `"tools":[{"type":"function","function":{"name":"bash","description":"Run bash","parameters":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}}}]`

func TestCompleteChatToolCall(t *testing.T) {
	sp := &scriptedProvider{
		responses: []string{"```tool\n{\"name\":\"bash\",\"args\":{\"command\":\"ls\"}}\n```"},
		loggedIn:  true,
	}
	provider.Register("test-tool-ns", sp)
	srv := New("test-tool-ns", 8128)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer srv.Stop()

	body := `{"model":"m","messages":[{"role":"user","content":"jalankan ls"}],"stream":false,` + toolBodyRequest + `}`
	resp, data := postJSON(t, "http://localhost:8128/v1/chat/completions", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, string(data))
	}

	var out chatCompletionResponse
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	c := out.Choices[0]
	if c.FinishReason != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls: %s", c.FinishReason, string(data))
	}
	if len(c.Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %+v, want 1 call", c.Message.ToolCalls)
	}
	tc := c.Message.ToolCalls[0]
	if tc.Function.Name != "bash" || tc.Type != "function" || tc.ID == "" {
		t.Fatalf("tool call tidak valid: %+v", tc)
	}
	var args map[string]any
	if err := json.Unmarshal(tc.Function.Arguments, &args); err != nil || args["command"] != "ls" {
		t.Fatalf("arguments tidak valid: %s", string(tc.Function.Arguments))
	}
}

func TestCompleteChatToolCallMulti(t *testing.T) {
	sp := &scriptedProvider{
		responses: []string{"```tool\n{\"name\":\"bash\",\"args\":{\"command\":\"a\"}}\n```\n```tool\n{\"name\":\"bash\",\"args\":{\"command\":\"b\"}}\n```"},
		loggedIn:  true,
	}
	provider.Register("test-tool-multi", sp)
	srv := New("test-tool-multi", 8131)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer srv.Stop()

	body := `{"model":"m","messages":[{"role":"user","content":"jalankan a dan b"}],"stream":false,` + toolBodyRequest + `}`
	resp, data := postJSON(t, "http://localhost:8131/v1/chat/completions", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, string(data))
	}
	var out chatCompletionResponse
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if got := len(out.Choices[0].Message.ToolCalls); got != 2 {
		t.Fatalf("jumlah tool_calls = %d, want 2: %s", got, string(data))
	}
}

func TestCompleteChatPlainTextWithTools(t *testing.T) {
	sp := &scriptedProvider{
		responses: []string{"Ini jawaban biasa tanpa tool."},
		loggedIn:  true,
	}
	provider.Register("test-tool-text", sp)
	srv := New("test-tool-text", 8132)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer srv.Stop()

	body := `{"model":"m","messages":[{"role":"user","content":"apa itu? 2+2"}],"stream":false,` + toolBodyRequest + `}`
	resp, data := postJSON(t, "http://localhost:8132/v1/chat/completions", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, string(data))
	}
	var out chatCompletionResponse
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	c := out.Choices[0]
	if c.FinishReason != "stop" || len(c.Message.ToolCalls) != 0 {
		t.Fatalf("respon harus teks biasa: %+v", c)
	}
	if c.Message.Content == "" {
		t.Fatalf("content kosong")
	}
}

func TestCompleteChatUnknownToolBlock(t *testing.T) {
	sp := &scriptedProvider{
		responses: []string{"```tool\n{\"name\":\"tidakAda\",\"args\":{}}\n```"},
		loggedIn:  true,
	}
	provider.Register("test-tool-unknown", sp)
	srv := New("test-tool-unknown", 8133)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer srv.Stop()

	body := `{"model":"m","messages":[{"role":"user","content":"panggil tool aneh"}],"stream":false,` + toolBodyRequest + `}`
	resp, data := postJSON(t, "http://localhost:8133/v1/chat/completions", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, string(data))
	}
	var out chatCompletionResponse
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	c := out.Choices[0]
	if c.FinishReason != "stop" || len(c.Message.ToolCalls) != 0 {
		t.Fatalf("blok tool tak dikenal harus jatuh ke teks: %+v", c)
	}
}

func TestStreamToolCall(t *testing.T) {
	sp := &scriptedProvider{
		responses: []string{"```tool\n{\"name\":\"bash\",\"args\":{\"command\":\"echo SSE-Tool\"}}\n```"},
		loggedIn:  true,
	}
	provider.Register("test-tool-stream", sp)
	srv := New("test-tool-stream", 8134)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer srv.Stop()

	body := `{"model":"m","messages":[{"role":"user","content":"jalankan bash"}],"stream":true,` + toolBodyRequest + `}`
	req, _ := http.NewRequest("POST", "http://localhost:8134/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("content-type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var sawToolCall, sawToolFinish, sawDone bool
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		if line == "data: [DONE]" {
			sawDone = true
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		var chunk chatCompletionChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			t.Fatalf("chunk decode error: %v (line=%q)", err, line)
		}
		if len(chunk.Choices) != 1 {
			t.Fatalf("chunk tidak valid: %s", line)
		}
		c := chunk.Choices[0]
		if tcs, ok := c.Delta["tool_calls"].([]any); ok {
			for _, raw := range tcs {
				tc := raw.(map[string]any)
				fn := tc["function"].(map[string]any)
				if fn["name"] == "bash" {
					sawToolCall = true
				}
			}
		}
		if c.FinishReason == "tool_calls" {
			sawToolFinish = true
		}
	}
	if !sawToolCall || !sawToolFinish || !sawDone {
		t.Fatalf("stream tool_calls tidak lengkap: call=%v finish=%v done=%v", sawToolCall, sawToolFinish, sawDone)
	}
}

func TestStreamToolCallWithProsePrefix(t *testing.T) {
	// Model sering menulis prosa dulu lalu blok ```tool — harus tetap
	// diterjemahkan menjadi tool_calls native.
	sp := &scriptedProvider{
		responses: []string{"Saya akan mengecek dulu.\n\n```tool\n{\"name\":\"bash\",\"args\":{\"command\":\"ls\"}}\n```\nSelesai."},
		loggedIn:  true,
	}
	provider.Register("test-tool-prose", sp)
	srv := New("test-tool-prose", 8136)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer srv.Stop()

	body := `{"model":"m","messages":[{"role":"user","content":"jalankan bash"}],"stream":true,` + toolBodyRequest + `}`
	req, _ := http.NewRequest("POST", "http://localhost:8136/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("content-type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var sawToolCall, sawToolFinish, sawDone bool
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		if line == "data: [DONE]" {
			sawDone = true
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		var chunk chatCompletionChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			t.Fatalf("chunk decode error: %v (line=%q)", err, line)
		}
		if tcs, ok := chunk.Choices[0].Delta["tool_calls"].([]any); ok {
			for _, raw := range tcs {
				fn := raw.(map[string]any)["function"].(map[string]any)
				if fn["name"] == "bash" {
					sawToolCall = true
				}
			}
		}
		if chunk.Choices[0].FinishReason == "tool_calls" {
			sawToolFinish = true
		}
	}
	if !sawToolCall || !sawToolFinish || !sawDone {
		t.Fatalf("blok tool ber-prosa tidak jadi tool_calls: call=%v finish=%v done=%v", sawToolCall, sawToolFinish, sawDone)
	}
}

func TestStreamPlainTextWithTools(t *testing.T) {
	sp := &scriptedProvider{
		responses: []string{"Teks jawaban biasa."},
		loggedIn:  true,
	}
	provider.Register("test-tool-stream-text", sp)
	srv := New("test-tool-stream-text", 8135)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer srv.Stop()

	body := `{"model":"m","messages":[{"role":"user","content":"jawab biasa"}],"stream":true,` + toolBodyRequest + `}`
	req, _ := http.NewRequest("POST", "http://localhost:8135/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("content-type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var sawContent, sawStop, sawDone bool
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		if line == "data: [DONE]" {
			sawDone = true
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		var chunk chatCompletionChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			t.Fatalf("chunk decode error: %v (line=%q)", err, line)
		}
		if c := chunk.Choices[0].Delta["content"]; c != nil && c != "" {
			sawContent = true
		}
		if chunk.Choices[0].FinishReason == "stop" {
			sawStop = true
		}
	}
	if !sawContent || !sawStop || !sawDone {
		t.Fatalf("stream teks tidak lengkap: content=%v stop=%v done=%v", sawContent, sawStop, sawDone)
	}
}

func TestRequireLoopback(t *testing.T) {
	handler := requireLoopback(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	cases := []struct {
		remote string
		want   int
	}{
		{"127.0.0.1:5678", http.StatusOK},
		{"[::1]:5678", http.StatusOK},
		{"[::ffff:127.0.0.1]:5678", http.StatusOK},
		{"192.168.1.5:5678", http.StatusForbidden},
		{"10.0.0.1:5678", http.StatusForbidden},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "http://localhost/v1/models", nil)
		req.RemoteAddr = c.remote
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Fatalf("remote=%q status=%d, want %d", c.remote, rec.Code, c.want)
		}
	}
}

var _ = context.Background
