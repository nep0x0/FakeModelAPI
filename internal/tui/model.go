package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

type ChatMsg struct {
	Role string // "user" | "assistant"
	Content string
}

type Model struct {
	width, height int

	// UI state per plan.md
	showLogo bool
	mode string // Build | Plan | Chat
	provider string
	modelName string
	endpoint string
	variant string
	serverOn bool
	version string
	tipIndex int

	// components
	textarea textarea.Model
	viewport viewport.Model
	spinner spinner.Model
	help help.Model
	keys keyMap

	// chat
	messages []ChatMsg
	isStreaming bool
	streamBuffer string
	slashMode bool
	commandList []string
	filteredCmds []string
	tabIndex int
	showCmdPalette bool
	selectedIdx int

	// providers cycle
	providers []string
	models []string
}

func NewModel() Model {
	ta := textarea.New()
	ta.Placeholder = `Type "/" for commands`
	ta.Focus()
	ta.CharLimit = 5000
	ta.SetWidth(60)
	ta.SetHeight(1)
	ta.ShowLineNumbers = false
	ta.Prompt = ""

	vp := viewport.New(0, 0)
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = TipDotStyle

	h := help.New()

	providers := []string{"DeepSeek Chat Free", "Qwen Chat Free", "Gemini Chat Free"}
	modelsList := []string{"DeepSeek V4 Flash Free", "Qwen3 Coder Plus", "Gemini 2.5 Pro"}

	m := Model{
		showLogo: true,
		mode: "Chat",
		provider: providers[0],
		modelName: modelsList[0],
		endpoint: "localhost:8000",
		variant: "max",
		serverOn: false,
		version: "v0.1.0",
		tipIndex: 0,
		textarea: ta,
		viewport: vp,
		spinner: sp,
		help: h,
		keys: newKeyMap(),
		messages: []ChatMsg{},
		isStreaming: false,
		slashMode: false,
		commandList: []string{"/login", "/logout", "/start", "/stop", "/status", "/model", "/clear", "/exit"},
		filteredCmds: []string{"/login", "/logout", "/start", "/stop", "/status", "/model", "/clear", "/exit"},
		tabIndex: 0,
		showCmdPalette: false,
		selectedIdx: 0,
		providers: providers,
		models: modelsList,
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		textarea.Blink,
		tipTick(),
	)
}

func tipTick() tea.Cmd {
	return tea.Tick(10*time.Second, func(t time.Time) tea.Msg {
		return tipRotateMsg{}
	})
}

type tipRotateMsg struct{}
type chatResponseChunkMsg struct{ chunk string }
type chatDoneMsg struct{}
type providerSwitchedMsg struct{}
