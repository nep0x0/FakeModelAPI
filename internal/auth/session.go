package auth

import (
	"net/http"
	"time"
)

// Session menyimpan cookies dan metadata untuk satu provider.
type Session struct {
	Provider string        `json:"provider"`
	Token    string        `json:"token,omitempty"`
	Cookies  []http.Cookie `json:"cookies"`
	SavedAt  time.Time     `json:"saved_at"`
}

// defaultVault adalah vault bawaan: file JSON per provider di
// ~/.fakeapi/sessions/. Core memanggilnya lewat fungsi bantuan di bawah;
// vault alternatif bisa dipasang lewat SetVault (mis. untuk tes).
var defaultVault Vault = NewFileVault()

// SetVault mengganti vault bawaan (untuk tes / mode khusus).
func SetVault(v Vault) {
	if v != nil {
		defaultVault = v
	}
}

// GetVault mengembalikan vault aktif.
func GetVault() Vault {
	return defaultVault
}

// SaveSession menyimpan session ke vault aktif.
func SaveSession(provider string, cookies []http.Cookie) error {
	return defaultVault.Save(provider, Session{Provider: provider, Cookies: cookies})
}

// SaveSessionWithToken menyimpan session (cookies + bearer token) ke vault.
func SaveSessionWithToken(provider, token string, cookies []http.Cookie) error {
	return defaultVault.Save(provider, Session{Provider: provider, Token: token, Cookies: cookies})
}

// LoadSession memuat session dari vault aktif.
func LoadSession(provider string) (*Session, error) {
	return defaultVault.Load(provider)
}

// ClearSession menghapus session dari vault aktif.
func ClearSession(provider string) error {
	return defaultVault.Delete(provider)
}

// SessionStatusOf mengambil status session dari vault aktif.
// Mengembalikan status kosong (bukan error) jika belum ada session.
func SessionStatusOf(provider string) (SessionStatus, error) {
	st, err := defaultVault.Status(provider)
	if err != nil && !IsNoSessionError(err) {
		return SessionStatus{}, err
	}
	return st, nil
}

// hasRealExpiry membedakan expiry nyata dari sentinel "session cookie":
// banyak klien menyimpan cookie tanpa Expires sebagai 1970-01-01 (epoch).
// Nilai sebelum tahun 2000 dianggap tidak punya expiry nyata.
func hasRealExpiry(t time.Time) bool {
	return t.Year() >= 2000
}

// IsExpired mengecek apakah session sudah kedaluwarsa.
func (s *Session) IsExpired() bool {
	for _, c := range s.Cookies {
		if hasRealExpiry(c.Expires) && time.Now().After(c.Expires) {
			continue
		}
		// Jika ada cookie yang belum expired (atau session cookie tanpa
		// expiry), session masih valid.
		return false
	}
	return true
}
