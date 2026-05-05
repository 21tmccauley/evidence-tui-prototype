package screens

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/paramify/evidence-tui-prototype/internal/app"
	"github.com/paramify/evidence-tui-prototype/internal/components"
	"github.com/paramify/evidence-tui-prototype/internal/mock"
	"github.com/paramify/evidence-tui-prototype/internal/secrets"
	"github.com/paramify/evidence-tui-prototype/internal/uploader"
)

type uploadPhase int

const (
	uploadIdle uploadPhase = iota
	uploadRunning
	uploadDone
)

type uploadTickMsg struct{}

// UploadFinishedMsg is emitted when a Paramify upload goroutine finishes.
type UploadFinishedMsg struct {
	Summary uploader.Summary
	Err     error
}

// OpenSecretsForReviewMsg routes from Review to the Secrets screen when the
// upload trigger discovers PARAMIFY_UPLOAD_API_TOKEN is unset (e.g. cleared
// mid-session). The root model returns to Review after Secrets is dismissed.
type OpenSecretsForReviewMsg struct{}

// ParamifyFactory builds a fresh uploader using current secret values. The
// factory is invoked at upload time, not at Review construction, so secrets
// edited during the session take effect without restarting the TUI.
type ParamifyFactory func() (uploader.Uploader, error)

func runParamifyUpload(c uploader.Uploader, dir string) tea.Cmd {
	return func() tea.Msg {
		sum, err := c.ProcessEvidenceDir(context.Background(), dir)
		return UploadFinishedMsg{Summary: sum, Err: err}
	}
}

type ReviewModel struct {
	keys        app.KeyMap
	profile     string
	results     []RunResult
	evidenceDir string

	cursor int
	offset int

	phase    uploadPhase
	progress progress.Model
	uploaded int

	store           secrets.Store
	paramifyFactory ParamifyFactory
	uploadSummary   uploader.Summary
	uploadErr       error

	width, height int
}

func NewReview(keys app.KeyMap, profile string, results []RunResult) ReviewModel {
	pr := progress.New(progress.WithGradient("#9ECE6A", "#7DCFFF"))
	pr.ShowPercentage = true
	return ReviewModel{
		keys:     keys,
		profile:  profile,
		results:  results,
		progress: pr,
	}
}

// WithEvidenceDir sets the absolute evidence path shown on Review (empty hides the row).
func (m ReviewModel) WithEvidenceDir(dir string) ReviewModel {
	m.evidenceDir = dir
	return m
}

// WithParamifyUpload enables Paramify upload from the Review screen.
// store is used to re-check PARAMIFY_UPLOAD_API_TOKEN at trigger time so the
// upload re-routes to Secrets if the token was cleared during the session.
// factory builds a fresh uploader with the latest environ at trigger time.
func (m ReviewModel) WithParamifyUpload(store secrets.Store, factory ParamifyFactory) ReviewModel {
	m.store = store
	m.paramifyFactory = factory
	return m
}

type QuitMsg struct{}

type RestartMsg struct{}

func (m ReviewModel) Init() tea.Cmd { return nil }

func (m ReviewModel) Resize(w, h int) ReviewModel {
	m.width, m.height = w, h
	m.progress.Width = w / 3
	return m
}

