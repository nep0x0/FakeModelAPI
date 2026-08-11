package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fakemodelapi/internal/auth"
	"fakemodelapi/internal/config"
	"fakemodelapi/internal/doctor"
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

	// Prioritas config: file ~/.fakeapi/config.json → env FAKEAPI_* → flag CLI.
	cfg := config.Default()
	if fc, err := config.Load(config.DefaultPath()); err == nil {
		cfg = fc
	}
	cfg = config.FromEnv()

	// Subcommand sederhana: fakeapi status | fakeapi doctor | fakeapi config init | fakeapi -headless ...
	sub := ""
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "status", "doctor", "config":
			sub = os.Args[1]
		}
	}
	if sub != "" {
		rest := os.Args[2:]
		switch sub {
		case "config":
			runConfig(rest)
			return
		default:
			asJSON := false
			fs := flag.NewFlagSet("fakeapi "+sub, flag.ExitOnError)
			fs.StringVar(&cfg.Provider, "provider", cfg.Provider, "provider aktif")
			fs.IntVar(&cfg.Port, "port", cfg.Port, "port server lokal")
			fs.StringVar(&cfg.Token, "token", cfg.Token, "auth token lokal")
			if sub == "status" {
				fs.BoolVar(&asJSON, "json", false, "output dalam format JSON")
			}
			_ = fs.Parse(rest)
			if sub == "status" && asJSON {
				printStatusJSON(cfg, pForStatus(cfg))
				return
			}
			runSubcommand(sub, cfg)
			return
		}
	}

	headless := false
	args := make([]string, 0, len(os.Args)-1)
	for _, a := range os.Args[1:] {
		if a == "-headless" {
			headless = true
			continue
		}
		args = append(args, a)
	}

	fs := flag.NewFlagSet("fakeapi", flag.ExitOnError)
	fs.IntVar(&cfg.Port, "port", cfg.Port, "port server lokal")
	fs.StringVar(&cfg.Provider, "provider", cfg.Provider, "provider aktif")
	fs.StringVar(&cfg.Token, "token", cfg.Token, "auth token lokal (opsional, wajib dikirim client sebagai Bearer)")
	fs.DurationVar(&cfg.Timeout, "timeout", cfg.Timeout, "timeout request ke provider")
	_ = fs.Parse(args)

	if headless {
		runHeadless(cfg)
		return
	}

	m := tui.NewModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

// runConfig mengelola konfigurasi: `fakeapi config init [--force]`.
// Menulis default (atau nilai env saat ini) ke ~/.fakeapi/config.json.
func runConfig(args []string) {
	fs := flag.NewFlagSet("fakeapi config", flag.ExitOnError)
	force := fs.Bool("force", false, "timpa file konfigurasi yang sudah ada")
	_ = fs.Parse(args)

	initCmd := false
	for _, a := range args {
		if a == "init" {
			initCmd = true
		}
	}

	path := config.DefaultPath()
	if initCmd {
		if path == "" {
			fmt.Println("✗ tidak bisa tentukan home directory")
			os.Exit(1)
		}
		if _, err := os.Stat(path); err == nil && !*force {			fmt.Println("konfigurasi sudah ada di " + path)
			fmt.Println("gunakan --force untuk menimpa.")
			return
		}
		c := config.Default()
		if err := config.Save(path, c); err != nil {
			fmt.Println("✗ gagal menulis konfigurasi:", err)
			os.Exit(1)
		}
		fmt.Println("✓ konfigurasi ditulis ke", path)
		fmt.Printf("  port=%d provider=%s timeout=%s\n", c.Port, c.Provider, c.Timeout)
		fmt.Println("Edit file ini, atau set env FAKEAPI_PORT/FAKEAPI_PROVIDER/FAKEAPI_TIMEOUT untuk override.")
		return
	}

	c, err := config.Load(path)
	if err != nil {
		fmt.Println("✗", err)
		os.Exit(1)
	}
	fmt.Printf("port=%d\nprovider=%s\ntimeout=%s\ntoken=%s\n",
		c.Port, c.Provider, c.Timeout, tokenPreview(c.Token))
	fmt.Println("file:", path)
}

func tokenPreview(tok string) string {
	if tok == "" {
		return "(kosong)"
	}
	if len(tok) <= 4 {
		return "***"
	}
	return tok[:2] + "***" + tok[len(tok)-2:]
}

// pForStatus mengambil provider dari registry; nil jika tidak dikenal.
func pForStatus(cfg config.Config) provider.Provider {
	p, err := provider.Get(cfg.Provider)
	if err != nil {
		return nil
	}
	return p
}

