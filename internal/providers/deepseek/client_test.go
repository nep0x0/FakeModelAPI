package deepseek

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"fakemodelapi/internal/errs"
	"fakemodelapi/internal/provider"
)

// buildTestChallenge membuat challenge PoW yang valid: target hash dihitung
// untuk nonce 0 dengan difficulty kecil supaya SolveChallenge cepat.
func buildTestChallenge() Challenge {
	salt := "test-salt-123"
	expireAt := int64(1786304784836)
	h := hashV1(fmt.Sprintf("%s_%d_", salt, expireAt), 0)
	return Challenge{
		Algorithm:  "DeepSeekHashV1",
		Challenge:  hex.EncodeToString(h[:]),
		Salt:       salt,
		Difficulty: 1000,
		ExpireAt:   expireAt,
		Signature:  "sig-test",
	}
}

// mockDeepSeekServer mensimulasikan API web DeepSeek: buat session,
// beri challenge PoW, lalu kirim aliran SSE.
func mockDeepSeekServer(t *testing.T, completionStatus int, sseBody string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/chat_session/create", func(w http.ResponseWriter, r *http.Request) {
		writeMockJSON(w, map[string]any{
			"code": 0, "msg": "",
			"data": map[string]any{
				"biz_data": map[string]any{
					"chat_session": map[string]any{"id": "sess-mock-1"},
				},
			},
		})
	})

	mux.HandleFunc("/chat/create_pow_challenge", func(w http.ResponseWriter, r *http.Request) {
		writeMockJSON(w, map[string]any{
			"code": 0, "msg": "",
			"data": map[string]any{"biz_data": map[string]any{"challenge": buildTestChallenge()}},
		})
	})

	mux.HandleFunc("/chat/completion", func(w http.ResponseWriter, r *http.Request) {
		// Validasi header auth yang dikirim client.
		if auth := r.Header.Get("authorization"); !strings.HasPrefix(auth, "Bearer tok-") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("x-ds-pow-response") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(completionStatus)
		_, _ = w.Write([]byte(sseBody))
	})

	return httptest.NewServer(mux)
}

func writeMockJSON(w http.ResponseWriter, v any) {
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// TestClientSendsModelType memverifikasi bahwa pilihan model diteruskan ke web
// API sebagai model_type yang benar: deepseek-reasoner → "expert",
// deepseek-chat → "default", keduanya dengan thinking_enabled=true.
func TestClientSendsModelType(t *testing.T) {
	cases := []struct {
		modelID  string
		wantType string
	}{
		{"deepseek-chat", "default"},
		{"deepseek-reasoner", "expert"},
	}
	for _, c := range cases {
		var gotBody ChatRequest
		var bodyReady = make(chan struct{}, 1)

		mux := http.NewServeMux()
		mux.HandleFunc("/chat_session/create", func(w http.ResponseWriter, r *http.Request) {
			writeMockJSON(w, map[string]any{"code": 0, "data": map[string]any{"biz_data": map[string]any{"chat_session": map[string]any{"id": "sess-1"}}}})
		})
		mux.HandleFunc("/chat/create_pow_challenge", func(w http.ResponseWriter, r *http.Request) {
			writeMockJSON(w, map[string]any{"code": 0, "data": map[string]any{"biz_data": map[string]any{"challenge": buildTestChallenge()}}})
		})
		mux.HandleFunc("/chat/completion", func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			bodyReady <- struct{}{}
			w.Header().Set("content-type", "text/event-stream")
			_, _ = w.Write([]byte(mockSSEBody))
		})
		srv := httptest.NewServer(mux)

		cl := NewClientWithBase("tok-x", nil, srv.URL)
		req, err := BuildChatRequest([]provider.Message{{Role: "user", Content: "halo"}}, c.modelID, nil)
		if err != nil {
			t.Fatalf("BuildChatRequest(%q): %v", c.modelID, err)
		}
		events, err := cl.SendMessage(context.Background(), req)
		if err != nil {
			t.Fatalf("SendMessage(%q): %v", c.modelID, err)
		}
		for ev := range events {
			if ev.Kind == EventFinish {
				break
			}
		}
		<-bodyReady
		srv.Close()

		if gotBody.ModelType != c.wantType {
			t.Errorf("model %q: model_type yang dikirim = %q, want %q", c.modelID, gotBody.ModelType, c.wantType)
		}
		if !gotBody.ThinkingEnabled {
			t.Errorf("model %q: thinking_enabled harus true", c.modelID)
		}
	}
}

