package components

import (
	"strings"
	"github.com/charmbracelet/lipgloss"
)

func RenderPalette(width, height int, filtered []string, selectedIdx int, selStyle, sugStyle lipgloss.Style) string {
	paletteW := 50
	if width-20 < paletteW {
		paletteW = width - 20
	}
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Render("Commands"))
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#8A8A8A")).Render("Type to filter, ↑↓ to navigate, enter to select, esc to close"))
	b.WriteString("\n\n")
	for i, c := range filtered {
		if i >= 8 {
			break
		}
		if i == selectedIdx {
			b.WriteString(selStyle.Width(paletteW - 4).Render("> "+c) + "\n")
		} else {
			b.WriteString(sugStyle.Width(paletteW-4).Render("  "+c) + "\n")
		}
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#4A4A4A")).
		Padding(1, 2).
		Width(paletteW).
		Render(b.String())
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
