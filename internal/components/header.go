package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/paramify/evidence-tui-prototype/internal/app"
)

type HeaderProps struct {
	Profile   string
	Region    string
	Crumb     string
	Width     int
	Now       time.Time
	StatusDot string // optional small status dot/text on the right
}

// Render produces the persistent top bar.
func RenderHeader(p HeaderProps) string {
	if p.Width <= 0 {
		p.Width = 80
	}
	title := app.StyleTitle.Render("paramify fetcher")
	dot := app.StyleAccent.Render("•")

	left := lipgloss.JoinHorizontal(lipgloss.Top,
		title, " ", dot, " ",
		app.StyleSubtle.Render("evidence tui"),
	)

	mid := app.StyleInfo.Render(p.Crumb)

	now := p.Now
	if now.IsZero() {
		now = time.Now()
	}
	rightParts := []string{}
	if p.Profile != "" {
		rightParts = append(rightParts, app.StyleAccent.Render(p.Profile))
	}
	if p.Region != "" {
		rightParts = append(rightParts, app.StyleSubtle.Render(p.Region))
	}
	rightParts = append(rightParts, app.StyleSubtle.Render(now.Format("15:04:05")))
	if p.StatusDot != "" {
		rightParts = append(rightParts, p.StatusDot)
	}
	right := strings.Join(rightParts, app.StyleSubtle.Render(" │ "))

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	midW := lipgloss.Width(mid)
	gap := p.Width - leftW - midW - rightW - 4
	if gap < 1 {
		gap = 1
	}
	pad1 := (gap) / 2
	pad2 := gap - pad1

	bar := lipgloss.JoinHorizontal(lipgloss.Top,
		left,
		strings.Repeat(" ", pad1+1),
		mid,
		strings.Repeat(" ", pad2+1),
		right,
	)

	rule := lipgloss.NewStyle().Foreground(app.ColorSubtle).Render(strings.Repeat("─", p.Width))
	return fmt.Sprintf("%s\n%s", bar, rule)
}
