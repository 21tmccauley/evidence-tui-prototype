// Logo sheen demo: same animation as the welcome screen (internal/app/logo_sheen.go).
//
//	go run ./cmd/logo-demo
package main

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/paramify/evidence-tui-prototype/internal/app"
)

type tickMsg struct{}

type resumeSweepMsg struct{}

func sweepTickCmd() tea.Cmd {
	return tea.Tick(app.LogoSheenSweepStep, func(time.Time) tea.Msg { return tickMsg{} })
}

func idleCmd() tea.Cmd {
	return tea.Tick(app.LogoSheenIdleBetween, func(time.Time) tea.Msg { return resumeSweepMsg{} })
}

type model struct {
	width  int
	height int
	column int
	idle   bool
}

func newModel() model {
	return model{column: -app.LogoSheenRadius}
}

func (m model) Init() tea.Cmd {
	return sweepTickCmd()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tickMsg:
		if m.idle {
			return m, nil
		}
		m.column++
		if m.column > app.LogoSheenMaxColumn()+app.LogoSheenRadius {
			m.idle = true
			return m, idleCmd()
		}
		return m, sweepTickCmd()
	case resumeSweepMsg:
		m.idle = false
		m.column = -app.LogoSheenRadius
		return m, sweepTickCmd()
	}
	return m, nil
}

func (m model) View() string {
	logo := app.RenderLogoSheen(m.column)

	sub := lipgloss.NewStyle().
		Foreground(app.ColorSubtle).
		Render("sheen demo — same timing as welcome — q quit")

	block := lipgloss.JoinVertical(lipgloss.Left, logo, "", sub)

	w := m.width
	if w <= 0 {
		w = 80
	}
	h := m.height
	if h <= 0 {
		h = 24
	}
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, block)
}

func main() {
	p := tea.NewProgram(newModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println(err)
	}
}
