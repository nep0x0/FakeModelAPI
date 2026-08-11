package toolcall

import (
	"testing"

	"fakemodelapi/internal/openai"
)

func TestDetect(t *testing.T) {
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
		calls, ok := Detect(c.in)
		if ok != c.ok {
			t.Fatalf("Detect(%q) = (%+v, %v), want ok=%v", c.in, calls, ok, c.ok)
		}
		if !ok {
			continue
		}
		if len(calls) != len(c.names) {
			t.Fatalf("Detect(%q) = %d call, want %d", c.in, len(calls), len(c.names))
		}
		for i, want := range c.names {
			if calls[i].Name != want {
				t.Fatalf("Detect(%q)[%d].Name = %q, want %q", c.in, i, calls[i].Name, want)
			}
		}
	}
}

func TestHasFence(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"```tool\n...", true},
		{"teks biasa ```json", true},
		{"tidak ada fence", false},
		{"", false},
		{"```javascript", false},
	}
	for _, c := range cases {
		if got := HasFence(c.in); got != c.want {
			t.Fatalf("HasFence(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestValidate(t *testing.T) {
	tools := []openai.Tool{
		{Function: openai.Function{Name: "bash"}},
		{Function: openai.Function{Name: "read"}},
	}
	allowed := Allowed(tools)

	calls, ok := Detect("```tool\n{\"name\":\"bash\",\"args\":{\"command\":\"ls\"}}\n```\n```tool\n{\"name\":\"edit\",\"args\":{}}\n```")
	if !ok {
		t.Fatal("Detect gagal untuk dua blok tool")
	}
	known := Validate(calls, allowed)
	if len(known) != 1 || known[0].Name != "bash" {
		t.Fatalf("Validate = %+v, want hanya bash", known)
	}
	if known[0].Args == nil {
		t.Fatalf("Args tidak dinormalisasi")
	}
}

func TestValidateMaxCalls(t *testing.T) {
	allowed := map[string]bool{"a": true}
	calls := make([]Call, 0, MaxCallsPerResponse+5)
	for i := 0; i < MaxCallsPerResponse+5; i++ {
		calls = append(calls, Call{Name: "a", Args: map[string]any{}})
	}
	if got := len(Validate(calls, allowed)); got != MaxCallsPerResponse {
		t.Fatalf("Validate melebihi batas: %d, want %d", got, MaxCallsPerResponse)
	}
}

func TestMapToOpenAI(t *testing.T) {
	calls := []Call{{Name: "bash", Args: map[string]any{"command": "ls"}}}
	out := MapToOpenAI(calls)
	if len(out) != 1 {
		t.Fatalf("MapToOpenAI = %d call, want 1", len(out))
	}
	tc := out[0]
	if tc.ID == "" || tc.Type != "function" || tc.Function.Name != "bash" {
		t.Fatalf("tool call tidak valid: %+v", tc)
	}
	if string(tc.Function.Arguments) != `{"command":"ls"}` {
		t.Fatalf("arguments = %s", string(tc.Function.Arguments))
	}
}

// TestDetectArgumentsKey memastikan tool call bergaya OpenAI ("arguments"
// sebagai objek atau string JSON) tetap dikenali, bukan hanya "args".
func TestDetectArgumentsKey(t *testing.T) {
	cases := []struct {
		in      string
		wantCmd string
	}{
		{"```tool\n{\"name\":\"bash\",\"arguments\":{\"command\":\"ls -la\"}}\n```", "ls -la"},
		{"```tool\n{\"name\":\"bash\",\"arguments\":\"{\\\"command\\\":\\\"ls\\\"}\"}\n```", "ls"},
		{"{\"name\":\"edit\",\"arguments\":{\"filePath\":\"a.go\",\"oldString\":\"x\",\"newString\":\"y\"}}", ""},
	}
	for _, c := range cases {
		calls, ok := Detect(c.in)
		if !ok {
			t.Fatalf("Detect(%q) tidak dikenali", c.in)
		}
		if len(calls) != 1 {
			t.Fatalf("Detect(%q) = %d call, want 1", c.in, len(calls))
		}
		if c.wantCmd != "" {
			if got, _ := calls[0].Args["command"].(string); got != c.wantCmd {
				t.Fatalf("Detect(%q): command = %q, want %q", c.in, got, c.wantCmd)
			}
		}
		if len(calls[0].Args) == 0 {
			t.Fatalf("Detect(%q): args tidak terisi", c.in)
		}
	}
}
