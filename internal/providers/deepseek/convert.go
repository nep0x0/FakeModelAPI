package deepseek

import (
	"encoding/json"
	"fmt"
	"strings"

	"fakemodelapi/internal/provider"
)

// maxPromptLen membatasi panjang prompt yang diratakan agar konteks
// DeepSeek web tidak meledak (riwayat terpotong dari depan).
const maxPromptLen = 60000

// BuildChatRequest converts provider messages into the DeepSeek web payload.
// The web API is stateless per message: only a single prompt is sent, and
// conversation context normally rides on the parent_message_id chain. To make
// the proxy behave like a real OpenAI provider, the FULL history (system,
// user, assistant, tool calls, tool results) is flattened into the prompt
// with role markers so multi-turn tool loops stay deterministic.
func BuildChatRequest(msgs []provider.Message, modelID string, parentID *int64) (ChatRequest, error) {
	if len(msgs) == 0 {
		return ChatRequest{}, nil
	}

	var b strings.Builder
	for _, m := range msgs {
		if strings.TrimSpace(m.Content) == "" && len(m.ToolCalls) == 0 {
			continue
		}
		switch m.Role {
		case "system":
			b.WriteString("[System]\n" + m.Content + "\n\n")
		case "user":
			b.WriteString("[User]\n" + m.Content + "\n\n")
		case "assistant":
			if len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					argsJSON, err := json.Marshal(tc.Arguments)
					if err != nil {
						argsJSON = []byte("{}")
					}
					b.WriteString(fmt.Sprintf("[Assistant] memanggil tool %s dengan argumen: %s\n", tc.Name, argsJSON))
				}
				if strings.TrimSpace(m.Content) != "" {
					b.WriteString("[Assistant]\n" + m.Content + "\n\n")
				}
				continue
			}
			b.WriteString("[Assistant]\n" + m.Content + "\n\n")
		case "tool":
			id := m.ToolCallID
			if id == "" {
				id = "?"
			}
			b.WriteString("[Hasil tool " + id + "]\n" + m.Content + "\n\n")
		default:
			b.WriteString("[" + m.Role + "]\n" + m.Content + "\n\n")
		}
	}

	prompt := strings.TrimSpace(b.String())
	if prompt == "" {
		return ChatRequest{}, nil
	}
	if len(prompt) > maxPromptLen {
		prompt = "[Bagian awal percakapan terpotong]\n\n" + prompt[len(prompt)-maxPromptLen:]
	}

	return ChatRequest{
		ParentMessageID: parentID,
		Prompt:          prompt,
		RefFileIDs:      []string{},
		ThinkingEnabled: isReasoner(modelID),
		SearchEnabled:   false,
		ModelType:       "default",
		Preempt:         false,
		Action:          nil,
	}, nil
}

func isReasoner(modelID string) bool {
	return strings.Contains(modelID, "reasoner") || strings.Contains(modelID, "r1") || strings.Contains(modelID, "thinking")
}

// EventToChunk converts a parsed DeepSeek SSE event to a provider.Chunk.
func EventToChunk(ev Event) provider.Chunk {
	switch ev.Kind {
	case EventText:
		return provider.Chunk{Delta: ev.Content}
	case EventFinish:
		return provider.Chunk{FinishReason: "stop"}
	default:
		return provider.Chunk{}
	}
}
