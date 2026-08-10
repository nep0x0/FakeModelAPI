package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Session menyimpan cookies dan metadata untuk satu provider.
type Session struct {
	Provider string        `json:"provider"`
	Token    string        `json:"token,omitempty"`
	Cookies  []http.Cookie `json:"cookies"`
	SavedAt  time.Time     `json:"saved_at"`
}

func sessionDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("tidak bisa temukan home dir: %w", err)
	}
	dir := filepath.Join(home, ".fakeapi", "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("tidak bisa buat direktori sessions: %w", err)
	}
	return dir, nil
}

func sessionPath(provider string) (string, error) {
	dir, err := sessionDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, provider+".json"), nil
}

// SaveSession menyimpan session ke disk.
func SaveSession(provider string, cookies []http.Cookie) error {
	return SaveSessionWithToken(provider, "", cookies)
}

// SaveSessionWithToken menyimpan session (cookies + bearer token) ke disk.
func SaveSessionWithToken(provider, token string, cookies []http.Cookie) error {
	path, err := sessionPath(provider)
	if err != nil {
		return err
	}
	s := Session{
		Provider: provider,
		Token:    token,
		Cookies:  cookies,
		SavedAt:  time.Now(),
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("gagal marshal session: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("gagal tulis session: %w", err)
	}
	return nil
}

// LoadSession memuat session dari disk.
func LoadSession(provider string) (*Session, error) {
	path, err := sessionPath(provider)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("belum login untuk %s, gunakan /login dulu", provider)
		}
		return nil, fmt.Errorf("gagal baca session: %w", err)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("gagal unmarshal session: %w", err)
	}
	return &s, nil
}

// ClearSession menghapus session dari disk.
func ClearSession(provider string) error {
	path, err := sessionPath(provider)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("gagal hapus session: %w", err)
	}
	return nil
}

// IsExpired mengecek apakah session sudah kedaluwarsa.
func (s *Session) IsExpired() bool {
	for _, c := range s.Cookies {
		if !c.Expires.IsZero() && time.Now().After(c.Expires) {
			continue
		}
		// Jika ada cookie yang belum expired, session masih valid.
		return false
	}
	return true
}
