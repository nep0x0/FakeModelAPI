package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"fakemodelapi/internal/provider"
)

// DefaultPort adalah port server API lokal.
const DefaultPort = 8000

// Server adalah HTTP server lokal yang menerima request OpenAI-format dan
// meneruskannya ke provider yang terdaftar di registry.
type Server struct {
	mu           sync.RWMutex
	running      bool
	httpSrv      *http.Server
	listener     net.Listener
	providerName string
	port         int
}

// New membuat Server untuk satu provider (di-resolve dari registry per
// request, jadi ganti provider di TUI tidak mempengaruhi server yang berjalan).
func New(providerName string, port int) *Server {
	return &Server{
		providerName: providerName,
		port:         port,
	}
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
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)

	// Hanya bind ke localhost: server mengeksekusi tool (bash, edit file),
	// jadi tidak boleh bisa diakses dari jaringan.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", s.port))
	if err != nil {
		return fmt.Errorf("tidak bisa listen port %d: %w", s.port, err)
	}

	s.httpSrv = &http.Server{Handler: mux}
	s.listener = ln
	s.running = true

	go func() {
		if err := s.httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
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
