package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"fakemodelapi/internal/provider"
	"fakemodelapi/internal/providers/deepseek"
	"fakemodelapi/internal/providers/dummy"
	"fakemodelapi/internal/server"
	"fakemodelapi/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Register providers
	provider.Register("dummy", dummy.New())
	provider.Register("deepseek", deepseek.New())

	if len(os.Args) > 1 && os.Args[1] == "-headless" {
		runHeadless()
		return
	}

	m := tui.NewModel()
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

// runHeadless menjalankan server OpenAI-compatible tanpa TUI.
// Cocok untuk background service: fakeapi -headless
func runHeadless() {
	srv := server.New("deepseek", server.DefaultPort)
	if err := srv.Start(); err != nil {
		log.Fatalf("gagal start server: %v", err)
	}
	log.Printf("FakeModelAPI berjalan di http://%s (provider: deepseek)", srv.Addr())

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	if err := srv.Stop(); err != nil {
		log.Printf("gagal stop server: %v", err)
	}
	log.Println("server dihentikan")
}
