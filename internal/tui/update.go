package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"fakemodelapi/internal/auth"
	"fakemodelapi/internal/provider"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		inputW := m.width - 20
		if inputW > 68 {
			inputW = 68
		}
		if inputW < 40 {
			inputW = 40
		}
		m.textarea.SetWidth(inputW - 4)
		m.viewport.Width = inputW
		// viewport height = topAvail (height - bottom), bukan fixed 14 biar logo gak geser
		// bottom kira2 6 baris: input(2)+hint(1)+gap(2)+tip(1) = ~6
		bottomReserve := 8
		vh := m.height - 1 - bottomReserve
		if vh < 3 {
			vh = 3
		}
		m.viewport.Height = vh
		m.help.Width = m.width
		return m, nil

	case tipRotateMsg:
		m.tipIndex++
		if m.tipIndex > 10 {
			m.tipIndex = 0
		}
		return m, tipTick()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case chatResponseChunkMsg:
		// This message is sent from the stream goroutine; just update the buffer.
		m.streamBuffer += msg.chunk
		if len(m.messages) > 0 && m.messages[len(m.messages)-1].Role == "assistant" {
			m.messages[len(m.messages)-1].Content = m.streamBuffer
		}
		m.viewport.SetContent(m.renderChatContent())
		m.viewport.GotoBottom()
		// Schedule reading the next chunk from the provider's channel.
		if m.streamChan != nil {
			cmds = append(cmds, readNextChunk(m.streamChan))
		}
		return m, tea.Batch(cmds...)

	case streamStartMsg:
		m.streamChan = msg.ch
		if msg.chunk.Delta != "" {
			m.streamBuffer += msg.chunk.Delta
			if len(m.messages) > 0 && m.messages[len(m.messages)-1].Role == "assistant" {
				m.messages[len(m.messages)-1].Content = m.streamBuffer
			}
		}
		m.viewport.SetContent(m.renderChatContent())
		m.viewport.GotoBottom()
		cmds = append(cmds, readNextChunk(m.streamChan))
		return m, tea.Batch(cmds...)

	case chatDoneMsg:
		m.isStreaming = false
		m.streamBuffer = ""
		m.streamChan = nil
		m.viewport.SetContent(m.renderChatContent())
		m.viewport.GotoBottom()
		return m, tea.Batch(cmds...)

	case authDoneMsg:
		if msg.result.Error != nil {
			m.messages = append(m.messages, ChatMsg{Role: "assistant", Content: "login gagal: " + msg.result.Error.Error()})
		} else {
			// Simpan session (cookies + bearer token)
			err := auth.SaveSessionWithToken(msg.provider, msg.result.Token, msg.result.Cookies)
			if err != nil {
				m.messages = append(m.messages, ChatMsg{Role: "assistant", Content: "gagal menyimpan session: " + err.Error()})
			} else if msg.result.Token == "" {
				m.messages = append(m.messages, ChatMsg{Role: "assistant", Content: fmt.Sprintf("login berhasil ✓, %d cookies disimpan (tanpa token)", len(msg.result.Cookies))})
			} else {
				m.messages = append(m.messages, ChatMsg{Role: "assistant", Content: fmt.Sprintf("login berhasil ✓, %d cookies + token disimpan", len(msg.result.Cookies))})
			}
		}
		m.viewport.SetContent(m.renderChatContent())
		m.viewport.GotoBottom()
		return m, nil

	case loginMsgInit:
		m.loginProvider = msg.provider
		m.loginProgress = msg.progress
		m.loginDone = msg.done
		return m, m.nextLoginMsg()

	case authProgressMsg:
		// Update pesan terakhir assistant dengan progress
		if len(m.messages) > 0 && m.messages[len(m.messages)-1].Role == "assistant" {
			m.messages[len(m.messages)-1].Content = msg.text
		}
		m.viewport.SetContent(m.renderChatContent())
		m.viewport.GotoBottom()
		// Lanjutkan mendengarkan progress dari goroutine login
		return m, m.nextLoginMsg()

	case tea.KeyMsg:
		if m.showCmdPalette {
			switch msg.String() {
			case "esc", "ctrl+p":
				m.showCmdPalette = false
				m.paletteMode = ""
				return m, nil
			case "up":
				if m.selectedIdx > 0 {
					m.selectedIdx--
				}
				return m, nil
			case "down":
				if m.selectedIdx < len(m.filteredCmds)-1 {
					m.selectedIdx++
				}
				return m, nil
			case "enter":
				if m.paletteMode == "models" {
					models := m.activeProvider.Models()
					if m.selectedIdx >= 0 && m.selectedIdx < len(models) {
						sel := models[m.selectedIdx]
						m.activeProvider.SetModel(sel.ID)
						m.modelName = sel.DisplayName
						m.showCmdPalette = false
						m.paletteMode = ""
						m.filteredCmds = []string{}
						m.selectedIdx = 0
					}
					return m, nil
				}
				if len(m.filteredCmds) > 0 {
					cmd := m.filteredCmds[m.selectedIdx]
					m.textarea.SetValue(cmd + " ")
					m.showCmdPalette = false
					m.selectedIdx = 0
					// FIX 2: setelah pilih, dropdown harus hilang
					m.slashMode = false
					m.filteredCmds = []string{}
				}
				return m, nil
			default:
				filter := m.textarea.Value()
				if filter != "" {
					list := m.commandList
					if m.paletteMode == "models" {
						list = nil
						for _, mo := range m.activeProvider.Models() {
							list = append(list, mo.DisplayName)
						}
					}
					var f []string
					for _, c := range list {
						if strings.Contains(c, strings.TrimSpace(filter)) {
							f = append(f, c)
						}
					}
					if len(f) > 0 {
						m.filteredCmds = f
					}
				}
			}
		}

		if m.slashMode &&!m.showCmdPalette {
			switch msg.String() {
			case "up":
				if m.selectedIdx > 0 {
					m.selectedIdx--
				}
				return m, nil
			case "down":
				if m.selectedIdx < len(m.filteredCmds)-1 {
					m.selectedIdx++
				}
				return m, nil
			case "esc":
				m.slashMode = false
				m.selectedIdx = 0
				m.filteredCmds = []string{}
				return m, nil
			case "enter", "tab":
				if len(m.filteredCmds) > 0 && m.selectedIdx < len(m.filteredCmds) {
					// FIX 2: pilih dari dropdown -> isi textarea + tutup dropdown
					m.textarea.SetValue(m.filteredCmds[m.selectedIdx] + " ")
					m.slashMode = false
					m.filteredCmds = []string{}
					m.selectedIdx = 0
					return m, nil
				}
			}
		}

		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "ctrl+p":
			m.showCmdPalette =!m.showCmdPalette
			m.filteredCmds = m.commandList
			m.selectedIdx = 0
			return m, nil
		case "tab":
			m.tabIndex = (m.tabIndex + 1) % len(m.providers)
			m.provider = m.providers[m.tabIndex]
			m.modelName = m.models[m.tabIndex]
			m.activeProvider = provider.MustGet(m.providerKeys[m.tabIndex])
			m.activeProvider.Reset()
			return m, nil
		case "esc":
			if m.textarea.Value() == "" && len(m.messages) == 0 {
				return m, tea.Quit
			}
			if m.slashMode {
				m.slashMode = false
				m.filteredCmds = []string{}
				return m, nil
			}
		case "enter":
			if m.isStreaming {
				return m, nil
			}
			val := strings.TrimSpace(m.textarea.Value())
			if val == "" {
				return m, nil
			}
			if strings.HasPrefix(val, "/") {
				cmd := strings.Fields(val)[0]
				switch cmd {
				case "/exit", "/quit", "q":
					return m, tea.Quit
				case "/start":
					m.serverOn = true
					m.messages = append(m.messages, ChatMsg{Role: "assistant", Content: "● server started at localhost:8000 (dummy)"})
					m.showLogo = false
					m.textarea.Reset()
					m.slashMode = false
					m.filteredCmds = []string{}
					m.viewport.SetContent(m.renderChatContent())
					m.viewport.GotoBottom()
					return m, nil
				case "/stop":
					m.serverOn = false
					m.messages = append(m.messages, ChatMsg{Role: "assistant", Content: "○ server stopped"})
					m.showLogo = false
					m.textarea.Reset()
					m.slashMode = false
					m.filteredCmds = []string{}
					m.viewport.SetContent(m.renderChatContent())
					m.viewport.GotoBottom()
					return m, nil
				case "/status":
					ai := m.activeProvider.AuthStatus()
					login := "not logged in"
					if ai.LoggedIn {
						login = "logged in (" + ai.Username + ")"
					}
					server := "off"
					if m.serverOn {
						server = "on ✓"
					}
					status := fmt.Sprintf("server: %s\nlogin: %s\nprovider: %s\nendpoint: localhost:8000\nmodel: %s",
						server, login, m.provider, m.modelName)
					m.messages = append(m.messages, ChatMsg{Role: "user", Content: val}, ChatMsg{Role: "assistant", Content: status})
					m.showLogo = false
					m.textarea.Reset()
					m.slashMode = false
					m.filteredCmds = []string{}
					m.viewport.SetContent(m.renderChatContent())
					m.viewport.GotoBottom()
					return m, nil
				case "/clear":
					m.messages = []ChatMsg{}
					m.activeProvider.Reset()
					m.showLogo = true
					m.textarea.Reset()
					m.slashMode = false
					m.filteredCmds = []string{}
					m.viewport.SetContent("")
					return m, nil
				case "/login":
					cfg, ok := loginConfigs[m.providerKeys[m.tabIndex]]
					if !ok || cfg.URL == "" {
						m.messages = append(m.messages, ChatMsg{Role: "user", Content: val},
							ChatMsg{Role: "assistant", Content: "provider ini belum punya fitur login"})
						m.showLogo = false
						m.textarea.Reset()
						m.slashMode = false
						m.filteredCmds = []string{}
						m.viewport.SetContent(m.renderChatContent())
						m.viewport.GotoBottom()
						return m, nil
					}
					m.messages = append(m.messages, ChatMsg{Role: "user", Content: val})
					m.showLogo = false
					m.textarea.Reset()
					m.slashMode = false
					m.filteredCmds = []string{}
					m.viewport.SetContent(m.renderChatContent())
					m.viewport.GotoBottom()
					m.messages = append(m.messages, ChatMsg{Role: "assistant", Content: "membuka browser..."})
					m.viewport.SetContent(m.renderChatContent())
					m.viewport.GotoBottom()
					return m, startLogin(cfg, m.providerKeys[m.tabIndex])
				case "/model":
					m.showCmdPalette = true
					m.paletteMode = "models"
					m.filteredCmds = nil
					for _, mo := range m.activeProvider.Models() {
						m.filteredCmds = append(m.filteredCmds, mo.DisplayName)
					}
					m.selectedIdx = 0
					return m, nil
				default:
					m.messages = append(m.messages, ChatMsg{Role: "user", Content: val}, ChatMsg{Role: "assistant", Content: "Unknown command: " + cmd + " (dummy)"})
					m.showLogo = false
					m.textarea.Reset()
					m.slashMode = false
					m.filteredCmds = []string{}
					m.viewport.SetContent(m.renderChatContent())
					m.viewport.GotoBottom()
					return m, nil
				}
			}
			m.messages = append(m.messages, ChatMsg{Role: "user", Content: val})
			m.showLogo = false
			m.textarea.Reset()
			m.slashMode = false
			m.filteredCmds = []string{}
			m.isStreaming = true
			m.streamBuffer = ""
			m.messages = append(m.messages, ChatMsg{Role: "assistant", Content: ""})
			m.viewport.SetContent(m.renderChatContent())
			m.viewport.GotoBottom()
			return m, startStream(m.activeProvider, val)
		}
	}

	var cmd tea.Cmd
	oldVal := m.textarea.Value()
	m.textarea, cmd = m.textarea.Update(msg)
	cmds = append(cmds, cmd)

	newVal := m.textarea.Value()
	trimmed := strings.TrimSpace(newVal)

	// FIX 2: logika slash — hilang jika exact match atau sudah sempurna
	if strings.HasPrefix(newVal, "/") {
		isExact := false
		for _, c := range m.commandList {
			if trimmed == c {
				isExact = true
				break
			}
		}
		if isExact {
			// sudah ketik sempurna -> dropdown hilang
			m.slashMode = false
			m.filteredCmds = []string{}
			m.selectedIdx = 0
		} else {
			m.slashMode = true
			prefix := strings.Fields(newVal)[0]
			var filtered []string
			if prefix == "/" {
				filtered = m.commandList
			} else {
				for _, c := range m.commandList {
					if strings.HasPrefix(c, prefix) {
						filtered = append(filtered, c)
					}
				}
				if len(filtered) == 0 {
					for _, c := range m.commandList {
						if strings.Contains(c, prefix) {
							filtered = append(filtered, c)
						}
					}
				}
			}
			if len(filtered) > 0 {
				m.filteredCmds = filtered
				if m.selectedIdx >= len(m.filteredCmds) {
					m.selectedIdx = 0
				}
			} else {
				// tidak ada yang cocok -> tutup
				m.slashMode = false
				m.filteredCmds = []string{}
			}
		}
	} else {
		if oldVal!= "" && newVal == "" {
			m.slashMode = false
			m.filteredCmds = []string{}
		}
		if!strings.HasPrefix(newVal, "/") && m.slashMode && newVal == "" {
			m.slashMode = false
			m.filteredCmds = []string{}
		}
		// jika tidak mulai dengan "/" pastikan tertutup
		if!strings.HasPrefix(newVal, "/") {
			m.slashMode = false
		}
	}

	if m.selectedIdx >= len(m.filteredCmds) {
		m.selectedIdx = 0
	}
	if m.selectedIdx < 0 {
		m.selectedIdx = 0
	}

	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

