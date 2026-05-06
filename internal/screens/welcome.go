package screens

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/paramify/evidence-tui-prototype/internal/app"
	"github.com/paramify/evidence-tui-prototype/internal/components"
	"github.com/paramify/evidence-tui-prototype/internal/preflight"
)

type Profile struct {
	Name   string
	Region string
	Note   string
}

func defaultProfiles() []Profile {
	return []Profile{
		{Name: "paramify-prod", Region: "us-east-1", Note: "production tenant — read-only role"},
		{Name: "paramify-staging", Region: "us-west-2", Note: "staging tenant"},
		{Name: "customer-acme", Region: "us-east-1", Note: "ACME Corp engagement"},
		{Name: "demo (no aws)", Region: "—", Note: "offline demo, runs against fixtures"},
	}
}

type WelcomeModel struct {
	profiles   []Profile
	tools      []preflight.ToolStatus
	credential *preflight.Service
	cursor     int
	keys       app.KeyMap
	width      int
	height     int

	checking     bool
	loginRunning bool
	status       string
	statusError  bool
	ssoReady     bool

	logoSheenCol  int
	logoSheenIdle bool
}

func NewWelcome(keys app.KeyMap) WelcomeModel {
	return NewWelcomeWithOptions(keys, WelcomeOptions{})
}

type WelcomeOptions struct {
	Profiles    []Profile
	Tools       []preflight.ToolStatus
	Credential  *preflight.Service
	InitialName string
}

func NewWelcomeWithOptions(keys app.KeyMap, opts WelcomeOptions) WelcomeModel {
	profiles := opts.Profiles
	if len(profiles) == 0 {
		profiles = defaultProfiles()
	}
	cursor := 0
	if opts.InitialName != "" {
		for i, p := range profiles {
			if p.Name == opts.InitialName {
				cursor = i
				break
			}
		}
	}
	return WelcomeModel{
		profiles:     profiles,
		tools:        opts.Tools,
		credential:   opts.Credential,
		cursor:       cursor,
		keys:         keys,
		logoSheenCol: -app.LogoSheenRadius,
	}
}

type SelectedProfileMsg struct{ Profile Profile }
type OpenSecretsMsg struct{}

type profileCheckDoneMsg struct {
	profile Profile
	result  preflight.Result
}

type ssoLoginDoneMsg struct {
	profile Profile
	err     error
}

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
	case profileCheckDoneMsg:
		m.checking = false
		m.ssoReady = false
		if msg.result.OK {
			if msg.result.FromCache {
				m.status = "AWS credentials verified from cache"
			} else {
				m.status = "AWS credentials verified"
			}
			m.statusError = false
			return m, func() tea.Msg { return SelectedProfileMsg{Profile: msg.profile} }
		}
		m.statusError = true
		m.ssoReady = msg.result.SSOError
		if msg.result.Err != nil {
			m.status = msg.result.Err.Error()
		} else {
			m.status = "AWS credential check failed"
		}
	case ssoLoginDoneMsg:
		m.loginRunning = false
		if msg.err != nil {
			m.statusError = true
			m.status = msg.err.Error()
			return m, nil
		}
		m.status = "AWS SSO login completed; checking credentials again"
		m.statusError = false
		m.checking = true
		return m, m.checkProfileCmd(msg.profile)
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Up):
			if !m.busy() && m.cursor > 0 {
				m.cursor--
				m.clearStatus()
			}
		case key.Matches(msg, m.keys.Down):
			if !m.busy() && m.cursor < len(m.profiles)-1 {
				m.cursor++
				m.clearStatus()
			}
		case key.Matches(msg, m.keys.Enter):
			if m.busy() || len(m.profiles) == 0 {
				return m, nil
			}
			p := m.profiles[m.cursor]
			return m, func() tea.Msg {
				return SelectedProfileMsg{Profile: p}
			}
		case msg.String() == "o":
			if !m.ssoReady || m.busy() || len(m.profiles) == 0 {
				return m, nil
			}
			p := m.profiles[m.cursor]
			m.loginRunning = true
			m.statusError = false
			m.status = fmt.Sprintf("running aws sso login for %s", p.Name)
			return m, m.loginCmd(p)
		case msg.String() == "s":
			if m.busy() {
				return m, nil
			}
			return m, func() tea.Msg { return OpenSecretsMsg{} }
		}
	}
	return m, nil
}

