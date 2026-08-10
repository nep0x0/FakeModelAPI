package deepseek

import (
	"encoding/json"
	"strings"
)

// EventKind describes the type of a parsed SSE event.
type EventKind int

const (
	// EventSkip is a keep-alive or metadata event (updated_at, etc).
	EventSkip EventKind = iota
	// EventIDs carries the request/response message ids used for multi-turn.
	EventIDs
	// EventText carries an incremental content delta.
	EventText
	// EventFinish marks the end of the response.
	EventFinish
	// EventError carries an error payload from the stream.
	EventError
)

// Event is a parsed DeepSeek SSE event.
type Event struct {
	Kind        EventKind
	Content     string
	ParentMsgID int64
	ErrMsg      string
}

// ParseSSELine parses a single line of the DeepSeek SSE stream.
// Non-data lines ("event:", empty) return EventSkip, ok=true.
func ParseSSELine(line string) (Event, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return Event{Kind: EventSkip}, true
	}

	data := line
	if strings.HasPrefix(line, "event:") {
		// "event: done" terminates the stream; the final data event
		// (response/status FINISHED) already fired before it.
		return Event{Kind: EventSkip}, true
	}
	if strings.HasPrefix(line, ":") {
		return Event{Kind: EventSkip}, true
	}
	if strings.HasPrefix(data, "data: ") {
		data = data[6:]
	} else if strings.HasPrefix(data, "data:") {
		data = data[5:]
	} else {
		return Event{Kind: EventSkip}, true
	}

	data = strings.TrimSpace(data)
	if data == "" {
		return Event{Kind: EventSkip}, true
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		// Not JSON — ignore, mirroring the web app's tolerant parser.
		return Event{Kind: EventSkip}, true
	}

	// First event: {"request_message_id":15,"response_message_id":16,"model_type":"default"}
	if raw["request_message_id"] != nil && raw["response_message_id"] != nil {
		var parent int64
		_ = json.Unmarshal(raw["response_message_id"], &parent)
		return Event{Kind: EventIDs, ParentMsgID: parent}, true
	}

	// Keep-alive / session updates: {"updated_at": ...}
	if raw["updated_at"] != nil {
		return Event{Kind: EventSkip}, true
	}

	// {"v": {"response": {...}}}
	if v, ok := raw["v"]; ok {
		var vObj struct {
			Response *struct {
				Fragments []struct {
					Type    string `json:"type"`
					Content any    `json:"content"`
				} `json:"fragments"`
			} `json:"response"`
		}
		if err := json.Unmarshal(v, &vObj); err == nil && vObj.Response != nil {
			for _, f := range vObj.Response.Fragments {
				if f.Type != "RESPONSE" {
					continue
				}
				if s, ok := f.Content.(string); ok && strings.TrimSpace(s) != "" {
					return Event{Kind: EventText, Content: s}, true
				}
			}
		}
	}

	// Status: {"p":"response/status","v":"FINISHED"}
	if string(raw["p"]) == `"response/status"` {
		var status string
		_ = json.Unmarshal(raw["v"], &status)
		switch status {
		case "FINISHED", "COMPLETED":
			return Event{Kind: EventFinish}, true
		default:
			return Event{Kind: EventSkip}, true
		}
	}

	// Content append: {"p":"response/fragments/-1/content","o":"APPEND","v":"..."}
	if string(raw["p"]) == `"response/fragments/-1/content"` {
		if v, ok := raw["v"]; ok {
			var s string
			if err := json.Unmarshal(v, &s); err == nil && s != "" {
				return Event{Kind: EventText, Content: s}, true
			}
		}
		return Event{Kind: EventSkip}, true
	}

	// Simple content: {"v":"..."}
	if v, ok := raw["v"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err == nil && s != "" {
			return Event{Kind: EventText, Content: s}, true
		}
	}

	// Error payloads: {"code":40003,"msg":"...","data":null}
	if _, ok := raw["code"]; ok {
		var msg string
		_ = json.Unmarshal(raw["msg"], &msg)
		return Event{Kind: EventError, ErrMsg: msg}, true
	}

	return Event{Kind: EventSkip}, true
}