const mockSSEBody = "" +
	`data: {"request_message_id":10,"response_message_id":11,"model_type":"default"}` + "\n" +
	`data: {"p":"response/fragments/-1/content","o":"APPEND","v":"Halo"}` + "\n" +
	`data: {"v":" dunia"}` + "\n" +
	`data: {"p":"response/status","v":"FINISHED"}` + "\n" +
	"event: done\n"

func TestClientSendMessageStream(t *testing.T) {
	srv := mockDeepSeekServer(t, http.StatusOK, mockSSEBody)
	defer srv.Close()

	c := NewClientWithBase("tok-abc", nil, srv.URL)
	req, err := BuildChatRequest([]provider.Message{{Role: "user", Content: "halo"}}, "deepseek-chat", nil)
	if err != nil {
		t.Fatalf("BuildChatRequest: %v", err)
	}

	events, err := c.SendMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	var gotText string
	var gotParent *int64
	gotFinish := false
	for ev := range events {
		switch ev.Kind {
		case EventIDs:
			gotParent = &ev.ParentMsgID
		case EventText:
			gotText += ev.Content
		case EventFinish:
			gotFinish = true
		}
	}

	if gotParent == nil || *gotParent != 11 {
		t.Fatalf("EventIDs parent = %v, want 11", gotParent)
	}
	if gotText != "Halo dunia" {
		t.Fatalf("text = %q, want %q", gotText, "Halo dunia")
	}
	if !gotFinish {
		t.Fatal("tidak ada EventFinish")
	}
}

func TestClientSendMessageSessionReuse(t *testing.T) {
	srv := mockDeepSeekServer(t, http.StatusOK, mockSSEBody)
	defer srv.Close()

	c := NewClientWithBase("tok-abc", nil, srv.URL)
	req, _ := BuildChatRequest([]provider.Message{{Role: "user", Content: "a"}}, "deepseek-chat", nil)

	// Kirim dua pesan berurutan; session hanya dibuat sekali.
	if _, err := c.SendMessage(context.Background(), req); err != nil {
		t.Fatalf("SendMessage #1: %v", err)
	}
	c.mu.Lock()
	sess1 := c.chatSessionID
	c.mu.Unlock()
	if _, err := c.SendMessage(context.Background(), req); err != nil {
		t.Fatalf("SendMessage #2: %v", err)
	}
	c.mu.Lock()
	sess2 := c.chatSessionID
	c.mu.Unlock()
	if sess1 != "sess-mock-1" || sess2 != "sess-mock-1" {
		t.Fatalf("session tidak reuse: %q -> %q", sess1, sess2)
	}
}

func TestClientSendMessageRateLimited(t *testing.T) {
	srv := mockDeepSeekServer(t, http.StatusTooManyRequests, "")
	defer srv.Close()

	c := NewClientWithBase("tok-abc", nil, srv.URL)
	req, _ := BuildChatRequest([]provider.Message{{Role: "user", Content: "a"}}, "deepseek-chat", nil)

	_, err := c.SendMessage(context.Background(), req)
	if err == nil {
		t.Fatal("expected error 429")
	}
	if !errs.Is(err, errs.KindRateLimited) {
		t.Fatalf("error harus berkategori rate_limited, got: %v", err)
	}
}

func TestClientSendMessageUnauthorized(t *testing.T) {
	// Server ini selalu menolak: header auth tidak cocok.
	srv := mockDeepSeekServer(t, http.StatusUnauthorized, "")
	defer srv.Close()

	c := NewClientWithBase("tok-beda", nil, srv.URL)
	req, _ := BuildChatRequest([]provider.Message{{Role: "user", Content: "a"}}, "deepseek-chat", nil)

	_, err := c.SendMessage(context.Background(), req)
	if err == nil {
		t.Fatal("expected error 401")
	}
	if !errors.Is(err, provider.ErrNotAuthenticated) {
		t.Fatalf("error harus unwrap ke provider.ErrNotAuthenticated, got: %v", err)
	}
}
