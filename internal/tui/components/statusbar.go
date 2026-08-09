package components

import (
	"strings"
	"github.com/charmbracelet/lipgloss"
)

func RenderStatusBar(width int, serverOn bool, version string, barStyle, dotOn, dotOff, verStyle lipgloss.Style) string {
	tilde := barStyle.Render("~")
	dot := "○"
	style := dotOff
	text := "server off"
	if serverOn {
		dot = "●"
		style = dotOn
		text = "server on"
	}
	serverPart := style.Render(dot) + " " + barStyle.Render(text)
	mcp := barStyle.Render("1 MCP")
	cmd := barStyle.Render("/status")
	left := tilde + " " + serverPart + " " + mcp + " " + cmd
	right := verStyle.Render(version)
	sp := width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if sp < 0 {
		sp = 0
	}
	return barStyle.Width(width).Render(left + strings.Repeat(" ", sp) + right)
}
