package app

import (
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Logo sheen timing (welcome screen + cmd/logo-demo).
const (
	LogoSheenSweepStep   = 20 * time.Millisecond
	LogoSheenIdleBetween = 6 * time.Second
	LogoSheenRadius      = 10
)

var logoSheenMaxCol = sync.OnceValue(computeLogoSheenMaxCol)

func computeLogoSheenMaxCol() int {
	max := 0
	for _, ln := range LogoLines() {
		n := len([]rune(ln))
		if n > max {
			max = n
		}
	}
	return max
}

// LogoSheenMaxColumn is the rune width of the widest logo line.
func LogoSheenMaxColumn() int {
	return logoSheenMaxCol()
}

var (
	logoSheenStyleBase = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	logoSheenStyleGlow = lipgloss.NewStyle().Foreground(ColorMuted).Bold(true)
	logoSheenStylePeak = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	logoSheenStyleCore = lipgloss.NewStyle().Foreground(ColorFg).Bold(true)
)

func logoSheenStyleForDist(dist int) lipgloss.Style {
	switch {
	case dist <= 1:
		return logoSheenStyleCore
	case dist <= 3:
		return logoSheenStylePeak
	case dist <= 6:
		return logoSheenStyleGlow
	default:
		return logoSheenStyleBase
	}
}

// RenderLogoSheen paints LogoLines with a vertical sheen centered at column center (0-based rune index per line).
func RenderLogoSheen(center int) string {
	lines := LogoLines()
	var b strings.Builder
	for _, line := range lines {
		runes := []rune(line)
		for i, r := range runes {
			d := i - center
			if d < 0 {
				d = -d
			}
			b.WriteString(logoSheenStyleForDist(d).Render(string(r)))
		}
		b.WriteByte('\n')
	}
	return strings.TrimSuffix(b.String(), "\n")
}
