package provider

import (
	"context"
	"errors"
)

// ErrNotAuthenticated adalah sentinel untuk error session/login tidak valid.
// Server & TUI memetakannya ke status 401 tanpa bergantung pada package
// provider konkret.
var ErrNotAuthenticated = errors.New("not authenticated")

// MessageToolCall mewakili satu panggilan tool yang dibuat model (role "assistant").
type MessageToolCall struct {
	ID        string // id tool call, dirujuk pesan role "tool"
	Name      string
	Arguments any // map[string]any yang sudah di-decode
}

// Message represents a chat message.
type Message struct {
	Role       string // "system", "user", "assistant", "tool"
	Content    string
	ToolCallID string            // role "tool": id dari tool call yang dijawab
	ToolCalls  []MessageToolCall // role "assistant": panggilan tool yang dibuat
}

// Chunk represents a piece of streaming response.
type Chunk struct {
	Delta        string
	FinishReason string
}

// AuthInfo holds authentication status.
type AuthInfo struct {
	LoggedIn bool
	Username string
	Expired  bool
}

// ModelInfo describes a model.
type ModelInfo struct {
	ID          string
	DisplayName string
}

// Capabilities mendeklarasikan kemampuan provider. Gateway & UI memakainya
// untuk menyesuaikan perilaku: membangun prompt tool, menampilkan status,
// dan menyiapkan fallback.
type Capabilities struct {
	// SupportsStreaming menandakan provider punya jalur ChatStream.
	SupportsStreaming bool
	// SupportsTools menandakan provider bisa mengikuti protokol tool call
	// yang diemulasi gateway (blok ```tool dalam prompt).
	SupportsTools bool
	// SupportsSystemRole menandakan pesan role "system" didukung.
	SupportsSystemRole bool
	// RequiresSessionLogin menandakan perlu session login web sebelum chat.
	RequiresSessionLogin bool
	// SupportsModelSelection menandakan model bisa dipilih pengguna.
	SupportsModelSelection bool
	// MaxConcurrent membatasi request chat bersamaan; 0 = tanpa batas.
	MaxConcurrent int
}

// Provider is the interface that all AI providers must implement.
type Provider interface {
	// ID mengembalikan nama registry provider, mis. "deepseek".
	ID() string

	// Name mengembalikan nama tampilan provider.
	Name() string

	// Capabilities mengembalikan kemampuan provider.
	Capabilities() Capabilities

	// Chat sends messages and returns the complete response. model boleh
	// kosong untuk memakai model default yang dipilih SetModel.
	Chat(ctx context.Context, model string, messages []Message) (string, error)

	// ChatStream sends messages and returns a channel of streaming chunks.
	// model boleh kosong untuk memakai model default yang dipilih SetModel.
	ChatStream(ctx context.Context, model string, messages []Message) (<-chan Chunk, error)

	// AuthStatus returns current authentication info.
	AuthStatus() AuthInfo

	// Models returns available models.
	Models() []ModelInfo

	// SetModel selects the model used for subsequent chat calls.
	SetModel(modelID string)

	// Reset starts a fresh conversation on the next chat call.
	Reset()
}
