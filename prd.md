# PRD FakeModelAPI

## 1. Visi
Aplikasi terminal sesimple dan sebersih OpenCode.
Satu kotak input di tengah, status minimal, tanpa clutter.
Di belakang layar: server lokal yang mengubah Web AI gratis (DeepSeek, Qwen, Gemini) menjadi API kompatibel OpenAI untuk OpenCode.

## 2. Tampilan (hanya ini, tidak lebih)

	███████  █████  ██   ██ ███████ ███    ███  ██████  ██████  ███████ ██       █████  ██████  ██
	██      ██   ██ ██  ██  ██      ████  ████ ██    ██ ██   ██ ██      ██      ██   ██ ██   ██ ██
	█████   ███████ █████   █████   ██ ████ ██ ██    ██ ██   ██ █████   ██      ███████ ██████  ██
	██      ██   ██ ██  ██  ██      ██  ██  ██ ██    ██ ██   ██ ██      ██      ██   ██ ██      ██
	██      ██   ██ ██   ██ ███████ ██      ██  ██████  ██████  ███████ ███████ ██   ██ ██      ██ 
                                                                                                       
      (logo ASCII art di tengah)
      jarak logo dan di bawahnya ini 4 enter
  +----------------------------------------------+
  | Type "/"...                                  | 
  |                                              |
  | Chat - DeepSeek Chat Free - localhost:8000   |
  +----------------------------------------------+
  tab providers   ctrl+p commands

  * Tip: pakai /login untuk menghubungkan akun DeepSeek

~  (o) server off  /status (sudut kiri)                     v0.1.0 (sudut kanan)  (bagian ini paling bawah)

Enam elemen di layar:
1. Logo ASCII - di tengah, hanya saat start
2. Kotak input - satu-satunya tempat mengetik
3. Baris mode - mode · model · endpoint
4. Hint keys - tab & ctrl+p
5. Baris tip - bantuan kecil
6. Status bar - kiri status server, kanan versi

Tanpa panel. Tanpa tabel. Tanpa tombol. Itu saja.

## 3. Cara pakai

Ketik + Enter      -> chat dengan AI
tab                -> ganti provider/model
ctrl+p             -> buka command palette
ctrl+c atau /exit  -> keluar

## 4. Slash commands

/login   - buka browser, cookies otomatis tertangkap, tanpa copy-paste
/logout  - hapus session
/start   - nyalakan server API lokal untuk OpenCode
/stop    - matikan server
/status  - lihat status server & auth
/model   - pilih model
/exit    - keluar

## 5. Fungsi inti (cuma dua)

1. Chat di terminal
   Ketik di kotak -> jawaban AI gratis muncul di terminal.
2. Server API lokal
   /start menjalankan API kompatibel OpenAI di http://localhost:8000
   supaya OpenCode bisa pakai AI gratis tanpa API key.

## 6. Teknologi

- Go
- Bubble Tea (charmbracelet/bubbletea) - framework TUI
- Bubbles - komponen textarea, spinner, help
- Lipgloss - warna & styling

## 7. Prinsip desain

- Less is more: tiap elemen di layar harus punya tujuan
- Keyboard first: semua tercapai tanpa mouse
- Zero config: jalan langsung tanpa setup ribet
- No clutter: kalau tidak penting, jangan ditampilkan

## 8. Phase development

Phase 1 - Skeleton: logo + kotak input + status bar; bisa ketik, bisa quit.
Phase 2 - Chat: kirim pesan, tampilkan jawaban (dummy provider dulu).
Phase 3 - Commands: /login, /start, /stop, /status, tab switcher, ctrl+p.
Phase 4 - Real: sambung provider asli + server lokal asli.

## 9. Kriteria sukses MVP

- fakeapi dijalankan -> layar simple ala OpenCode muncul
- ketik halo -> AI balas di terminal
- /login -> browser terbuka -> login -> balik ke TUI dengan status ready
- /start -> OpenCode bisa konek ke localhost:8000
- Selain itu tidak perlu. Kalau fitur tidak muat di flow ini, jangan dibuat.