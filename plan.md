# Plan Eksekusi FakeModelAPI

> **Konsep inti:** FakeModelAPI adalah reverse proxy + format translator.
> User login ke Web AI (DeepSeek/Qwen/Gemini) lewat browser → cookies/session ditangkap otomatis →
> server lokal di `localhost:8000` menerima request OpenAI-format dari OpenCode →
> menerjemahkan ke format native Web AI → mengirim dengan cookies user →
> menerjemahkan respons balik ke OpenAI format → mengirim ke OpenCode.

## Arsitektur Package
```
cmd/fakeapi/main.go              ← entry point, menjalankan TUI + server
internal/
  tui/                            ← TUI (Bubble Tea), sudah ada
    components/                   ← komponen UI (input, chatview, statusbar, dll)
  provider/                       ← [NEW] abstraksi interface Provider
    provider.go                   ← interface: Chat(), ChatStream(), AuthStatus(), Models()
    registry.go                   ← registry provider (map[name]Provider)
  providers/                      ← [NEW] implementasi konkret per Web AI
    deepseek/                     ← DeepSeek Web provider
      client.go                   ← HTTP client ke API DeepSeek Web
      auth.go                     ← tangkap & kelola cookies/session DeepSeek
      convert.go                  ← konversi OpenAI req/res ↔ DeepSeek payload
    qwen/                         ← Qwen Web provider (struktur sama)
    gemini/                       ← Gemini Web provider (struktur sama)
  server/                         ← [NEW] HTTP server OpenAI-compatible
    server.go                     ← lifecycle (start/stop), routing
    handlers.go                   ← GET /v1/models, POST /v1/chat/completions
    sse.go                        ← SSE streaming writer
  auth/                           ← [NEW] auth engine (browser cookie capture)
    browser.go                    ← buka browser, tangkap cookies via CDP (playwright-go)
    session.go                    ← simpan/load/refresh session ke disk
```

## Phase 1: TUI Skeleton & Core Flow ✅ SELESAI
- [x] Inisialisasi struktur proyek Go.
- [x] Komponen TUI: Logo, Input, ChatView, Statusbar, Hintbar, Tipbar, Palette, Slash.
- [x] Layout bersih sesuai PRD.
- [x] Slash commands (`/login`, `/logout`, `/start`, `/stop`, `/status`, `/model`, `/exit`).
- [x] Command Palette (`Ctrl+P`) dengan filter real-time.
- [x] Tab cycling provider (DeepSeek ↔ Qwen ↔ Gemini).
- [x] Dummy streaming response (simulasi ketik per karakter).

## Phase 2: Provider Interface & Registry
**Tujuan:** Memisahkan logika provider dari TUI agar TUI tidak bergantung pada implementasi konkret.
- [ ] Buat `internal/provider/provider.go`:
  - Interface `Provider` dengan method:
    - `Chat(ctx, messages []Message) (string, error)` — non-streaming
    - `ChatStream(ctx, messages []Message) (<-chan Chunk, error)` — streaming (channel of chunks)
    - `AuthStatus() AuthInfo` — apakah sudah login, user info, expired
    - `Models() []ModelInfo` — daftar model yang tersedia
  - Struct `Message` (Role, Content), `Chunk` (Delta, FinishReason), `AuthInfo`, `ModelInfo`
- [ ] Buat `internal/provider/registry.go`:
  - Global registry `map[string]Provider`
  - Fungsi `Register(name, Provider)`, `Get(name) Provider`, `List() []string`
- [ ] Buat `internal/providers/dummy/` (pindahkan logika dummy streaming dari `update.go` ke sini):
  - Implement `Provider` interface dengan respons dummy
  - Daftarkan ke registry sebagai `"dummy"` (default sebelum provider asli siap)
- [ ] Refactor `update.go` agar memanggil `registry.Get(activeProvider).ChatStream()` alih-alih dummy inline

