package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"fakemodelapi/internal/errs"
	"fakemodelapi/internal/telemetry"
)

// maxBodyBytes membatasi ukuran body request (10 MB).
const maxBodyBytes = 10 << 20

type ctxKey int

const ctxKeyRequestID ctxKey = 0

func newRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// requestID mengambil request id dari context.
func requestID(r *http.Request) string {
	if v, ok := r.Context().Value(ctxKeyRequestID).(string); ok {
		return v
	}
	return ""
}

// statusRecorder menangkap status code yang ditulis handler untuk logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush meneruskan flush agar streaming SSE tetap bekerja.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// requestIDMiddleware menambahkan id unik per request (header X-Request-Id)
// supaya setiap request bisa dilacak di log.
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set("x-request-id", id)
		ctx := context.WithValue(r.Context(), ctxKeyRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// recoverMiddleware mengubah panic handler menjadi 500 alih-alih mematikan
// server.
func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				writeErrorKind(w, errs.KindInternal, "internal server error", fmt.Sprintf("panic: %v", rec), nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware mencatat setiap request ke Logger + ActivityLog.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		latency := time.Since(start)
		s.logger.Request(telemetry.Request{
			ID:       requestID(r),
			Method:   r.Method,
			Path:     r.URL.Path,
			Status:   rec.status,
			Latency:  latency,
			Provider: s.providerName,
		})
		summary := fmt.Sprintf("%s %s -> %d (%s)", r.Method, r.URL.Path, rec.status, latency.Round(time.Millisecond))
		if rec.status >= 400 {
			s.activity.Add("error", summary, nil)
		} else {
			s.activity.Add("request", summary, nil)
		}
	})
}

// authMiddleware memberlakukan token lokal bila di-set (FAKEAPI_TOKEN /
// --token). Jika token kosong, akses cukup dibatasi loopback.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.token != "" {
			auth := r.Header.Get("authorization")
			token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
			if auth == "" || token != s.token {
				writeErrorKind(w, errs.KindUnauthorized, "token lokal tidak valid", "Set header Authorization: Bearer <token> sesuai nilai FAKEAPI_TOKEN / --token yang dipakai server.", nil)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// bodyLimitMiddleware membatasi ukuran body request.
func bodyLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// timeoutMiddleware membatasi durasi seluruh handler.
func timeoutMiddleware(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if d <= 0 {
				next.ServeHTTP(w, r)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
