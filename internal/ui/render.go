package ui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

var (
	markdownRenderers      = make(map[int]*glamour.TermRenderer)
	markdownRendererWidths []int
	markdownRenderersMu    sync.Mutex
)

const maxMarkdownRenderers = 8

// RenderMarkdown renders markdown content for terminal display.
// Falls back to raw text if rendering fails.
func RenderMarkdown(content string, width int) string {
	if content == "" {
		return content
	}

	// Only attempt rendering if content looks like it might have markdown
	if !looksLikeMarkdown(content) {
		return content
	}

	renderer := markdownRendererForWidth(width)
	if renderer == nil {
		return content
	}

	rendered, err := renderer.Render(content)
	if err != nil {
		return content
	}

	// Trim trailing whitespace/newlines that glamour adds
	rendered = strings.TrimRight(rendered, "\n ")

	return rendered
}

func markdownRendererForWidth(width int) *glamour.TermRenderer {
	if width <= 0 {
		width = 100
	}

	markdownRenderersMu.Lock()
	defer markdownRenderersMu.Unlock()

	if renderer, ok := markdownRenderers[width]; ok {
		touchMarkdownRendererWidth(width)
		return renderer
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		markdownRenderers[width] = nil
		touchMarkdownRendererWidth(width)
		evictMarkdownRendererWidths()
		return nil
	}

	markdownRenderers[width] = renderer
	touchMarkdownRendererWidth(width)
	evictMarkdownRendererWidths()
	return renderer
}

func touchMarkdownRendererWidth(width int) {
	for i, cachedWidth := range markdownRendererWidths {
		if cachedWidth != width {
			continue
		}
		copy(markdownRendererWidths[i:], markdownRendererWidths[i+1:])
		markdownRendererWidths = markdownRendererWidths[:len(markdownRendererWidths)-1]
		break
	}
	markdownRendererWidths = append(markdownRendererWidths, width)
}

func evictMarkdownRendererWidths() {
	for len(markdownRendererWidths) > maxMarkdownRenderers {
		evicted := markdownRendererWidths[0]
		markdownRendererWidths = markdownRendererWidths[1:]
		delete(markdownRenderers, evicted)
	}
}

// looksLikeMarkdown does a cheap check for markdown-ish content
func looksLikeMarkdown(s string) bool {
	markers := []string{
		"```", "**", "##", "- ", "* ", "> ", "1. ",
		"[", "![",
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
