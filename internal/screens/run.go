package screens

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/paramify/evidence-tui-prototype/internal/app"
	"github.com/paramify/evidence-tui-prototype/internal/components"
	"github.com/paramify/evidence-tui-prototype/internal/mock"
	"github.com/paramify/evidence-tui-prototype/internal/runner"
)

type RunResult struct {
	ID          runner.FetcherID
	Name        string
	Source      string
	Status      runner.Status
	Duration    time.Duration
	OutputTail  []string
	ExitCode    int
	ErrorReason string
}

type RunCompleteMsg struct{ Results []RunResult }

const maxOutputLines = 400

type runState struct {
	id           runner.FetcherID
	target       runner.Target
	fetcher      mock.Fetcher
	status       runner.Status
	stalled      bool
	startedAt    time.Time
	finishedAt   time.Time
	lastOutputAt time.Time
	output       []string
	exitCode     int
	errorReason  string
}

type RunModel struct {
	keys    app.KeyMap
	profile string

	targets     []runner.FetcherID
	catalog     map[runner.FetcherID]mock.Fetcher
	states      map[runner.FetcherID]*runState
	targetsMeta map[runner.FetcherID]runner.Target

	runner runner.Runner

	focusIdx int
	pinnedID runner.FetcherID

	progress progress.Model
	spinner  spinner.Model
	viewport viewport.Model
	cardsVP  viewport.Model

	startedAt time.Time
	now       time.Time
	finished  bool

	width, height int
}

func NewRun(keys app.KeyMap, profile string, ids []runner.FetcherID, r runner.Runner) RunModel {
	cat := mock.Catalog()
	catMap := map[runner.FetcherID]mock.Fetcher{}
	for _, f := range cat {
		catMap[f.ID] = f
	}

	states := map[runner.FetcherID]*runState{}
	targetsMeta := map[runner.FetcherID]runner.Target{}
	for _, id := range ids {
		target := runner.Target{ID: id, BaseID: id}
		targetsMeta[id] = target
		states[id] = newRunState(target, catMap)
	}

	pr := progress.New(progress.WithGradient("#7AA2F7", "#BB9AF7"))
	pr.ShowPercentage = true
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = lipgloss.NewStyle().Foreground(app.ColorPrimary)

	vp := viewport.New(60, 16)
	vp.Style = lipgloss.NewStyle()
	cardsVP := viewport.New(42, 16)
	cardsVP.Style = lipgloss.NewStyle()
	cardsVP.MouseWheelEnabled = true

	return RunModel{
		keys:        keys,
		profile:     profile,
		targets:     ids,
		catalog:     catMap,
		states:      states,
		targetsMeta: targetsMeta,
		runner:      r,
		progress:    pr,
		spinner:     sp,
		viewport:    vp,
		cardsVP:     cardsVP,
		startedAt:   time.Now(),
		now:         time.Now(),
	}
}

func newRunState(target runner.Target, catMap map[runner.FetcherID]mock.Fetcher) *runState {
	baseID := target.BaseID
	if baseID == "" {
		baseID = target.ID
	}
	f, ok := catMap[baseID]
	if !ok {
		f = mock.Fetcher{
			ID:     target.ID,
			Name:   string(baseID),
			Source: "unknown",
		}
	}
	f.ID = target.ID
	if target.Label != "" && target.Label != "missing" {
		f.Name = fmt.Sprintf("%s (%s)", f.Name, target.Label)
	}
	return &runState{
		id:      target.ID,
		target:  target,
		fetcher: f,
		status:  runner.StatusQueued,
	}
}

func (m RunModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		runner.ScheduleStallTick(),
		m.runner.Init(),
		m.runner.Start(m.targets),
	)
}

func (m RunModel) Resize(w, h int) RunModel {
	m.width, m.height = w, h
	rightW, vpH := m.viewportDims()
	m.viewport.Width = rightW
	m.viewport.Height = vpH
	m.cardsVP.Width = 42
	m.cardsVP.Height = vpH + 2
	m.progress.Width = w / 3
	return m
}

func (m RunModel) viewportDims() (int, int) {
	width := m.width
	if width <= 0 {
		width = 120
	}
	height := m.height
	if height <= 0 {
		height = 30
	}
	leftW := 42
	rightW := width - leftW - 5
	if rightW < 30 {
		rightW = 30
	}
	chrome := 4 + 4 + 3 // header + topbar + footer-ish
	bodyH := height - chrome - 3
	if bodyH < 8 {
		bodyH = 8
	}
	return rightW, bodyH
}

