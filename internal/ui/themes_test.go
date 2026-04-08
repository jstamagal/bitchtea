package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestSetTheme(t *testing.T) {
	originalTheme := Theme

	tests := []struct {
		name   string
		theme  string
		wantOK bool
	}{
		{"valid theme bitchx", "bitchx", true},
		{"unknown theme", "nonexistent", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetTheme("bitchx")

			got := SetTheme(tt.theme)
			if got != tt.wantOK {
				t.Errorf("SetTheme(%q) = %v, want %v", tt.theme, got, tt.wantOK)
			}
		})
	}

	SetTheme(originalTheme.Name)
}

func TestSetThemeUpdatesStyles(t *testing.T) {
	originalName := Theme.Name
	defer SetTheme(originalName)

	SetTheme("bitchx")

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

func TestSetThemeUnknownDoesNotChangeState(t *testing.T) {
	originalName := Theme.Name
	defer SetTheme(originalName)

	SetTheme("bitchx")
	before := Theme

	ok := SetTheme("totally-fake-theme")
	if ok {
		t.Error("SetTheme should return false for unknown theme")
	}

	if Theme.Name != before.Name {
		t.Errorf("Theme.Name changed from %q to %q after failed SetTheme", before.Name, Theme.Name)
	}
	if Theme.Cyan != before.Cyan {
		t.Error("Theme.Cyan changed after failed SetTheme")
	}
}

func TestAllThemes(t *testing.T) {
	originalName := Theme.Name
	defer SetTheme(originalName)

	themeNames := ListThemes()

	expectedThemes := []string{"bitchx"}
	for _, expected := range expectedThemes {
		found := false
		for _, name := range themeNames {
			if name == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ListThemes() missing %q", expected)
		}
	}

	for _, name := range themeNames {
		t.Run("theme_"+name, func(t *testing.T) {
			ok := SetTheme(name)
			if !ok {
				t.Fatalf("SetTheme(%q) returned false — theme not found in map", name)
			}

			if CurrentThemeName() == "" {
				t.Error("CurrentThemeName() returned empty string")
			}

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
					t.Errorf("%s.Render(\"x\") returned empty for theme %q", styleName, name)
				}
			}

			sep := Separator(40)
			if sep == "" {
				t.Errorf("Separator(40) returned empty for theme %q", name)
			}
		})
	}
}

func TestListThemesCompleteness(t *testing.T) {
	names := ListThemes()
	if len(names) != len(themes) {
		t.Errorf("ListThemes returned %d names but themes map has %d entries", len(names), len(themes))
	}

	for _, name := range names {
		if _, ok := themes[name]; !ok {
			t.Errorf("ListThemes returned %q which is not in themes map", name)
		}
	}
}

func TestListThemesSorted(t *testing.T) {
	names := ListThemes()
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Errorf("ListThemes not sorted: %q comes after %q", names[i], names[i-1])
		}
	}
}

func TestCurrentThemeName(t *testing.T) {
	originalName := Theme.Name
	defer SetTheme(originalName)

	SetTheme("bitchx")
	if got := CurrentThemeName(); got != "BitchX" {
		t.Errorf("CurrentThemeName() = %q, want %q", got, "BitchX")
	}
}

func TestThinkingBarStyleUsesThemeColors(t *testing.T) {
	originalName := Theme.Name
	defer SetTheme(originalName)

	SetTheme("bitchx")

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
	originalName := Theme.Name
	defer SetTheme(originalName)

	for _, name := range ListThemes() {
		t.Run(name, func(t *testing.T) {
			ok := SetTheme(name)
			if !ok {
				t.Fatalf("SetTheme(%q) failed", name)
			}

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
					t.Errorf("Theme.%s is empty for theme %q", colorName, name)
				}
			}
		})
	}
}