func (m WelcomeModel) checkProfileCmd(p Profile) tea.Cmd {
	service := m.credential
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return profileCheckDoneMsg{profile: p, result: service.CheckAWS(ctx, p.Name, cleanRegion(p.Region))}
	}
}

func (m WelcomeModel) loginCmd(p Profile) tea.Cmd {
	service := m.credential
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		return ssoLoginDoneMsg{profile: p, err: service.LoginAWS(ctx, p.Name)}
	}
}

func (m WelcomeModel) busy() bool {
	return m.checking || m.loginRunning
}

func (m *WelcomeModel) clearStatus() {
	m.status = ""
	m.statusError = false
	m.ssoReady = false
}

func cleanRegion(region string) string {
	if region == "—" {
		return ""
	}
	return region
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
	subtitle := app.StyleSubtle.Render(
		"collect compliance evidence from your stack",
	)

	rows := make([]string, 0, len(m.profiles))
	for i, p := range m.profiles {
		left := app.StyleAccent.Render(p.Name)
		mid := app.StyleInfo.Render(p.Region)
		right := app.StyleSubtle.Render(p.Note)
		row := lipgloss.JoinHorizontal(lipgloss.Top,
			padRight(left, 24), "  ",
			padRight(mid, 12), "  ",
			right,
		)
		style := app.StyleBorder.Width(width - 8)
		if i == m.cursor {
			style = app.StyleBorderActive.Width(width - 8)
			row = lipgloss.JoinHorizontal(lipgloss.Top,
				app.StyleAccent.Render("▸ "),
				row,
			)
		} else {
			row = lipgloss.JoinHorizontal(lipgloss.Top, "  ", row)
		}
		rows = append(rows, style.Render(row))
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		"",
		lipgloss.PlaceHorizontal(width, lipgloss.Center, logo),
		lipgloss.PlaceHorizontal(width, lipgloss.Center, tagline),
		"",
		lipgloss.PlaceHorizontal(width, lipgloss.Center, subtitle),
		"",
		lipgloss.PlaceHorizontal(width, lipgloss.Center, app.StyleTitle.Render("select a profile")),
		"",
		lipgloss.JoinVertical(lipgloss.Left, rows...),
		m.statusView(width),
		m.toolsView(width),
	)

	hints := []components.Hint{
		{Key: "↑/↓", Desc: "move"},
		{Key: "enter", Desc: "continue"},
		{Key: "s", Desc: "secrets"},
	}
	if m.ssoReady {
		hints = append(hints, components.Hint{Key: "o", Desc: "sso login"})
	}
	hints = append(hints, components.Hint{Key: "q", Desc: "quit"})
	footer := components.RenderFooter(width, hints)

	used := lipgloss.Height(header) + lipgloss.Height(body) + lipgloss.Height(footer)
	if pad := height - used; pad > 0 {
		body = body + strings.Repeat("\n", pad)
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m WelcomeModel) statusView(width int) string {
	if m.status == "" {
		if m.credential == nil {
			return "\n" + lipgloss.PlaceHorizontal(width, lipgloss.Center,
				app.StyleSubtle.Render("demo mode: AWS credential pre-flight skipped"),
			)
		}
		return "\n" + lipgloss.PlaceHorizontal(width, lipgloss.Center,
			app.StyleSubtle.Render("press enter to continue; credentials are checked for selected fetchers"),
		)
	}
	style := app.StyleInfo
	if m.statusError {
		style = app.StyleDanger
	}
	msg := style.Render(m.status)
	if m.ssoReady {
		msg += "\n" + app.StyleAccent.Render("press o to run aws sso login for this profile")
	}
	return "\n" + lipgloss.PlaceHorizontal(width, lipgloss.Center, msg)
}

func (m WelcomeModel) toolsView(width int) string {
	if len(m.tools) == 0 {
		return ""
	}
	missing := []string{}
	for _, tool := range m.tools {
		if !tool.Found {
			missing = append(missing, tool.Name)
		}
	}
	if len(missing) == 0 {
		return "\n" + lipgloss.PlaceHorizontal(width, lipgloss.Center,
			app.StyleSuccess.Render("tools ready: aws jq bash python3 kubectl curl"),
		)
	}
	msg := "missing optional tools: " + strings.Join(missing, ", ") + " (you can continue)"
	return "\n" + lipgloss.PlaceHorizontal(width, lipgloss.Center, app.StyleWarning.Render(msg))
}

func padRight(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}
