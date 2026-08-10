package deepseek

import "testing"

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
			name:  "thinking fragment skipped",
			line:  `data: {"v":{"response":{"fragments":[{"id":1,"type":"THINKING","content":"reasoning...","stage_id":1}]}}}`,
			kind:  EventSkip,
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