func (m RunModel) focusedID() runner.FetcherID {
	if m.pinnedID != "" {
		return m.pinnedID
	}
	if m.focusIdx >= 0 && m.focusIdx < len(m.targets) {
		return m.targets[m.focusIdx]
	}
	return ""
}

func (m RunModel) applyTargets(targets []runner.Target) RunModel {
	if len(targets) == 0 {
		return m
	}
	ids := make([]runner.FetcherID, 0, len(targets))
	states := make(map[runner.FetcherID]*runState, len(targets))
	meta := make(map[runner.FetcherID]runner.Target, len(targets))
	for _, target := range targets {
		if target.BaseID == "" {
			target.BaseID = target.ID
		}
		ids = append(ids, target.ID)
		meta[target.ID] = target
		if existing := m.states[target.ID]; existing != nil {
			existing.target = target
			states[target.ID] = existing
		} else {
			states[target.ID] = newRunState(target, m.catalog)
		}
	}
	m.targets = ids
	m.states = states
	m.targetsMeta = meta
	if m.focusIdx >= len(m.targets) {
		m.focusIdx = len(m.targets) - 1
	}
	if m.focusIdx < 0 {
		m.focusIdx = 0
	}
	if _, ok := m.states[m.pinnedID]; !ok {
		m.pinnedID = ""
	}
	return m
}

func (m RunModel) Update(msg tea.Msg) (RunModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		rightW, vpH := m.viewportDims()
		m.viewport.Width = rightW
		m.viewport.Height = vpH
		m.cardsVP.Width = 42
		m.cardsVP.Height = vpH + 2
		m.progress.Width = msg.Width / 3
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	case runner.StallTickMsg:
		m.now = time.Now()
		for _, st := range m.states {
			if st.status == runner.StatusRunning && !st.lastOutputAt.IsZero() {
				st.stalled = time.Since(st.lastOutputAt) > runner.StallThreshold
			} else {
				st.stalled = false
			}
		}
		if !m.finished {
			cmds = append(cmds, runner.ScheduleStallTick())
		}
	case runner.TargetsMsg:
		m = m.applyTargets(msg.Targets)
	case runner.StartedMsg:
		if st, ok := m.states[msg.ID]; ok {
			st.status = runner.StatusRunning
			st.startedAt = time.Now()
			st.lastOutputAt = time.Now()
		}
	case runner.OutputMsg:
		if st, ok := m.states[msg.ID]; ok {
			st.output = append(st.output, msg.Line)
			if len(st.output) > maxOutputLines {
				st.output = st.output[len(st.output)-maxOutputLines:]
			}
			st.lastOutputAt = time.Now()
			st.stalled = false
		}
	case runner.FinishedMsg:
		if st, ok := m.states[msg.ID]; ok {
			st.status = msg.Status
			st.exitCode = msg.ExitCode
			st.errorReason = msg.ErrorReason
			st.finishedAt = time.Now()
			st.stalled = false
			if m.allDone() {
				m.finished = true
				cmds = append(cmds, m.emitComplete())
			}
		}
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Up):
			if m.focusIdx > 0 {
				m.focusIdx--
			}
		case key.Matches(msg, m.keys.Down):
			if m.focusIdx < len(m.targets)-1 {
				m.focusIdx++
			}
		case key.Matches(msg, m.keys.Pin):
			id := m.focusedID()
			if m.pinnedID == id {
				m.pinnedID = ""
			} else {
				m.pinnedID = id
			}
		case key.Matches(msg, m.keys.Cancel):
			id := m.focusedID()
			if st, ok := m.states[id]; ok && !st.status.Terminal() {
				cmds = append(cmds, m.runner.Cancel(id))
			}
		case key.Matches(msg, m.keys.Retry):
			id := m.focusedID()
			st, ok := m.states[id]
			if ok && st.status.Terminal() && st.status != runner.StatusOK {
				st.status = runner.StatusQueued
				st.output = nil
				st.startedAt = time.Time{}
				st.finishedAt = time.Time{}
				st.lastOutputAt = time.Time{}
				st.stalled = false
				st.errorReason = ""
				m.finished = false
				cmds = append(cmds, m.runner.Retry(id))
			}
		case key.Matches(msg, m.keys.Enter):
			if m.finished {
				cmds = append(cmds, m.emitComplete())
			}
		}
	}

	var rcmd tea.Cmd
	m.runner, rcmd = m.runner.Update(msg)
	cmds = append(cmds, rcmd)

	id := m.focusedID()
	if st, ok := m.states[id]; ok {
		m.viewport.SetContent(strings.Join(st.output, "\n"))
		m.viewport.GotoBottom()
	}

	return m, tea.Batch(cmds...)
}

