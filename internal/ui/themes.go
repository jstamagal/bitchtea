package ui

import "github.com/charmbracelet/lipgloss"

// Theme holds colors for the TUI
var Theme = struct {
	Name string

	Cyan    lipgloss.Color
	Green   lipgloss.Color
	Magenta lipgloss.Color
	Yellow  lipgloss.Color
	Red     lipgloss.Color
	Blue    lipgloss.Color
	White   lipgloss.Color
	Gray    lipgloss.Color
	DarkBg  lipgloss.Color
	BarBg   lipgloss.Color

	ThinkingBarFg lipgloss.Color
	ThinkingBarBg lipgloss.Color
}{
	Name:    "BitchX",
	Cyan:    lipgloss.Color("14"),
	Green:   lipgloss.Color("10"),
	Magenta: lipgloss.Color("13"),
	Yellow:  lipgloss.Color("11"),
	Red:     lipgloss.Color("9"),
	Blue:    lipgloss.Color("12"),
	White:   lipgloss.Color("15"),
	Gray:    lipgloss.Color("8"),
	DarkBg:  lipgloss.Color("0"),
	BarBg:   lipgloss.Color("4"),

	ThinkingBarFg: lipgloss.Color("15"),
	ThinkingBarBg: lipgloss.Color("4"),
}

// CurrentThemeName returns the name of the active theme
func CurrentThemeName() string {
	return Theme.Name
}

// rebuildStyles recreates all styles from the current theme
func rebuildStyles() {
	TopBarStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(Theme.White).
		Background(Theme.BarBg).
		Padding(0, 1)

	BottomBarStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(Theme.White).
		Background(Theme.BarBg).
		Padding(0, 1)

	TimestampStyle = lipgloss.NewStyle().
		Foreground(Theme.Gray)

	UserNickStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(Theme.Green)

	AgentNickStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(Theme.Magenta)

	SystemMsgStyle = lipgloss.NewStyle().
		Foreground(Theme.Yellow).
		Bold(true)

	ErrorMsgStyle = lipgloss.NewStyle().
		Foreground(Theme.Red).
		Bold(true)

	ToolCallStyle = lipgloss.NewStyle().
		Foreground(Theme.Cyan)

	ToolOutputStyle = lipgloss.NewStyle().
		Foreground(Theme.Gray)

	InputPromptStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(Theme.Cyan)

	InputTextStyle = lipgloss.NewStyle().
		Foreground(Theme.White)

	SeparatorStyle = lipgloss.NewStyle().
		Foreground(Theme.Gray)

	ThinkingStyle = lipgloss.NewStyle().
		Foreground(Theme.Magenta).
		Italic(true)

	StatsStyle = lipgloss.NewStyle().
		Foreground(Theme.Gray)

	DimStyle = lipgloss.NewStyle().
		Foreground(Theme.Gray)

	BoldWhite = lipgloss.NewStyle().
		Bold(true).
		Foreground(Theme.White)

	ThinkingBarStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(Theme.ThinkingBarFg).
		Background(Theme.ThinkingBarBg).
		Padding(0, 1)
}

func init() {
	rebuildStyles()
}
