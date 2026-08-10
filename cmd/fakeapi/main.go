package main

import (
	"fmt"
	"os"

	"fakemodelapi/internal/provider"
	"fakemodelapi/internal/providers/deepseek"
	"fakemodelapi/internal/providers/dummy"
	"fakemodelapi/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Register providers
	provider.Register("dummy", dummy.New())
	provider.Register("deepseek", deepseek.New())

	m := tui.NewModel()
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
