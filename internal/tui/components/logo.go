package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var logoLines = []string{
	"███████  █████  ██   ██ ███████ ███    ███  ██████  ██████  ███████ ██       █████  ██████  ██",
	"██      ██   ██ ██  ██  ██      ████  ████ ██    ██ ██   ██ ██      ██      ██   ██ ██   ██ ██",
	"█████   ███████ █████   █████   ██ ████ ██ ██    ██ ██   ██ █████   ██      ███████ ██████  ██",
	"██      ██   ██ ██  ██  ██      ██  ██  ██ ██    ██ ██   ██ ██      ██      ██   ██ ██      ██",
	"██      ██   ██ ██   ██ ███████ ██      ██  ██████  ██████  ███████ ███████ ██   ██ ██      ██",
}

var (
	LogoLight = lipgloss.NewStyle().Foreground(lipgloss.Color("#8AA8FF")).Bold(true)
	LogoDark  = lipgloss.NewStyle().Foreground(lipgloss.Color("#4A5A8A")).Bold(true)
)

func RenderLogo(width int) string {
	var b strings.Builder
	for i, line := range logoLines {
		if i < 3 {
			b.WriteString(LogoLight.Render(line))
		} else {
			b.WriteString(LogoDark.Render(line))
		}
		if i < len(logoLines)-1 {
			b.WriteString("\n")
		}
	}
	return lipgloss.Place(width, lipgloss.Height(b.String()), lipgloss.Center, lipgloss.Center, b.String())
}
