package screens

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/paramify/evidence-tui-prototype/internal/app"
	"github.com/paramify/evidence-tui-prototype/internal/components"
	"github.com/paramify/evidence-tui-prototype/internal/platforms"
)

// WelcomeModel is the launchpad shown on startup. It summarizes what was
// discovered (platforms, fetchers, .env path) and offers a single action:
// press enter to select fetchers. Secrets is reachable via `s`; quit is
// handled at the root level.
type WelcomeModel struct {
	keys app.KeyMap

	platforms      []platforms.Platform
	envFilePath    string
	envFileLoaded  bool
	envExamplePath string
	platformCount  int
	fetcherCount   int

	width  int
	height int

	logoSheenCol  int
	logoSheenIdle bool
}

func NewWelcome(keys app.KeyMap) WelcomeModel {
	return NewWelcomeWithOptions(keys, WelcomeOptions{})
}

// WelcomeOptions configures the launchpad. Platforms drives the discovery
// summary; EnvFilePath is the .env file the user is told to edit for
// secrets. EnvFileLoaded distinguishes "file present and merged" from
// "we looked here but found nothing" so the summary can tell the user
// where to put their .env. EnvExamplePath is set when a sibling
// .env.example exists alongside the missing .env, so the hint can suggest
// the exact `cp` command.
type WelcomeOptions struct {
	Platforms      []platforms.Platform
	EnvFilePath    string
	EnvFileLoaded  bool
	EnvExamplePath string
}

func NewWelcomeWithOptions(keys app.KeyMap, opts WelcomeOptions) WelcomeModel {
	fetcherCount := 0
	for _, p := range opts.Platforms {
		fetcherCount += len(p.Fetchers)
	}
	return WelcomeModel{
		keys:           keys,
		platforms:      opts.Platforms,
		envFilePath:    strings.TrimSpace(opts.EnvFilePath),
		envFileLoaded:  opts.EnvFileLoaded,
		envExamplePath: strings.TrimSpace(opts.EnvExamplePath),
		platformCount:  len(opts.Platforms),
		fetcherCount:   fetcherCount,
		logoSheenCol:   -app.LogoSheenRadius,
	}
}

// ContinueMsg signals the Welcome → Select transition.
type ContinueMsg struct{}
type OpenSecretsMsg struct{}

type welcomeLogoSheenTickMsg struct{}

type welcomeLogoSheenResumeMsg struct{}

func welcomeLogoSheenSweepCmd() tea.Cmd {
	return tea.Tick(app.LogoSheenSweepStep, func(time.Time) tea.Msg {
		return welcomeLogoSheenTickMsg{}
	})
}

func welcomeLogoSheenIdleCmd() tea.Cmd {
	return tea.Tick(app.LogoSheenIdleBetween, func(time.Time) tea.Msg {
		return welcomeLogoSheenResumeMsg{}
	})
}

func (m WelcomeModel) Init() tea.Cmd {
	return welcomeLogoSheenSweepCmd()
}

func (m WelcomeModel) Update(msg tea.Msg) (WelcomeModel, tea.Cmd) {
	switch msg := msg.(type) {
	case welcomeLogoSheenTickMsg:
		if m.logoSheenIdle {
			return m, nil
		}
		m.logoSheenCol++
		if m.logoSheenCol > app.LogoSheenMaxColumn()+app.LogoSheenRadius {
			m.logoSheenIdle = true
			return m, welcomeLogoSheenIdleCmd()
		}
		return m, welcomeLogoSheenSweepCmd()
	case welcomeLogoSheenResumeMsg:
		m.logoSheenIdle = false
		m.logoSheenCol = -app.LogoSheenRadius
		return m, welcomeLogoSheenSweepCmd()
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Enter):
			return m, func() tea.Msg { return ContinueMsg{} }
		case msg.String() == "s":
			return m, func() tea.Msg { return OpenSecretsMsg{} }
		}
	}
	return m, nil
}

func (m WelcomeModel) Resize(w, h int) WelcomeModel {
	m.width, m.height = w, h
	return m
}

func (m WelcomeModel) View() string {
	width := m.width
	if width <= 0 {
		width = 100
	}
	height := m.height
	if height <= 0 {
		height = 30
	}

	header := components.RenderHeader(components.HeaderProps{
		Width: width,
		Crumb: "welcome",
		Now:   time.Now(),
	})

	logo := app.RenderLogoSheen(m.logoSheenCol)
	tagline := app.StyleAccent.Render("fetcher")
	subtitle := app.StyleSubtle.Render("collect compliance evidence from your stack")

	summary := m.renderSummary(width)

	body := lipgloss.JoinVertical(lipgloss.Left,
		"",
		lipgloss.PlaceHorizontal(width, lipgloss.Center, logo),
		lipgloss.PlaceHorizontal(width, lipgloss.Center, tagline),
		"",
		lipgloss.PlaceHorizontal(width, lipgloss.Center, subtitle),
		"",
		summary,
	)

	hints := []components.Hint{
		{Key: "enter", Desc: "select fetchers"},
		{Key: "s", Desc: "secrets"},
		{Key: "q", Desc: "quit"},
	}
	footer := components.RenderFooter(width, hints)

	used := lipgloss.Height(header) + lipgloss.Height(body) + lipgloss.Height(footer)
	if pad := height - used; pad > 0 {
		body = body + strings.Repeat("\n", pad)
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

// renderSummary lists what was discovered: platform count, fetcher count,
// env file path. Empty when nothing is configured (demo mode without a
// repo) — the user sees the logo and tagline and presses enter to proceed.
func (m WelcomeModel) renderSummary(width int) string {
	if m.platformCount == 0 && m.envFilePath == "" {
		hint := app.StyleSubtle.Render("press enter to continue")
		return lipgloss.PlaceHorizontal(width, lipgloss.Center, hint)
	}

	rows := []string{}
	if m.platformCount > 0 {
		rows = append(rows, fmt.Sprintf("%s  %s",
			padRight(app.StyleSubtle.Render("platforms"), 16),
			app.StyleAccent.Render(fmt.Sprintf("%d", m.platformCount)),
		))
		rows = append(rows, fmt.Sprintf("%s  %s",
			padRight(app.StyleSubtle.Render("fetchers"), 16),
			app.StyleAccent.Render(fmt.Sprintf("%d", m.fetcherCount)),
		))
	}
	if m.envFilePath != "" {
		var value string
		switch {
		case m.envFileLoaded:
			value = app.StyleInfo.Render(m.envFilePath)
		case m.envExamplePath != "":
			value = app.StyleInfo.Render(m.envFilePath) + "  " +
				app.StyleWarning.Render("(not found)")
		default:
			value = app.StyleInfo.Render(m.envFilePath) + "  " +
				app.StyleWarning.Render("(not found — create this file to set values)")
		}
		rows = append(rows, fmt.Sprintf("%s  %s",
			padRight(app.StyleSubtle.Render("env file"), 16),
			value,
		))
		if !m.envFileLoaded && m.envExamplePath != "" {
			rows = append(rows, fmt.Sprintf("%s  %s",
				padRight(app.StyleSubtle.Render(""), 16),
				app.StyleAccent.Render("run: cp "+m.envExamplePath+" "+m.envFilePath),
			))
		}
	}
	block := strings.Join(rows, "\n")
	boxed := app.StyleBorder.Padding(0, 2).Render(block)
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, boxed)
}

func padRight(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}
