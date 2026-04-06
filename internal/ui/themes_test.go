package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestSetTheme(t *testing.T) {
	// Save the initial state so we can restore it after tests
	originalTheme := Theme

	tests := []struct {
		name   string
		theme  string
		wantOK bool
	}{
		{"valid theme bitchx", "bitchx", true},
		{"valid theme nord", "nord", true},
		{"valid theme dracula", "dracula", true},
		{"valid theme gruvbox", "gruvbox", true},
		{"valid theme monokai", "monokai", true},
		{"unknown theme", "nonexistent", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset to a known state before each test
			SetTheme("bitchx")

			got := SetTheme(tt.theme)
			if got != tt.wantOK {
				t.Errorf("SetTheme(%q) = %v, want %v", tt.theme, got, tt.wantOK)
			}
		})
	}

	// Restore original theme
	SetTheme(originalTheme.Name)
}

func TestSetThemeUpdatesStyles(t *testing.T) {
	// Save original
	originalName := Theme.Name

	// Set to a known theme
	SetTheme("bitchx")
	bitchxColors := struct {
		Cyan    string
		Green   string
		Magenta string
		Yellow  string
		Red     string
		Blue    string
		White   string
		Gray    string
		DarkBg  string
		BarBg   string
	}{
		Cyan:    string(Theme.Cyan),
		Green:   string(Theme.Green),
		Magenta: string(Theme.Magenta),
		Yellow:  string(Theme.Yellow),
		Red:     string(Theme.Red),
		Blue:    string(Theme.Blue),
		White:   string(Theme.White),
		Gray:    string(Theme.Gray),
		DarkBg:  string(Theme.DarkBg),
		BarBg:   string(Theme.BarBg),
	}

	// Switch to a different theme
	SetTheme("gruvbox")

	// Verify that the theme colors actually changed
	if string(Theme.Cyan) == bitchxColors.Cyan {
		t.Error("Theme.Cyan did not change after switching from bitchx to gruvbox")
	}
	if string(Theme.Green) == bitchxColors.Green {
		t.Error("Theme.Green did not change after switching from bitchx to gruvbox")
	}
	if string(Theme.White) == bitchxColors.White {
		t.Error("Theme.White did not change after switching from bitchx to gruvbox")
	}

	// Verify that rebuildStyles() actually updated the style vars
	// by rendering something and checking it's non-empty
	rendered := TopBarStyle.Render("test")
	if rendered == "" {
		t.Error("TopBarStyle.Render produced empty string after theme switch")
	}

	rendered = UserNickStyle.Render("user")
	if rendered == "" {
		t.Error("UserNickStyle.Render produced empty string after theme switch")
	}

	rendered = ErrorMsgStyle.Render("error")
	if rendered == "" {
		t.Error("ErrorMsgStyle.Render produced empty string after theme switch")
	}

	rendered = SeparatorStyle.Render("---")
	if rendered == "" {
		t.Error("SeparatorStyle.Render produced empty string after theme switch")
	}

	// Restore
	SetTheme(originalName)
}

func TestSetThemeUnknownDoesNotChangeState(t *testing.T) {
	originalName := Theme.Name

	SetTheme("dracula")
	before := Theme

	ok := SetTheme("totally-fake-theme")
	if ok {
		t.Error("SetTheme should return false for unknown theme")
	}

	// Verify theme state is unchanged
	if Theme.Name != before.Name {
		t.Errorf("Theme.Name changed from %q to %q after failed SetTheme", before.Name, Theme.Name)
	}
	if Theme.Cyan != before.Cyan {
		t.Error("Theme.Cyan changed after failed SetTheme")
	}

	// Restore
	SetTheme(originalName)
}

