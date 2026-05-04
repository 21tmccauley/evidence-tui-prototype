package app

import "github.com/charmbracelet/lipgloss"

// Tokyo Night-inspired palette.
var (
	ColorBg      = lipgloss.Color("#1A1B26")
	ColorFg      = lipgloss.Color("#C0CAF5")
	ColorSubtle  = lipgloss.Color("#565F89")
	ColorMuted   = lipgloss.Color("#7AA2F7")
	ColorPrimary = lipgloss.Color("#7AA2F7") // blue
	ColorAccent  = lipgloss.Color("#BB9AF7") // purple
	ColorCyan    = lipgloss.Color("#7DCFFF")
	ColorSuccess = lipgloss.Color("#9ECE6A")
	ColorWarning = lipgloss.Color("#E0AF68")
	ColorDanger  = lipgloss.Color("#F7768E")
	ColorOrange  = lipgloss.Color("#FF9E64")
)

var (
	StyleBase = lipgloss.NewStyle().Foreground(ColorFg)

	StyleTitle = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true)

	StyleSubtle = lipgloss.NewStyle().Foreground(ColorSubtle)

	StyleAccent = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)

	StyleSuccess = lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true)
	StyleWarning = lipgloss.NewStyle().Foreground(ColorWarning).Bold(true)
	StyleDanger  = lipgloss.NewStyle().Foreground(ColorDanger).Bold(true)
	StyleInfo    = lipgloss.NewStyle().Foreground(ColorCyan)

	StyleSelected = lipgloss.NewStyle().
			Foreground(ColorBg).
			Background(ColorPrimary).
			Bold(true)

	StyleBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorSubtle).
			Padding(0, 1)

	StyleBorderActive = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorPrimary).
				Padding(0, 1)

	StyleHeader = lipgloss.NewStyle().
			Foreground(ColorFg).
			Background(ColorBg).
			Padding(0, 1).
			Bold(true)

	StyleFooter = lipgloss.NewStyle().
			Foreground(ColorSubtle).
			Padding(0, 1)

	StyleKey  = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	StyleHint = lipgloss.NewStyle().Foreground(ColorSubtle)

	StyleBadgeOK     = lipgloss.NewStyle().Foreground(ColorSuccess)
	StyleBadgeRun    = lipgloss.NewStyle().Foreground(ColorPrimary)
	StyleBadgeQueue  = lipgloss.NewStyle().Foreground(ColorSubtle)
	StyleBadgeFail   = lipgloss.NewStyle().Foreground(ColorDanger)
	StyleBadgeWarn   = lipgloss.NewStyle().Foreground(ColorWarning)
	StyleBadgeStall  = lipgloss.NewStyle().Foreground(ColorOrange)
	StyleBadgeCancel = lipgloss.NewStyle().Foreground(ColorSubtle).Strikethrough(true)
)

// LogoLines returns the ASCII title used on the welcome screen.
func LogoLines() []string {
	return []string{
		" ██████╗  █████╗ ██████╗  █████╗ ███╗   ███╗██╗███████╗██╗   ██╗",
		" ██╔══██╗██╔══██╗██╔══██╗██╔══██╗████╗ ████║██║██╔════╝╚██╗ ██╔╝",
		" ██████╔╝███████║██████╔╝███████║██╔████╔██║██║█████╗   ╚████╔╝ ",
		" ██╔═══╝ ██╔══██║██╔══██╗██╔══██║██║╚██╔╝██║██║██╔══╝    ╚██╔╝  ",
		" ██║     ██║  ██║██║  ██║██║  ██║██║ ╚═╝ ██║██║██║        ██║   ",
		" ╚═╝     ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝  ╚═╝╚═╝     ╚═╝╚═╝╚═╝        ╚═╝   ",
	}
}
