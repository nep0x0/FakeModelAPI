package deepseek

import (
	"strings"
	"testing"
)

func TestParseSSELine(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		kind  EventKind
		delta string
	}{
		{
			name: "message ids",
			line: `data: {"request_message_id":15,"response_message_id":16,"model_type":"default"}`,
			kind: EventIDs,
		},
		{
			name: "session update skipped",
			line: `data: {"updated_at":1777894328.7964919}`,
			kind: EventSkip,
		},
		{
			name:  "append content",
			line:  `data: {"p":"response/fragments/-1/content","o":"APPEND","v":"om"}`,
			kind:  EventText,
			delta: "om",
		},
		{
			name:  "simple content",
			line:  `data: {"v":" dia"}`,
			kind:  EventText,
			delta: " dia",
		},
		{
			name:  "full response fragment",
			line:  `data: {"v":{"response":{"message_id":16,"fragments":[{"id":2,"type":"RESPONSE","content":"B","stage_id":1}],"status":"WIP"}}}`,
			kind:  EventText,
			delta: "B",
		},
		{
			name: "thinking fragment skipped",
			line: `data: {"v":{"response":{"fragments":[{"id":1,"type":"THINKING","content":"reasoning...","stage_id":1}]}}}`,
			kind: EventSkip,
		},
		{
			name: "finished",
			line: `data: {"p":"response/status","v":"FINISHED"}`,
			kind: EventFinish,
		},
		{
			name:  "error payload",
			line:  `data: {"code":40003,"msg":"invalid token","data":null}`,
			kind:  EventError,
			delta: "",
		},
		{
			name: "event line ignored",
			line: `event: ready`,
			kind: EventSkip,
		},
		{
			name: "empty line ignored",
			line: ``,
			kind: EventSkip,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev, ok := ParseSSELine(tc.line)
			if !ok {
				t.Fatal("expected ok=true")
			}
			if ev.Kind != tc.kind {
				t.Fatalf("kind = %d, want %d (event: %s)", ev.Kind, tc.kind, tc.line)
			}
			if ev.Content != tc.delta {
				t.Fatalf("content = %q, want %q", ev.Content, tc.delta)
			}
			if tc.name == "message ids" && ev.ParentMsgID != 16 {
				t.Fatalf("ParentMsgID = %d, want 16", ev.ParentMsgID)
			}
		})
	}
}

// TestStreamParserSkipsThinking verifies that with thinking enabled, the THINK
// fragment text (appends targeting fragment -1 while the last fragment is
// THINK) is NOT emitted, while the RESPONSE fragment content and its
// subsequent appends ARE.
func TestStreamParserSkipsThinking(t *testing.T) {
	lines := []string{
		`data: {"request_message_id":1,"response_message_id":2}`,
		`data: {"v":{"response":{"fragments":[{"id":2,"type":"THINK","content":"K"}]}}}`,
		`data: {"p":"response/fragments/-1/content","o":"APPEND","v":"ita perlu"}`,
		`data: {"v":" berpikir"}`,
		`data: {"p":"response/fragments","o":"APPEND","v":[{"id":3,"type":"RESPONSE","content":"Python"}]}`,
		`data: {"p":"response/fragments/-1/content","v":","}`,
		`data: {"v":" JavaScript"}`,
		`data: {"p":"response/status","v":"FINISHED"}`,
		"event: done",
	}

	p := NewStreamParser()
	var got string
	finished := false
	for _, line := range lines {
		ev, ok := p.Parse(line)
		if !ok {
			continue
		}
		switch ev.Kind {
		case EventText:
			got += ev.Content
		case EventFinish:
			finished = true
		}
	}

	if !strings.Contains(got, "Python") {
		t.Fatalf("jawaban harus memuat fragment RESPONSE, got %q", got)
	}
	if strings.Contains(got, "ita perlu") || strings.Contains(got, "berpikir") {
		t.Fatalf("teks thinking bocor ke jawaban: %q", got)
	}
	if !finished {
		t.Fatal("harus ada EventFinish")
	}
}
