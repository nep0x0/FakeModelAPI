// Package config berisi konfigurasi runtime minimal FakeModelAPI:
// nilai bawaan aman + override dari env var FAKEAPI_*, file
// ~/.fakeapi/config.json, dan flag CLI (env menimpa file).
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Config adalah konfigurasi runtime minimal.
type Config struct {
	Port     int
	Provider string
	Token    string // auth token lokal opsional (kosong = tanpa auth)
	Timeout  time.Duration
}

// Default mengembalikan konfigurasi dengan nilai bawaan yang aman.
func Default() Config {
	return Config{
		Port:     8000,
		Provider: "deepseek",
		Timeout:  120 * time.Second,
	}
}

// FromEnv menimpa default dari variabel lingkungan FAKEAPI_PORT,
// FAKEAPI_PROVIDER, FAKEAPI_TOKEN, dan FAKEAPI_TIMEOUT.
func FromEnv() Config {
	c := Default()
	if v := os.Getenv("FAKEAPI_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n < 65536 {
			c.Port = n
		}
	}
	if v := os.Getenv("FAKEAPI_PROVIDER"); v != "" {
		c.Provider = v
	}
	if v := os.Getenv("FAKEAPI_TOKEN"); v != "" {
		c.Token = v
	}
	if v := os.Getenv("FAKEAPI_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			c.Timeout = d
		}
	}
	return c
}

// DefaultPath mengembalikan lokasi file konfigurasi user.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".fakeapi", "config.json")
}

// Load membaca konfigurasi dari file JSON. Field yang hilang diisi default.
func Load(path string) (Config, error) {
	c := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return c, fmt.Errorf("gagal baca konfigurasi: %w", err)
	}
	var raw struct {
		Port     int    `json:"port"`
		Provider string `json:"provider"`
		Token    string `json:"token"`
		Timeout  string `json:"timeout"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return c, fmt.Errorf("konfigurasi tidak valid: %w", err)
	}
	if raw.Port > 0 {
		c.Port = raw.Port
	}
	if raw.Provider != "" {
		c.Provider = raw.Provider
	}
	if raw.Token != "" {
		c.Token = raw.Token
	}
	if raw.Timeout != "" {
		if d, err := time.ParseDuration(raw.Timeout); err == nil && d > 0 {
			c.Timeout = d
		}
	}
	return c, nil
}

// Save menulis konfigurasi ke file JSON (mode 0600).
func Save(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("tidak bisa buat direktori konfigurasi: %w", err)
	}
	raw := struct {
		Port     int    `json:"port"`
		Provider string `json:"provider"`
		Token    string `json:"token"`
		Timeout  string `json:"timeout"`
	}{
		Port:     c.Port,
		Provider: c.Provider,
		Token:    c.Token,
		Timeout:  c.Timeout.String(),
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("gagal tulis konfigurasi: %w", err)
	}
	return nil
}
