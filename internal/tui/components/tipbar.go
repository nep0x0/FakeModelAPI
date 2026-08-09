package components

import (
	"strings"
	"github.com/charmbracelet/lipgloss"
)

var DefaultTips = []string{
	"pakai /login untuk menghubungkan akun DeepSeek",
	"tekan tab untuk ganti provider/model",
	"pakai /start untuk nyalakan server lokal",
	"Press ctrl+alt+g, end to jump to the most recent message",
	"pakai /status untuk cek server & provider",
}

func RenderTipBarComponent(width, tipIndex int) string {
	if len(DefaultTips) == 0 {
		return ""
	}
	tip := DefaultTips[tipIndex%len(DefaultTips)]
	dot := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")).Render("●")
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")).Bold(true).Render(" Tip ")
	textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#8A8A8A"))
	hotStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true)

	var text string
	if strings.Contains(tip, "ctrl+alt+g, end") {
		parts := strings.Split(tip, "ctrl+alt+g, end")
		text = textStyle.Render(parts[0]) + hotStyle.Render("ctrl+alt+g, end") + textStyle.Render(parts[1])
	} else {
		text = textStyle.Render(tip)
	}
	full := dot + label + text
	return lipgloss.Place(width, 1, lipgloss.Center, lipgloss.Center, full)
}
