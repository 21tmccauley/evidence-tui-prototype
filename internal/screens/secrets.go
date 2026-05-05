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
	"github.com/paramify/evidence-tui-prototype/internal/mock"
	"github.com/paramify/evidence-tui-prototype/internal/secrets"
)

type SecretsDoneMsg struct{}

// SecretsOptions tweaks the Secrets screen at construction time.
//
// FocusKeys collapses the screen into a single-section view of just those
// keys (used by Review when the upload token is missing). When empty, the
// screen renders the catalog-source two-pane layout.
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

type secretsPane int

const (
	secretsPaneSources secretsPane = iota
	secretsPaneKeys
)

type SecretsModel struct {
	keys   app.KeyMap
	store  secrets.Store
	prompt string

	// sources is the left-pane list. In default mode it's the paramify
	// pseudo-source plus every catalog source from mock.Sources(); in
	// focused mode it's a single synthesized "focused" entry.
	sources []secrets.SourceSecrets
	focused bool // true iff constructed with FocusKeys

	srcIdx int
	keyIdx int
	pane   secretsPane

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

	sources, focused := buildSecretsSources(opts.FocusKeys)
	pane := secretsPaneSources
	if focused {
		pane = secretsPaneKeys
	}

	return SecretsModel{
		keys:    keys,
		store:   store,
		sources: sources,
		focused: focused,
		pane:    pane,
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
				spec, ok := m.currentSpec()
				if !ok {
					m.editing = false
					m.input.Blur()
					m.input.SetValue("")
					return m, nil
				}
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
		case key.Matches(msg, m.keys.Tab), key.Matches(msg, m.keys.Right):
			if !m.focused && m.currentSourceHasKeys() {
				m.pane = secretsPaneKeys
				m.keyIdx = 0
			}
		case key.Matches(msg, m.keys.Left):
			if !m.focused {
				m.pane = secretsPaneSources
			}
		case key.Matches(msg, m.keys.Up):
			m.moveCursor(-1)
		case key.Matches(msg, m.keys.Down):
			m.moveCursor(1)
		case key.Matches(msg, m.keys.Enter):
			return m.handleEnter()
		case msg.String() == "x":
			return m.handleClear()
		case key.Matches(msg, m.keys.Back):
			return m, func() tea.Msg { return SecretsDoneMsg{} }
		}
	}
	return m, nil
}

func (m *SecretsModel) moveCursor(delta int) {
	if m.focused || m.pane == secretsPaneKeys {
		spec := m.currentSource()
		if !spec.HasKeys() {
			return
		}
		next := m.keyIdx + delta
		if next < 0 {
			next = 0
		}
		if next >= len(spec.Keys) {
			next = len(spec.Keys) - 1
		}
		m.keyIdx = next
		return
	}
	next := m.srcIdx + delta
	if next < 0 {
		next = 0
	}
	if next >= len(m.sources) {
		next = len(m.sources) - 1
	}
	m.srcIdx = next
	m.keyIdx = 0
}

