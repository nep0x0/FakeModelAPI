package components

import (
	"strings"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

func RenderChatView(vp viewport.Model, messages []string) string {
	return vp.View()
}

func FormatUserMsg(content string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#E5E5E5")).Bold(true).Render("> "+content)
}

func FormatAssistantMsg(content string, streaming bool) string {
	if content == "" && streaming {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#8A8A8A")).Render("⠋ thinking...")
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#CCCCCC")).Render(content)
}

func BuildChatContent(msgs []struct{Role, Content string}, streamBuf string, isStreaming bool) string {
	var b strings.Builder
	for _, m := range msgs {
		if m.Role == "user" {
			b.WriteString(FormatUserMsg(m.Content) + "\n\n")
		} else {
			c := m.Content
			if c == "" && isStreaming {
				c = streamBuf
			}
			b.WriteString(FormatAssistantMsg(c, isStreaming) + "\n\n")
		}
	}
	return b.String()
}
