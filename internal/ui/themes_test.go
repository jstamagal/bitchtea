package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestBuiltInThemeUpdatesStyles(t *testing.T) {
	// All style vars should produce non-empty output after setting theme
	rendered := TopBarStyle.Render("test")
	if rendered == "" {
		t.Error("TopBarStyle.Render produced empty string")
	}

	rendered = UserNickStyle.Render("user")
	if rendered == "" {
		t.Error("UserNickStyle.Render produced empty string")
	}

	rendered = ErrorMsgStyle.Render("error")
	if rendered == "" {
		t.Error("ErrorMsgStyle.Render produced empty string")
	}

	rendered = ThinkingBarStyle.Render("thinking...")
	if rendered == "" {
		t.Error("ThinkingBarStyle.Render produced empty string")
	}
}

func TestBuiltInThemeStylesRemainNonEmpty(t *testing.T) {
	styles := map[string]lipgloss.Style{
		"TopBarStyle":      TopBarStyle,
		"BottomBarStyle":   BottomBarStyle,
		"TimestampStyle":   TimestampStyle,
		"UserNickStyle":    UserNickStyle,
		"AgentNickStyle":   AgentNickStyle,
		"SystemMsgStyle":   SystemMsgStyle,
		"ErrorMsgStyle":    ErrorMsgStyle,
		"ToolCallStyle":    ToolCallStyle,
		"ToolOutputStyle":  ToolOutputStyle,
		"InputPromptStyle": InputPromptStyle,
		"InputTextStyle":   InputTextStyle,
		"SeparatorStyle":   SeparatorStyle,
		"ThinkingStyle":    ThinkingStyle,
		"ThinkingBarStyle": ThinkingBarStyle,
		"StatsStyle":       StatsStyle,
		"DimStyle":         DimStyle,
		"BoldWhite":        BoldWhite,
	}

	for styleName, style := range styles {
		rendered := style.Render("x")
		if rendered == "" {
			t.Errorf("%s.Render(\"x\") returned empty", styleName)
		}
	}

	sep := Separator(40)
	if sep == "" {
		t.Error("Separator(40) returned empty")
	}
}

func TestCurrentThemeName(t *testing.T) {
	if got := CurrentThemeName(); got != "BitchX" {
		t.Errorf("CurrentThemeName() = %q, want %q", got, "BitchX")
	}
}

func TestThinkingBarStyleUsesThemeColors(t *testing.T) {
	if string(Theme.ThinkingBarFg) == "" {
		t.Error("ThinkingBarFg is empty")
	}
	if string(Theme.ThinkingBarBg) == "" {
		t.Error("ThinkingBarBg is empty")
	}

	// ThinkingBar colors should differ from BarBg (that's the whole point)
	if Theme.ThinkingBarBg == Theme.BarBg {
		t.Errorf("ThinkingBarBg (%s) should differ from BarBg (%s)", Theme.ThinkingBarBg, Theme.BarBg)
	}
}

func TestSeparatorNonEmpty(t *testing.T) {
	short := Separator(5)
	long := Separator(80)

	if short == "" {
		t.Error("Separator(5) returned empty")
	}
	if long == "" {
		t.Error("Separator(80) returned empty")
	}
	if len(strings.TrimRight(short, "─")) > len(strings.TrimRight(long, "─")) {
		t.Error("Separator(80) should not be shorter than Separator(5)")
	}
}

func TestThemeAllColorsNonZero(t *testing.T) {
	colors := map[string]string{
		"Cyan":          string(Theme.Cyan),
		"Green":         string(Theme.Green),
		"Magenta":       string(Theme.Magenta),
		"Yellow":        string(Theme.Yellow),
		"Red":           string(Theme.Red),
		"Blue":          string(Theme.Blue),
		"White":         string(Theme.White),
		"Gray":          string(Theme.Gray),
		"DarkBg":        string(Theme.DarkBg),
		"BarBg":         string(Theme.BarBg),
		"ThinkingBarFg": string(Theme.ThinkingBarFg),
		"ThinkingBarBg": string(Theme.ThinkingBarBg),
	}

	for colorName, val := range colors {
		if val == "" {
			t.Errorf("Theme.%s is empty", colorName)
		}
	}
}
