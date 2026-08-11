package tui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"fakemodelapi/internal/auth"
	"fakemodelapi/internal/config"
	"fakemodelapi/internal/doctor"
	"fakemodelapi/internal/provider"
	"fakemodelapi/internal/server"
	"fakemodelapi/internal/telemetry"

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
		// viewport height = sisa layar setelah konten bawah + status bar,
		// dihitung dari tinggi konten bawah yang sebenarnya supaya total
		// frame pas (tidak overflow yang menggeser layar).
		bottomH := m.bottomContentHeight()
		vh := m.height - 1 - bottomH - 1
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

	case testResultMsg:
		if msg.Err != nil {
			m.messages = append(m.messages, ChatMsg{Role: "assistant", Content: "✗ AI tidak terkoneksi: " + msg.Err.Error()})
		} else {
			reply := msg.Reply
			r := []rune(reply)
			if len(r) > 200 {
				reply = string(r[:200]) + "..."
			}
			m.messages = append(m.messages, ChatMsg{Role: "assistant", Content: fmt.Sprintf("✓ AI terkoneksi (respon dalam %s): %s", msg.Elapsed, reply)})
		}
		m.messages = append(m.messages, ChatMsg{Role: "assistant", Content: "server: " + msg.ServerState})
		m.viewport.SetContent(m.renderChatContent())
		m.viewport.GotoBottom()
		return m, nil

	case doctorDoneMsg:
		m.messages = append(m.messages, ChatMsg{Role: "assistant", Content: msg.result.Render()})
		m.viewport.SetContent(m.renderChatContent())
		m.viewport.GotoBottom()
		return m, nil

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

		if m.slashMode && !m.showCmdPalette {
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
			if m.server != nil {
				_ = m.server.Stop()
			}
			return m, tea.Quit
		case "ctrl+p":
			m.showCmdPalette = !m.showCmdPalette
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
				if m.server != nil {
					_ = m.server.Stop()
				}
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
				case "/test":
					m.messages = append(m.messages, ChatMsg{Role: "user", Content: val})
					m.showLogo = false
					m.textarea.Reset()
					m.slashMode = false
					m.filteredCmds = []string{}
					m.viewport.SetContent(m.renderChatContent())
					m.viewport.GotoBottom()

					if !m.activeProvider.AuthStatus().LoggedIn {
						m.messages = append(m.messages, ChatMsg{Role: "assistant", Content: "✗ tidak terkoneksi: belum login. Jalankan /login dulu."})
						m.viewport.SetContent(m.renderChatContent())
						m.viewport.GotoBottom()
						return m, nil
					}

					serverState := "off"
					if m.server != nil && m.server.Running() {
						serverState = "on (" + m.server.Addr() + ")"
					}
					m.messages = append(m.messages, ChatMsg{Role: "assistant", Content: "⏳ testing koneksi ke AI..."})
					m.viewport.SetContent(m.renderChatContent())
					m.viewport.GotoBottom()
					return m, testConnection(m.activeProvider, serverState)
				case "/exit", "/quit", "q":
					if m.server != nil {
						_ = m.server.Stop()
					}
					return m, tea.Quit
				case "/start":
					if m.server != nil && m.server.Running() {
						m.messages = append(m.messages, ChatMsg{Role: "assistant", Content: "● server sudah berjalan di " + m.server.Addr()})
					} else {
					m.server = server.New(m.providerKeys[m.tabIndex], m.cfg.Port,
						server.WithToken(m.cfg.Token), server.WithTimeout(m.cfg.Timeout),
						server.WithActivityLog(m.activity),
						// Log request jangan sampai ke stdout: TUI memakai
						// alt-screen, baris log akan merusak layar. Aktivitas
						// tetap tercatat di ActivityLog (lihat /logs).
						server.WithLogger(telemetry.NewLoggerTo(io.Discard)))
						if err := m.server.Start(); err != nil {
							m.server = nil
							msg := "✗ gagal start server: " + err.Error()
							if strings.Contains(err.Error(), "address already in use") {
								msg += "\nPort " + fmt.Sprint(m.cfg.Port) + " dipakai proses lain (mungkin instance fakeapi lain masih berjalan). Stop dulu proses itu."
							}
							m.messages = append(m.messages, ChatMsg{Role: "assistant", Content: msg})
						} else {
							m.serverOn = true
							ai := m.activeProvider.AuthStatus()
							login := "not logged in"
							if ai.LoggedIn {
								login = "logged in ✓"
							}
							info := fmt.Sprintf("● server started\nendpoint: http://%s/v1\nprovider: %s\nmodel: %s\nlogin: %s\n\nKonfigurasi OpenCode: baseURL = http://%s/v1, lalu ketik /status untuk detail atau /test untuk cek koneksi AI.",
								m.server.Addr(), m.provider, m.modelName, login, m.server.Addr())
							m.messages = append(m.messages, ChatMsg{Role: "assistant", Content: info})
						}
					}
					m.showLogo = false
					m.textarea.Reset()
					m.slashMode = false
					m.filteredCmds = []string{}
					m.viewport.SetContent(m.renderChatContent())
					m.viewport.GotoBottom()
					return m, nil
				case "/stop":
					wasRunning := m.server != nil && m.server.Running()
					if m.server != nil {
						if err := m.server.Stop(); err != nil {
							m.messages = append(m.messages, ChatMsg{Role: "assistant", Content: "✗ gagal stop server: " + err.Error()})
						}
						m.server = nil
					}
					m.serverOn = false
					if wasRunning {
						m.messages = append(m.messages, ChatMsg{Role: "assistant", Content: "○ server stopped"})
					} else {
						m.messages = append(m.messages, ChatMsg{Role: "assistant", Content: "○ server tidak berjalan"})
					}
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
					if m.server != nil && m.server.Running() {
						server = "on ✓ (" + m.server.Addr() + ")"
					}
					session := ""
					if ai.LoggedIn {
						st, err := auth.GetVault().Status(m.activeProvider.ID())
						if err != nil {
							session = "\nsession: ?"
						} else if st.Expired {
							session = "\nsession: kedaluwarsa ⚠ (jalankan /login ulang)"
						} else {
							session = "\nsession: valid ✓"
							if !st.ExpiresAt.IsZero() {
								session += " (expires " + st.ExpiresAt.Local().Format("02 Jan 15:04") + ")"
							}
						}
					}
					status := fmt.Sprintf("server: %s\nlogin: %s%s\nprovider: %s\nendpoint: localhost:%d\nmodel: %s",
						server, login, session, m.provider, m.cfg.Port, m.modelName)
					m.messages = append(m.messages, ChatMsg{Role: "user", Content: val}, ChatMsg{Role: "assistant", Content: status})
					m.showLogo = false
					m.textarea.Reset()
					m.slashMode = false
					m.filteredCmds = []string{}
					m.viewport.SetContent(m.renderChatContent())
					m.viewport.GotoBottom()
					return m, nil
				case "/doctor":
					m.messages = append(m.messages, ChatMsg{Role: "user", Content: val})
					m.showLogo = false
					m.textarea.Reset()
					m.slashMode = false
					m.filteredCmds = []string{}
					m.viewport.SetContent(m.renderChatContent())
					m.viewport.GotoBottom()
					return m, runDoctor(m)
				case "/config":
					tok := m.cfg.Token
					if tok != "" {
						tok = tokenPreview(tok)
					}
					out := fmt.Sprintf("port: %d\nprovider: %s\ntimeout: %s\ntoken: %s\n\nPrioritas: file ~/.fakeapi/config.json → env FAKEAPI_* → flag CLI. Jalankan `fakeapi config init` di terminal untuk membuat file config.",
						m.cfg.Port, m.cfg.Provider, m.cfg.Timeout, tok)
					m.messages = append(m.messages, ChatMsg{Role: "user", Content: val}, ChatMsg{Role: "assistant", Content: out})
					m.showLogo = false
					m.textarea.Reset()
					m.slashMode = false
					m.filteredCmds = []string{}
					m.viewport.SetContent(m.renderChatContent())
					m.viewport.GotoBottom()
					return m, nil
				case "/logs":
					events := m.activity.Events()
					if len(events) == 0 {
						m.messages = append(m.messages, ChatMsg{Role: "user", Content: val},
							ChatMsg{Role: "assistant", Content: "belum ada aktivitas tercatat."})
					} else {
						var sb strings.Builder
						sb.WriteString("log aktivitas (terbaru akhir):\n")
						start := 0
						if len(events) > 30 {
							start = len(events) - 30
							sb.WriteString(fmt.Sprintf("(menampilkan %d dari %d event)\n", len(events)-start, len(events)))
						}
						for _, e := range events[start:] {
							line := fmt.Sprintf("%s [%s] %s", e.Time.Local().Format("15:04:05"), e.Kind, e.Summary)
							if e.Err != "" {
								line += " — err: " + e.Err
							}
							sb.WriteString(line + "\n")
						}
						m.messages = append(m.messages, ChatMsg{Role: "user", Content: val},
							ChatMsg{Role: "assistant", Content: sb.String()})
					}
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
			m.messages = append(m.messages, ChatMsg{Role: "user", Content: val},
				ChatMsg{Role: "assistant", Content: "TUI ini untuk kontrol server. Chat ke AI dilakukan lewat OpenCode (server di localhost:8000). Gunakan /test untuk cek koneksi AI, atau ketik / untuk daftar perintah."})
			m.showLogo = false
			m.textarea.Reset()
			m.slashMode = false
			m.filteredCmds = []string{}
			m.viewport.SetContent(m.renderChatContent())
			m.viewport.GotoBottom()
			return m, nil
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
		if oldVal != "" && newVal == "" {
			m.slashMode = false
			m.filteredCmds = []string{}
		}
		if !strings.HasPrefix(newVal, "/") && m.slashMode && newVal == "" {
			m.slashMode = false
			m.filteredCmds = []string{}
		}
		// jika tidak mulai dengan "/" pastikan tertutup
		if !strings.HasPrefix(newVal, "/") {
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

// tokenPreview menyembunyikan token di tampilan, hanya sisa 2 karakter
// pertama dan terakhir yang ditampilkan.
func tokenPreview(tok string) string {
	if tok == "" {
		return "(kosong)"
	}
	if len(tok) <= 4 {
		return "***"
	}
	return tok[:2] + "***" + tok[len(tok)-2:]
}

// testConnection mengirim ping singkat ke provider dan mengembalikan hasilnya
// ke TUI sebagai testResultMsg.
func testConnection(p provider.Provider, serverState string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		start := time.Now()
		reply, err := p.Chat(ctx, "", []provider.Message{{Role: "user", Content: "ping"}})
		elapsed := time.Since(start).Round(10 * time.Millisecond)

		if err != nil {
			return testResultMsg{ServerState: serverState, Err: err, Elapsed: elapsed}
		}
		return testResultMsg{ServerState: serverState, Reply: reply, Elapsed: elapsed}
	}
}

// testResultMsg membawa hasil /test ke TUI.
type testResultMsg struct {
	ServerState string
	Reply       string
	Err         error
	Elapsed     time.Duration
}

// runDoctor menjalankan pemeriksaan doctor di goroutine dan mengirim
// hasilnya ke TUI sebagai doctorDoneMsg.
func runDoctor(m Model) tea.Cmd {
	return func() tea.Msg {
		cfg := config.Config{Provider: m.activeProvider.ID(), Port: m.cfg.Port, Timeout: m.cfg.Timeout, Token: m.cfg.Token}
		res := doctor.All(context.Background(), cfg, m.activeProvider)
		return doctorDoneMsg{result: res}
	}
}

// doctorDoneMsg membawa hasil /doctor ke TUI.
type doctorDoneMsg struct {
	result doctor.Result
}

// startStream initiates a streaming request to the provider and returns the command
// that starts reading from the channel.
func startStream(p provider.Provider, content string) tea.Cmd {
	ctx := context.Background()
	msgs := []provider.Message{{Role: "user", Content: content}}

	ch, err := p.ChatStream(ctx, "", msgs)
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
