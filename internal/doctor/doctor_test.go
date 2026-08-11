package doctor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"fakemodelapi/internal/config"
	"fakemodelapi/internal/providers/dummy"
)

func TestCheckConfigUnknownProvider(t *testing.T) {
	c := checkConfig(config.Config{Provider: "nope", Port: 8000}, nil)
	if c.OK {
		t.Fatal("expected fail untuk provider tidak dikenal")
	}
	if !strings.Contains(c.Action, "deepseek") {
		t.Fatalf("action harus menyebut provider yang tersedia, got: %q", c.Action)
	}
}

func TestCheckConfigValid(t *testing.T) {
	c := checkConfig(config.Config{Provider: "deepseek", Port: 8000}, dummy.New())
	if !c.OK {
		t.Fatalf("expected ok, got: %+v", c)
	}
}

func TestCheckSessionNotLoggedIn(t *testing.T) {
	c := checkSession(dummy.New())
	if c.OK {
		t.Fatal("dummy tidak login — harusnya fail")
	}
	if !strings.Contains(c.Action, "/login") {
		t.Fatalf("action harus menyebut /login, got: %q", c.Action)
	}
}

func TestCheckServerUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","provider":"deepseek"}`))
	}))
	defer srv.Close()

	port := srv.URL[len("http://127.0.0.1:"):]
	c := checkServer(context.Background(), config.Config{Port: mustAtoi(t, port)})
	if !c.OK {
		t.Fatalf("expected ok, got: %+v", c)
	}
}

func TestCheckServerDown(t *testing.T) {
	c := checkServer(context.Background(), config.Config{Port: 1})
	if c.OK {
		t.Fatal("port 1 tidak ada server — harusnya fail")
	}
	if !strings.Contains(c.Action, "headless") {
		t.Fatalf("action harus menyebut fakeapi -headless, got: %q", c.Action)
	}
}

func TestRenderIncludesIcons(t *testing.T) {
	r := Result{Checks: []Check{{Name: "x", OK: true}, {Name: "y", OK: false, Action: "coba ini"}}}
	out := r.Render()
	if !strings.Contains(out, "✓") || !strings.Contains(out, "✗") {
		t.Fatalf("render harus memuat ikon status:\n%s", out)
	}
	if !strings.Contains(out, "→ coba ini") {
		t.Fatalf("action harus ditampilkan:\n%s", out)
	}
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	var n int
	for _, ch := range s {
		n = n*10 + int(ch-'0')
	}
	return n
}
