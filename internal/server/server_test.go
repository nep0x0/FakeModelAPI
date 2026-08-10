package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
// berurutan (untuk menguji agent loop).
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
	return nil, fmt.Errorf("tidak dipakai di test agent")
}

func (p *scriptedProvider) AuthStatus() provider.AuthInfo {
	return provider.AuthInfo{LoggedIn: p.loggedIn}
}

func (p *scriptedProvider) Models() []provider.ModelInfo {
	return nil
}

func (p *scriptedProvider) SetModel(modelID string) {}
func (p *scriptedProvider) Reset()                  {}

func TestParseToolCall(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		name string
	}{
		{"```tool\n{\"name\":\"bash\",\"args\":{\"command\":\"ls\"}}\n```", true, "bash"},
		{"```json\n{\"tool\":\"read\",\"args\":{\"filePath\":\"x.go\"}}\n```", true, "read"},
		{"{\"name\":\"glob\",\"args\":{\"pattern\":\"**/*.go\"}}", true, "glob"},
		{"jawaban biasa saja", false, ""},
		{"", false, ""},
		{"```tool\n{\"args\":{\"command\":\"ls\"}}\n```", false, ""},
	}
	for _, c := range cases {
		call, ok := parseToolCall(c.in)
		if ok != c.ok || (ok && call.Name != c.name) {
			t.Fatalf("parseToolCall(%q) = (%+v, %v), want ok=%v name=%q", c.in, call, ok, c.ok, c.name)
		}
	}
}

func TestAgentLoopExecutesTool(t *testing.T) {
	tmp := t.TempDir()
	outFile := filepath.Join(tmp, "hasil.txt")

	sp := &scriptedProvider{
		responses: []string{
			"```tool\n{\"name\":\"bash\",\"args\":{\"command\":\"echo halo-dunia > " + outFile + "\"}}\n```",
			"File sudah dibuat berisi: " + outFile,
		},
		loggedIn: true,
	}
	provider.Register("test-agent", sp)
	srv := New("test-agent", 8128)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer srv.Stop()

	body := `{"model":"m","messages":[
		{"role":"system","content":"Working directory: ` + tmp + `\nYou are an agent."},
		{"role":"user","content":"buat file hasil.txt berisi halo-dunia lalu konfirmasi"}
	],"stream":false,"tools":[{"type":"function","function":{"name":"bash","description":"Run bash","parameters":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}}}]}`

	resp, data := postJSON(t, "http://localhost:8128/v1/chat/completions", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, string(data))
	}

	// 1. Tool bash benar-benar dieksekusi.
	if _, err := os.Stat(outFile); err != nil {
		t.Fatalf("file hasil tool tidak ada: %v", err)
	}

	// 2. Jawaban final diteruskan ke client.
	if !strings.Contains(string(data), "hasil.txt") {
		t.Fatalf("jawaban final tidak mengandung hasil: %s", string(data))
	}
}

func TestAgentLoopFeedsToolError(t *testing.T) {
	sp := &scriptedProvider{
		responses: []string{
			"```tool\n{\"name\":\"bash\",\"args\":{\"command\":\"ls /path/yang/tidak/ada/xyz\"}}\n```",
			"Perintah gagal karena path tidak ada.",
		},
		loggedIn: true,
	}
	provider.Register("test-agent-err", sp)
	srv := New("test-agent-err", 8129)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer srv.Stop()

	body := `{"model":"m","messages":[{"role":"user","content":"cek path"}],"stream":false,
		"tools":[{"type":"function","function":{"name":"bash","description":"Run bash","parameters":{}}}]}`

	resp, data := postJSON(t, "http://localhost:8129/v1/chat/completions", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, string(data))
	}
	if !strings.Contains(string(data), "tidak ada") {
		t.Fatalf("model tidak menerima hasil error tool: %s", string(data))
	}
}

var _ = context.Background
