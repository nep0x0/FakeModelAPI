package telemetry

import (
	"errors"
	"testing"
)

func TestActivityLog(t *testing.T) {
	l := NewActivityLog(3)
	l.Add("request", "ok", nil)
	l.Add("error", "gagal", errors.New("boom"))
	l.Add("request", "ok2", nil)
	l.Add("request", "ok3", nil)

	events := l.Events()
	if len(events) != 3 {
		t.Fatalf("len(events) = %d, want 3 (ring buffer)", len(events))
	}
	if events[0].Summary != "gagal" {
		t.Fatalf("event tertua salah: %+v", events[0])
	}
	last := l.Last()
	if last == nil || last.Summary != "ok3" {
		t.Fatalf("Last() salah: %+v", last)
	}
	lastErr := l.LastError()
	if lastErr == nil || lastErr.Summary != "gagal" || lastErr.Err != "boom" {
		t.Fatalf("LastError() salah: %+v", lastErr)
	}
}

func TestActivityLogEmpty(t *testing.T) {
	l := NewActivityLog(10)
	if l.Last() != nil || l.LastError() != nil {
		t.Fatal("log kosong harus mengembalikan nil")
	}
	if len(l.Events()) != 0 {
		t.Fatal("log kosong harus tanpa event")
	}
}