## Phase 3: Auth Engine (Browser Cookie Capture)
**Tujuan:** User login ke DeepSeek Web lewat browser, cookies otomatis tertangkap tanpa copy-paste.
- [ ] Buat `internal/auth/browser.go`:
  - Gunakan library **playwright-go** (`github.com/mxschmitt/playwright-go`) untuk Chrome DevTools Protocol
  - Fungsi `CaptureCookies(url string) ([]http.Cookie, error)`:
    1. Buka Chrome/Chromium (detect existing atau launch baru)
    2. Navigasi ke `https://chat.deepseek.com`
    3. User login manual di browser (tunggu user selesai — deteksi redirect/state)
    4. Setelah login sukses, baca semua cookies dari domain `.deepseek.com`
    5. Kembalikan cookies sebagai `[]http.Cookie`
  - Timeout handling & feedback ke TUI (spinner + "menunggu login...")
- [ ] Buat `internal/auth/session.go`:
  - Simpan cookies ke disk (`~/.fakeapi/sessions/deepseek.json`) dalam format JSON
  - Load cookies dari disk
  - Cek expired (bisa dari cookie `Expires` atau test request kecil)
  - Fungsi `RefreshIfNeeded()` — jika expired, trigger ulang browser login
- [ ] Integrasi dengan `/login` slash command di `update.go`:
  - Panggil `auth.CaptureCookies()` untuk provider aktif
  - Tampilkan status di chat view ("membuka browser..." → "login berhasil ✓" / "gagal ✗")

## Phase 4: DeepSeek Web Provider (Provider Asli Pertama)
**Tujuan:** Implementasi penuh provider DeepSeek — reverse engineer API web, konversi format, streaming.
- [ ] **4a. Reverse engineer API DeepSeek Web:**
  - Buka DevTools Network tab di browser, login ke chat.deepseek.com
  - Kirim beberapa chat, catat:
    - Endpoint URL (misal `https://chat.deepseek.com/api/v1/chat`)
    - Request headers (Cookie, CSRF token, Content-Type, dll)
    - Request body format (model, messages, stream, dll)
    - Response format (non-stream & SSE stream)
    - Struktur SSE event (`data: {...}`)
  - Dokumentasikan di `internal/providers/deepseek/SPEC.md`
- [ ] **4b. HTTP Client (`client.go`):**
  - Fungsi `NewClient(cookies []http.Cookie) *Client`
  - Method `SendMessage(ctx, payload) (*http.Response, error)` — kirim request dengan cookies
  - Method `SendMessageStream(ctx, payload) (<-chan string, error)` — baca SSE stream, kirim chunk per chunk lewat channel
  - Handle error: 401 (session expired → trigger re-auth), 429 (rate limit → retry after), 5xx
- [ ] **4c. Format converter (`convert.go`):**
  - `OpenAIToDeepSeek(req openai.ChatCompletionRequest) deepseek.Payload`
    - Mapping: `messages`, `model`, `stream`, `temperature`, `max_tokens`
  - `DeepSeekChunkToOpenAI(chunk deepseek.SSEChunk) openai.ChatCompletionChunk`
    - Mapping: delta content, finish_reason, index
  - `DeepSeekToOpenAI(resp deepseek.Response) openai.ChatCompletionResponse`
    - Untuk non-streaming
- [ ] **4d. Provider implementation (`provider.go`):**
  - Struct `DeepSeekProvider` implement `provider.Provider`
  - `Chat()` → panggil `SendMessage` non-stream, convert respons
  - `ChatStream()` → panggil `SendMessageStream`, convert tiap chunk, kirim ke channel
  - `AuthStatus()` → cek apakah cookies valid
  - `Models()` → return daftar model DeepSeek (DeepSeek-V3, DeepSeek-R1, dll)
- [ ] **4e. Integrasi dengan TUI:**
  - Ganti provider dummy dengan DeepSeek asli di registry
  - Pastikan streaming tampil di chatview dengan baik
  - Handle error state (session expired → auto prompt re-login)

## Phase 5: Local OpenAI-Compatible Server (Port 8000)
**Tujuan:** HTTP server yang menerima request OpenAI-format dan meneruskannya ke provider aktif.
- [ ] Buat `internal/server/server.go`:
  - Struct `Server` dengan field `http.Server`, `providerName`, `port`
  - Method `Start() error` — mulai HTTP server di goroutine
  - Method `Stop() error` — graceful shutdown
  - Variabel global `ActiveServer *Server` (diakses TUI untuk status)
