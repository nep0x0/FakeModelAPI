package tui

import (
	"strings"
	"fakemodelapi/internal/tui/components"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

var logoLines = []string{
	"███████  █████  ██   ██ ███████ ███    ███  ██████  ██████  ███████ ██       █████  ██████  ██",
	"██      ██   ██ ██  ██  ██      ████  ████ ██    ██ ██   ██ ██      ██      ██   ██ ██   ██ ██",
	"█████   ███████ █████   █████   ██ ████ ██ ██    ██ ██   ██ █████   ██      ███████ ██████  ██",
	"██      ██   ██ ██  ██  ██      ██  ██  ██ ██    ██ ██   ██ ██      ██      ██   ██ ██      ██",
	"██      ██   ██ ██   ██ ███████ ██      ██  ██████  ██████  ███████ ███████ ██   ██ ██      ██",
}

func overlayBox(bg string, fg string, fgStartY int, totalWidth int) string {
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")

	fgWidth := lipgloss.Width(fg)
	leftPad := (totalWidth - fgWidth) / 2
	if leftPad < 0 {
		leftPad = 0
	}

	for i, fgLine := range fgLines {
		targetY := fgStartY + i
		if targetY < 0 || targetY >= len(bgLines) {
			continue
		}

		bgLine := bgLines[targetY]
		leftBg := truncateANSI(bgLine, leftPad)
		leftW := lipgloss.Width(leftBg)
		if leftW < leftPad {
			leftBg += strings.Repeat(" ", leftPad-leftW)
		}

		rightBg := cutLeftANSI(bgLine, leftPad+fgWidth)
		bgLines[targetY] = leftBg + fgLine + rightBg
	}

	return strings.Join(bgLines, "\n")
}

func truncateANSI(s string, mw int) string {
	var buf strings.Builder
	w := 0
	inAnsi := false
	for _, r := range s {
		if r == '\x1b' {
			inAnsi = true
		}
		if inAnsi {
			buf.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inAnsi = false
			}
			continue
		}
		rw := runewidth.RuneWidth(r)
		if w+rw > mw {
			break
		}
		w += rw
		buf.WriteRune(r)
	}
	buf.WriteString("\x1b[0m")
	return buf.String()
}

func cutLeftANSI(s string, cutWidth int) string {
	var buf strings.Builder
	w := 0
	inAnsi := false
	skipping := true
	for _, r := range s {
		if r == '\x1b' {
			inAnsi = true
		}
		if skipping {
			if inAnsi {
				buf.WriteRune(r)
				if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
					inAnsi = false
				}
				continue
			}
			rw := runewidth.RuneWidth(r)
			if w+rw > cutWidth {
				skipping = false
				buf.WriteRune(r)
			}
			w += rw
		} else {
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

func (m Model) View() string {
	if m.width == 0 {
		return "loading..."
	}
	if m.width < 80 || m.height < 24 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(RedDot).Bold(true).Render("terminal too small (min 80x24)"))
	}
	if m.showCmdPalette {
		return m.renderPaletteOverlay()
	}
	inputBox := m.renderInputBox()
	hintBar := components.RenderHintBar(m.width, m.width-20)
	tipBar := components.RenderTipBarComponent(m.width, m.tipIndex)
	bottomContent := inputBox + "\n\n" + hintBar + "\n\n" + tipBar
	isLogo := m.showLogo && len(m.messages) == 0 && !m.isStreaming
	var centeredBase string
	if isLogo {
		logoStr := m.renderLogoCentered()
		content := lipgloss.JoinVertical(
			lipgloss.Center,
			logoStr,
			"\n",
			inputBox,
			"\n",
			hintBar,
			"\n",
			tipBar,
		)
		centeredBase = lipgloss.Place(m.width, m.height-1, lipgloss.Center, lipgloss.Center, content)
	} else {
		var topContent string
		if len(m.messages) > 0 {
			topContent = m.viewport.View()
		}
		var baseContent string
		if topContent != "" {
			baseContent = topContent + "\n" + bottomContent
		} else {
			baseContent = bottomContent
		}
		centeredBase = lipgloss.Place(m.width, m.height-1, lipgloss.Center, lipgloss.Center, baseContent)
	}
	if m.slashMode && len(m.filteredCmds) > 0 {
		slashBox := components.RenderSlashSuggest(m.width, m.filteredCmds, m.selectedIdx)
		if slashBox != "" {
			slashH := lipgloss.Height(slashBox)
			var inputY int
			if isLogo {
				logoH := lipgloss.Height(m.renderLogoCentered())
				contentH := logoH + 1 + lipgloss.Height(inputBox) + 1 + lipgloss.Height(hintBar) + 1 + lipgloss.Height(tipBar)
				contentTop := (m.height - 1 - contentH) / 2
				if contentTop < 0 {
					contentTop = 0
				}
				inputY = contentTop + logoH + 1
			} else {
				if len(m.messages) > 0 {
					vh := m.viewport.Height
					if vh == 0 {
						vh = 10
					}
					centerTop := (m.height - 1 - (vh + lipgloss.Height(bottomContent) + 1)) / 2
					inputY = centerTop + vh + 1
				} else {
					inputY = (m.height - 1 - lipgloss.Height(bottomContent)) / 2
				}
			}

			dropdownStartY := inputY - slashH
			if dropdownStartY < 0 {
				dropdownStartY = 0
			}

			centeredBase = overlayBox(centeredBase, slashBox, dropdownStartY, m.width)
		}
	}
	status := components.RenderStatusBar(m.width, m.serverOn, m.version, StatusBarStyle, StatusDotOnStyle, StatusDotOffStyle, VersionStyle)
	return lipgloss.JoinVertical(lipgloss.Left, centeredBase, status)
}

