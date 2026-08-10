package deepseek

import (
	"context"
	"fmt"
	"sync"

	"fakemodelapi/internal/auth"
	"fakemodelapi/internal/provider"
)

// Provider implements provider.Provider for the DeepSeek web API.
type Provider struct {
	mu        sync.Mutex
	client    *Client
	token     string
	modelID   string
}

// New creates a DeepSeek provider. It is not logged in until /login captures
// a session (or a saved session is found on disk).
func New() *Provider {
	return &Provider{}
}

var _ provider.Provider = (*Provider)(nil)

func (p *Provider) Chat(ctx context.Context, messages []provider.Message) (string, error) {
	ch, err := p.ChatStream(ctx, messages)
	if err != nil {
		return "", err
	}
	var b []byte
	var errMsg string
	for chunk := range ch {
		if chunk.Delta != "" {
			b = append(b, chunk.Delta...)
		}
		if chunk.FinishReason == "error" {
			errMsg = string(b)
		}
	}
	if errMsg != "" {
		return "", fmt.Errorf("%s", errMsg)
	}
	return string(b), nil
}

func (p *Provider) ChatStream(ctx context.Context, messages []provider.Message) (<-chan provider.Chunk, error) {
	client, err := p.getClient()
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	modelID := p.modelID
	p.mu.Unlock()

	req, err := BuildChatRequest(messages, modelID, client.parentMsgID)
	if err != nil {
		return nil, err
	}

	events, err := client.SendMessage(ctx, req)
	if err != nil {
		return nil, err
	}

	ch := make(chan provider.Chunk, 16)
	go func() {
		defer close(ch)
		for {
			select {
			case ev, ok := <-events:
				if !ok {
					return
				}
				switch ev.Kind {
				case EventIDs:
					client.parentMsgID = &ev.ParentMsgID
				case EventText:
					ch <- provider.Chunk{Delta: ev.Content}
				case EventFinish:
					ch <- provider.Chunk{FinishReason: "stop"}
				case EventError:
					ch <- provider.Chunk{Delta: "\n[error] " + ev.ErrMsg}
					ch <- provider.Chunk{FinishReason: "error"}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

func (p *Provider) AuthStatus() provider.AuthInfo {
	sess, err := auth.LoadSession("deepseek")
	if err != nil || sess.Token == "" {
		return provider.AuthInfo{LoggedIn: false}
	}
	return provider.AuthInfo{LoggedIn: true, Username: "deepseek web"}
}

// SetModel selects the model used for the next messages
// (deepseek-chat or deepseek-reasoner).
func (p *Provider) SetModel(modelID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.modelID = modelID
}

func (p *Provider) Models() []provider.ModelInfo {
	return []provider.ModelInfo{
		{ID: "deepseek-chat", DisplayName: "DeepSeek V4 Flash Free"},
		{ID: "deepseek-reasoner", DisplayName: "DeepSeek R1 (DeepThink)"},
	}
}

// Reset starts a fresh DeepSeek conversation on the next message.
func (p *Provider) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client != nil {
		p.client.ResetConversation()
	}
}

// getClient loads the saved session and (re)builds the HTTP client when the
// token changed (e.g. after a new /login).
func (p *Provider) getClient() (*Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	sess, err := auth.LoadSession("deepseek")
	if err != nil {
		return nil, err
	}
	if sess.Token == "" {
		return nil, fmt.Errorf("session DeepSeek tanpa token, coba /login lagi")
	}
	if p.client == nil || p.token != sess.Token {
		p.client = NewClient(sess.Token, sess.Cookies)
		p.token = sess.Token
	}
	return p.client, nil
}
