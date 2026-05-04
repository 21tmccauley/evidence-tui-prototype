package app

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Left   key.Binding
	Right  key.Binding
	Enter  key.Binding
	Space  key.Binding
	Tab    key.Binding
	Filter key.Binding
	All    key.Binding
	Pin    key.Binding
	Cancel key.Binding
	Retry  key.Binding
	Upload key.Binding
	Export key.Binding
	Back   key.Binding
	Help   key.Binding
	Quit   key.Binding
}

func DefaultKeys() KeyMap {
	return KeyMap{
		Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Left:   key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "left")),
		Right:  key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "right")),
		Enter:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
		Space:  key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")),
		Tab:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next pane")),
		Filter: key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		All:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "select all")),
		Pin:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pin output")),
		Cancel: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "cancel")),
		Retry:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "retry")),
		Upload: key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "upload")),
		Export: key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "export")),
		Back:   key.NewBinding(key.WithKeys("esc", "b"), key.WithHelp("esc", "back")),
		Help:   key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}
