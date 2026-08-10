package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// CaptureResult holds the result of a browser login attempt.
type CaptureResult struct {
	Cookies []http.Cookie
	Token   string // bearer token from localStorage, if requested
	Error   error
}

// CaptureSessionInfo describes what to capture for one provider.
type CaptureSessionInfo struct {
	URL             string // login page (e.g. "https://chat.deepseek.com")
	Domain          string // cookie domain filter (e.g. ".deepseek.com")
	TokenStorageKey string // localStorage key holding the bearer token (e.g. "userToken")
}

// CaptureCookies membuka browser, menunggu user login, lalu menangkap cookies.
// url: URL halaman login (misal "https://chat.deepseek.com")
// domain: domain cookies yang akan ditangkap (misal ".deepseek.com")
// timeout: maksimum waktu menunggu user login
// progress: channel untuk mengirim status ke TUI (opsional, bisa nil)
func CaptureCookies(url, domain string, timeout time.Duration, progress chan<- string) *CaptureResult {
	return capture(url, domain, "", timeout, progress)
}

// CaptureSession membuka browser, menunggu user login, lalu menangkap cookies
// sekaligus token dari localStorage (key: localStorageKey).
// Token ini biasanya adalah Bearer token yang dipakai request API (misal
// DeepSeek "userToken").
func CaptureSession(info CaptureSessionInfo, timeout time.Duration, progress chan<- string) *CaptureResult {
	return capture(info.URL, info.Domain, info.TokenStorageKey, timeout, progress)
}

func capture(url, domain, tokenKey string, timeout time.Duration, progress chan<- string) *CaptureResult {
	if progress != nil {
		defer close(progress)
	}

	sendProgress(progress, "meluncurkan browser...")

	pw, err := playwright.Run()
	if err != nil {
		return &CaptureResult{Error: fmt.Errorf("gagal menjalankan playwright: %w", err)}
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(false),
	})
	if err != nil {
		return &CaptureResult{Error: fmt.Errorf("gagal meluncurkan browser: %w", err)}
	}
	defer browser.Close()

	browserCtx, err := browser.NewContext()
	if err != nil {
		return &CaptureResult{Error: fmt.Errorf("gagal membuat browser context: %w", err)}
	}
	defer browserCtx.Close()

	page, err := browserCtx.NewPage()
	if err != nil {
		return &CaptureResult{Error: fmt.Errorf("gagal membuka halaman: %w", err)}
	}

	if os.Getenv("FAKEAPI_DEBUG_DUMP") != "" {
		// Pantau request app web ke API untuk melihat header auth asli
		page.On("request", func(req playwright.Request) {
			u := req.URL()
			if !strings.Contains(u, "/api/v0/") {
				return
			}
			auth := req.Headers()["authorization"]
			if len(auth) > 40 {
				auth = auth[:40] + "..."
			}
			fmt.Printf("[debug] REQ %s\n[debug]   auth=%q\n", u, auth)
		})
	}

	sendProgress(progress, fmt.Sprintf("membuka %s...", url))

	if _, err := page.Goto(url); err != nil {
		return &CaptureResult{Error: fmt.Errorf("gagal navigasi ke %s: %w", url, err)}
	}

	sendProgress(progress, "silahkan login di browser...")

	// Tunggu sampai URL berubah dari halaman login (user sudah login)
	// Untuk DeepSeek, setelah login redirect ke /chat
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err = waitForLogin(ctx, page, url, tokenKey)
	if err != nil {
		return &CaptureResult{Error: fmt.Errorf("waktu login habis: %w", err)}
	}

	sendProgress(progress, "login terdeteksi, mengambil cookies...")

	var token string
	if tokenKey != "" {
		v, evalErr := page.Evaluate(fmt.Sprintf("() => localStorage.getItem('%s') || ''", tokenKey))
		if evalErr == nil {
			if s, ok := v.(string); ok {
				token = unwrapAppKitValue(s)
			}
		}
	}

	if os.Getenv("FAKEAPI_DEBUG_DUMP") != "" {
		// beri waktu app web menyelesaikan inisialisasi token
		time.Sleep(3 * time.Second)
		url := page.URL()
		title, _ := page.Title()
		fmt.Printf("[debug] URL=%s title=%q\n", url, title)
		if _, err := page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String("/tmp/opencode/debug_screen.png")}); err == nil {
			fmt.Println("[debug] screenshot -> /tmp/opencode/debug_screen.png")
		}
		dumpBrowserState(page)
		// Hipotesis: userToken di-set lazy saat pesan pertama dikirim.
		// Simulasi mengetik di web UI untuk memicu inisialisasi.
		fmt.Println("[debug] mencoba mengetik di web UI untuk memicu init token...")
		typed := false
		for _, sel := range []string{"textarea", "[contenteditable='true']", ".ds-input", "input[type='text']"} {
			loc := page.Locator(sel)
			if n, err := loc.Count(); err == nil && n > 0 {
				loc.First().Click()
				page.Keyboard().Type("hi")
				page.Keyboard().Press("Enter")
				typed = true
				break
			}
		}
		if !typed {
			page.Keyboard().Type("hi")
			page.Keyboard().Press("Enter")
		}
		time.Sleep(8 * time.Second)
		fmt.Println("[debug] state setelah ketik:")
		dumpBrowserState(page)
	}

	cookies, err := browserCtx.Cookies()
	if err != nil {
		return &CaptureResult{Error: fmt.Errorf("gagal membaca cookies: %w", err)}
	}

	var result []http.Cookie
	for _, c := range cookies {
		result = append(result, http.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  time.Unix(int64(c.Expires), 0),
			HttpOnly: c.HttpOnly,
			Secure:   c.Secure,
		})
	}

	sendProgress(progress, fmt.Sprintf("berhasil menangkap %d cookies", len(result)))

	return &CaptureResult{Cookies: result, Token: token}
}

