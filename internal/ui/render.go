package ui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// markdownRenderer renders markdown content for the terminal
var markdownRenderer *glamour.TermRenderer

func init() {
	var err error
	markdownRenderer, err = glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(100),
	)
	if err != nil {
		// Fall back to no rendering
		markdownRenderer = nil
	}
}

// RenderMarkdown renders markdown content for terminal display.
// Falls back to raw text if rendering fails.
func RenderMarkdown(content string, width int) string {
	if markdownRenderer == nil || content == "" {
		return content
	}

	// Only attempt rendering if content looks like it might have markdown
	if !looksLikeMarkdown(content) {
		return content
	}

	rendered, err := markdownRenderer.Render(content)
	if err != nil {
		return content
	}

	// Trim trailing whitespace/newlines that glamour adds
	rendered = strings.TrimRight(rendered, "\n ")

	return rendered
}

// looksLikeMarkdown does a cheap check for markdown-ish content
func looksLikeMarkdown(s string) bool {
	markers := []string{
		"```", "**", "##", "- ", "* ", "> ", "1. ",
		"[", "![", "`",
	}
	for _, m := range markers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// WrapText wraps text to the given width, respecting ANSI escape codes
func WrapText(s string, width int) string {
	if width <= 0 {
		return s
	}

	var result strings.Builder
	lines := strings.Split(s, "\n")

	for i, line := range lines {
		if i > 0 {
			result.WriteByte('\n')
		}

		// Check visible width (excluding ANSI codes)
		visible := lipgloss.Width(line)
		if visible <= width {
			result.WriteString(line)
			continue
		}

		// Soft-wrap long lines
		result.WriteString(softWrap(line, width))
	}

	return result.String()
}

// softWrap wraps a single line at word boundaries
func softWrap(line string, width int) string {
	if width <= 0 {
		return line
	}

	words := strings.Fields(line)
	if len(words) == 0 {
		return line
	}

	var result strings.Builder
	lineWidth := 0

	for i, word := range words {
		wordWidth := lipgloss.Width(word)

		if i > 0 && lineWidth+1+wordWidth > width {
			result.WriteByte('\n')
			result.WriteString("  ") // indent continuation
			lineWidth = 2
		} else if i > 0 {
			result.WriteByte(' ')
			lineWidth++
		}

		result.WriteString(word)
		lineWidth += wordWidth
	}

	return result.String()
}
