package components

import (
	"github.com/charmbracelet/lipgloss"
)

func RenderHintBar(width, inputWidth int) string {
	// clamp ke lebar input box yang sama (40-68) biar tetap sejajar visual
	// tapi content-nya di-center tengah layar sesuai request
	iw := inputWidth
	if iw > 68 {
		iw = 68
	}
	if iw < 40 {
		iw = 40
	}
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6E6E6E"))
	content := keyStyle.Render("tab") + descStyle.Render(" providers   ") + keyStyle.Render("ctrl+p") + descStyle.Render(" commands")
	// FIX: sebelumnya Align Left -> jadi keliatan ke kiri, sekarang Center
	centered := lipgloss.NewStyle().Width(iw).Align(lipgloss.Center).Render(content)
	return lipgloss.Place(width, 1, lipgloss.Center, lipgloss.Center, centered)
}

func RenderHintBarSimple(keyStyle, descStyle lipgloss.Style) string {
	return keyStyle.Render("tab") + descStyle.Render(" providers   ") + keyStyle.Render("ctrl+p") + descStyle.Render(" commands")
}

func RenderTipBar(width int, tipIndex int, tips []string) string {
	if len(tips) == 0 {
		return ""
	}
	dot := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")).Render("●")
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")).Bold(true).Render(" Tip ")
	tipTextStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#8A8A8A"))
	text := tips[tipIndex%len(tips)]
	full := dot + label + tipTextStyle.Render(text)
	return lipgloss.Place(width, 1, lipgloss.Center, lipgloss.Center, full)
}
