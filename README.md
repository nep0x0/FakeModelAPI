# FakeModelAPI

Bridge lokal yang menyediakan endpoint API **kompatibel OpenAI** di `localhost:8000`, dengan layanan web AI (DeepSeek) sebagai backend — untuk digunakan bersama [OpenCode](https://opencode.ai).

```
OpenCode ──HTTP──▶ FakeModelAPI (localhost:8000) ──▶ chat.deepseek.com (sesi web Anda)
                    (format OpenAI ↔ format web)
```

## Apa itu FakeModelAPI?

FakeModelAPI berdiri di antara OpenCode dan layanan web DeepSeek:

- **Bagi OpenCode**, ia tampak seperti provider OpenAI biasa: endpoint `http://localhost:8000/v1` dengan `/chat/completions` (streaming & non-streaming), dan emulasi **tool call native** (`tool_calls` di OpenAI format).
- **Bagi DeepSeek**, ia memanfaatkan sesi web pribadi Anda (token + cookies hasil login manual via browser), tanpa memerlukan kunci API terpisah.

Dengan begitu, fitur OpenCode penuh tetap berfungsi: agent mode, tool execution, permission per tool, model switching via `/models`.

## Kepatuhan & Penggunaan yang Bertanggung Jawab

Sebelum memakai tool ini, pastikan Anda memahami hal berikut:

- Gunakan **hanya akun milik Anda sendiri**. Berbagi akun, membeli akun, atau membuat banyak akun untuk menghindari batasan tidak dibenarkan.
- Anda bertanggung jawab penuh untuk mematuhi **Syarat Layanan, Kebijakan Privasi, dan ketentuan penggunaan** dari penyedia layanan web (mis. DeepSeek) yang berlaku saat Anda menggunakannya.
- Tool ini ditujukan untuk **penggunaan pribadi dan pengembangan**. Hormati batas wajar pemakaian dan rate limit; jangan gunakan untuk otomatisasi berskala besar.
- Jika penyedia membatasi, menonaktifkan, atau melarang pemakaian semacam ini, **hentikan penggunaan** dan jangan mengelak dari pembatasan tersebut.
- Proyek ini **tidak berafiliasi, didukung, atau disetujui** oleh DeepSeek, OpenAI, maupun OpenCode. Seluruh merek dagang adalah milik pemiliknya masing-masing.

## Model

Dua model dari web DeepSeek, keduanya dengan DeepThink aktif:

| ID (OpenCode) | `model_type` web | Nama | Keterangan |
|---|---|---|---|
| `deepseek-chat` | `default` | DeepSeek-chat-Instant-Think-Search | model cepat bawaan |
| `deepseek-reasoner` | `expert` | DeepSeek-chat-Expert-Think | model kuat, lebih lambat |

Ganti model di OpenCode lewat `/models` — nama tampilan yang kamu set di `opencode.json` yang dipakai.

## Prasyarat

- **Go 1.25+**
- **Akun DeepSeek** (https://chat.deepseek.com) — akun pribadi milik Anda sendiri
- **Chromium untuk Playwright** (dipakai `/login` untuk menangkap sesi):
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
          "name": "DeepSeek Chat"
        },
        "deepseek-reasoner": {
          "name": "DeepSeek Expert"
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
| `/login`  | Buka browser, tangkap sesi DeepSeek otomatis                  |
| `/logout` | Hapus sesi                                                    |
| `/start`  | Nyalakan server API lokal di `localhost:8000`                 |
| `/stop`   | Matikan server                                                |
| `/status` | Status server & login                                         |
| `/test`   | Tes koneksi AI (respon + waktu)                               |
| `/doctor` | Pemeriksaan menyeluruh (config, sesi, koneksi)                |
| `/config` | Tampilkan konfigurasi aktif                                   |
| `/logs`   | Log aktivitas request server (ring buffer, 30 terakhir)       |
| `/model`  | Pilih model                                                   |
| `/clear`  | Bersihkan riwayat chat TUI + reset percakapan                 |
| `/exit`   | Keluar (atau `/quit`, Ctrl+C)                                 |

Tombol: `Tab` ganti provider, `Ctrl+P` command palette.

## Catatan Teknis

- **Sesi bisa kedaluwarsa** → `/login` ulang.
- **Server hanya menerima koneksi loopback** (localhost) demi keamanan.
- **Tool call dieksekusi oleh OpenCode** dengan sistem permission-nya sendiri (persetujuan per tool, mode plan dihormati) — proxy tidak pernah menjalankan tool.
- **Thread web**: request berisi riwayat lengkap (opencode/klien API) selalu memulai thread baru yang bersih; chat TUI multi-turn (1 pesan per turn) meneruskan rantai percakapan.
- **Teks thinking (DeepThink) difilter** agar tidak bocor ke jawaban.
- **Model terkunci saat thread web dibuat** — ganti model otomatis membuat sesi/thread baru.
- **Log request**: di TUI aktif log tidak dicetak ke layar (biar TUI tidak rusak), lihat lewat `/logs`; di mode `-headless` log dicetak ke stdout.
- **Konfigurasi**: prioritas `~/.fakeapi/config.json` → env `FAKEAPI_*` → flag CLI. Jalankan `fakeapi config init` untuk membuat file config.
- **Terminal minimal 80x24** untuk TUI.
