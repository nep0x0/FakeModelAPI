package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func RenderSlashSuggest(width int, filtered []string, selectedIdx int) string {
	if len(filtered) == 0 {
		return ""
	}

	inputW := width - 20
	if inputW > 68 {
		inputW = 68
	}
	if inputW < 40 {
		inputW = 40
	}

	innerWidth := inputW - 2

	selStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#FFB07C")).
		Foreground(lipgloss.Color("#000000")).
		Bold(true).
		Width(innerWidth)

	cmdStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#8A8A8A"))

	var lines []string
	for i, c := range filtered {
		if i >= 8 {
			break
		}
		parts := strings.SplitN(c, " ", 2)
		cmd := parts[0]
		desc := ""
		if len(parts) > 1 {
			desc = parts[1]
		}

		cmdWidth := 16
		descWidth := innerWidth - cmdWidth - 1
		if descWidth < 0 {
			descWidth = 0
		}

		cmdFormatted := fmt.Sprintf("%-*s", cmdWidth, cmd)
		if len(cmd) > cmdWidth {
			cmdFormatted = cmd[:cmdWidth]
		}

		lineStr := ""
		if i == selectedIdx {
			rowText := fmt.Sprintf(" %s %s", cmdFormatted, desc)
			lineStr = selStyle.Render(rowText)
		} else {
			cmdPart := cmdStyle.Render(fmt.Sprintf(" %s", cmdFormatted))
			descPart := descStyle.Render(fmt.Sprintf("%-*s ", descWidth, desc))
			lineStr = cmdPart + descPart
		}
		lines = append(lines, lineStr)
	}

	content := strings.Join(lines, "\n")

	borderStyle := lipgloss.NewStyle().
		Width(inputW).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#4A4A4A"))

	box := borderStyle.Render(content)
	return box
}

func RenderSlashSuggestStyled(filtered []string, selectedIdx int, selStyle, sugStyle lipgloss.Style) string {
	if len(filtered) == 0 {
		return ""
	}
	var out string
	for i, c := range filtered {
		if i >= 5 {
			break
		}
		if i == selectedIdx {
			out += selStyle.Render(c) + "\n"
		} else {
			out += sugStyle.Render(c) + "\n"
		}
	}
	return out
}
