// Package errs berisi taxonomy error terpusat untuk FakeModelAPI.
// Semua error dari provider/gateway dipetakan ke kategori stabil di sini
// supaya server bisa mengembalikan respons error OpenAI-compatible yang
// konsisten, dan TUI/CLI bisa menampilkan pesan yang actionable.
package errs

import (
	"errors"
	"fmt"
)

// Kind adalah kategori stabil sebuah error.
type Kind string

const (
	KindUnauthorized        Kind = "unauthorized"
	KindSessionExpired      Kind = "session_expired"
	KindRateLimited         Kind = "rate_limited"
	KindProviderUnavailable Kind = "provider_unavailable"
	KindInvalidRequest      Kind = "invalid_request"
	KindInvalidResponse     Kind = "invalid_response"
	KindUnsupportedFeature  Kind = "unsupported_feature"
	KindTimeout             Kind = "timeout"
	KindRequestTooLarge     Kind = "request_too_large"
	KindInternal            Kind = "internal"
)

// Error adalah error bertipe gateway: kategori + pesan + saran aksi untuk
// user. Field Err membawa error asli (untuk debug) tanpa membocorkan
// ke respons HTTP.
type Error struct {
	Kind   Kind
	Msg    string
	Action string // saran tindakan user (dipakai TUI/CLI)
	Err    error
}

func (e *Error) Error() string {
	if e.Msg == "" {
		if e.Err != nil {
			return e.Err.Error()
		}
		return string(e.Kind)
	}
	if e.Err != nil {
		return e.Msg + ": " + e.Err.Error()
	}
	return e.Msg
}

func (e *Error) Unwrap() error { return e.Err }

// New membuat error bertipe dengan pesan dan saran aksi.
func New(kind Kind, msg, action string) *Error {
	return &Error{Kind: kind, Msg: msg, Action: action}
}

// Wrap membungkus error asli dengan kategori dan saran aksi.
func Wrap(kind Kind, err error, action string) *Error {
	return &Error{Kind: kind, Msg: err.Error(), Action: action, Err: err}
}

// Is mengembalikan true jika err (atau rantai unwrap-nya) mengandung
// kategori kind di lapisan mana pun.
func Is(err error, kind Kind) bool {
	for err != nil {
		var e *Error
		if errors.As(err, &e) && e.Kind == kind {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

// KindOf mengembalikan kategori err (KindInternal jika tidak bertipe).
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return KindInternal
}

// HTTPStatus memetakan kategori ke status HTTP respons OpenAI-compatible.
func HTTPStatus(kind Kind) int {
	switch kind {
	case KindUnauthorized, KindSessionExpired:
		return httpStatusUnauthorized
	case KindRateLimited:
		return httpStatusTooManyRequests
	case KindTimeout:
		return httpStatusGatewayTimeout
	case KindProviderUnavailable, KindInvalidResponse:
		return httpStatusBadGateway
	case KindRequestTooLarge:
		return httpStatusRequestTooLarge
	case KindInvalidRequest, KindUnsupportedFeature:
		return httpStatusBadRequest
	default:
		return httpStatusInternalServerError
	}
}

const (
	httpStatusBadRequest          = 400
	httpStatusUnauthorized        = 401
	httpStatusRequestTooLarge     = 413
	httpStatusTooManyRequests     = 429
	httpStatusInternalServerError = 500
	httpStatusBadGateway          = 502
	httpStatusGatewayTimeout      = 504
)

// OpenAIType memetakan kategori ke field "type" pada schema error OpenAI.
func OpenAIType(kind Kind) string {
	switch kind {
	case KindUnauthorized, KindSessionExpired:
		return "authentication_error"
	case KindRateLimited:
		return "rate_limit_error"
	case KindTimeout, KindProviderUnavailable, KindInvalidResponse:
		return "api_error"
	case KindRequestTooLarge, KindInvalidRequest, KindUnsupportedFeature:
		return "invalid_request_error"
	default:
		return "internal_error"
	}
}

// ActionOf mengembalikan saran aksi dari err (kosong jika tidak bertipe).
func ActionOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Action
	}
	return ""
}

// Format menyusun pesan actionable untuk ditampilkan ke user:
// apa yang terjadi + saran perbaikan.
func Format(err error) string {
	var e *Error
	if errors.As(err, &e) {
		if e.Action != "" {
			return fmt.Sprintf("%s\n-> %s", e.Msg, e.Action)
		}
		return e.Msg
	}
	return err.Error()
}