func (m ReviewModel) Update(msg tea.Msg) (ReviewModel, tea.Cmd) {
	const pageSize = 10
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.progress.Width = msg.Width / 3
	case uploadTickMsg:
		if m.phase == uploadRunning && m.paramifyFactory == nil {
			m.uploaded++
			if m.uploaded >= len(m.results) {
				m.phase = uploadDone
				return m, nil
			}
			return m, tea.Tick(180*time.Millisecond, func(time.Time) tea.Msg {
				return uploadTickMsg{}
			})
		}
	case UploadFinishedMsg:
		if m.phase == uploadRunning {
			m.phase = uploadDone
			m.uploadSummary = msg.Summary
			m.uploadErr = msg.Err
		}
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}
		case key.Matches(msg, m.keys.Down):
			if m.cursor < len(m.results)-1 {
				m.cursor++
			}
		case msg.String() == "pgup" || msg.String() == "ctrl+u":
			m.cursor -= pageSize
			if m.cursor < 0 {
				m.cursor = 0
			}
		case msg.String() == "pgdown" || msg.String() == "ctrl+d":
			m.cursor += pageSize
			if m.cursor > len(m.results)-1 {
				m.cursor = len(m.results) - 1
			}
		case msg.String() == "home":
			m.cursor = 0
		case msg.String() == "end":
			if len(m.results) > 0 {
				m.cursor = len(m.results) - 1
			}
		case key.Matches(msg, m.keys.Upload):
			if m.phase != uploadIdle {
				break
			}
			if m.evidenceDir == "" {
				// Demo mode: play the upload progress animation.
				m.phase = uploadRunning
				m.uploaded = 0
				return m, tea.Tick(180*time.Millisecond, func(time.Time) tea.Msg {
					return uploadTickMsg{}
				})
			}
			if m.paramifyFactory == nil {
				m.phase = uploadRunning
				return m, func() tea.Msg {
					return UploadFinishedMsg{Err: secrets.ErrNotConfigured}
				}
			}
			if m.store != nil {
				if _, found, err := m.store.Get(secrets.KeyParamifyUploadAPIToken); err == nil && !found {
					return m, func() tea.Msg { return OpenSecretsForReviewMsg{} }
				}
			}
			c, err := m.paramifyFactory()
			if err != nil || c == nil {
				m.phase = uploadRunning
				return m, func() tea.Msg {
					if err == nil {
						err = secrets.ErrNotConfigured
					}
					return UploadFinishedMsg{Err: err}
				}
			}
			m.phase = uploadRunning
			return m, runParamifyUpload(c, m.evidenceDir)
		case key.Matches(msg, m.keys.Export):
		case key.Matches(msg, m.keys.Back):
			return m, func() tea.Msg { return RestartMsg{} }
		case key.Matches(msg, m.keys.Quit):
			return m, func() tea.Msg { return QuitMsg{} }
		}
	}

	if m.cursor < m.offset {
		m.offset = m.cursor
	} else if m.cursor >= m.offset+pageSize {
		m.offset = m.cursor - (pageSize - 1)
	}
	maxOffset := maxInt(0, len(m.results)-pageSize)
	if m.offset > maxOffset {
		m.offset = maxOffset
	}

	return m, nil
}

