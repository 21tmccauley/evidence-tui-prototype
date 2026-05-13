package runner

import tea "github.com/charmbracelet/bubbletea"

// Runner is the Run screen's execution backend (mock or real subprocess runner).
type Runner interface {
	Init() tea.Cmd
	Update(msg tea.Msg) (Runner, tea.Cmd)
	Start(ids []FetcherID) tea.Cmd
	Cancel(id FetcherID) tea.Cmd
	Retry(id FetcherID) tea.Cmd
	Bind(s Sender)
}
