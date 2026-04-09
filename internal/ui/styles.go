package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// BitchX color palette — dark, aggressive, high contrast
var (
	// Core colors
	ColorCyan    = lipgloss.Color("14")  // bright cyan — primary accent
	ColorGreen   = lipgloss.Color("10")  // bright green — user nicks, success
	ColorMagenta = lipgloss.Color("13")  // bright magenta — agent nicks
	ColorYellow  = lipgloss.Color("11")  // bright yellow — warnings, system
	ColorRed     = lipgloss.Color("9")   // bright red — errors
	ColorBlue    = lipgloss.Color("12")  // bright blue — info, paths
	ColorWhite   = lipgloss.Color("15")  // bright white — primary text
	ColorGray    = lipgloss.Color("8")   // dark gray — timestamps, dim text
	ColorDarkBg  = lipgloss.Color("0")   // black background

	// Styles
	TopBarStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorWhite).
			Background(lipgloss.Color("4")). // dark blue bg
			Padding(0, 1)

	BottomBarStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorWhite).
			Background(lipgloss.Color("4")).
			Padding(0, 1)

	TimestampStyle = lipgloss.NewStyle().
			Foreground(ColorGray)

	UserNickStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorGreen)

	AgentNickStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorMagenta)

	SystemMsgStyle = lipgloss.NewStyle().
			Foreground(ColorYellow).
			Bold(true)

	ErrorMsgStyle = lipgloss.NewStyle().
			Foreground(ColorRed).
			Bold(true)

	ToolCallStyle = lipgloss.NewStyle().
			Foreground(ColorCyan)

	ToolOutputStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("7")) // normal white

	InputPromptStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorCyan)

	InputTextStyle = lipgloss.NewStyle().
			Foreground(ColorWhite)

	SeparatorStyle = lipgloss.NewStyle().
			Foreground(ColorGray)

	ThinkingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("5")). // magenta
			Italic(true)

	StatsStyle = lipgloss.NewStyle().
			Foreground(ColorGray)

	DimStyle = lipgloss.NewStyle().
			Foreground(ColorGray)

	BoldWhite = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorWhite)

	ThinkingBarStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorWhite).
				Background(lipgloss.Color("4")).
				Padding(0, 1)
)

// Separator returns a full-width separator line
func Separator(width int) string {
	return SeparatorStyle.Render(strings.Repeat("─", width))
}
