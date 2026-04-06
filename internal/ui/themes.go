package ui

import (
	"sort"

	"github.com/charmbracelet/lipgloss"
)

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
}{
	Name:    "bitchx",
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
}

// Built-in themes
var themes = map[string]struct {
	Name    string
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
}{
	"bitchx": {
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
	},
	"nord": {
		Name:    "Nord",
		Cyan:    lipgloss.Color("6"),
		Green:   lipgloss.Color("14"),
		Magenta: lipgloss.Color("13"),
		Yellow:  lipgloss.Color("3"),
		Red:     lipgloss.Color("1"),
		Blue:    lipgloss.Color("4"),
		White:   lipgloss.Color("7"),
		Gray:    lipgloss.Color("8"),
		DarkBg:  lipgloss.Color("0"),
		BarBg:   lipgloss.Color("10"),
	},
	"dracula": {
		Name:    "Dracula",
		Cyan:    lipgloss.Color("6"),
		Green:   lipgloss.Color("76"),
		Magenta: lipgloss.Color("13"),
		Yellow:  lipgloss.Color("11"),
		Red:     lipgloss.Color("9"),
		Blue:    lipgloss.Color("4"),
		White:   lipgloss.Color("7"),
		Gray:    lipgloss.Color("8"),
		DarkBg:  lipgloss.Color("0"),
		BarBg:   lipgloss.Color("13"),
	},
	"gruvbox": {
		Name:    "Gruvbox",
		Cyan:    lipgloss.Color("109"),
		Green:   lipgloss.Color("142"),
		Magenta: lipgloss.Color("175"),
		Yellow:  lipgloss.Color("214"),
		Red:     lipgloss.Color("167"),
		Blue:    lipgloss.Color("109"),
		White:   lipgloss.Color("223"),
		Gray:    lipgloss.Color("245"),
		DarkBg:  lipgloss.Color("235"),
		BarBg:   lipgloss.Color("237"),
	},
	"monokai": {
		Name:    "Monokai",
		Cyan:    lipgloss.Color("45"),
		Green:   lipgloss.Color("76"),
		Magenta: lipgloss.Color("5"),
		Yellow:  lipgloss.Color("3"),
		Red:     lipgloss.Color("1"),
		Blue:    lipgloss.Color("4"),
		White:   lipgloss.Color("15"),
		Gray:    lipgloss.Color("7"),
		DarkBg:  lipgloss.Color("0"),
		BarBg:   lipgloss.Color("13"),
	},
}

// ListThemes returns all available theme names
func ListThemes() []string {
	names := make([]string, 0, len(themes))
	for name := range themes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// SetTheme sets the active theme and rebuilds styles
func SetTheme(name string) bool {
	t, ok := themes[name]
	if !ok {
		return false
	}
	Theme = struct {
		Name    string
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
	}(t)
	rebuildStyles()
	return true
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
}

func init() {
	rebuildStyles()
}
