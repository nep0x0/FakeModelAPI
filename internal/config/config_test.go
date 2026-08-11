package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	c := Default()
	c.Port = 9000
	c.Provider = "dummy"
	c.Token = "rahasia"
	c.Timeout = 5 * time.Minute

	if err := Save(path, c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != c {
		t.Fatalf("roundtrip: got %+v, want %+v", got, c)
	}
}

func TestLoadMissingFileReturnsDefault(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "tidak-ada.json"))
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if got != Default() {
		t.Fatalf("missing file harus default, got %+v", got)
	}
}

func TestLoadInvalidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{bukan json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("file rusak harus error")
	}
}

func TestSaveCreatesDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "config.json")
	if err := Save(path, Default()); err != nil {
		t.Fatalf("Save harus buat direktori: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file tidak ada: %v", err)
	}
}
