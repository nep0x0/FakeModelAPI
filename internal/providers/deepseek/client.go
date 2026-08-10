package deepseek

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"fakemodelapi/internal/provider"
)

const (
	apiBase    = "https://chat.deepseek.com/api/v0"
	targetPath = "/api/v0/chat/completion"
)

// XClientVersion must roughly match the current web app version.
// If the web app starts rejecting requests with "update to the latest
// version", bump this to the current version served at chat.deepseek.com.
var XClientVersion = "2.0.2"

// ChatRequest is the payload sent to /chat/completion.
type ChatRequest struct {
	ChatSessionID   string   `json:"chat_session_id"`
	ParentMessageID *int64   `json:"parent_message_id"`
	Prompt          string   `json:"prompt"`
	RefFileIDs      []string `json:"ref_file_ids"`
	ThinkingEnabled bool     `json:"thinking_enabled"`
	SearchEnabled   bool     `json:"search_enabled"`
	ModelType       string   `json:"model_type"`
	Preempt         bool     `json:"preempt"`
	Action          any      `json:"action"`
}

// ErrNotAuthenticated is returned when the DeepSeek session is invalid/expired.
// It wraps provider.ErrNotAuthenticated so callers can errors.Is it.
type ErrNotAuthenticated struct{ Msg string }

func (e *ErrNotAuthenticated) Error() string {
	return "session DeepSeek tidak valid: " + e.Msg
}

func (e *ErrNotAuthenticated) Unwrap() error { return provider.ErrNotAuthenticated }

// Client talks to the DeepSeek web API using a captured bearer token + cookies.
type Client struct {
	token   string
	cookies []string // raw "name=value" pairs from the captured session
	http    *http.Client

	mu            sync.Mutex
	chatSessionID string
	parentMsgID   *int64
}

// NewClient builds a Client from a captured token and cookies.
func NewClient(token string, cookies []http.Cookie) *Client {
	pairs := make([]string, 0, len(cookies))
	for _, c := range cookies {
		if c.Name == "" {
			continue
		}
		pairs = append(pairs, c.Name+"="+c.Value)
	}
	return &Client{
		token:   token,
		cookies: pairs,
		http:    &http.Client{},
	}
}

// ResetConversation drops the cached chat session so the next message starts fresh.
func (c *Client) ResetConversation() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.chatSessionID = ""
	c.parentMsgID = nil
}

func (c *Client) setParentMessageID(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.parentMsgID = &id
}

func (c *Client) parentMessageID() *int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.parentMsgID == nil {
		return nil
	}
	id := *c.parentMsgID
	return &id
}

