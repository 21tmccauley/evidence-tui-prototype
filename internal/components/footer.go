package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/paramify/evidence-tui-prototype/internal/app"
)

type Hint struct {
	Key  string
	Desc string
}

func RenderFooter(width int, hints []Hint) string {
	if width <= 0 {
		width = 80
	}
	parts := make([]string, 0, len(hints))
	for _, h := range hints {
		parts = append(parts,
			app.StyleKey.Render(h.Key)+" "+app.StyleHint.Render(h.Desc),
		)
	}
	body := strings.Join(parts, app.StyleSubtle.Render("  "))
	rule := lipgloss.NewStyle().Foreground(app.ColorSubtle).Render(strings.Repeat("─", width))
	return rule + "\n" + app.StyleFooter.Render(body)
}
