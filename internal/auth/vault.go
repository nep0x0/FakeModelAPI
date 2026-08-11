package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ErrNoSession adalah sentinel untuk session yang belum tersimpan.
var ErrNoSession = errors.New("belum login")

// IsNoSessionError mengecek apakah error berarti session belum ada.
func IsNoSessionError(err error) bool {
	return errors.Is(err, ErrNoSession)
}

// Vault adalah abstraksi penyimpanan session per provider. Implementasi
// bisa berupa file encrypted, OS keychain, atau memory-only — core tidak
// bergantung pada detail penyimpanan.
type Vault interface {
	Save(provider string, s Session) error
	Load(provider string) (*Session, error)
	Delete(provider string) error
	Status(provider string) (SessionStatus, error)
}

// SessionStatus adalah ringkasan status session untuk status panel & doctor.
type SessionStatus struct {
	Exists    bool
	Expired   bool
	SavedAt   time.Time
	ExpiresAt time.Time // cookie tercepat yang expired; zero = tidak diketahui
	TokenLen  int
}

// FileVault menyimpan session sebagai file JSON per provider di
// ~/.fakeapi/sessions/<provider>.json (mode 0600).
type FileVault struct{}

// NewFileVault membuat FileVault.
func NewFileVault() *FileVault { return &FileVault{} }

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

// Save menyimpan session ke disk.
func (FileVault) Save(provider string, s Session) error {
	path, err := sessionPath(provider)
	if err != nil {
		return err
	}
	s.Provider = provider
	s.SavedAt = time.Now()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("gagal marshal session: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("gagal tulis session: %w", err)
	}
	return nil
}

// Load memuat session dari disk.
func (FileVault) Load(provider string) (*Session, error) {
	path, err := sessionPath(provider)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w untuk %s, gunakan /login dulu", ErrNoSession, provider)
		}
		return nil, fmt.Errorf("gagal baca session: %w", err)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("gagal unmarshal session: %w", err)
	}
	return &s, nil
}

// Delete menghapus session dari disk.
func (FileVault) Delete(provider string) error {
	path, err := sessionPath(provider)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("gagal hapus session: %w", err)
	}
	return nil
}

// Status mengecek keberadaan dan kedaluwarsa session tanpa memuat penuh.
func (v FileVault) Status(provider string) (SessionStatus, error) {
	sess, err := v.Load(provider)
	if err != nil {
		return SessionStatus{}, err
	}
	st := SessionStatus{
		Exists:   true,
		SavedAt:  sess.SavedAt,
		TokenLen: len(sess.Token),
	}
	if st.TokenLen == 0 && len(sess.Cookies) == 0 {
		return st, nil
	}
	st.ExpiresAt = earliestExpiry(sess.Cookies)
	st.Expired = sess.IsExpired()
	return st, nil
}

// earliestExpiry mengembalikan waktu cookie yang paling cepat expired.
// Cookie tanpa expiry nyata (sentinel epoch 1970) diabaikan.
func earliestExpiry(cookies []http.Cookie) time.Time {
	var earliest time.Time
	for _, c := range cookies {
		if !hasRealExpiry(c.Expires) {
			continue
		}
		if earliest.IsZero() || c.Expires.Before(earliest) {
			earliest = c.Expires
		}
	}
	return earliest
}

// InMemoryVault menyimpan session di memori — dipakai untuk tes.
type InMemoryVault struct {
	sessions map[string]Session
}

// NewInMemoryVault membuat vault memory-only.
func NewInMemoryVault() *InMemoryVault {
	return &InMemoryVault{sessions: make(map[string]Session)}
}

func (v *InMemoryVault) Save(provider string, s Session) error {
	s.Provider = provider
	s.SavedAt = time.Now()
	v.sessions[provider] = s
	return nil
}

func (v *InMemoryVault) Load(provider string) (*Session, error) {
	s, ok := v.sessions[provider]
	if !ok {
		return nil, fmt.Errorf("%w untuk %s, gunakan /login dulu", ErrNoSession, provider)
	}
	return &s, nil
}

func (v *InMemoryVault) Delete(provider string) error {
	delete(v.sessions, provider)
	return nil
}

func (v *InMemoryVault) Status(provider string) (SessionStatus, error) {
	sess, err := v.Load(provider)
	if err != nil {
		return SessionStatus{}, err
	}
	st := SessionStatus{Exists: true, SavedAt: sess.SavedAt, TokenLen: len(sess.Token)}
	if st.TokenLen == 0 && len(sess.Cookies) == 0 {
		return st, nil
	}
	st.ExpiresAt = earliestExpiry(sess.Cookies)
	st.Expired = sess.IsExpired()
	return st, nil
}
