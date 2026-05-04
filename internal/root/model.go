package root

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/paramify/evidence-tui-prototype/internal/app"
	"github.com/paramify/evidence-tui-prototype/internal/runner"
	"github.com/paramify/evidence-tui-prototype/internal/screens"
	"github.com/paramify/evidence-tui-prototype/internal/uploader"
)

type Screen int

const (
	ScreenWelcome Screen = iota
	ScreenSelect
	ScreenRun
	ScreenReview
)

type Model struct {
	keys        app.KeyMap
	screen      Screen
	showHelp    bool
	width       int
	height      int
	profile     string
	region      string
	runner      runner.Runner
	welcomeOpts screens.WelcomeOptions
	evidenceDir string
	paramify    uploader.Uploader

	welcome screens.WelcomeModel
	sel     screens.SelectModel
	run     screens.RunModel
	review  screens.ReviewModel
}

// New constructs the root model with the given runner.
func New(r runner.Runner) Model {
	return NewWithOptions(r, Options{})
}

type Options struct {
	Welcome screens.WelcomeOptions

	// EvidenceDir is shown on Review when non-empty (empty in demo).
	EvidenceDir string

	// Paramify enables upload from Review when non-nil.
	Paramify uploader.Uploader
}

func NewWithOptions(r runner.Runner, opts Options) Model {
	keys := app.DefaultKeys()
	return Model{
		keys:        keys,
		screen:      ScreenWelcome,
		welcome:     screens.NewWelcomeWithOptions(keys, opts.Welcome),
		runner:      r,
		welcomeOpts: opts.Welcome,
		evidenceDir: opts.EvidenceDir,
		paramify:    opts.Paramify,
	}
}

func (m Model) Init() tea.Cmd {
	return m.welcome.Init()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "Q":
			return m, tea.Quit
		}
		if k.String() == "q" && m.screen != ScreenSelect {
			return m, tea.Quit
		}
		if k.String() == "q" && m.screen == ScreenSelect && !m.sel.IsFiltering() {
			return m, tea.Quit
		}
		if key.Matches(k, m.keys.Help) {
			m.showHelp = !m.showHelp
			return m, nil
		}
		if m.showHelp {
			if key.Matches(k, m.keys.Back) {
				m.showHelp = false
			}
			return m, nil
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.welcome = m.welcome.Resize(msg.Width, msg.Height)
		m.sel = m.sel.Resize(msg.Width, msg.Height)
		m.run = m.run.Resize(msg.Width, msg.Height)
		m.review = m.review.Resize(msg.Width, msg.Height)

	case screens.SelectedProfileMsg:
		m.profile = msg.Profile.Name
		m.region = msg.Profile.Region
		if configurable, ok := m.runner.(runner.ProfileConfigurer); ok {
			configurable.ConfigureProfile(msg.Profile.Name, msg.Profile.Region)
		}
		m.sel = screens.NewSelect(m.keys, m.profile).Resize(m.width, m.height)
		m.screen = ScreenSelect
		return m, m.sel.Init()

	case screens.SelectionConfirmedMsg:
		m.run = screens.NewRun(m.keys, m.profile, msg.IDs, m.runner).Resize(m.width, m.height)
		m.screen = ScreenRun
		return m, m.run.Init()

	case screens.RunCompleteMsg:
		rev := screens.NewReview(m.keys, m.profile, msg.Results).
			WithEvidenceDir(m.evidenceDir)
		if m.paramify != nil {
			rev = rev.WithParamifyUpload(m.paramify)
		}
		m.review = rev.Resize(m.width, m.height)
		m.screen = ScreenReview
		return m, m.review.Init()

	case screens.RestartMsg:
		m.welcome = screens.NewWelcomeWithOptions(m.keys, m.welcomeOpts).Resize(m.width, m.height)
		m.screen = ScreenWelcome
		return m, m.welcome.Init()

	case screens.QuitMsg:
		return m, tea.Quit
	}

	var cmd tea.Cmd
	switch m.screen {
	case ScreenWelcome:
		m.welcome, cmd = m.welcome.Update(msg)
	case ScreenSelect:
		m.sel, cmd = m.sel.Update(msg)
	case ScreenRun:
		m.run, cmd = m.run.Update(msg)
	case ScreenReview:
		m.review, cmd = m.review.Update(msg)
	}
	return m, cmd
}

func (m Model) View() string {
	if m.showHelp {
		return m.helpView()
	}
	return m.screenView()
}

func (m Model) screenView() string {
	switch m.screen {
	case ScreenWelcome:
		return m.welcome.View()
	case ScreenSelect:
		return m.sel.View()
	case ScreenRun:
		return m.run.View()
	case ScreenReview:
		return m.review.View()
	}
	return ""
}

type helpSection struct {
	title string
	items []helpItem
}

type helpItem struct {
	key  string
	desc string
}

func (m Model) helpView() string {
	width := m.width
	if width <= 0 {
		width = 100
	}
	height := m.height
	if height <= 0 {
		height = 30
	}

	lines := []string{
		app.StyleTitle.Render("keyboard help"),
		app.StyleSubtle.Render("press ? or esc to return"),
		"",
	}
	for _, section := range m.helpSections() {
		lines = append(lines, app.StyleAccent.Render(section.title))
		for _, item := range section.items {
			lines = append(lines, fmt.Sprintf("  %s  %s",
				app.StyleKey.Render(fmt.Sprintf("%-14s", item.key)),
				app.StyleHint.Render(item.desc),
			))
		}
		lines = append(lines, "")
	}
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	panelWidth := clampInt(width-8, 40, 72)
	panel := app.StyleBorderActive.Width(panelWidth).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, panel)
}

func (m Model) helpSections() []helpSection {
	current := helpSection{title: "Current Screen"}
	switch m.screen {
	case ScreenWelcome:
		current.title = "Welcome"
		current.items = []helpItem{
			{key: "up/down or k/j", desc: "move between profiles"},
			{key: "enter", desc: "check credentials and continue"},
			{key: "o", desc: "run SSO login when prompted"},
		}
	case ScreenSelect:
		current.title = "Select Fetchers"
		current.items = []helpItem{
			{key: "tab/right", desc: "switch pane"},
			{key: "left", desc: "focus sources"},
			{key: "up/down or k/j", desc: "move selection"},
			{key: "space", desc: "toggle fetcher"},
			{key: "a", desc: "select or clear visible fetchers"},
			{key: "/", desc: "filter fetchers"},
			{key: "enter", desc: "run selected fetchers"},
		}
	case ScreenRun:
		current.title = "Run"
		current.items = []helpItem{
			{key: "up/down or k/j", desc: "focus fetcher"},
			{key: "p", desc: "pin output"},
			{key: "c", desc: "cancel focused fetcher"},
			{key: "r", desc: "retry focused fetcher"},
			{key: "enter", desc: "review results when complete"},
		}
	case ScreenReview:
		current.title = "Review"
		current.items = []helpItem{
			{key: "up/down or k/j", desc: "select result"},
			{key: "pgup/pgdn", desc: "page results"},
			{key: "home/end", desc: "jump to first or last result"},
			{key: "u", desc: "upload evidence"},
			{key: "e", desc: "export"},
			{key: "esc/b", desc: "back to welcome"},
		}
	}

	global := helpSection{
		title: "Global",
		items: []helpItem{
			{key: "?", desc: "toggle help"},
			{key: "esc", desc: "close help"},
			{key: "q", desc: "quit"},
			{key: "ctrl+c", desc: "quit"},
		},
	}
	return []helpSection{current, global}
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
