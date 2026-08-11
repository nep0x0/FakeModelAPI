package errs

import (
	"errors"
	"testing"
)

func TestKindMapping(t *testing.T) {
	cases := []struct {
		kind   Kind
		status int
		typ    string
	}{
		{KindUnauthorized, 401, "authentication_error"},
		{KindSessionExpired, 401, "authentication_error"},
		{KindRateLimited, 429, "rate_limit_error"},
		{KindTimeout, 504, "api_error"},
		{KindProviderUnavailable, 502, "api_error"},
		{KindInvalidResponse, 502, "api_error"},
		{KindRequestTooLarge, 413, "invalid_request_error"},
		{KindInvalidRequest, 400, "invalid_request_error"},
		{KindUnsupportedFeature, 400, "invalid_request_error"},
		{KindInternal, 500, "internal_error"},
	}
	for _, c := range cases {
		if got := HTTPStatus(c.kind); got != c.status {
			t.Fatalf("HTTPStatus(%s) = %d, want %d", c.kind, got, c.status)
		}
		if got := OpenAIType(c.kind); got != c.typ {
			t.Fatalf("OpenAIType(%s) = %q, want %q", c.kind, got, c.typ)
		}
	}
}

func TestIsAndWrap(t *testing.T) {
	inner := New(KindRateLimited, "limit", "tunggu")
	if !Is(inner, KindRateLimited) {
		t.Fatal("Is tidak mendeteksi kind langsung")
	}
	wrapped := Wrap(KindProviderUnavailable, inner, "periksa koneksi")
	if !Is(wrapped, KindRateLimited) {
		t.Fatal("Is tidak menelusuri rantai unwrap")
	}
	if !Is(wrapped, KindProviderUnavailable) {
		t.Fatal("Is tidak mendeteksi kind pembungkus")
	}
	if Is(errors.New("biasa"), KindTimeout) {
		t.Fatal("error biasa tidak boleh terdeteksi sebagai errs")
	}
	if ActionOf(wrapped) != "periksa koneksi" {
		t.Fatalf("ActionOf = %q", ActionOf(wrapped))
	}
}

func TestFormat(t *testing.T) {
	e := New(KindSessionExpired, "session habis", "jalankan /login")
	out := Format(e)
	if out != "session habis\n-> jalankan /login" {
		t.Fatalf("Format = %q", out)
	}
	if Format(errors.New("polos")) != "polos" {
		t.Fatal("Format error polos harus sama")
	}
}
