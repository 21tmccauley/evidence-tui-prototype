package screens

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/paramify/evidence-tui-prototype/internal/app"
	"github.com/paramify/evidence-tui-prototype/internal/components"
	"github.com/paramify/evidence-tui-prototype/internal/secrets"
)

type SecretsDoneMsg struct{}

type SecretsOptions struct {
	FocusKeys []string
	Prompt    string
}

type secretsLoadedMsg struct {
	present map[string]bool
	source  map[string]string
	err     error
}

type secretSavedMsg struct {
	key     string
	deleted bool
	err     error
}

type SecretsModel struct {
	keys   app.KeyMap
	store  secrets.Store
	specs  []secrets.ServiceSecret
	cursor int
	prompt string

	present map[string]bool
	source  map[string]string
	status  string
	isError bool

	editing bool
	input   textinput.Model

	width  int
	height int
}

func NewSecrets(keys app.KeyMap, store secrets.Store) SecretsModel {
	return NewSecretsWithOptions(keys, store, SecretsOptions{})
}

func NewSecretsWithOptions(keys app.KeyMap, store secrets.Store, opts SecretsOptions) SecretsModel {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.CharLimit = 512
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.Placeholder = "paste secret value"
	specs := secrets.KnownServiceSecrets()
	if len(opts.FocusKeys) > 0 {
		specs = filterSecretsByKeys(specs, opts.FocusKeys)
	}

	return SecretsModel{
		keys:    keys,
		store:   store,
		specs:   specs,
		present: map[string]bool{},
		source:  map[string]string{},
		input:   ti,
		prompt:  strings.TrimSpace(opts.Prompt),
	}
}

func (m SecretsModel) Init() tea.Cmd {
	return m.loadCmd()
}

func (m SecretsModel) Resize(w, h int) SecretsModel {
	m.width, m.height = w, h
	m.input.Width = maxInt(20, w-26)
	return m
}

func (m SecretsModel) Update(msg tea.Msg) (SecretsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = maxInt(20, msg.Width-26)
	case secretsLoadedMsg:
		if msg.err != nil {
			m.status = "failed to load secret status"
			m.isError = true
			return m, nil
		}
		m.present = msg.present
		m.source = msg.source
		return m, nil
	case secretSavedMsg:
		if msg.err != nil {
			m.status = "failed to save secret"
			m.isError = true
			return m, nil
		}
		m.present[msg.key] = !msg.deleted
		if msg.deleted {
			delete(m.source, msg.key)
			m.status = msg.key + " cleared"
		} else {
			if m.store != nil {
				m.source[msg.key] = m.store.Source()
			}
			m.status = msg.key + " saved to " + m.store.Source()
		}
		m.isError = false
		return m, nil
	case tea.KeyMsg:
		if m.editing {
			switch {
			case key.Matches(msg, m.keys.Back):
				m.editing = false
				m.input.Blur()
				m.input.SetValue("")
				return m, nil
			case key.Matches(msg, m.keys.Enter):
				spec := m.currentSpec()
				value := strings.TrimSpace(m.input.Value())
				m.editing = false
				m.input.Blur()
				m.input.SetValue("")
				return m, m.saveCmd(spec.Key, value)
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}

		switch {
		case key.Matches(msg, m.keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}
		case key.Matches(msg, m.keys.Down):
			if m.cursor < len(m.specs)-1 {
				m.cursor++
			}
		case key.Matches(msg, m.keys.Enter):
			if len(m.specs) == 0 {
				return m, nil
			}
			if m.store == nil || !m.store.Writable() {
				m.status = "backend is read-only — set values via your shell or rerun with a writable backend"
				m.isError = true
				return m, nil
			}
			m.editing = true
			m.input.SetValue("")
			m.input.Focus()
			return m, textinput.Blink
		case msg.String() == "x":
			if len(m.specs) == 0 {
				return m, nil
			}
			if m.store == nil || !m.store.Writable() {
				m.status = "backend is read-only — cannot clear from TUI"
				m.isError = true
				return m, nil
			}
			spec := m.currentSpec()
			return m, m.saveCmd(spec.Key, "")
		case key.Matches(msg, m.keys.Back):
			return m, func() tea.Msg { return SecretsDoneMsg{} }
		}
	}
	return m, nil
}

