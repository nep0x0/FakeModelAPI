package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"fakemodelapi/internal/auth"
	"fakemodelapi/internal/config"
	"fakemodelapi/internal/provider"
)

// Check adalah satu hasil pemeriksaan doctor.
type Check struct {
	Name    string // nama pemeriksaan
	OK      bool
	Warn    bool // true = tidak fatal tapi perlu perhatian
	Message string
	Action  string // saran perbaikan yang actionable
}

// Result adalah kumpulan hasil pemeriksaan.
type Result struct {
	Checks []Check
}

// All runs semua pemeriksaan dan mengembalikan Result.
func All(ctx context.Context, cfg config.Config, p provider.Provider) Result {
	return Result{Checks: []Check{
		checkConfig(cfg, p),
		checkSession(p),
		checkServer(ctx, cfg),
	}}
}

func checkConfig(cfg config.Config, p provider.Provider) Check {
	if p == nil {
		return Check{Name: "konfigurasi", OK: false,
			Message: "provider '" + cfg.Provider + "' tidak terdaftar",
			Action:  "Gunakan provider yang tersedia: deepseek, dummy."}
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return Check{Name: "konfigurasi", OK: false,
			Message: "port tidak valid: " + fmt.Sprint(cfg.Port),
			Action:  "Set FAKEAPI_PORT atau --port ke angka 1-65535."}
	}
	return Check{Name: "konfigurasi", OK: true,
		Message: fmt.Sprintf("provider=%s port=%d timeout=%s", cfg.Provider, cfg.Port, cfg.Timeout)}
}

func checkSession(p provider.Provider) Check {
	ai := p.AuthStatus()
	if !ai.LoggedIn {
		return Check{Name: "session login", OK: false,
			Message: "belum login ke provider",
			Action:  "Jalankan /login (TUI) atau fakeapi login."}
	}
	st, err := auth.GetVault().Status(p.ID())
	if err == nil && st.Expired {
		return Check{Name: "session login", OK: false, Warn: true,
			Message: "session sudah kedaluwarsa",
			Action:  "Jalankan /login lagi untuk memperbarui session."}
	}
	return Check{Name: "session login", OK: true,
		Message: fmt.Sprintf("login aktif (%s)", ai.Username)}
}

func checkServer(ctx context.Context, cfg config.Config) Check {
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", cfg.Port)
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return Check{Name: "server API", OK: false, Message: err.Error()}
	}
	if cfg.Token != "" {
		req.Header.Set("authorization", "Bearer "+cfg.Token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Check{Name: "server API", OK: false,
			Message: "tidak bisa hubungi " + url,
			Action:  "Jalankan fakeapi -headless atau /start di TUI untuk menyalakan server."}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Check{Name: "server API", OK: false,
			Message: fmt.Sprintf("HTTP %d dari %s", resp.StatusCode, url),
			Action:  "Periksa log server (fakeapi /logs atau file log)."}
	}

	var body struct {
		Status   string `json:"status"`
		Provider string `json:"provider"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Check{Name: "server API", OK: false,
			Message: "respons /healthz tidak valid: " + err.Error()}
	}
	if body.Status != "ok" {
		return Check{Name: "server API", OK: false,
			Message: "status server: " + body.Status}
	}
	return Check{Name: "server API", OK: true,
		Message: fmt.Sprintf("server berjalan di %s (provider=%s)", url, body.Provider)}
}

// Render mencetak hasil pemeriksaan dalam format teks yang mudah dibaca.
func (r Result) Render() string {
	out := ""
	for _, c := range r.Checks {
		icon := "✓"
		if !c.OK {
			icon = "✗"
		} else if c.Warn {
			icon = "⚠"
		}
		out += fmt.Sprintf("%s %s: %s\n", icon, c.Name, c.Message)
		if c.Action != "" {
			out += "   → " + c.Action + "\n"
		}
	}
	return out
}

// AllOK mengembalikan true jika tidak ada pemeriksaan yang gagal.
func (r Result) AllOK() bool {
	for _, c := range r.Checks {
		if !c.OK {
			return false
		}
	}
	return true
}
