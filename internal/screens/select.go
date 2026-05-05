package screens

import (
	"fmt"
	"sort"
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

type SelectionConfirmedMsg struct {
	IDs []mock.FetcherID
}

type pane int

const (
	paneSources pane = iota
	paneFetchers
)

type SelectModel struct {
	keys    app.KeyMap
	catalog []mock.Fetcher
	sources []string
	store   secrets.Store

	profile string
	idToSrc map[mock.FetcherID]string

	focused    pane
	sourceIdx  int
	fetcherIdx int

	selected   map[mock.FetcherID]bool
	filter     textinput.Model
	filterMode bool

	status      string
	statusError bool

	width, height int
}

// MissingSecret describes a required secret key that isn't set, plus the
// fetcher names from the current selection that need it.
type MissingSecret struct {
	Key      string
	Fetchers []string
}

// WithStatus sets a transient status banner rendered on the Select screen.
// Used by the root model to surface errors from gate checks instead of
// silently no-op'ing on enter.
func (m SelectModel) WithStatus(msg string, isError bool) SelectModel {
	m.status = msg
	m.statusError = isError
	return m
}

func NewSelect(keys app.KeyMap, profile string, store secrets.Store) SelectModel {
	cat := mock.Catalog()
	sources := mock.Sources(cat)
	idToSrc := make(map[mock.FetcherID]string, len(cat))
	for _, f := range cat {
		idToSrc[f.ID] = f.Source
	}

	ti := textinput.New()
	ti.Placeholder = "filter…"
	ti.Prompt = "/ "
	ti.CharLimit = 40

	return SelectModel{
		keys:     keys,
		catalog:  cat,
		sources:  sources,
		store:    store,
		profile:  profile,
		idToSrc:  idToSrc,
		focused:  paneSources,
		selected: map[mock.FetcherID]bool{},
		filter:   ti,
	}
}

func (m SelectModel) Init() tea.Cmd { return nil }

// IsFiltering reports whether the filter field is focused.
func (m SelectModel) IsFiltering() bool { return m.filterMode }

func (m SelectModel) Resize(w, h int) SelectModel {
	m.width, m.height = w, h
	return m
}

func (m SelectModel) currentSource() string {
	if m.sourceIdx < 0 || m.sourceIdx >= len(m.sources) {
		return ""
	}
	return m.sources[m.sourceIdx]
}

func (m SelectModel) visibleFetchers() []mock.Fetcher {
	src := m.currentSource()
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	out := []mock.Fetcher{}
	for _, f := range m.catalog {
		if f.Source != src {
			continue
		}
		if q != "" {
			hay := strings.ToLower(f.Name + " " + f.ID.String() + " " + strings.Join(f.Tags, " "))
			if !strings.Contains(hay, q) {
				continue
			}
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (m SelectModel) Update(msg tea.Msg) (SelectModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.filter.Width = msg.Width / 3
	case tea.KeyMsg:
		if m.status != "" && !m.filterMode {
			m.status = ""
			m.statusError = false
		}
		if m.filterMode {
			switch msg.String() {
			case "esc":
				m.filterMode = false
				m.filter.Blur()
				m.filter.SetValue("")
				m.fetcherIdx = 0
				return m, nil
			case "enter":
				m.filterMode = false
				m.filter.Blur()
				m.fetcherIdx = 0
				return m, nil
			}
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(msg)
			m.fetcherIdx = 0
			return m, cmd
		}

		switch {
		case key.Matches(msg, m.keys.Filter):
			m.filterMode = true
			m.focused = paneFetchers
			m.filter.Focus()
			return m, textinput.Blink
		case key.Matches(msg, m.keys.Tab), key.Matches(msg, m.keys.Right):
			if m.focused == paneSources {
				m.focused = paneFetchers
			} else {
				m.focused = paneSources
			}
		case key.Matches(msg, m.keys.Left):
			m.focused = paneSources
		case key.Matches(msg, m.keys.Up):
			if m.focused == paneSources {
				if m.sourceIdx > 0 {
					m.sourceIdx--
				}
				m.fetcherIdx = 0
			} else {
				if m.fetcherIdx > 0 {
					m.fetcherIdx--
				}
			}
		case key.Matches(msg, m.keys.Down):
			if m.focused == paneSources {
				if m.sourceIdx < len(m.sources)-1 {
					m.sourceIdx++
				}
				m.fetcherIdx = 0
			} else {
				vis := m.visibleFetchers()
				if m.fetcherIdx < len(vis)-1 {
					m.fetcherIdx++
				}
			}
		case key.Matches(msg, m.keys.Space):
			vis := m.visibleFetchers()
			if m.focused == paneFetchers && m.fetcherIdx < len(vis) {
				id := vis[m.fetcherIdx].ID
				if m.selected[id] {
					delete(m.selected, id)
				} else {
					m.selected[id] = true
				}
			}
		case key.Matches(msg, m.keys.All):
			vis := m.visibleFetchers()
			allOn := true
			for _, f := range vis {
				if !m.selected[f.ID] {
					allOn = false
					break
				}
			}
			for _, f := range vis {
				if allOn {
					delete(m.selected, f.ID)
				} else {
					m.selected[f.ID] = true
				}
			}
		case key.Matches(msg, m.keys.Enter):
			ids := m.orderedSelection()
			if len(ids) == 0 {
				return m, nil
			}
			return m, func() tea.Msg {
				return SelectionConfirmedMsg{IDs: ids}
			}
		case msg.String() == "s":
			return m, func() tea.Msg { return OpenSecretsMsg{} }
		}
	}
	return m, nil
}

// orderedSelection returns selected IDs in catalog order.
func (m SelectModel) orderedSelection() []mock.FetcherID {
	ids := []mock.FetcherID{}
	for _, f := range m.catalog {
		if m.selected[f.ID] {
			ids = append(ids, f.ID)
		}
	}
	return ids
}

func (m SelectModel) View() string {
	width := m.width
	if width <= 0 {
		width = 120
	}
	height := m.height
	if height <= 0 {
		height = 30
	}

	header := components.RenderHeader(components.HeaderProps{
		Width:   width,
		Crumb:   "select fetchers",
		Profile: m.profile,
		Region:  "us-east-1",
		Now:     time.Now(),
	})

	rule := 1
	footerH := 4 // status + rule + hint line
	bodyH := height - lipgloss.Height(header) - footerH - rule
	if bodyH < 8 {
		bodyH = 8
	}
	leftW := 24
	rightW := width - leftW - 4

	srcLines := []string{}
	counts := mock.CountBySource(m.catalog)
	for i, s := range m.sources {
		selCount := 0
		for _, f := range m.catalog {
			if f.Source == s && m.selected[f.ID] {
				selCount++
			}
		}
		label := fmt.Sprintf("%-12s %s", s, app.StyleSubtle.Render(fmt.Sprintf("%d", counts[s])))
		if selCount > 0 {
			label = fmt.Sprintf("%-12s %s", s,
				app.StyleSuccess.Render(fmt.Sprintf("%d/%d", selCount, counts[s])),
			)
		}
		secretBadge := m.sourceSecretBadge(s)
		if secretBadge != "" {
			label = padRight(label, 26) + " " + secretBadge
		}
		if i == m.sourceIdx {
			marker := app.StyleAccent.Render("▸ ")
			label = marker + app.StyleAccent.Bold(true).Render(label)
		} else {
			label = "  " + label
		}
		srcLines = append(srcLines, label)
	}
	srcStyle := app.StyleBorder.Width(leftW).Height(bodyH)
	if m.focused == paneSources {
		srcStyle = app.StyleBorderActive.Width(leftW).Height(bodyH)
	}
	leftPane := srcStyle.Render(
		app.StyleTitle.Render("sources") + "\n\n" +
			strings.Join(srcLines, "\n"),
	)

	vis := m.visibleFetchers()
	colWName := rightW - 24 - 14 - 4
	if colWName < 18 {
		colWName = 18
	}

	rowsRendered := []string{}
	headerRow := lipgloss.JoinHorizontal(lipgloss.Top,
		app.StyleSubtle.Render(padRight("    name", colWName)),
		app.StyleSubtle.Render(padRight("tags", 24)),
		app.StyleSubtle.Render(padRight("est.", 8)),
	)
	rowsRendered = append(rowsRendered, headerRow,
		lipgloss.NewStyle().Foreground(app.ColorSubtle).Render(strings.Repeat("─", rightW-2)),
	)
	for i, f := range vis {
		check := " "
		if m.selected[f.ID] {
			check = app.StyleSuccess.Render("✓")
		}
		name := f.Name
		if len(name) > colWName-4 {
			name = name[:colWName-5] + "…"
		}
		nameCell := fmt.Sprintf(" [%s] %s", check, name)
		tags := strings.Join(f.Tags, ",")
		if len(tags) > 22 {
			tags = tags[:21] + "…"
		}
		dur := fmt.Sprintf("%ds", int(f.EstDuration.Seconds()))
		row := lipgloss.JoinHorizontal(lipgloss.Top,
			padRight(nameCell, colWName),
			app.StyleInfo.Render(padRight(tags, 24)),
			app.StyleSubtle.Render(padRight(dur, 8)),
		)
		if m.focused == paneFetchers && i == m.fetcherIdx {
			row = app.StyleSelected.Render(padRight(row, rightW-4))
		}
		rowsRendered = append(rowsRendered, row)
	}
	if len(vis) == 0 {
		rowsRendered = append(rowsRendered,
			"", app.StyleSubtle.Render("  (no fetchers match the current filter)"))
	}

	rightTitle := app.StyleTitle.Render(m.currentSource() + " fetchers")
	if m.filterMode || m.filter.Value() != "" {
		rightTitle = lipgloss.JoinHorizontal(lipgloss.Top,
			rightTitle, "  ", m.filter.View(),
		)
	}
	rightStyle := app.StyleBorder.Width(rightW).Height(bodyH)
	if m.focused == paneFetchers {
		rightStyle = app.StyleBorderActive.Width(rightW).Height(bodyH)
	}
	rightBody := rightTitle + "\n\n" + strings.Join(rowsRendered, "\n")
	rightPane := rightStyle.Render(rightBody)

	body := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, " ", rightPane)

	totalSel := len(m.selected)
	totalDur := time.Duration(0)
	for _, f := range m.catalog {
		if m.selected[f.ID] {
			totalDur += f.EstDuration
		}
	}
	status := lipgloss.JoinHorizontal(lipgloss.Top,
		app.StyleAccent.Render(fmt.Sprintf("%d selected", totalSel)),
		app.StyleSubtle.Render("   ·   "),
		app.StyleInfo.Render(fmt.Sprintf("est. ~%s", totalDur.Round(time.Second))),
	)

	footer := components.RenderFooter(width, []components.Hint{
		{Key: "tab", Desc: "switch pane"},
		{Key: "space", Desc: "toggle"},
		{Key: "a", Desc: "all"},
		{Key: "/", Desc: "filter"},
		{Key: "enter", Desc: "run"},
		{Key: "s", Desc: "secrets"},
		{Key: "q", Desc: "quit"},
	})

	statusBar := lipgloss.NewStyle().Padding(0, 1).Render(status)
	parts := []string{header, body}
	if m.status != "" {
		style := app.StyleInfo
		if m.statusError {
			style = app.StyleDanger
		}
		parts = append(parts, lipgloss.NewStyle().Padding(0, 1).Render(style.Render(m.status)))
	}
	parts = append(parts, statusBar, footer)
	page := lipgloss.JoinVertical(lipgloss.Left, parts...)

	used := lipgloss.Height(page)
	if pad := height - used; pad > 0 {
		page = page + strings.Repeat("\n", pad)
	}
	return page
}

// MissingSecretsForSelection returns the required secret keys not yet set in
// the store, each annotated with the fetcher names from ids that need it.
func (m SelectModel) MissingSecretsForSelection(ids []mock.FetcherID) ([]MissingSecret, error) {
	nameByID := map[mock.FetcherID]string{}
	for _, f := range m.catalog {
		nameByID[f.ID] = f.Name
	}

	needsByKey := map[string]map[string]bool{}
	for _, id := range ids {
		source := m.idToSrc[id]
		keys := secrets.RequiredKeysForSource(source)
		if len(keys) == 0 {
			continue
		}
		name := nameByID[id]
		for _, k := range keys {
			if _, ok := needsByKey[k]; !ok {
				needsByKey[k] = map[string]bool{}
			}
			needsByKey[k][name] = true
		}
	}

	out := []MissingSecret{}
	for key, namesSet := range needsByKey {
		_, found, err := m.store.Get(key)
		if err != nil {
			return nil, err
		}
		if found {
			continue
		}
		names := make([]string, 0, len(namesSet))
		for n := range namesSet {
			names = append(names, n)
		}
		sort.Strings(names)
		out = append(out, MissingSecret{Key: key, Fetchers: names})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// MissingRequiredKeysForSelection returns just the missing keys, used by the
// post-Secrets recheck path that doesn't need fetcher provenance.
func (m SelectModel) MissingRequiredKeysForSelection(ids []mock.FetcherID) ([]string, error) {
	missing, err := m.MissingSecretsForSelection(ids)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(missing))
	for _, ms := range missing {
		out = append(out, ms.Key)
	}
	return out, nil
}

func (m SelectModel) sourceSecretBadge(source string) string {
	required := secrets.RequiredKeysForSource(source)
	if len(required) == 0 {
		return app.StyleSubtle.Render("secrets: none")
	}
	missing, err := secrets.MissingRequiredKeys(m.store, required)
	if err != nil {
		return app.StyleWarning.Render("secrets: unknown")
	}
	if len(missing) == 0 {
		return app.StyleSuccess.Render("secrets: ready")
	}
	return app.StyleWarning.Render(fmt.Sprintf("secrets: missing %d", len(missing)))
}
