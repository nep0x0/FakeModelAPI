package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"fakemodelapi/internal/errs"
)

func TestAuthMiddlewareRejectsWithoutToken(t *testing.T) {
	srv := New("test-dummy", 0, WithToken("rahasia123"))
	handler := srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "http://localhost/v1/models", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("tanpa token: status = %d, want 401", rec.Code)
	}
	var body struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error.Type != "authentication_error" {
		t.Fatalf("type = %q, want authentication_error", body.Error.Type)
	}
}

func TestAuthMiddlewareAcceptsValidToken(t *testing.T) {
	srv := New("test-dummy", 0, WithToken("rahasia123"))
	handler := srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "http://localhost/v1/models", nil)
	req.Header.Set("authorization", "Bearer rahasia123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("dengan token valid: status = %d, want 200", rec.Code)
	}
}

func TestAuthMiddlewareSkippedWithoutConfig(t *testing.T) {
	srv := New("test-dummy", 0)
	handler := srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "http://localhost/v1/models", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tanpa token terkonfigurasi: status = %d, want 200", rec.Code)
	}
}

func TestBodyLimitMiddleware(t *testing.T) {
	handler := bodyLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf[:n])
	}))

	big := strings.Repeat("x", maxBodyBytes+1024)
	req := httptest.NewRequest("POST", "http://localhost/v1/chat/completions", strings.NewReader(big))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (limit diterapkan saat baca)", rec.Code)
	}
}

func TestRecoverMiddleware(t *testing.T) {
	handler := recoverMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("booom")
	}))
	req := httptest.NewRequest("GET", "http://localhost/v1/models", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != errs.HTTPStatus(errs.KindInternal) {
		t.Fatalf("panic: status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "panic: booom") {
		t.Fatalf("detail panic tidak ada: %s", rec.Body.String())
	}
}

func TestRequestIDMiddleware(t *testing.T) {
	var got string
	handler := requestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = requestID(r)
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "http://localhost/v1/models", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got == "" {
		t.Fatal("request id kosong di context")
	}
	if rec.Header().Get("x-request-id") == "" {
		t.Fatal("header x-request-id kosong")
	}
}
