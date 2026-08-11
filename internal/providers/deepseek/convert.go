package deepseek

import (
	"strings"

	"fakemodelapi/internal/conversation"
	"fakemodelapi/internal/provider"
)

// maxPromptLen membatasi panjang prompt yang diratakan agar konteks
// DeepSeek web tidak meledak (riwayat terpotong dari depan; system prompt
// dipertahankan utuh oleh conversation.Flatten).
const maxPromptLen = 100000

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

	prompt := conversation.Flatten(msgs, maxPromptLen)
	if prompt == "" {
		return ChatRequest{}, nil
	}

	return ChatRequest{
		ParentMessageID: parentID,
		Prompt:          prompt,
		RefFileIDs:      []string{},
		ThinkingEnabled: true, // DeepThink aktif untuk kedua model (pilihan user)
		SearchEnabled:   false,
		ModelType:       modelTypeFor(modelID),
		Preempt:         false,
		Action:          nil,
	}, nil
}

// modelTypeFor memetakan ID model publik ke nilai model_type wire DeepSeek
// web: "default" = DeepSeek-chat-Instant-Think-Search (model cepat),
// "expert" = DeepSeek-chat-Expert-Think (model kuat/lambat).
func modelTypeFor(modelID string) string {
	if isReasoner(modelID) {
		return "expert"
	}
	return "default"
}

func isReasoner(modelID string) bool {
	return strings.Contains(modelID, "reasoner") || strings.Contains(modelID, "expert") || strings.Contains(modelID, "r1")
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
