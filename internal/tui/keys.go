package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Quit         key.Binding
	Tab          key.Binding
	CommandPal   key.Binding
	Slash        key.Binding
	At           key.Binding
	Bang         key.Binding
	Up           key.Binding
	Down         key.Binding
	Enter        key.Binding
	Esc          key.Binding
	JumpEnd      key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c", "esc"),
			key.WithHelp("ctrl+c/esc", "quit"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "providers"),
		),
		CommandPal: key.NewBinding(
			key.WithKeys("ctrl+p"),
			key.WithHelp("ctrl+p", "commands"),
		),
		Slash: key.NewBinding(key.WithKeys("/")),
		At:    key.NewBinding(key.WithKeys("@")),
		Bang:  key.NewBinding(key.WithKeys("!")),
		Up:    key.NewBinding(key.WithKeys("up", "k")),
		Down:  key.NewBinding(key.WithKeys("down", "j")),
		Enter: key.NewBinding(key.WithKeys("enter")),
		Esc:   key.NewBinding(key.WithKeys("esc")),
		JumpEnd: key.NewBinding(
			key.WithKeys("ctrl+alt+g", "end"),
			key.WithHelp("ctrl+alt+g,end", "jump to recent"),
		),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Tab, k.CommandPal, k.Quit}
}
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Tab, k.CommandPal, k.Slash, k.At, k.Bang}, {k.Quit, k.JumpEnd}}
}
