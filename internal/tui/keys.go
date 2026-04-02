package tui

import (
	"github.com/charmbracelet/bubbles/key"
)

// KeyMap defines the key bindings for the TUI.
type KeyMap struct {
	Submit   key.Binding
	Quit     key.Binding
	Cancel   key.Binding
	Clear    key.Binding
	Newline  key.Binding
	History  key.Binding
}

// DefaultKeyMap returns the default key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Submit: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "submit"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+d"),
			key.WithHelp("ctrl+d", "quit"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("ctrl+c", "esc"),
			key.WithHelp("ctrl+c/esc", "cancel"),
		),
		Clear: key.NewBinding(
			key.WithKeys("ctrl+l"),
			key.WithHelp("ctrl+l", "clear screen"),
		),
		Newline: key.NewBinding(
			key.WithKeys("shift+enter", "ctrl+enter"),
			key.WithHelp("shift+enter", "newline"),
		),
		History: key.NewBinding(
			key.WithKeys("up", "down"),
			key.WithHelp("↑/↓", "history"),
		),
	}
}

// ShortHelp returns short help for the key bindings.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Submit, k.Quit, k.Cancel, k.Clear}
}

// FullHelp returns full help for the key bindings.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Submit, k.Newline},
		{k.Quit, k.Cancel},
		{k.Clear, k.History},
	}
}
