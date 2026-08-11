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
	mu     sync.Mutex
	clients map[string]*Client // satu Client per model: thread web terpisah per model
	token   string
	modelID string

	// streamMu menserialkan seluruh ChatStream. DeepSeek web menyimpan konteks
	// percakapan server-side (chat_session_id + parent_message_id), jadi dua
	// request bersamaan (mis. title + main dari OpenCode) akan saling
	// menimpa rantai pesan dan menghasilkan respons kosong/rusak.
	streamMu sync.Mutex
}

// New creates a DeepSeek provider. It is not logged in until /login captures
// a session (or a saved session is found on disk).
func New() *Provider {
	return &Provider{clients: make(map[string]*Client)}
}

var _ provider.Provider = (*Provider)(nil)

func (p *Provider) ID() string { return "deepseek" }

func (p *Provider) Name() string { return "DeepSeek Chat" }

func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		SupportsStreaming:      true,
		SupportsTools:          true,
		SupportsSystemRole:     true,
		RequiresSessionLogin:   true,
		SupportsModelSelection: true,
		// DeepSeek web menyimpan konteks server-side, jadi request chat
		// diserialkan satu-satu (lihat streamMu).
		MaxConcurrent: 1,
	}
}

func (p *Provider) Chat(ctx context.Context, model string, messages []provider.Message) (string, error) {
	ch, err := p.ChatStream(ctx, model, messages)
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

func (p *Provider) ChatStream(ctx context.Context, model string, messages []provider.Message) (<-chan provider.Chunk, error) {
	// Seluruh sesi dipakai satu-satu: buat session, kirim pesan, dan baca
	// stream selesai sebelum request berikutnya berjalan. Model dipilih di
	// dalam critical section ini (parameter request), sehingga dua instance
	// dengan model berbeda yang berjalan bersamaan tidak saling menimpa.
	p.streamMu.Lock()

	if model == "" {
		p.mu.Lock()
		model = p.modelID
		p.mu.Unlock()
	}

	client, err := p.getClient(model)
	if err != nil {
		p.streamMu.Unlock()
		return nil, err
	}

	// Request berisi riwayat lengkap (system prompt + pesan) selalu memulai
	// thread web baru: prompt sudah me-flatten seluruh konteks, jadi rantai
	// parent lama hanya menambahkan konteks basi dari sesi-sesi sebelumnya
	// (model jadi bingung / menjawab topik lama). Khusus 1 pesan user (chat
	// multi-turn TUI yang mengirim delta per turn) rantai parent dipertahankan.
	if wantsFreshThread(messages) {
		client.ResetConversation()
	}

	req, err := BuildChatRequest(messages, model, client.parentMessageID())
	if err != nil {
		p.streamMu.Unlock()
		return nil, err
	}

	events, err := client.SendMessage(ctx, req)
	if err != nil {
		p.streamMu.Unlock()
		return nil, err
	}

	ch := make(chan provider.Chunk, 16)
	go func() {
		defer close(ch)
		defer p.streamMu.Unlock()
		for {
			select {
			case ev, ok := <-events:
				if !ok {
					return
				}
				switch ev.Kind {
				case EventIDs:
					client.setParentMessageID(ev.ParentMsgID)
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

// SetModel selects the model used for the next messages. Setelah model
// berubah, thread percakapan di-reset: DeepSeek web mengunci model pada saat
// thread dibuat, jadi ganti model di tengah thread tidak akan berefek tanpa
// session baru.
func (p *Provider) SetModel(modelID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if modelID == "" {
		return
	}
	if modelID != p.modelID {
		p.modelID = modelID
		if c, ok := p.clients[modelID]; ok {
			c.ResetConversation()
		}
	}
}

func (p *Provider) Models() []provider.ModelInfo {
	return []provider.ModelInfo{
		{ID: "deepseek-chat", DisplayName: "DeepSeek-chat-Instant-Think-Search"},
		{ID: "deepseek-reasoner", DisplayName: "DeepSeek-chat-Expert-Think"},
	}
}

// Reset starts a fresh DeepSeek conversation on the next message.
func (p *Provider) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.clients {
		c.ResetConversation()
	}
}

// wantsFreshThread memutuskan apakah request harus memulai thread web baru.
// opencode dan klien API lain mengirim riwayat lengkap (biasanya diawali
// system prompt) dan mengharapkan percakapan independen tiap request —
// prompt-nya sudah di-flatten penuh, jadi thread baru selalu aman. TUI
// mengirim tepat 1 pesan user per turn dan bergantung pada rantai
// parent_message_id untuk kontinuitas, jadi kasus itu memakai thread lama.
func wantsFreshThread(messages []provider.Message) bool {
	return len(messages) != 1 || messages[0].Role != "user"
}

// getClient loads the saved session and (re)builds the HTTP client for a
// model when the token changed (e.g. after a new /login). Setiap model punya
// client sendiri (chat_session_id + parent_message_id terpisah) sehingga dua
// instance OpenCode — satu deepseek-chat, satu deepseek-reasoner — yang jalan
// bersamaan tidak saling menimpa rantai percakapan.
func (p *Provider) getClient(modelID string) (*Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	sess, err := auth.LoadSession("deepseek")
	if err != nil {
		return nil, err
	}
	if sess.Token == "" {
		return nil, fmt.Errorf("session DeepSeek tanpa token, coba /login lagi")
	}
	if p.token != sess.Token {
		// Token berganti (login ulang): buang semua client lama.
		p.clients = make(map[string]*Client)
		p.token = sess.Token
	}
	c, ok := p.clients[modelID]
	if !ok {
		c = NewClient(sess.Token, sess.Cookies)
		p.clients[modelID] = c
	}
	return c, nil
}
