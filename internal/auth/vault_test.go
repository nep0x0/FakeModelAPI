package auth

import (
	"net/http"
	"testing"
	"time"
)

func TestInMemoryVault(t *testing.T) {
	v := NewInMemoryVault()

	// Belum ada session -> ErrNoSession.
	if _, err := v.Load("deepseek"); !IsNoSessionError(err) {
		t.Fatalf("Load tanpa session harus ErrNoSession, got %v", err)
	}
	if _, err := v.Status("deepseek"); !IsNoSessionError(err) {
		t.Fatalf("Status tanpa session harus ErrNoSession, got %v", err)
	}

	if err := v.Save("deepseek", Session{
		Token: "tok-123",
		Cookies: []http.Cookie{{
			Name: "sessionid", Value: "abc",
			Expires: time.Now().Add(5 * time.Hour),
		}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	st, err := v.Status("deepseek")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Exists || st.TokenLen != 7 || st.Expired {
		t.Fatalf("Status salah: %+v", st)
	}
	if st.ExpiresAt.IsZero() {
		t.Fatal("ExpiresAt harus terisi dari cookie")
	}

	sess, err := v.Load("deepseek")
	if err != nil || sess.Token != "tok-123" {
		t.Fatalf("Load salah: %+v, %v", sess, err)
	}

	if err := v.Delete("deepseek"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := v.Load("deepseek"); !IsNoSessionError(err) {
		t.Fatal("session harus hilang setelah Delete")
	}
}

func TestSessionIsExpired(t *testing.T) {
	sess := Session{Cookies: []http.Cookie{{Name: "a", Value: "1", Expires: time.Now().Add(-1 * time.Hour)}}}
	if !sess.IsExpired() {
		t.Fatal("semua cookie expired harus menghasilkan IsExpired=true")
	}
	sess.Cookies = append(sess.Cookies, http.Cookie{Name: "b", Value: "2", Expires: time.Now().Add(1 * time.Hour)})
	if sess.IsExpired() {
		t.Fatal("satu cookie valid harus menghasilkan IsExpired=false")
	}
	sess.Cookies = []http.Cookie{{Name: "c", Value: "3"}}
	if sess.IsExpired() {
		t.Fatal("cookie tanpa expiry dianggap valid")
	}
	// Sentinel epoch 1970 (cookie session) juga harus dianggap valid.
	sess.Cookies = []http.Cookie{{Name: "d", Value: "4", Expires: time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)}}
	if sess.IsExpired() {
		t.Fatal("cookie dengan sentinel epoch dianggap valid")
	}
}

func TestEarliestExpirySkipsEpochSentinel(t *testing.T) {
	real := time.Now().Add(5 * time.Hour)
	st := SessionStatus{}
	st.ExpiresAt = earliestExpiry([]http.Cookie{
		{Name: "sess", Value: "1", Expires: time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Name: "real", Value: "2", Expires: real},
	})
	if st.ExpiresAt != real {
		t.Fatalf("earliestExpiry = %v, want %v", st.ExpiresAt, real)
	}
}

func TestSetVault(t *testing.T) {
	v := NewInMemoryVault()
	SetVault(v)
	defer SetVault(NewFileVault())

	if err := SaveSessionWithToken("deepseek", "tok", nil); err != nil {
		t.Fatalf("SaveSessionWithToken: %v", err)
	}
	sess, err := LoadSession("deepseek")
	if err != nil || sess.Token != "tok" {
		t.Fatalf("LoadSession: %+v, %v", sess, err)
	}
	if err := ClearSession("deepseek"); err != nil {
		t.Fatalf("ClearSession: %v", err)
	}
	st, err := SessionStatusOf("deepseek")
	if err != nil {
		t.Fatalf("SessionStatusOf harus tidak error tanpa session: %v", err)
	}
	if st.Exists {
		t.Fatal("session harus sudah hilang")
	}
}