func (m SecretsModel) handleEnter() (SecretsModel, tea.Cmd) {
	if !m.focused && m.pane == secretsPaneSources {
		if m.currentSourceHasKeys() {
			m.pane = secretsPaneKeys
			m.keyIdx = 0
		}
		return m, nil
	}
	if _, ok := m.currentSpec(); !ok {
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
}

func (m SecretsModel) handleClear() (SecretsModel, tea.Cmd) {
	if !m.focused && m.pane == secretsPaneSources {
		return m, nil
	}
	spec, ok := m.currentSpec()
	if !ok {
		return m, nil
	}
	if m.store == nil || !m.store.Writable() {
		m.status = "backend is read-only — cannot clear from TUI"
		m.isError = true
		return m, nil
	}
	return m, m.saveCmd(spec.Key, "")
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

	subtitle := "values are masked and stored via the configured backend"
	readOnly := m.store == nil || !m.store.Writable()
	if readOnly && m.store != nil {
		subtitle = "read-only backend (" + m.store.Source() + ") — values must be set in the shell environment"
	}

	body := m.renderBody(width)

	bodyRows := []string{
		"",
		lipgloss.PlaceHorizontal(width, lipgloss.Center, app.StyleTitle.Render("secrets")),
		lipgloss.PlaceHorizontal(width, lipgloss.Center, app.StyleSubtle.Render(subtitle)),
		"",
		body,
		"",
	}
	if m.prompt != "" {
		for _, line := range strings.Split(m.prompt, "\n") {
			bodyRows = append(bodyRows, "  "+app.StyleWarning.Render(line))
		}
	}
	switch {
	case m.editing:
		spec, _ := m.currentSpec()
		bodyRows = append(bodyRows, "  "+app.StyleAccent.Render("enter value for "+spec.Key))
		bodyRows = append(bodyRows, "  "+m.input.View())
		bodyRows = append(bodyRows, "  "+app.StyleSubtle.Render("press enter to save; esc to cancel"))
	case readOnly:
		bodyRows = append(bodyRows, "  "+app.StyleSubtle.Render("read-only — editing disabled"))
	default:
		bodyRows = append(bodyRows, "  "+app.StyleSubtle.Render("enter to edit; x clears; tab switches pane"))
	}
	if m.status != "" {
		statusStyle := app.StyleInfo
		if m.isError {
			statusStyle = app.StyleDanger
		}
		bodyRows = append(bodyRows, "  "+statusStyle.Render(m.status))
	}

	hints := []components.Hint{
		{Key: "↑/↓", Desc: "move"},
		{Key: "tab", Desc: "switch pane"},
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
			{Key: "tab", Desc: "switch pane"},
			{Key: "esc/b", Desc: "back"},
		}
	}
	footer := components.RenderFooter(width, hints)

	page := lipgloss.JoinVertical(lipgloss.Left, append([]string{header}, append(bodyRows, footer)...)...)
	used := lipgloss.Height(page)
	if pad := height - used; pad > 0 {
		page += strings.Repeat("\n", pad)
	}
	return page
}

func (m SecretsModel) renderBody(width int) string {
	if m.focused {
		return m.renderFocusedKeys(width)
	}
	leftW := 24
	rightW := width - leftW - 4
	if rightW < 30 {
		rightW = 30
	}

	leftLines := []string{app.StyleTitle.Render("sources"), ""}
	for i, src := range m.sources {
		marker := "  "
		label := src.Label
		if !src.HasKeys() {
			label = label + app.StyleSubtle.Render("  (info)")
		}
		if i == m.srcIdx {
			marker = app.StyleAccent.Render("▸ ")
			label = app.StyleAccent.Bold(true).Render(src.Label)
			if !src.HasKeys() {
				label = label + " " + app.StyleSubtle.Render("(info)")
			}
		}
		leftLines = append(leftLines, marker+label)
	}
	leftStyle := app.StyleBorder.Width(leftW)
	if m.pane == secretsPaneSources {
		leftStyle = app.StyleBorderActive.Width(leftW)
	}
	leftPane := leftStyle.Render(strings.Join(leftLines, "\n"))

	rightStyle := app.StyleBorder.Width(rightW)
	if m.pane == secretsPaneKeys {
		rightStyle = app.StyleBorderActive.Width(rightW)
	}
	rightPane := rightStyle.Render(m.renderRightPane(rightW))

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPane, " ", rightPane)
}

func (m SecretsModel) renderRightPane(_ int) string {
	src := m.currentSource()
	title := app.StyleTitle.Render(src.Label)
	if !src.HasKeys() {
		note := src.Note
		if note == "" {
			note = "no secrets configured for this source"
		}
		return strings.Join([]string{
			title, "",
			app.StyleSubtle.Render(note),
		}, "\n")
	}

	rows := []string{title, ""}
	for i, spec := range src.Keys {
		marker := "  "
		if m.pane == secretsPaneKeys && i == m.keyIdx {
			marker = app.StyleAccent.Render("▸ ")
		}
		status := app.StyleWarning.Render("missing")
		if m.present[spec.Key] {
			label := "set"
			if s := m.source[spec.Key]; s != "" {
				label = "set (" + s + ")"
			}
			status = app.StyleSuccess.Render(label)
		}
		opt := app.StyleAccent.Render("required")
		if spec.Optional {
			opt = app.StyleSubtle.Render("optional")
		}
		row := fmt.Sprintf("%s  %s  %s", padRight(spec.Key, 28), status, opt)
		rows = append(rows, marker+row)
		if spec.Description != "" {
			rows = append(rows, "    "+app.StyleSubtle.Render(spec.Description))
		}
	}
	return strings.Join(rows, "\n")
}