func (c *Client) headers(pow string) http.Header {
	h := http.Header{}
	h.Set("accept", "*/*")
	h.Set("content-type", "application/json")
	h.Set("authorization", "Bearer "+c.token)
	h.Set("origin", "https://chat.deepseek.com")
	h.Set("referer", "https://chat.deepseek.com/")
	h.Set("user-agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	h.Set("x-app-version", "2.0.0")
	h.Set("x-client-locale", "en_US")
	h.Set("x-client-platform", "web")
	h.Set("x-client-version", XClientVersion)
	if pow != "" {
		h.Set("x-ds-pow-response", pow)
	}
	if len(c.cookies) > 0 {
		h.Set("cookie", strings.Join(c.cookies, "; "))
	}
	return h
}

// postJSON sends a JSON POST and decodes the standard envelope
// {"code":0,"msg":"","data":{...}}.
func (c *Client) postJSON(ctx context.Context, path string, body any) (map[string]any, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	var resp *http.Response
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+path, bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		req.Header = c.headers("")

		resp, err = c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request gagal: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized {
			return nil, &ErrNotAuthenticated{Msg: "401 unauthorized"}
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("HTTP %d dari DeepSeek", resp.StatusCode)
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			bodyText, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("HTTP %d dari DeepSeek: %s", resp.StatusCode, truncate(string(bodyText), 200))
		}

		var envelope struct {
			Code int            `json:"code"`
			Msg  string         `json:"msg"`
			Data map[string]any `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			return nil, fmt.Errorf("gagal decode respons: %w", err)
		}
		if envelope.Code != 0 {
			return nil, fmt.Errorf("DeepSeek error %d: %s", envelope.Code, envelope.Msg)
		}
		return envelope.Data, nil
	}
	return nil, lastErr
}

// ensureSession lazily creates the chat session for multi-turn continuity.
func (c *Client) ensureSession(ctx context.Context) error {
	c.mu.Lock()
	if c.chatSessionID != "" {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	data, err := c.postJSON(ctx, "/chat_session/create", map[string]any{"character_id": nil})
	if err != nil {
		return fmt.Errorf("gagal buat chat session: %w", err)
	}
	biz, ok := data["biz_data"].(map[string]any)
	if !ok {
		return fmt.Errorf("respons chat_session/create tidak valid")
	}
	sess, ok := biz["chat_session"].(map[string]any)
	if !ok {
		return fmt.Errorf("respons chat_session/create tidak valid")
	}
	id, ok := sess["id"].(string)
	if !ok || id == "" {
		return fmt.Errorf("chat_session tanpa id")
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.chatSessionID == "" {
		c.chatSessionID = id
	}
	return nil
}

// getChallenge fetches a fresh proof-of-work challenge.
func (c *Client) getChallenge(ctx context.Context) (Challenge, error) {
	data, err := c.postJSON(ctx, "/chat/create_pow_challenge", map[string]any{"target_path": targetPath})
	if err != nil {
		return Challenge{}, err
	}
	biz, ok := data["biz_data"].(map[string]any)
	if !ok {
		return Challenge{}, fmt.Errorf("respons challenge tidak valid")
	}
	chRaw, ok := biz["challenge"]
	if !ok {
		return Challenge{}, fmt.Errorf("challenge kosong dari DeepSeek")
	}
	if os.Getenv("FAKEAPI_DEBUG_DUMP") != "" {
		rawDBG, _ := json.Marshal(chRaw)
		fmt.Printf("[debug] challenge mentah: %s\n", rawDBG)
	}
	raw, err := json.Marshal(chRaw)
	if err != nil {
		return Challenge{}, err
	}
	var ch Challenge
	if err := json.Unmarshal(raw, &ch); err != nil {
		return Challenge{}, fmt.Errorf("gagal decode challenge: %w", err)
	}
	return ch, nil
}

// SendMessage sends one message and returns the parsed SSE event stream.
// The caller must drain the channel; it closes when the stream ends.
func (c *Client) SendMessage(ctx context.Context, req ChatRequest) (<-chan Event, error) {
	if err := c.ensureSession(ctx); err != nil {
		return nil, err
	}
	c.mu.Lock()
	req.ChatSessionID = c.chatSessionID
	c.mu.Unlock()
	req.ParentMessageID = c.parentMessageID()

	challenge, err := c.getChallenge(ctx)
	if err != nil {
		return nil, err
	}
	pow, err := SolveChallenge(challenge, targetPath)
	if err != nil {
		return nil, fmt.Errorf("gagal solve PoW: %w", err)
	}

	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/chat/completion", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header = c.headers(pow)

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("chat request gagal: %w", err)
	}

	if httpResp.StatusCode == http.StatusUnauthorized {
		httpResp.Body.Close()
		return nil, &ErrNotAuthenticated{Msg: "401 unauthorized"}
	}
	if httpResp.StatusCode != http.StatusOK {
		bodyText, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, fmt.Errorf("HTTP %d dari DeepSeek: %s", httpResp.StatusCode, truncate(string(bodyText), 200))
	}

	events := make(chan Event, 16)
	go func() {
		defer close(events)
		defer httpResp.Body.Close()
		reader := bufio.NewReader(httpResp.Body)
		for {
			line, err := reader.ReadString('\n')
			if line != "" {
				if ev, ok := ParseSSELine(line); ok && ev.Kind != EventSkip {
					select {
					case events <- ev:
					case <-ctx.Done():
						return
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return events, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