func (m ReviewModel) View() string {
	width := m.width
	if width <= 0 {
		width = 120
	}
	height := m.height
	if height <= 0 {
		height = 30
	}

	hints := []components.Hint{
		{Key: "↑/↓", Desc: "select"},
		{Key: "pgup/pgdn", Desc: "page"},
		{Key: "u", Desc: "upload"},
		{Key: "e", Desc: "export"},
		{Key: "esc", Desc: "back"},
		{Key: "q", Desc: "quit"},
	}
	footer := components.RenderFooter(width, hints)

	header := components.RenderHeader(components.HeaderProps{
		Width:   width,
		Crumb:   "review",
		Profile: m.profile,
		Region:  "us-east-1",
		Now:     time.Now(),
	})

	counts := map[mock.RunStatus]int{}
	totalDur := time.Duration(0)
	for _, r := range m.results {
		counts[r.Status]++
		totalDur += r.Duration
	}
	tile := func(label string, n int, st lipgloss.Style) string {
		return st.Render(fmt.Sprintf("%2d  %s", n, label))
	}
	tiles := lipgloss.JoinHorizontal(lipgloss.Top,
		tile("ok", counts[mock.StatusOK], app.StyleBadgeOK.Bold(true)),
		"   ",
		tile("partial", counts[mock.StatusPartial], app.StyleBadgeWarn.Bold(true)),
		"   ",
		tile("failed", counts[mock.StatusFailed], app.StyleBadgeFail.Bold(true)),
		"   ",
		tile("cancelled", counts[mock.StatusCancelled], app.StyleBadgeCancel),
		"   ",
		app.StyleSubtle.Render(fmt.Sprintf("· total %s", totalDur.Round(time.Second))),
	)
	tilesBox := app.StyleBorderActive.Width(width - 4).Render(tiles)

	contentWidth := width - 6
	if contentWidth < 40 {
		contentWidth = 40
	}
	clamp := func(v, minV, maxV int) int {
		if v < minV {
			return minV
		}
		if v > maxV {
			return maxV
		}
		return v
	}
	durW := 12
	statusW := 12
	sourceW := clamp(14, 10, 20)
	minFetcherW := 20
	fetcherW := contentWidth - (sourceW + statusW + durW)
	if fetcherW < minFetcherW {
		need := minFetcherW - fetcherW
		shrink := func(w *int, minW int) {
			if need <= 0 {
				return
			}
			if *w > minW {
				d := *w - minW
				if d > need {
					d = need
				}
				*w -= d
				need -= d
			}
		}
		shrink(&sourceW, 10)
		shrink(&statusW, 8)
		shrink(&durW, 10)
		fetcherW = contentWidth - (sourceW + statusW + durW)
		if fetcherW < minFetcherW {
			fetcherW = minFetcherW
		}
	}

	fixed := func(w int, s string) string {
		if w <= 0 {
			return ""
		}
		return lipgloss.NewStyle().Width(w).Render(s)
	}

	tableLines := []string{
		lipgloss.JoinHorizontal(lipgloss.Top,
			app.StyleSubtle.Render(fixed(fetcherW, "    fetcher")),
			app.StyleSubtle.Render(fixed(sourceW, "source")),
			app.StyleSubtle.Render(fixed(statusW, "status")),
			app.StyleSubtle.Render(fixed(durW, "duration")),
		),
		app.StyleSubtle.Render(strings.Repeat("─", contentWidth)),
	}
	const pageSize = 10
	start := m.offset
	if start < 0 {
		start = 0
	}
	if start > len(m.results) {
		start = len(m.results)
	}
	end := start + pageSize
	if end > len(m.results) {
		end = len(m.results)
	}

	for i := start; i < end; i++ {
		r := m.results[i]
		ico, statusLabel := badgeFor(r.Status)
		prefix := fmt.Sprintf(" %s  ", ico)
		nameW := fetcherW - lipgloss.Width(prefix)
		if nameW < 0 {
			nameW = 0
		}
		fetcherCell := fixed(fetcherW, prefix+fixed(nameW, r.Name))
		row := lipgloss.JoinHorizontal(lipgloss.Top,
			fetcherCell,
			app.StyleInfo.Render(fixed(sourceW, r.Source)),
			fixed(statusW, statusLabel),
			app.StyleSubtle.Render(fixed(durW, r.Duration.Round(100*time.Millisecond).String())),
		)
		if i == m.cursor {
			row = app.StyleSelected.Render(padRight(row, width-8))
		}
		tableLines = append(tableLines, row)
	}

	if len(m.results) > pageSize {
		tableLines = append(tableLines, app.StyleSubtle.Render(
			fmt.Sprintf("  showing %d–%d of %d", start+1, end, len(m.results)),
		))
	}
	tableBox := app.StyleBorder.Width(width - 4).Render(strings.Join(tableLines, "\n"))

	detail := ""
	if m.cursor >= 0 && m.cursor < len(m.results) {
		r := m.results[m.cursor]
		header := lipgloss.JoinHorizontal(lipgloss.Top,
			app.StyleAccent.Render(r.Name),
			app.StyleSubtle.Render("  ·  "),
			app.StyleInfo.Render(string(r.ID)),
		)
		tail := []string{}
		for _, ln := range r.OutputTail {
			tail = append(tail, "  "+ln)
		}
		if len(tail) == 0 {
			tail = []string{app.StyleSubtle.Render("  (no output captured)")}
		}
		detail = header + "\n" + strings.Join(tail, "\n")
	}
	detailBox := app.StyleBorder.Width(width - 4).Render(detail)

	var uploadRow string
	switch m.phase {
	case uploadIdle:
		uploadRow = app.StyleSubtle.Render("  press ") + app.StyleKey.Render("u") + app.StyleSubtle.Render(" to upload to paramify")
	case uploadRunning:
		if m.paramifyFactory != nil && m.evidenceDir != "" {
			uploadRow = "  " + app.StyleAccent.Render("uploading to Paramify…")
			break
		}
		pct := float64(m.uploaded) / float64(maxInt(len(m.results), 1))
		uploadRow = lipgloss.JoinHorizontal(lipgloss.Top,
			"  ",
			app.StyleAccent.Render("uploading"),
			"  ",
			m.progress.ViewAs(pct),
			"  ",
			app.StyleSubtle.Render(fmt.Sprintf("%d / %d", m.uploaded, len(m.results))),
		)
	case uploadDone:
		switch {
		case m.uploadErr != nil:
			errText := m.uploadErr.Error()
			if errors.Is(m.uploadErr, secrets.ErrNotConfigured) {
				errText = "set PARAMIFY_UPLOAD_API_TOKEN in Secrets (press s on Welcome) or export it before launch"
			}
			uploadRow = lipgloss.JoinHorizontal(lipgloss.Top,
				"  ",
				app.StyleBadgeFail.Render("upload failed"),
				"  ",
				app.StyleSubtle.Render(errText),
			)
		case m.paramifyFactory != nil && m.evidenceDir != "":
			s := m.uploadSummary
			uploadRow = "  " + app.StyleSuccess.Render("✓ upload complete ") +
				app.StyleSubtle.Render(fmt.Sprintf(
					"(%d ok · %d failed · %d skipped · %d rows in upload_log.json)",
					s.Successful, s.FailedUploads, s.Skipped, len(s.Results),
				))
		default:
			uploadRow = "  " + app.StyleSuccess.Render("✓ uploaded ") +
				app.StyleSubtle.Render(fmt.Sprintf("%d evidence sets to paramify-prod", len(m.results)))
		}
	}

	rows := []string{
		"",
		tilesBox,
		"",
		tableBox,
		"",
		detailBox,
		"",
		uploadRow,
	}
	if m.evidenceDir != "" {
		rows = append(rows, "  "+app.StyleSubtle.Render("evidence: ")+app.StyleInfo.Render(m.evidenceDir))
	}
	bodyContent := lipgloss.JoinVertical(lipgloss.Left, rows...)

	page := lipgloss.JoinVertical(lipgloss.Left,
		header,
		bodyContent,
		footer,
	)
	used := lipgloss.Height(page)
	if pad := height - used; pad > 0 {
		page = page + strings.Repeat("\n", pad)
	}
	return page
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func badgeFor(s mock.RunStatus) (string, string) {
	switch s {
	case mock.StatusOK:
		return app.StyleBadgeOK.Render("✓"), app.StyleBadgeOK.Render("ok")
	case mock.StatusPartial:
		return app.StyleBadgeWarn.Render("⚠"), app.StyleBadgeWarn.Render("partial")
	case mock.StatusFailed:
		return app.StyleBadgeFail.Render("✗"), app.StyleBadgeFail.Render("failed")
	case mock.StatusCancelled:
		return app.StyleBadgeCancel.Render("∅"), app.StyleBadgeCancel.Render("cancelled")
	case mock.StatusRunning:
		return app.StyleBadgeRun.Render("…"), app.StyleBadgeRun.Render("running")
	case mock.StatusQueued:
		return app.StyleBadgeQueue.Render("◌"), app.StyleBadgeQueue.Render("queued")
	}
	return " ", ""
}
