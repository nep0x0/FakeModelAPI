package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"fakemodelapi/internal/config"
	"fakemodelapi/internal/provider"
	"fakemodelapi/internal/telemetry"
)

// DefaultPort adalah port server API lokal.
const DefaultPort = 8000

// Option menyesuaikan perilaku Server saat dibuat.
type Option func(*Server)

// WithToken mengaktifkan auth token lokal untuk semua request API.
func WithToken(token string) Option {
	return func(s *Server) { s.token = token }
}

// WithTimeout mengatur batas waktu handler (default 120s).
func WithTimeout(d time.Duration) Option {
	return func(s *Server) { s.timeout = d }
}

// WithActivityLog memakai ActivityLog eksternal (mis. dibagi dengan TUI).
func WithActivityLog(a *telemetry.ActivityLog) Option {
	return func(s *Server) { s.activity = a }
}

// WithLogger memakai Logger eksternal.
func WithLogger(l *telemetry.Logger) Option {
	return func(s *Server) { s.logger = l }
}

// Server adalah HTTP server lokal yang menerima request OpenAI-format dan
// meneruskannya ke provider yang terdaftar di registry. Core gateway:
// format OpenAI + middleware — tidak bergantung pada detail satu provider.
type Server struct {
	mu           sync.RWMutex
	running      bool
	httpSrv      *http.Server
	listener     net.Listener
	providerName string
	port         int

	// Observability
	activity *telemetry.ActivityLog
	logger   *telemetry.Logger

	// Keamanan & kontrol
	token     string // auth token lokal opsional
	timeout   time.Duration
	startedAt time.Time
}

// New membuat Server untuk satu provider (di-resolve dari registry per
// request, jadi ganti provider di TUI tidak mempengaruhi server yang berjalan).
func New(providerName string, port int, opts ...Option) *Server {
	s := &Server{
		providerName: providerName,
		port:         port,
		activity:     telemetry.NewActivityLog(50),
		logger:       telemetry.NewLogger(),
		timeout:      config.Default().Timeout,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// ActivityLog mengembalikan activity log server (untuk TUI / status panel).
func (s *Server) ActivityLog() *telemetry.ActivityLog {
	return s.activity
}

// Start meluncurkan server di goroutine. Mengembalikan error jika port
// sudah dipakai atau provider tidak ada di registry.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server sudah berjalan")
	}
	if _, err := provider.Get(s.providerName); err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)

	// Middleware chain: Recover -> RequestID -> Logging -> Loopback -> Auth
	// -> BodyLimit -> Timeout -> Handler. Loopback wajib paling awal dari
	// sisi keamanan: server ini bisa mengeksekusi tool atas nama user lokal.
	var h http.Handler = mux
	h = timeoutMiddleware(s.timeout)(h)
	h = bodyLimitMiddleware(h)
	h = s.authMiddleware(h)
	h = requireLoopback(h)
	h = s.loggingMiddleware(h)
	h = recoverMiddleware(h)
	h = requestIDMiddleware(h)

	// Bind ke semua interface (IPv4 + IPv6) supaya "localhost" yang resolve
	// ke ::1 maupun 127.0.0.1 tetap bisa terhubung. Keamanan dijamin lewat
	// requireLoopback + auth token: hanya klien loopback (atau yang punya
	// token) yang boleh mengakses.
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("tidak bisa listen port %d: %w", s.port, err)
	}

	s.httpSrv = &http.Server{Handler: h}
	s.listener = ln
	s.running = true
	s.startedAt = time.Now()
	s.activity.Add("server", fmt.Sprintf("server started di port %d", s.port), nil)

	go func() {
		if err := s.httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
			s.activity.Add("error", "server crash: "+err.Error(), nil)
		}
	}()

	return nil
}

// Stop menghentikan server secara graceful (timeout 3 detik).
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := s.httpSrv.Shutdown(ctx)
	s.running = false
	s.listener = nil
	s.httpSrv = nil
	s.activity.Add("server", "server stopped", nil)
	return err
}

// Running mengembalikan true jika server sedang berjalan.
func (s *Server) Running() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// Addr mengembalikan alamat publik server, misal "localhost:8000".
func (s *Server) Addr() string {
	return fmt.Sprintf("localhost:%d", s.port)
}

// currentProvider me-resolve provider aktif dari registry.
func (s *Server) currentProvider() (provider.Provider, error) {
	return provider.Get(s.providerName)
}

// requireLoopback menolak semua request dari remote non-loopback. Server ini
// mengeksekusi tool (bash, edit file) atas nama user lokal, jadi hanya
// localhost yang boleh memakainya.
func requireLoopback(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			w.Header().Set("content-type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"message":"forbidden: hanya localhost yang diizinkan","type":"invalid_request_error","code":403}}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
