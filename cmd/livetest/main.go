// Command livetest is a temporary debugging tool for Phase 4 live testing.
// It captures a DeepSeek session via browser login, then sends one message
// through the provider stack without any TUI in the way.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"fakemodelapi/internal/auth"
	"fakemodelapi/internal/provider"
	"fakemodelapi/internal/providers/deepseek"
)

func main() {
	if os.Getenv("FAKEAPI_SKIP_LOGIN") == "" {
		cfg := auth.CaptureSessionInfo{
			URL:             "https://chat.deepseek.com",
			Domain:          ".deepseek.com",
			TokenStorageKey: "userToken",
		}

		fmt.Println(">>> membuka browser, silakan login...")
		progress := make(chan string, 10)
		go func() {
			for msg := range progress {
				fmt.Println(">>>", msg)
			}
		}()

		res := auth.CaptureSession(cfg, 2*time.Minute, progress)
		if res.Error != nil {
			fmt.Println("LOGIN GAGAL:", res.Error)
			os.Exit(1)
		}
		fmt.Printf("LOGIN OK: %d cookies, token len=%d\n", len(res.Cookies), len(res.Token))
		if res.Token == "" {
			fmt.Println("PERINGATAN: token kosong")
		}

		if err := auth.SaveSessionWithToken("deepseek", res.Token, res.Cookies); err != nil {
			fmt.Println("SAVE GAGAL:", err)
			os.Exit(1)
		}
	} else {
		sess, err := auth.LoadSession("deepseek")
		if err != nil {
			fmt.Println("LOAD SESSION GAGAL:", err)
			os.Exit(1)
		}
		fmt.Printf("SESSION DIMUAT: token len=%d, cookies=%d\n", len(sess.Token), len(sess.Cookies))
	}

	prov := deepseek.New()
	status := prov.AuthStatus()
	fmt.Println("AuthStatus:", status.LoggedIn, status.Username)

	ctx := context.Background()
	msgs := []provider.Message{{Role: "user", Content: "Halo, balas satu kalimat saja."}}

	ch, err := prov.ChatStream(ctx, "deepseek-reasoner", msgs)
	if err != nil {
		fmt.Println("CHATSTREAM GAGAL:", err)
		os.Exit(1)
	}

	fmt.Println("=== STREAM ===")
	for c := range ch {
		if c.Delta != "" {
			fmt.Print(c.Delta)
		}
		if c.FinishReason != "" {
			fmt.Printf("\n=== DONE: %s ===\n", c.FinishReason)
		}
	}
	fmt.Println("=== TEST SELESAI ===")
}

// probeUsersCurrent calls GET /api/v0/users/current with the captured cookies
// and prints whether it yields a usable bearer token (debug helper).
func probeUsersCurrent(cookies []http.Cookie) {
	req, _ := http.NewRequest("GET", "https://chat.deepseek.com/api/v0/users/current", nil)
	for _, c := range cookies {
		req.AddCookie(&c)
	}
	req.Header.Set("user-agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("origin", "https://chat.deepseek.com")
	req.Header.Set("referer", "https://chat.deepseek.com/")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("users/current GAGAL:", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("users/current: HTTP %d, body=%s\n", resp.StatusCode, string(body))
	// Cari token tersembunyi di body (masked)
	var env struct {
		Data map[string]any `json:"data"`
	}
	if json.Unmarshal(body, &env) == nil {
		keys := []string{"token", "access_token", "jwt", "auth_token"}
		for _, k := range keys {
			if v, ok := env.Data[k]; ok {
				s := fmt.Sprintf("%v", v)
				if len(s) > 20 {
					fmt.Printf("users/current data.%s = %s... (len=%d)\n", k, s[:20], len(s))
				}
			}
		}
	}
	// dump all top-level data keys
	if env.Data != nil {
		var ks []string
		for k := range env.Data {
			ks = append(ks, k)
		}
		fmt.Println("users/current data keys:", ks)
	}
}
