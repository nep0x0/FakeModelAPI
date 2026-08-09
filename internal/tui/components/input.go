package components

import (
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
)

func RenderInputBox(width int, taView, mode, modelName, endpoint, variant string) string {
	inputW := width - 20
	if inputW > 68 {
		inputW = 68
	}
	if inputW < 40 {
		inputW = 40
	}
	buildPart := lipgloss.NewStyle().Foreground(lipgloss.Color("#5FAFFF")).Bold(true).Render(mode)
	providerPart := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Render(modelName)
	endpointPart := lipgloss.NewStyle().Foreground(lipgloss.Color("#8A8A8A")).Render(endpoint)
	variantPart := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")).Bold(true).Render(variant)
	modeBar := buildPart + lipgloss.NewStyle().Foreground(lipgloss.Color("#6E6E6E")).Render(" · ") + providerPart + " " + endpointPart
	if variant != "" {
		modeBar += lipgloss.NewStyle().Foreground(lipgloss.Color("#6E6E6E")).Render(" · ") + variantPart
	}
	inner := taView + "\n" + modeBar
	boxStyle := lipgloss.NewStyle().
		Width(inputW).
		Border(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(lipgloss.Color("#5FAFFF")).
		BorderLeftForeground(lipgloss.Color("#5FAFFF")).
		Padding(0, 1)
	box := boxStyle.Render(inner)
	return lipgloss.Place(width, lipgloss.Height(box), lipgloss.Center, lipgloss.Center, box)
}

func NewTextarea() textarea.Model {
	ta := textarea.New()
	ta.Placeholder = `Type "/" for commands`
	ta.Focus()
	ta.CharLimit = 5000
	ta.SetWidth(60)
	ta.SetHeight(1)
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	return ta
}