// printStatusJSON mencetak status dalam format JSON (machine-readable).
func printStatusJSON(cfg config.Config, p provider.Provider) {
	type jsonStatus struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Port     int    `json:"port"`
		Endpoint string `json:"endpoint"`
		LoggedIn bool   `json:"logged_in"`
		Username string `json:"username,omitempty"`
		Expired  bool   `json:"expired"`
		Expires  string `json:"expires,omitempty"`
		ServerUp bool   `json:"server_up"`
		ServerOK bool   `json:"server_ok"`
	}

	st := jsonStatus{Provider: cfg.Provider, Port: cfg.Port,
		Endpoint: fmt.Sprintf("http://localhost:%d/v1", cfg.Port)}
	if p != nil {
		st.Provider = p.Name()
		st.Model = modelName(p)
		ai := p.AuthStatus()
		st.LoggedIn = ai.LoggedIn
		st.Username = ai.Username
		if ai.LoggedIn {
			if vs, err := auth.GetVault().Status(p.ID()); err == nil {
				st.Expired = vs.Expired
				if !vs.ExpiresAt.IsZero() {
					st.Expires = vs.ExpiresAt.Local().Format(time.RFC3339)
				}
			}
		}
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", cfg.Port)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err == nil && cfg.Token != "" {
		req.Header.Set("authorization", "Bearer "+cfg.Token)
	}
	if resp, err := http.DefaultClient.Do(req); err == nil {
		defer resp.Body.Close()
		st.ServerUp = true
		st.ServerOK = resp.StatusCode == http.StatusOK
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		fmt.Println("✗ gagal membuat JSON:", err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}

// runSubcommand menjalankan `fakeapi status` atau `fakeapi doctor`.
func runSubcommand(sub string, cfg config.Config) {
	p, err := provider.Get(cfg.Provider)
	if err != nil {
		fmt.Println("✗ provider tidak dikenal:", cfg.Provider)
		os.Exit(1)
	}

	switch sub {
	case "status":
		printStatus(cfg, p)
	case "doctor":
		res := doctor.All(context.Background(), cfg, p)
		fmt.Print(res.Render())
		if !res.AllOK() {
			fmt.Println("\nAda masalah. Ikuti saran (→) di atas untuk memperbaikinya.")
			os.Exit(1)
		}
	}
}

// printStatus mencetak status server + session ke stdout.
func printStatus(cfg config.Config, p provider.Provider) {
	fmt.Println("provider:  ", p.Name())
	fmt.Println("model:     ", modelName(p))
	fmt.Println("endpoint:  ", fmt.Sprintf("http://localhost:%d/v1", cfg.Port))

	ai := p.AuthStatus()
	if !ai.LoggedIn {
		fmt.Println("login:      ✗ belum login")
	} else {
		st, err := auth.GetVault().Status(p.ID())
		if err != nil {
			fmt.Printf("login:     ✓ aktif (%s) — detail status gagal: %v\n", ai.Username, err)
		} else if st.Expired {
			fmt.Printf("login:     ⚠ kedaluwarsa (%s) — jalankan /login ulang\n", ai.Username)
		} else {
			fmt.Printf("login:     ✓ aktif (%s)\n", ai.Username)
			if !st.ExpiresAt.IsZero() {
				fmt.Println("expires:   ", st.ExpiresAt.Local().Format("2006-01-02 15:04"))
			}
		}
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", cfg.Port)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err == nil && cfg.Token != "" {
		req.Header.Set("authorization", "Bearer "+cfg.Token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("server:    ○ mati (jalankan `fakeapi -headless` atau /start di TUI)")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		fmt.Println("server:    ● hidup")
	} else {
		fmt.Println("server:    ● hidup tapi respons HTTP", resp.StatusCode)
	}
}

func modelName(p provider.Provider) string {
	ms := p.Models()
	if len(ms) == 0 {
		return "?"
	}
	return ms[0].ID
}

// runHeadless menjalankan server OpenAI-compatible tanpa TUI.
// Cocok untuk background service: fakeapi -headless [--port 8000 --token ...]
func runHeadless(cfg config.Config) {
	srv := server.New(cfg.Provider, cfg.Port,
		server.WithToken(cfg.Token), server.WithTimeout(cfg.Timeout))
	if err := srv.Start(); err != nil {
		log.Fatalf("gagal start server: %v", err)
	}
	log.Printf("FakeModelAPI berjalan di http://%s (provider: %s)", srv.Addr(), cfg.Provider)
	if cfg.Token != "" {
		log.Printf("auth token lokal aktif: client wajib kirim Authorization: Bearer <token>")
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	if err := srv.Stop(); err != nil {
		log.Printf("gagal stop server: %v", err)
	}
	log.Println("server dihentikan")
}