func (m RunModel) allDone() bool {
	for _, id := range m.targets {
		st := m.states[id]
		if !st.status.Terminal() {
			return false
		}
	}
	return true
}

func (m RunModel) emitComplete() tea.Cmd {
	results := []RunResult{}
	for _, id := range m.targets {
		st := m.states[id]
		dur := time.Duration(0)
		if !st.finishedAt.IsZero() && !st.startedAt.IsZero() {
			dur = st.finishedAt.Sub(st.startedAt)
		}
		tail := st.output
		if len(tail) > 6 {
			tail = tail[len(tail)-6:]
		}
		results = append(results, RunResult{
			ID:          id,
			Name:        st.fetcher.Name,
			Source:      st.fetcher.Source,
			Status:      st.status,
			Duration:    dur,
			OutputTail:  tail,
			ExitCode:    st.exitCode,
			ErrorReason: st.errorReason,
		})
	}
	return func() tea.Msg { return RunCompleteMsg{Results: results} }
}

func (m RunModel) renderCard(id runner.FetcherID, focused bool) string {
	st := m.states[id]
	icon, label := m.statusBadge(st)

	name := st.fetcher.Name
	if len(name) > 28 {
		name = name[:27] + "…"
	}
	source := app.StyleSubtle.Render(st.fetcher.Source)

	dur := ""
	switch st.status {
	case runner.StatusRunning:
		dur = time.Since(st.startedAt).Round(100 * time.Millisecond).String()
	case runner.StatusQueued:
		dur = "queued"
	default:
		if !st.finishedAt.IsZero() && !st.startedAt.IsZero() {
			dur = st.finishedAt.Sub(st.startedAt).Round(100 * time.Millisecond).String()
		}
	}

	line1 := lipgloss.JoinHorizontal(lipgloss.Top,
		icon, " ",
		lipgloss.NewStyle().Bold(true).Foreground(app.ColorFg).Render(padRight(name, 30)),
	)
	line2 := lipgloss.JoinHorizontal(lipgloss.Top,
		"  ", source, "   ",
		label, "   ",
		app.StyleSubtle.Render(dur),
	)
	body := line1 + "\n" + line2

	if (st.status == runner.StatusFailed || st.status == runner.StatusPartial) && st.errorReason != "" {
		reason := st.errorReason
		if len(reason) > 80 {
			reason = reason[:77] + "…"
		}
		body += "\n  " + app.StyleDanger.Render(reason)
	}

	style := app.StyleBorder
	if focused {
		style = app.StyleBorderActive
	}
	if m.pinnedID == id {
		style = style.BorderForeground(app.ColorAccent)
	}
	return style.Width(40).Render(body)
}

func (m RunModel) statusBadge(st *runState) (string, string) {
	switch st.status {
	case runner.StatusQueued:
		return app.StyleBadgeQueue.Render("◌"), app.StyleBadgeQueue.Render("queued")
	case runner.StatusRunning:
		if st.stalled {
			return app.StyleBadgeStall.Render("…"), app.StyleBadgeStall.Render("stalled")
		}
		return app.StyleBadgeRun.Render(m.spinner.View()), app.StyleBadgeRun.Render("running")
	case runner.StatusOK:
		return app.StyleBadgeOK.Render("✓"), app.StyleBadgeOK.Render("ok")
	case runner.StatusPartial:
		return app.StyleBadgeWarn.Render("⚠"), app.StyleBadgeWarn.Render("partial")
	case runner.StatusFailed:
		return app.StyleBadgeFail.Render("✗"), app.StyleBadgeFail.Render("failed")
	case runner.StatusCancelled:
		return app.StyleBadgeCancel.Render("∅"), app.StyleBadgeCancel.Render("cancelled")
	}
	return " ", ""
}

