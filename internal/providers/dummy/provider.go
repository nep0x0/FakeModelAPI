package dummy

import (
	"context"
	"fmt"
	"time"

	"fakemodelapi/internal/provider"
)

// Provider is a mock AI provider that returns dummy responses.
type Provider struct {
	serverOn  bool
	loggedIn  bool
}

// New creates a new dummy provider.
func New() *Provider {
	return &Provider{
		serverOn: false,
		loggedIn: false,
	}
}

var _ provider.Provider = (*Provider)(nil)

func (p *Provider) Chat(ctx context.Context, messages []provider.Message) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("no messages")
	}
	last := messages[len(messages)-1]
	return fmt.Sprintf("Ini balasan dummy dari provider dummy: '%s' diterima. Server %s dengan model dummy-model.",
		last.Content, map[bool]string{true: "on", false: "off"}[p.serverOn]), nil
}

func (p *Provider) ChatStream(ctx context.Context, messages []provider.Message) (<-chan provider.Chunk, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages")
	}
	last := messages[len(messages)-1]
	ch := make(chan provider.Chunk)

	full := fmt.Sprintf("Ini balasan dummy dari provider dummy: '%s' diterima. Server %s.",
		last.Content, map[bool]string{true: "on", false: "off"}[p.serverOn])

	go func() {
		defer close(ch)
		for i := 0; i < len(full); {
			select {
			case <-ctx.Done():
				return
			default:
			}
			end := i + 6
			if end > len(full) {
				end = len(full)
			}
			ch <- provider.Chunk{Delta: full[i:end]}
			i = end
			time.Sleep(35 * time.Millisecond)
		}
		ch <- provider.Chunk{FinishReason: "stop"}
	}()

	return ch, nil
}

func (p *Provider) AuthStatus() provider.AuthInfo {
	return provider.AuthInfo{
		LoggedIn: p.loggedIn,
		Username: "dummy",
		Expired:  false,
	}
}

func (p *Provider) Models() []provider.ModelInfo {
	return []provider.ModelInfo{
		{ID: "dummy-model", DisplayName: "Dummy Model"},
	}
}

// SetModel is a no-op for the dummy provider.
func (p *Provider) SetModel(modelID string) {}

// Reset is a no-op for the dummy provider.
func (p *Provider) Reset() {}

// SetServerOn sets the server state.
func (p *Provider) SetServerOn(on bool) {
	p.serverOn = on
}

// SetLoggedIn sets the login state.
func (p *Provider) SetLoggedIn(loggedIn bool) {
	p.loggedIn = loggedIn
}