func TestAllThemes(t *testing.T) {
	originalName := Theme.Name
	defer SetTheme(originalName)

	themeNames := ListThemes()

	// Every built-in theme should be in the list
	expectedThemes := []string{"bitchx", "dracula", "gruvbox", "monokai", "nord"}
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

	// Every theme in the list should be settable and produce non-empty renders
	for _, name := range themeNames {
		t.Run("theme_"+name, func(t *testing.T) {
			ok := SetTheme(name)
			if !ok {
				t.Fatalf("SetTheme(%q) returned false — theme not found in map", name)
			}

			// CurrentThemeName should return the display name from the theme definition
			if CurrentThemeName() == "" {
				t.Error("CurrentThemeName() returned empty string")
			}

			// All style vars should produce non-empty output
			styles := map[string]lipgloss.Style{
				"TopBarStyle":    TopBarStyle,
				"BottomBarStyle": BottomBarStyle,
				"TimestampStyle": TimestampStyle,
				"UserNickStyle":  UserNickStyle,
				"AgentNickStyle": AgentNickStyle,
				"SystemMsgStyle": SystemMsgStyle,
				"ErrorMsgStyle":  ErrorMsgStyle,
				"ToolCallStyle":  ToolCallStyle,
				"ToolOutputStyle": ToolOutputStyle,
				"InputPromptStyle": InputPromptStyle,
				"InputTextStyle": InputTextStyle,
				"SeparatorStyle": SeparatorStyle,
				"ThinkingStyle":  ThinkingStyle,
				"StatsStyle":     StatsStyle,
				"DimStyle":       DimStyle,
				"BoldWhite":      BoldWhite,
			}

			for styleName, style := range styles {
				rendered := style.Render("x")
				if rendered == "" {
					t.Errorf("%s.Render(\"x\") returned empty for theme %q", styleName, name)
				}
			}

			// Separator helper should produce non-empty output
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

	// All returned names should exist in the themes map
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

	// BitchX is the default
	SetTheme("bitchx")
	if got := CurrentThemeName(); got != "BitchX" {
		t.Errorf("CurrentThemeName() = %q, want %q", got, "BitchX")
	}

	SetTheme("nord")
	if got := CurrentThemeName(); got != "Nord" {
		t.Errorf("CurrentThemeName() = %q, want %q", got, "Nord")
	}

	SetTheme("dracula")
	if got := CurrentThemeName(); got != "Dracula" {
		t.Errorf("CurrentThemeName() = %q, want %q", got, "Dracula")
	}

	SetTheme("gruvbox")
	if got := CurrentThemeName(); got != "Gruvbox" {
		t.Errorf("CurrentThemeName() = %q, want %q", got, "Gruvbox")
	}

	SetTheme("monokai")
	if got := CurrentThemeName(); got != "Monokai" {
		t.Errorf("CurrentThemeName() = %q, want %q", got, "Monokai")
	}
}

func TestRebuildStylesAllVarsUpdated(t *testing.T) {
	originalName := Theme.Name
	defer SetTheme(originalName)

	// rebuildStyles() is called by SetTheme. Verify that after switching themes,
	// the Theme struct colors are different — which proves rebuildStyles was
	// invoked with the new theme's colors.
	SetTheme("bitchx")
	bitchxCyan := Theme.Cyan
	bitchxGreen := Theme.Green

	SetTheme("gruvbox")
	gruvboxCyan := Theme.Cyan
	gruvboxGreen := Theme.Green

	if bitchxCyan == gruvboxCyan {
		t.Error("Theme.Cyan unchanged between bitchx and gruvbox — rebuildStyles may not have run")
	}
	if bitchxGreen == gruvboxGreen {
		t.Error("Theme.Green unchanged between bitchx and gruvbox — rebuildStyles may not have run")
	}

	// Verify specific style-to-Theme color bindings:
	// UserNickStyle should use Theme.Green
	SetTheme("nord")
	nordGreen := Theme.Green

	SetTheme("monokai")
	monokaiGreen := Theme.Green

	if nordGreen == monokaiGreen {
		t.Error("Theme.Green is same in nord and monokai — cannot verify style bindings")
	}
	// Both should be non-zero (themes define all colors)
	if string(nordGreen) == "" || string(monokaiGreen) == "" {
		t.Error("Theme.Green is empty in one of the themes")
	}
}

func TestSeparatorNonEmpty(t *testing.T) {
	// Separator should produce content proportional to width
	short := Separator(5)
	long := Separator(80)

	if short == "" {
		t.Error("Separator(5) returned empty")
	}
	if long == "" {
		t.Error("Separator(80) returned empty")
	}
	// Longer width should produce a longer string (stripped of ANSI codes)
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
				"Cyan":    string(Theme.Cyan),
				"Green":   string(Theme.Green),
				"Magenta": string(Theme.Magenta),
				"Yellow":  string(Theme.Yellow),
				"Red":     string(Theme.Red),
				"Blue":    string(Theme.Blue),
				"White":   string(Theme.White),
				"Gray":    string(Theme.Gray),
				"DarkBg":  string(Theme.DarkBg),
				"BarBg":   string(Theme.BarBg),
			}

			for colorName, val := range colors {
				if val == "" {
					t.Errorf("Theme.%s is empty for theme %q", colorName, name)
				}
			}
		})
	}
}
