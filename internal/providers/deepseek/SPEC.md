# DeepSeek Web API SPEC

Dokumentasi hasil reverse-engineer `chat.deepseek.com` (unofficial API).
Bisa berubah kapan saja — kalau request gagal, cek ulang lewat DevTools
Network tab.

## Base URL

```
https://chat.deepseek.com/api/v0
```

## Auth

Dua hal yang dibutuhkan:

| Item | Sumber | Valid |
|---|---|---|
| `Authorization: Bearer <token>` | localStorage `userToken` | ~24 jam |
| cookies (sbg `cookie` header) | browser session (`ds_session_id`, dll) | selama sesi login |

Token diambil lewat browser login flow (`auth.CaptureSession`), bukan
copy-paste manual.

## Alur satu pesan chat

```
1. POST /chat_session/create  {"character_id": null}
   → data.biz_data.chat_session.id   (cache untuk multi-turn)

2. POST /chat/create_pow_challenge  {"target_path": "/api/v0/chat/completion"}
   → data.biz_data.challenge:
     {algorithm, challenge(hex32), salt, difficulty(~144000), expire_at, signature}

3. Solve PoW (local, tanpa WASM):
   cari nonce ∈ [0, difficulty] s.t.
       Keccak-256(f"{salt}_{expire_at}_{nonce}") == challenge
   Jawaban di-pack:
     {"algorithm","challenge","salt","answer":nonce,"signature",
      "target_path":"/api/v0/chat/completion"}
   → base64 → header x-ds-pow-response

4. POST /chat/completion (SSE)
```

## Header request (chat)

```
accept: */*
content-type: application/json
authorization: Bearer <token>
origin: https://chat.deepseek.com
referer: https://chat.deepseek.com/
user-agent: Mozilla/5.0 ... Chrome/131.0.0.0 Safari/537.36
x-app-version: 2.0.0
x-client-locale: en_US
x-client-platform: web
x-client-version: 2.0.2        ← harus match versi web saat ini
x-ds-pow-response: <base64>    ← hasil solve PoW (hanya di /chat/completion)
cookie: name=value; ...
```

## Body POST /chat/completion

```json
{
  "chat_session_id": "uuid",
  "parent_message_id": 16,
  "prompt": "pesan user terbaru",
  "ref_file_ids": [],
  "thinking_enabled": false,
  "search_enabled": false,
  "model_type": "default",
  "preempt": false,
  "action": null
}
```

`parent_message_id` = `response_message_id` dari pesan sebelumnya (multi-turn
dipegang server via rantai parent id). `null` untuk pesan pertama.

### Nilai `model_type`

`model_type` memilih model yang menjawab (bukan toggle fitur):

| Nilai | Model | Keterangan |
|---|---|---|
| `default` | DeepSeek-chat-Instant-Think-Search | model cepat bawaan web |
| `expert` | DeepSeek-chat-Expert-Think | model kuat, lebih lambat |

DeepThink (`thinking_enabled`) dan web search (`search_enabled`) adalah toggle
terpisah yang bisa dikombinasikan dengan model mana pun.

Catatan penting: **model thread terkunci saat thread dibuat** di server web.
Ganti `model_type` di tengah thread (parent_message_id sudah terisi) tidak
berefek — proxy me-reset percakapan (session + parent id baru) setiap model
berubah. Setiap model juga memakai session terpisah (satu Client per model).

### Aturan thread per request

- **Request dengan riwayat lengkap** (multi-message, biasanya diawali system
  prompt — opencode & klien API lain): selalu memulai **thread web baru**.
  Prompt sudah me-flatten seluruh konteks, jadi rantai parent lama hanya
  menambahkan konteks basi dari sesi sebelumnya (model menjawab topik lama
  atau meng-echo prompt). Model per request dipilih via `model_type`, jadi
  ganti model antar request langsung berefek.
- **Request 1 pesan user saja** (chat multi-turn TUI yang mengirim delta per
  turn): memakai rantai `parent_message_id` untuk kontinuitas.

## SSE stream format (bukan OpenAI format!)

| Event | Arti |
|---|---|
| `event: ready` + `data: {"request_message_id":15,"response_message_id":16,"model_type":"default"}` | ambil `response_message_id` → parent id pesan berikutnya |
| `event: update_session` + `data: {"updated_at":...}` | skip |
| `data: {"p":"response/fragments/-1/content","o":"APPEND","v":"teks"}` | delta konten |
| `data: {"v":"teks"}` | delta konten (format sederhana) |
| `data: {"v":{"response":{"fragments":[{"type":"RESPONSE","content":"teks",...}]}}}` | fragmen penuh (awal respons) |
| `data: {"p":"response/status","v":"FINISHED"}` | selesai (finish_reason = stop) |
| `data: {"code":40003,"msg":"invalid token",...}` | error (40003 = token invalid/expired) |

Konten thinking muncul sebagai fragment `type: "THINKING"` — dilewati.

## Error codes

| Kode | Arti |
|---|---|
| 40003 | invalid token → session expired, perlu `/login` ulang |
| 401 / 429 | auth / rate limit |
| 5xx | server error (retry dengan backoff) |

## Catatan

- Endpoint lain yang berguna nanti: `GET /chat/history_messages`, `POST /chat/stop_stream`, `GET /client/settings?scope=model` (discovery model).
- Web app kadang meminta "update to the latest version" kalau `x-client-version` ketinggalan — naikkan konstanta `XClientVersion`.
- Rate limit tidak terdokumentasi; jangan spam — satu akun web.