func (m SecretsModel) renderFocusedKeys(width int) string {
	src := m.currentSource()
	rows := []string{}
	for i, spec := range src.Keys {
		prefix := "  "
		style := app.StyleBorder.Width(width - 6)
		if i == m.keyIdx {
			prefix = app.StyleAccent.Render("▸ ")
			style = app.StyleBorderActive.Width(width - 6)
		}
		status := app.StyleWarning.Render("missing")
		if m.present[spec.Key] {
			label := "set"
			if s := m.source[spec.Key]; s != "" {
				label = "set (" + s + ")"
			}
			status = app.StyleSuccess.Render(label)
		}
		opt := app.StyleAccent.Render("required")
		if spec.Optional {
			opt = app.StyleSubtle.Render("optional")
		}
		row := fmt.Sprintf("%s  %s  %s  %s", padRight(spec.ServiceName, 10), padRight(spec.Key, 28), status, opt)
		if spec.Description != "" {
			row += "\n" + app.StyleSubtle.Render("    "+spec.Description)
		}
		// Join marker beside the bordered block; string concat would prepend
		// the marker only to the first line and break border alignment.
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, prefix, style.Render(row)))
	}
	if len(rows) == 0 {
		rows = append(rows, app.StyleSubtle.Render("  (no keys to display)"))
	}
	return strings.Join(rows, "\n")
}

func (m SecretsModel) currentSource() secrets.SourceSecrets {
	if len(m.sources) == 0 {
		return secrets.SourceSecrets{}
	}
	idx := m.srcIdx
	if idx < 0 {
		idx = 0
	}
	if idx >= len(m.sources) {
		idx = len(m.sources) - 1
	}
	return m.sources[idx]
}

func (m SecretsModel) currentSourceHasKeys() bool {
	return m.currentSource().HasKeys()
}

func (m SecretsModel) currentSpec() (secrets.ServiceSecret, bool) {
	src := m.currentSource()
	if !src.HasKeys() {
		return secrets.ServiceSecret{}, false
	}
	idx := m.keyIdx
	if idx < 0 {
		idx = 0
	}
	if idx >= len(src.Keys) {
		idx = len(src.Keys) - 1
	}
	return src.Keys[idx], true
}

func (m SecretsModel) loadCmd() tea.Cmd {
	return func() tea.Msg {
		if m.store == nil {
			return secretsLoadedMsg{present: map[string]bool{}, source: map[string]string{}}
		}
		present := map[string]bool{}
		source := map[string]string{}
		for _, src := range m.sources {
			for _, spec := range src.Keys {
				if _, seen := present[spec.Key]; seen {
					continue
				}
				s, found, err := m.store.Locate(spec.Key)
				if err != nil {
					return secretsLoadedMsg{err: err}
				}
				present[spec.Key] = found
				if found {
					source[spec.Key] = s
				}
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

// buildSecretsSources returns the left-pane sources to display and a flag
// indicating whether the screen is in single-section "focused" mode (i.e.
// constructed via SecretsOptions.FocusKeys, used by Review's Paramify upload detour).
//
// Default mode: paramify pseudo-source pinned first, then every catalog
// source from mock.Sources() in catalog order. Sources without a table
// entry get a synthesized info row so the screen never lies about coverage.
//
// Focused mode: a single synthesized SourceSecrets entry containing only
// the requested keys, in the order given.
func buildSecretsSources(focusKeys []string) ([]secrets.SourceSecrets, bool) {
	if len(focusKeys) > 0 {
		keys := []secrets.ServiceSecret{}
		seen := map[string]bool{}
		all := secrets.AllSourceSecrets()
		index := map[string]secrets.ServiceSecret{}
		for _, src := range all {
			for _, k := range src.Keys {
				if _, ok := index[k.Key]; !ok {
					index[k.Key] = k
				}
			}
		}
		for _, k := range focusKeys {
			if seen[k] {
				continue
			}
			seen[k] = true
			if spec, ok := index[k]; ok {
				keys = append(keys, spec)
			}
		}
		return []secrets.SourceSecrets{{
			Source: "focused",
			Label:  "Required Secrets",
			Keys:   keys,
		}}, true
	}

	out := []secrets.SourceSecrets{secrets.SecretsForSource(secrets.SourceParamify)}
	added := map[string]bool{secrets.SourceParamify: true}

	for _, src := range mock.Sources(mock.Catalog()) {
		if added[src] {
			continue
		}
		added[src] = true
		out = append(out, secrets.SecretsForSource(src))
	}

	// Append any table-only sources that aren't in the catalog (defensive;
	// keeps us honest if the catalog and table diverge during refactors).
	for _, ss := range secrets.AllSourceSecrets() {
		if !added[ss.Source] {
			added[ss.Source] = true
			out = append(out, ss)
		}
	}
	return out, false
}
