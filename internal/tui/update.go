package tui

import (
	"strings"
	"time"

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
		m.streamBuffer += msg.chunk
		if len(m.messages) > 0 && m.messages[len(m.messages)-1].Role == "assistant" {
			m.messages[len(m.messages)-1].Content = m.streamBuffer
		}
		m.viewport.SetContent(m.renderChatContent())
		m.viewport.GotoBottom()
		full := "Ini balasan dummy dari " + m.provider + ": '" + m.messages[len(m.messages)-2].Content + "' diterima. Server " + map[bool]string{true: "on", false: "off"}[m.serverOn] + " di " + m.endpoint + " dengan model " + m.modelName + "."
		if len(m.streamBuffer) < len(full) {
			remaining := full[len(m.streamBuffer):]
			n := 6
			if len(remaining) < n {
				n = len(remaining)
			}
			chunk := remaining[:n]
			cmds = append(cmds, func() tea.Msg {
				time.Sleep(35 * time.Millisecond)
				return chatResponseChunkMsg{chunk: chunk}
			})
		} else {
			cmds = append(cmds, func() tea.Msg {
				time.Sleep(50 * time.Millisecond)
				return chatDoneMsg{}
			})
		}
		return m, tea.Batch(cmds...)

	case chatDoneMsg:
		m.isStreaming = false
		m.streamBuffer = ""
		m.viewport.SetContent(m.renderChatContent())
		m.viewport.GotoBottom()
		return m, tea.Batch(cmds...)

	case tea.KeyMsg:
		if m.showCmdPalette {
			switch msg.String() {
			case "esc", "ctrl+p":
				m.showCmdPalette = false
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
				if filter!= "" {
					var f []string
					for _, c := range m.commandList {
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
					status := "server: off\nproviders: DeepSeek, Qwen, Gemini\nendpoint: localhost:8000"
					if m.serverOn {
						status = "server: on ✓\nproviders: DeepSeek, Qwen, Gemini\nendpoint: localhost:8000\nmodel: " + m.modelName
					}
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
					m.showLogo = true
					m.textarea.Reset()
					m.slashMode = false
					m.filteredCmds = []string{}
					m.viewport.SetContent("")
					return m, nil
				case "/login":
					m.messages = append(m.messages, ChatMsg{Role: "user", Content: val}, ChatMsg{Role: "assistant", Content: "Opening browser for login... (dummy auth ready)"})
					m.showLogo = false
					m.textarea.Reset()
					m.slashMode = false
					m.filteredCmds = []string{}
					m.viewport.SetContent(m.renderChatContent())
					m.viewport.GotoBottom()
					return m, nil
				case "/model":
					m.showCmdPalette = true
					m.filteredCmds = []string{"DeepSeek V4 Flash Free", "Qwen3 Coder Plus", "Gemini 2.5 Pro"}
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
			provider := m.provider
			return m, func() tea.Msg {
				time.Sleep(100 * time.Millisecond)
				return chatResponseChunkMsg{chunk: "Ini balasan dummy dari " + provider + ": "}
			}
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
