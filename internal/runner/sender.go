package runner

import tea "github.com/charmbracelet/bubbletea"

// Sender is how the real runner posts tea.Msg from goroutines (*tea.Program implements it; Send is goroutine-safe).
type Sender interface {
	Send(msg tea.Msg)
}
