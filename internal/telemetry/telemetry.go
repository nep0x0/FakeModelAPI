// Package telemetry menyediakan observability dasar: logger request dan
// activity log ring buffer yang dipakai status panel TUI serta status --json.
package telemetry

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"
)

// Event adalah satu entri activity log.
type Event struct {
	Time    time.Time
	Kind    string // "request" | "error" | "server" | "login"
	Summary string
	Err     string
}

// ActivityLog adalah ring buffer event terbaru, aman untuk concurrent access.
type ActivityLog struct {
	mu     sync.Mutex
	events []Event
	max    int
}

// NewActivityLog membuat ring buffer dengan kapasitas max event.
func NewActivityLog(max int) *ActivityLog {
	if max <= 0 {
		max = 50
	}
	return &ActivityLog{max: max}
}

// Add mencatat satu event. Err diubah menjadi teks agar tidak membawa
// referensi error yang bisa bocor antar goroutine.
func (l *ActivityLog) Add(kind, summary string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := Event{Time: time.Now(), Kind: kind, Summary: summary}
	if err != nil {
		e.Err = err.Error()
	}
	l.events = append(l.events, e)
	if len(l.events) > l.max {
		l.events = l.events[len(l.events)-l.max:]
	}
}

// Events mengembalikan salinan seluruh event (terbaru di akhir).
func (l *ActivityLog) Events() []Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Event, len(l.events))
	copy(out, l.events)
	return out
}

// Last mengembalikan event terakhir, nil jika belum ada.
func (l *ActivityLog) Last() *Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.events) == 0 {
		return nil
	}
	e := l.events[len(l.events)-1]
	return &e
}

// LastError mengembalikan event error terbaru, nil jika tidak ada.
func (l *ActivityLog) LastError() *Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.events) - 1; i >= 0; i-- {
		if l.events[i].Err != "" {
			e := l.events[i]
			return &e
		}
	}
	return nil
}

// Request adalah satu baris observability untuk satu request HTTP.
type Request struct {
	ID         string
	Method     string
	Path       string
	Status     int
	Latency    time.Duration
	FirstToken time.Duration // > 0 hanya untuk stream yang sudah dapat token pertama
	Provider   string
	Model      string
	Err        string
}

// Logger menulis log observability ke stdout.
type Logger struct {
	l *slog.Logger
}

// NewLogger membuat logger yang menulis ke stdout.
func NewLogger() *Logger {
	return NewLoggerTo(os.Stdout)
}

// NewLoggerTo membuat logger yang menulis ke writer tertentu. Dipakai TUI
// untuk membuang log request (io.Discard) agar layar bubbletea tidak rusak;
// aktivitas tetap terekam di ActivityLog (lihat /logs).
func NewLoggerTo(w io.Writer) *Logger {
	return &Logger{l: slog.New(slog.NewTextHandler(w, nil))}
}

// Request mencatat satu request: id, status, latency, provider, model.
func (lg *Logger) Request(r Request) {
	attrs := []any{
		"id", r.ID,
		"method", r.Method,
		"path", r.Path,
		"status", r.Status,
		"latency_ms", r.Latency.Milliseconds(),
	}
	if r.FirstToken > 0 {
		attrs = append(attrs, "first_token_ms", r.FirstToken.Milliseconds())
	}
	if r.Provider != "" {
		attrs = append(attrs, "provider", r.Provider)
	}
	if r.Model != "" {
		attrs = append(attrs, "model", r.Model)
	}
	if r.Err != "" {
		attrs = append(attrs, "error", r.Err)
		lg.l.Error("request", attrs...)
		return
	}
	lg.l.Info("request", attrs...)
}

// Debug menulis pesan debug bebas.
func (lg *Logger) Debug(format string, args ...any) {
	lg.l.Debug(fmt.Sprintf(format, args...))
}
