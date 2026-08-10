# FakeModelAPI

Proxy lokal yang mengubah **Web AI gratis** (DeepSeek) menjadi **API kompatibel OpenAI** untuk [OpenCode](https://opencode.ai) — tanpa API key, tanpa kartu kredit.

```
OpenCode ──HTTP──▶ FakeModelAPI (localhost:8000) ──▶ chat.deepseek.com (session akun kamu)
                    (format OpenAI ↔ format web)
```

## Prasyarat

- **Go 1.25+**
- **Akun DeepSeek** (https://chat.deepseek.com) — gratis
- **Chromium untuk Playwright** (dipakai `/login` untuk menangkap session):
  ```bash
  go run github.com/mxschmitt/playwright-go/cmd/playwright@latest install chromium
  ```
  Di Linux, tambahkan dependency sistem:
  ```bash
  go run github.com/mxschmitt/playwright-go/cmd/playwright@latest install --with-deps chromium
  ```

## Build & Jalankan

```bash
go build ./cmd/fakeapi
./fakeapi
```

Tanpa TUI (untuk background service):

```bash
./fakeapi -headless
```

## Cara Pakai

1. Jalankan `./fakeapi` → muncul TUI.
2. Ketik `/login` — browser Chromium terbuka, **login manual ke DeepSeek** di browser itu. Setelah login, cookies & token tertangkap otomatis dan disimpan di `~/.fakeapi/sessions/`.
3. Ketik `/start` — server API lokal menyala di `http://localhost:8000/v1`.
4. Ketik `/test` untuk memastikan koneksi AI berfungsi, atau `/status` untuk detail.

## Konfigurasi OpenCode

Buat/ubah `opencode.json` di proyek kamu (atau `~/.config/opencode/opencode.json`):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "fakeapi": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "FakeModelAPI (DeepSeek Web)",
      "options": {
        "baseURL": "http://localhost:8000/v1",
        "apiKey": "fake"
      },
      "models": {
        "deepseek-chat": {
          "name": "DeepSeek V3 Chat"
        }
      }
    }
  },
  "model": "fakeapi/deepseek-chat"
}
```

Lalu jalankan `opencode` dari direktori proyek tersebut — pastikan server FakeModelAPI sudah `/start`.

## Slash Commands (TUI)

| Perintah  | Fungsi                                                        |
|-----------|---------------------------------------------------------------|
| `/login`  | Buka browser, tangkap session DeepSeek otomatis               |
| `/logout` | Hapus session                                                 |
| `/start`  | Nyalakan server API lokal di `localhost:8000`                 |
| `/stop`   | Matikan server                                                |
| `/status` | Status server & login                                         |
| `/test`   | Tes koneksi AI (respon + waktu)                               |
| `/model`  | Pilih model                                                   |
| `/clear`  | Bersihkan riwayat chat TUI                                    |
| `/exit`   | Keluar (atau `/quit`, Ctrl+C)                                 |

Tombol: `Tab` ganti provider, `Ctrl+P` command palette.

## Peringatan

- Ini memakai **API web tidak resmi** (reverse-engineered) — DeepSeek bisa mengubah protokol kapan saja, dan pemakaian melanggar syarat layanan web-nya; gunakan dengan akun yang tidak masalah bila dibatasi.
- Session bisa kedaluwarsa → `/login` ulang.
- Server hanya menerima koneksi **loopback** (localhost) demi keamanan.
- Tool call dieksekusi oleh OpenCode dengan sistem permission-nya sendiri (persetujuan per tool, mode plan dihormati) — proxy tidak pernah menjalankan tool.