func (m RunModel) View() string {
	width := m.width
	if width <= 0 {
		width = 120
	}
	height := m.height
	if height <= 0 {
		height = 30
	}

	done := 0
	failed := 0
	for _, st := range m.states {
		if st.status.Terminal() {
			done++
			if st.status == runner.StatusFailed {
				failed++
			}
		}
	}
	total := len(m.targets)
	pct := 0.0
	if total > 0 {
		pct = float64(done) / float64(total)
	}

	header := components.RenderHeader(components.HeaderProps{
		Width:   width,
		Crumb:   "running",
		Profile: m.profile,
		Region:  "us-east-1",
		Now:     m.now,
		StatusDot: func() string {
			if m.finished {
				if failed > 0 {
					return app.StyleDanger.Render("●")
				}
				return app.StyleSuccess.Render("●")
			}
			return app.StyleBadgeRun.Render(m.spinner.View())
		}(),
	})

	elapsed := time.Since(m.startedAt).Round(time.Second)
	topbar := lipgloss.JoinHorizontal(lipgloss.Top,
		"  ",
		app.StyleAccent.Render(fmt.Sprintf("%d/%d", done, total)),
		" ",
		app.StyleSubtle.Render("complete"),
		"   ",
		m.progress.ViewAs(pct),
		"   ",
		app.StyleInfo.Render(fmt.Sprintf("elapsed %s", elapsed)),
		"   ",
		failedBadge(failed),
	)

	cards := []string{}
	for i, id := range m.targets {
		cards = append(cards, m.renderCard(id, i == m.focusIdx))
	}
	cardsCol := lipgloss.JoinVertical(lipgloss.Left, cards...)
	m.cardsVP.SetContent(cardsCol)
	if m.focusIdx >= 0 && m.focusIdx < len(cards) && m.cardsVP.Height > 0 {
		focusedTop := 0
		for i := 0; i < m.focusIdx; i++ {
			focusedTop += lipgloss.Height(cards[i])
		}
		margin := m.cardsVP.Height / 4
		if focusedTop < m.cardsVP.YOffset+margin {
			off := focusedTop - margin
			if off < 0 {
				off = 0
			}
			m.cardsVP.YOffset = off
		} else if focusedTop > m.cardsVP.YOffset+m.cardsVP.Height-1-margin {
			off := focusedTop - (m.cardsVP.Height - 1 - margin)
			if off < 0 {
				off = 0
			}
			m.cardsVP.YOffset = off
		}
	}

	rightW, vpH := m.viewportDims()
	m.viewport.Width = rightW
	m.viewport.Height = vpH
	id := m.focusedID()
	st := m.states[id]
	tail := ""
	if st != nil {
		tail = strings.Join(colorizeOutput(st.output), "\n")
	}
	m.viewport.SetContent(tail)
	m.viewport.GotoBottom()
	vpTitle := app.StyleTitle.Render("output")
	if st != nil {
		ico, _ := m.statusBadge(st)
		vpTitle = lipgloss.JoinHorizontal(lipgloss.Top,
			ico, " ",
			app.StyleTitle.Render(st.fetcher.Name),
			"   ",
			app.StyleSubtle.Render(string(id)),
		)
	}
	if m.pinnedID != "" {
		vpTitle = lipgloss.JoinHorizontal(lipgloss.Top, vpTitle, "  ", app.StyleAccent.Render("📌 pinned"))
	}
	rightStyle := app.StyleBorderActive.Width(rightW).Height(vpH + 2)
	rightPane := rightStyle.Render(vpTitle + "\n" + m.viewport.View())

	leftStyle := lipgloss.NewStyle().MarginRight(1)
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		leftStyle.Render(m.cardsVP.View()),
		rightPane,
	)

	hints := []components.Hint{
		{Key: "↑/↓", Desc: "focus"},
		{Key: "p", Desc: "pin"},
		{Key: "c", Desc: "cancel"},
		{Key: "r", Desc: "retry"},
	}
	if m.finished {
		hints = append(hints, components.Hint{Key: "enter", Desc: "review"})
	}
	hints = append(hints, components.Hint{Key: "q", Desc: "quit"})
	footer := components.RenderFooter(width, hints)

	page := lipgloss.JoinVertical(lipgloss.Left,
		header,
		topbar,
		"",
		body,
		footer,
	)
	used := lipgloss.Height(page)
	if pad := height - used; pad > 0 {
		page = page + strings.Repeat("\n", pad)
	}
	return page
}

func failedBadge(n int) string {
	if n == 0 {
		return app.StyleSubtle.Render("0 failed")
	}
	return app.StyleDanger.Render(fmt.Sprintf("%d failed", n))
}

func colorizeOutput(lines []string) []string {
	out := make([]string, len(lines))
	for i, ln := range lines {
		l := ln
		switch {
		case strings.Contains(l, "ERROR"):
			out[i] = app.StyleDanger.Render(l)
		case strings.Contains(l, "WARN"):
			out[i] = app.StyleWarning.Render(l)
		case strings.HasPrefix(strings.TrimSpace(l), "==>"):
			out[i] = app.StyleAccent.Render(l)
		case strings.HasPrefix(strings.TrimSpace(l), "----"):
			out[i] = app.StyleSubtle.Render(l)
		case strings.HasPrefix(strings.TrimSpace(l), "ok:"):
			out[i] = app.StyleSuccess.Render(l)
		case strings.HasPrefix(strings.TrimSpace(l), "…"):
			out[i] = app.StyleInfo.Render(l)
		default:
			out[i] = l
		}
	}
	return out
}