func (m Model) renderLogoCentered() string {
	var b strings.Builder
	for i, line := range logoLines {
		if i < 3 {
			b.WriteString(LogoLightStyle.Render(line))
		} else {
			b.WriteString(LogoDarkStyle.Render(line))
		}
		b.WriteString("\n")
	}
	byStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFB07C")).Bold(true)
	b.WriteString(byStyle.Render("made by nep0x0"))
	logo := b.String()
	return lipgloss.Place(m.width, lipgloss.Height(logo), lipgloss.Center, lipgloss.Center, logo)
}

func (m Model) renderLogo() string {
	return m.renderLogoCentered()
}

func (m Model) renderInputBox() string {
	inputW := m.width - 20
	if inputW > 68 {
		inputW = 68
	}
	if inputW < 40 {
		inputW = 40
	}
	taView := m.textarea.View()
	buildPart := ModeBuildStyle.Render(m.mode)
	providerPart := ModeProviderStyle.Render(m.modelName)
	endpointPart := ModeEndpointStyle.Render(m.endpoint)
	variantPart := ModeMaxStyle.Render(m.variant)
	modeBar := buildPart + lipgloss.NewStyle().Foreground(FaintGray).Render(" · ") + providerPart + " " + endpointPart
	if m.variant!= "" {
		modeBar += lipgloss.NewStyle().Foreground(FaintGray).Render(" · ") + variantPart
	}
	inner := taView + "\n\n" + modeBar
	boxStyle := lipgloss.NewStyle().
		Width(inputW).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(DarkGray).
		Padding(0, 1)
	box := boxStyle.Render(inner)
	return lipgloss.Place(m.width, lipgloss.Height(box), lipgloss.Center, lipgloss.Center, box)
}

func (m Model) renderSlashSuggest() string {
	return components.RenderSlashSuggest(m.width, m.filteredCmds, m.selectedIdx)
}

func (m Model) renderPaletteOverlay() string {
	paletteW := 50
	if m.width-20 < paletteW {
		paletteW = m.width - 20
	}
	var b strings.Builder
	b.WriteString("\n")
	title := "Commands"
	if m.paletteMode == "models" {
		title = "Models"
	}
	b.WriteString(lipgloss.NewStyle().Foreground(White).Bold(true).Render(title))
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(FaintGray).Render("Type to filter, ↑↓ to navigate, enter to select, esc to close"))
	b.WriteString("\n\n")
	for i, c := range m.filteredCmds {
		if i == m.selectedIdx {
			b.WriteString(SlashSelectedStyle.Width(paletteW - 4).Render("> "+c) + "\n")
		} else {
			b.WriteString(SlashSuggestionStyle.Width(paletteW-4).Render(" "+c) + "\n")
		}
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(DarkGray).
		Padding(1, 2).
		Width(paletteW).
		Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