// waitForLogin menunggu sampai user selesai login dengan mendeteksi perubahan
// URL (atau token muncul di localStorage jika tokenKey diberikan).
func waitForLogin(ctx context.Context, page playwright.Page, loginURL, tokenKey string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if tokenKey != "" {
			v, err := page.Evaluate(fmt.Sprintf("() => localStorage.getItem('%s') || ''", tokenKey))
			if err == nil {
				if s, ok := v.(string); ok && unwrapAppKitValue(s) != "" {
					return nil
				}
			}
		}

		currentURL := page.URL()
		// Jika URL sudah berbeda dari halaman login (tidak mengandung kata kunci login)
		if currentURL != loginURL && currentURL != loginURL+"/" && currentURL != loginURL+"/sign_in" {
			// Tunggu sebentar untuk memastikan halaman sudah stabil
			time.Sleep(1 * time.Second)
			return nil
		}

		time.Sleep(500 * time.Millisecond)
	}
}

// dumpBrowserState prints localStorage keys and cookie names for debugging.
// Only runs when FAKEAPI_DEBUG_DUMP is set; values are masked.
func dumpBrowserState(page playwright.Page) {
	v, err := page.Evaluate("() => Object.keys(localStorage)")
	if err == nil {
		if keys, ok := v.([]any); ok {
			fmt.Println("[debug] localStorage keys:")
			for _, k := range keys {
				key := fmt.Sprintf("%v", k)
				val, _ := page.Evaluate(fmt.Sprintf("() => { const v = localStorage.getItem('%s'); return v ? v.slice(0, 60) : '' }", key))
				fmt.Printf("[debug]   %s = %v...\n", key, val)
			}
		}
	}
	all, err := page.Context().Cookies()
	if err == nil {
		fmt.Println("[debug] cookies:")
		for _, c := range all {
			fmt.Printf("[debug]   %s (domain=%s, httponly=%v)\n", c.Name, c.Domain, c.HttpOnly)
		}
	}

	// sessionStorage (token mungkin di sini)
	v, err = page.Evaluate("() => Object.keys(sessionStorage)")
	if err == nil {
		if keys, ok := v.([]any); ok {
			fmt.Println("[debug] sessionStorage keys:", keys)
			for _, k := range keys {
				key := fmt.Sprintf("%v", k)
				val, _ := page.Evaluate(fmt.Sprintf("() => { const v = sessionStorage.getItem('%s'); return v ? v.slice(0, 80) : '' }", key))
				fmt.Printf("[debug]   ss[%s] = %v\n", key, val)
			}
		}
	}

	// IndexedDB databases
	v, err = page.Evaluate("() => indexedDB.databases().then(dbs => dbs.map(d => d.name))")
	if err == nil {
		fmt.Printf("[debug] indexedDB: %v\n", v)
	}

	// Dump isi IndexedDB (token bisa disimpan di sini)
	script := `(async () => {
		const dbs = await indexedDB.databases();
		const out = [];
		for (const info of dbs) {
			const db = await new Promise((res, rej) => {
				const r = indexedDB.open(info.name);
				r.onsuccess = () => res(r.result);
				r.onerror = () => rej(r.error);
			});
			const entry = { db: info.name, stores: Array.from(db.objectStoreNames) };
			for (const storeName of entry.stores) {
				const req = db.transaction(storeName, "readonly").objectStore(storeName).getAll();
				await new Promise((res) => { req.onsuccess = res; req.onerror = res; });
				entry[storeName] = (req.result || []).map(v => {
					const s = JSON.stringify(v);
					return s ? s.slice(0, 300) : "";
				});
			}
			out.push(entry);
		}
		return JSON.stringify(out);
	})()`
	v, err = page.Evaluate(script)
	if err == nil {
		fmt.Printf("[debug] indexedDB dump: %v\n", v)
	}
}

// unwrapAppKitValue handles DeepSeek's localStorage wrapper format:
// the stored string is JSON like {"value":"actual-value","__version":"N"}.
// If parsing fails, the raw string is returned unchanged.
func unwrapAppKitValue(raw string) string {
	if raw == "" {
		return ""
	}
	var wrapped struct {
		Value any `json:"value"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapped); err == nil {
		switch v := wrapped.Value.(type) {
		case string:
			return v
		case nil:
			return ""
		}
	}
	return raw
}

func sendProgress(ch chan<- string, msg string) {
	if ch != nil {
		select {
		case ch <- msg:
		default:
		}
	}
}