func (m *Model) renderChatContent() string {
	var b strings.Builder
	for _, msg := range m.messages {
		if msg.Role == "user" {
			b.WriteString(UserMsgStyle.Render("> "+msg.Content) + "\n\n")
		} else {
			content := msg.Content
			if content == "" && m.isStreaming {
				content = m.spinner.View() + " thinking..."
			}
			b.WriteString(AssistantMsgStyle.Render(content) + "\n\n")
		}
	}
	return b.String()
}

var _ viewport.Model
var _ textarea.Model
var _ spinner.Model

// authDoneMsg is sent when the browser login process finishes.
type authDoneMsg struct {
	result   *auth.CaptureResult
	provider string
}

// authProgressMsg carries progress updates from the login goroutine.
type authProgressMsg struct {
	text string
}

// loginConfigs maps registry provider names to their browser capture settings.
var loginConfigs = map[string]auth.CaptureSessionInfo{
	"deepseek": {
		URL:             "https://chat.deepseek.com",
		Domain:          ".deepseek.com",
		TokenStorageKey: "userToken",
	},
}

// startLogin kicks off the browser login flow. The actual capture runs in a
// goroutine; progress updates and the final result are relayed to the TUI
// through channels consumed by nextLoginMsg.
func startLogin(cfg auth.CaptureSessionInfo, providerName string) tea.Cmd {
	return func() tea.Msg {
		return beginCapture(cfg, providerName)
	}
}