func (m SecretsModel) View() string {
	width := m.width
	if width <= 0 {
		width = 120
	}
	height := m.height
	if height <= 0 {
		height = 30
	}

	header := components.RenderHeader(components.HeaderProps{
		Width: width,
		Crumb: "secrets",
		Now:   time.Now(),
	})

	rows := make([]string, 0, len(m.specs))
	for i, spec := range m.specs {
		prefix := "  "
		style := app.StyleBorder.Width(width - 6)
		if i == m.cursor {
			prefix = app.StyleAccent.Render("▸ ")
			style = app.StyleBorderActive.Width(width - 6)
		}
		status := app.StyleWarning.Render("missing")
		if m.present[spec.Key] {
			label := "set"
			if src := m.source[spec.Key]; src != "" {
				label = "set (" + src + ")"
			}
			status = app.StyleSuccess.Render(label)
		}
		opt := ""
		if spec.Optional {
			opt = app.StyleSubtle.Render("optional")
		} else {
			opt = app.StyleAccent.Render("required")
		}
		row := fmt.Sprintf("%s  %s  %s  %s", padRight(spec.ServiceName, 10), padRight(spec.Key, 28), status, opt)
		if spec.Description != "" {
			row += "\n" + app.StyleSubtle.Render("    "+spec.Description)
		}
		rows = append(rows, prefix+style.Render(row))
	}

	readOnly := m.store == nil || !m.store.Writable()
	subtitle := "values are masked and stored via the configured backend"
	if readOnly && m.store != nil {
		subtitle = "read-only backend (" + m.store.Source() + ") — values must be set in the shell environment"
	}
	bodyRows := []string{
		"",
		lipgloss.PlaceHorizontal(width, lipgloss.Center, app.StyleTitle.Render("secrets")),
		lipgloss.PlaceHorizontal(width, lipgloss.Center, app.StyleSubtle.Render(subtitle)),
		"",
	}
	bodyRows = append(bodyRows, rows...)
	bodyRows = append(bodyRows, "")
	if m.prompt != "" {
		for _, line := range strings.Split(m.prompt, "\n") {
			bodyRows = append(bodyRows, "  "+app.StyleWarning.Render(line))
		}
	}
	switch {
	case m.editing:
		bodyRows = append(bodyRows, "  "+app.StyleAccent.Render("enter value for "+m.currentSpec().Key))
		bodyRows = append(bodyRows, "  "+m.input.View())
		bodyRows = append(bodyRows, "  "+app.StyleSubtle.Render("press enter to save; esc to cancel"))
	case readOnly:
		bodyRows = append(bodyRows, "  "+app.StyleSubtle.Render("read-only — editing disabled"))
	default:
		bodyRows = append(bodyRows, "  "+app.StyleSubtle.Render("press enter to edit selected key; x clears selected key"))
	}
	if m.status != "" {
		statusStyle := app.StyleInfo
		if m.isError {
			statusStyle = app.StyleDanger
		}
		bodyRows = append(bodyRows, "  "+statusStyle.Render(m.status))
	}
	body := lipgloss.JoinVertical(lipgloss.Left, bodyRows...)

	hints := []components.Hint{
		{Key: "↑/↓", Desc: "move"},
		{Key: "enter", Desc: "edit/save"},
		{Key: "x", Desc: "clear"},
		{Key: "esc/b", Desc: "back"},
	}
	switch {
	case m.editing:
		hints = []components.Hint{
			{Key: "enter", Desc: "save"},
			{Key: "esc", Desc: "cancel"},
		}
	case readOnly:
		hints = []components.Hint{
			{Key: "↑/↓", Desc: "move"},
			{Key: "esc/b", Desc: "back"},
		}
	}
	footer := components.RenderFooter(width, hints)

	page := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	used := lipgloss.Height(page)
	if pad := height - used; pad > 0 {
		page += strings.Repeat("\n", pad)
	}
	return page
}

func (m SecretsModel) currentSpec() secrets.ServiceSecret {
	if len(m.specs) == 0 {
		return secrets.ServiceSecret{}
	}
	if m.cursor < 0 {
		return m.specs[0]
	}
	if m.cursor >= len(m.specs) {
		return m.specs[len(m.specs)-1]
	}
	return m.specs[m.cursor]
}

func (m SecretsModel) loadCmd() tea.Cmd {
	return func() tea.Msg {
		if m.store == nil {
			return secretsLoadedMsg{present: map[string]bool{}, source: map[string]string{}}
		}
		present := map[string]bool{}
		source := map[string]string{}
		for _, spec := range m.specs {
			src, found, err := m.store.Locate(spec.Key)
			if err != nil {
				return secretsLoadedMsg{err: err}
			}
			present[spec.Key] = found
			if found {
				source[spec.Key] = src
			}
		}
		return secretsLoadedMsg{present: present, source: source}
	}
}

func (m SecretsModel) saveCmd(key, value string) tea.Cmd {
	return func() tea.Msg {
		if m.store == nil {
			return secretSavedMsg{key: key, err: fmt.Errorf("no secrets store configured")}
		}
		if strings.TrimSpace(value) == "" {
			return secretSavedMsg{key: key, deleted: true, err: m.store.Delete(key)}
		}
		return secretSavedMsg{key: key, deleted: false, err: m.store.Set(key, value)}
	}
}

func filterSecretsByKeys(specs []secrets.ServiceSecret, focusKeys []string) []secrets.ServiceSecret {
	index := map[string]secrets.ServiceSecret{}
	for _, spec := range specs {
		index[spec.Key] = spec
	}
	out := make([]secrets.ServiceSecret, 0, len(focusKeys))
	seen := map[string]bool{}
	for _, key := range focusKeys {
		if seen[key] {
			continue
		}
		seen[key] = true
		if spec, ok := index[key]; ok {
			out = append(out, spec)
		}
	}
	if len(out) > 0 {
		return out
	}
	return specs
}
