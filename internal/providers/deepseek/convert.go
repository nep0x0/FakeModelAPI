package deepseek

import (
	"strings"

	"fakemodelapi/internal/provider"
)

// BuildChatRequest converts provider messages into the DeepSeek web payload.
// DeepSeek's web API is stateless per message: only the latest prompt is sent,
// conversation context rides on the parent_message_id chain. System messages
// are prepended into the prompt.
func BuildChatRequest(msgs []provider.Message, modelID string, parentID *int64) (ChatRequest, error) {
	if len(msgs) == 0 {
		return ChatRequest{}, nil
	}

	var promptParts []string
	var lastUser string
	for _, m := range msgs {
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		if m.Role == "system" {
			promptParts = append(promptParts, m.Content)
		} else if m.Role == "user" {
			lastUser = m.Content
		}
	}
	promptParts = append(promptParts, lastUser)

	prompt := strings.Join(promptParts, "\n\n")
	if strings.TrimSpace(prompt) == "" {
		return ChatRequest{}, nil
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
