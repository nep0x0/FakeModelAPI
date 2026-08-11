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

// ParseSSELine parses a single line of the DeepSeek SSE stream secara
// stateless (tanpa pengetahuan fragment). Untuk stream sungguhan pakai
// streamParser yang melacak tipe fragment — dengan thinking_enabled,
// appends "response/fragments/-1/content" menunjuk fragment TERAKHIR yang
// bisa berupa THINK maupun RESPONSE.
func ParseSSELine(line string) (Event, bool) {
	return NewStreamParser().Parse(line)
}

// fragmentInfo adalah satu fragment yang dilaporkan stream.
type fragmentInfo struct {
	ID      int64  `json:"id"`
	Type    string `json:"type"` // "RESPONSE" | "THINK" | ...
	Content any    `json:"content"`
}

// streamParser melacak state sepanjang satu stream SSE: tipe setiap fragment
// dan path append aktif, supaya teks thinking tidak bocor ke jawaban.
type streamParser struct {
	fragments   map[int64]string // fragment id -> tipe
	lastFragID  int64            // fragment yang dirujuk index "-1"
	haveFrag    bool             // sudah pernah lihat daftar fragment
	activePath  string           // path append aktif (untuk bare {"v": ...})
}

// NewStreamParser membuat parser stream berstate.
func NewStreamParser() *streamParser {
	return &streamParser{fragments: make(map[int64]string)}
}

// Parse memproses satu baris (sudah termasuk "data: " bila ada) dan
// mengembalikan event + ok=true untuk baris yang membawa event.
func (p *streamParser) Parse(line string) (Event, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return Event{Kind: EventSkip}, true
	}
	if strings.HasPrefix(line, "event:") || strings.HasPrefix(line, ":") {
		// "event: done" mengakhiri stream; event data terakhir
		// (response/status FINISHED) sudah terkirim sebelumnya.
		return Event{Kind: EventSkip}, true
	}

	data := line
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

	// Snapshot: {"v": {"response": {"fragments": [...]}}}
	if v, ok := raw["v"]; ok {
		var vObj struct {
			Response *struct {
				Fragments []fragmentInfo `json:"fragments"`
			} `json:"response"`
		}
		if err := json.Unmarshal(v, &vObj); err == nil && vObj.Response != nil && vObj.Response.Fragments != nil {
			return p.handleFragments(vObj.Response.Fragments)
		}
	}

	// Append fragment baru: {"p":"response/fragments","o":"APPEND","v":[{...}]}
	if string(raw["p"]) == `"response/fragments"` && string(raw["o"]) == `"APPEND"` {
		var frags []fragmentInfo
		if v, ok := raw["v"]; ok && json.Unmarshal(v, &frags) == nil && frags != nil {
			return p.handleFragments(frags)
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
	// atau content set tanpa "o": {"p":".../-1/content","v":","}
	if string(raw["p"]) == `"response/fragments/-1/content"` {
		p.activePath = "response/fragments/-1/content"
		if v, ok := raw["v"]; ok {
			var s string
			if json.Unmarshal(v, &s) == nil && s != "" {
				if p.isLastFragmentThink() {
					// Teks thinking model — bukan jawaban.
					return Event{Kind: EventSkip}, true
				}
				return Event{Kind: EventText, Content: s}, true
			}
		}
		return Event{Kind: EventSkip}, true
	}

	// Simple content: {"v":"..."} — append ke path aktif.
	if v, ok := raw["v"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil && s != "" {
			if p.activePath == "response/fragments/-1/content" && p.isLastFragmentThink() {
				return Event{Kind: EventSkip}, true
			}
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

// handleFragments mencatat tipe tiap fragment, memperbarui fragment terakhir,
// dan mengembalikan konten fragment RESPONSE (jika ada).
func (p *streamParser) handleFragments(frags []fragmentInfo) (Event, bool) {
	last := Event{Kind: EventSkip}
	haveContent := false
	for _, f := range frags {
		p.fragments[f.ID] = f.Type
		if f.Type == "RESPONSE" {
			if s, ok := f.Content.(string); ok && strings.TrimSpace(s) != "" {
				if !haveContent {
					last = Event{Kind: EventText, Content: s}
					haveContent = true
				} else {
					last.Content += s
				}
			}
		}
	}
	if len(frags) > 0 {
		p.lastFragID = frags[len(frags)-1].ID
		p.haveFrag = true
	}
	return last, true
}

// isLastFragmentThink mengembalikan true jika index "-1" menunjuk fragment
// bertipe THINK. Tanpa informasi fragment (stream lama/mock), anggap
// RESPONSE agar perilaku lama tetap jalan.
func (p *streamParser) isLastFragmentThink() bool {
	if !p.haveFrag {
		return false
	}
	return p.fragments[p.lastFragID] == "THINK"
}