func beginCapture(cfg auth.CaptureSessionInfo, providerName string) tea.Msg {
	progress := make(chan string, 10)
	done := make(chan *auth.CaptureResult, 1)

	go func() {
		res := auth.CaptureSession(cfg, 2*time.Minute, progress)
		done <- res
	}()

	return loginMsgInit{cfg: cfg, provider: providerName, progress: progress, done: done}
}

type loginMsgInit struct {
	cfg      auth.CaptureSessionInfo
	provider string
	progress chan string
	done     chan *auth.CaptureResult
}

// nextLoginMsg returns a command that waits for the next login event.
func (m *Model) nextLoginMsg() tea.Cmd {
	return func() tea.Msg {
		select {
		case txt := <-m.loginProgress:
			return authProgressMsg{text: txt}
		case res := <-m.loginDone:
			return authDoneMsg{result: res, provider: m.loginProvider}
		}
	}
}

// startStream initiates a streaming request to the provider and returns the command
// that starts reading from the channel.
func startStream(p provider.Provider, content string) tea.Cmd {
	ctx := context.Background()
	msgs := []provider.Message{{Role: "user", Content: content}}

	ch, err := p.ChatStream(ctx, msgs)
	if err != nil {
		return func() tea.Msg {
			return chatResponseChunkMsg{chunk: "Error: " + err.Error()}
		}
	}

	// The first read sets streamChan on the model, which subsequent reads use.
	return func() tea.Msg {
		chunk, ok := <-ch
		if !ok {
			return chatDoneMsg{}
		}
		return streamStartMsg{ch: ch, chunk: chunk}
	}
}

// streamStartMsg is sent after the first chunk is read, carrying the channel forward.
type streamStartMsg struct {
	ch    <-chan provider.Chunk
	chunk provider.Chunk
}

// readNextChunk returns a command that reads the next chunk from a provider's channel.
func readNextChunk(ch <-chan provider.Chunk) tea.Cmd {
	return func() tea.Msg {
		chunk, ok := <-ch
		if !ok {
			return chatDoneMsg{}
		}
		if chunk.Delta != "" {
			return chatResponseChunkMsg{chunk: chunk.Delta}
		}
		// Skip empty deltas (like finish-only chunks)
		return chatDoneMsg{}
	}
}
