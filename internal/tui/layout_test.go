package tui

import (
	"testing"

	"fakemodelapi/internal/config"
	"fakemodelapi/internal/provider"
	"fakemodelapi/internal/providers/dummy"

	tea "github.com/charmbracelet/bubbletea"
)

func newTestModel(cfg config.Config) Model {
	provider.Register("deepseek", dummy.New())
	provider.Register("dummy", dummy.New())
	return NewModel(cfg)
}

func TestLayoutFitsTerminal(t *testing.T) {
	cases := []struct {
		name   string
		width  int
		height int
	}{
		{"min 80x24", 80, 24},
		{"typical 100x30", 100, 30},
		{"tall 120x40", 120, 40},
		{"wide 160x50", 160, 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(config.Config{})
			m2, _ := m.Update(tea.WindowSizeMsg{Width: tc.width, Height: tc.height})
			m = m2.(Model)

			// Total frame: viewport + 1 newline + bottom content + status bar
			// harus pas dengan tinggi terminal (tidak overflow).
			frameH := m.viewport.Height + 1 + m.bottomContentHeight() + 1
			if frameH != tc.height {
				t.Fatalf("frameH = %d, want %d (viewport=%d bottom=%d)",
					frameH, tc.height, m.viewport.Height, m.bottomContentHeight())
			}
			if m.viewport.Height < 3 {
				t.Fatalf("viewport height %d < 3", m.viewport.Height)
			}
			if m.width != tc.width || m.height != tc.height {
				t.Fatalf("size not applied: %dx%d", m.width, m.height)
			}
		})
	}
}

// TestLayoutWithMessages memastikan perhitungan tinggi juga konsisten setelah
// pesan masuk (mode chat dengan viewport berisi konten).
func TestLayoutWithMessages(t *testing.T) {
	m := newTestModel(config.Config{})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m2.(Model)
	m.messages = append(m.messages, ChatMsg{Role: "user", Content: "/start"},
		ChatMsg{Role: "assistant", Content: "server started"})
	m.showLogo = false
	m.viewport.SetContent(m.renderChatContent())

	_ = m.View()

	if got := m.viewport.Height; got < 3 {
		t.Fatalf("viewport height = %d < 3", got)
	}
}