- [ ] Buat `internal/server/handlers.go`:
  - `GET /v1/models`:
    - Panggil `registry.Get(provider).Models()`
    - Return JSON: `{"object": "list", "data": [{"id": "...", "object": "model", ...}]}`
  - `POST /v1/chat/completions`:
    - Parse body sebagai `openai.ChatCompletionRequest`
    - Jika `stream == true`:
      - Set header `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`
      - Panggil `provider.ChatStream()`, untuk setiap chunk:
        - Convert ke OpenAI SSE format: `data: {...}\n\n`
        - Flush ke response writer
      - Kirim `data: [DONE]\n\n` saat selesai
    - Jika `stream == false`:
      - Panggil `provider.Chat()`
      - Return JSON `openai.ChatCompletionResponse`
  - Handle error: provider not authenticated → 401, rate limited → 429, timeout → 504
- [ ] **Integrasi `/start` & `/stop`:**
  - `/start`: buat `Server`, panggil `Start()`, update `m.serverOn = true`
  - `/stop`: panggil `Stop()`, update `m.serverOn = false`
  - Status bar real-time dari `ActiveServer != nil && ActiveServer.Running()`
- [ ] **Test manual:**
  - Jalankan `/start`
  - `curl http://localhost:8000/v1/models`
  - `curl -X POST http://localhost:8000/v1/chat/completions -H "Content-Type: application/json" -d '{"model":"deepseek-v3","messages":[{"role":"user","content":"halo"}],"stream":false}'`

## Phase 6: Error Handling, Retry, & Session Refresh
- [ ] **Global error handler di server handlers:**
  - 401 Unauthorized → trigger re-auth (browser), return error ke client
  - 429 Too Many Requests → baca `Retry-After` header, return 429 ke client
  - 502 Bad Gateway → provider error, return 502
  - 504 Gateway Timeout → request ke provider timeout, return 504
- [ ] **Auto session refresh:**
  - Sebelum setiap request ke provider, panggil `auth.RefreshIfNeeded()`
  - Jika refresh gagal, return 401 dan tampilkan notifikasi di TUI: "session expired, silakan /login"
- [ ] **Retry logic di provider client:**
  - Retry hingga 3x untuk error 5xx dengan exponential backoff
  - Jangan retry untuk 4xx (kecuali 429 dengan Retry-After)

## Phase 7: Qwen & Gemini Providers
**Tujuan:** Setelah DeepSeek stabil, tambahkan provider Qwen dan Gemini dengan pola yang sama.
- [ ] Qwen Web (`internal/providers/qwen/`):
  - Reverse engineer API `chat.qwen.ai`
  - Auth (tangkap cookies dari `qwen.ai`)
  - Client, converter, provider implementation
- [ ] Gemini Web (`internal/providers/gemini/`):
  - Reverse engineer API `gemini.google.com`
  - Auth (tangkap cookies dari Google)
  - Client, converter, provider implementation
- [ ] Daftarkan semua ke registry
- [ ] Tab cycling di TUI bekerja untuk ketiga provider

## Phase 8: Polish, Testing & Integration
- [ ] **Integration test dengan OpenCode:**
  - Set OpenCode provider ke `http://localhost:8000/v1`
  - Chat dari OpenCode → harus tembus ke DeepSeek Web → respons kembali
  - Test streaming (ketik panjang, lihat muncul bertahap)
  - Test multi-turn conversation
- [ ] **UI polish:**
  - Animasi spinner saat streaming dari provider asli
  - Loading state saat `/login` (browser terbuka)
  - Error toast/notifikasi di TUI
  - Warna/waktu berbeda untuk setiap provider
- [ ] **Performa:**
  - Pastikan SSE streaming tidak buffered
  - Timeout configurable (default 120s)
  - Concurrent request handling di server
- [ ] **UX edge cases:**
  - Apa yang terjadi jika server sedang on lalu user ganti provider?
  - Apa yang terjadi jika session expired saat streaming?
  - Apa yang terjadi jika browser tidak terinstall (fallback: manual cookie paste?)
