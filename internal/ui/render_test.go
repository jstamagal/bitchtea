package ui

import (
	"strings"
	"testing"
)

func TestLooksLikeMarkdown(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"hello world", false},
		{"```go\nfmt.Println()\n```", true},
		{"**bold text**", true},
		{"## Heading", true},
		{"- list item", true},
		{"> quote", true},
		{"use `code` here", false},
		{"just plain text no markers", false},
	}

	for _, tt := range tests {
		got := looksLikeMarkdown(tt.input)
		if got != tt.expected {
			t.Errorf("looksLikeMarkdown(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestRenderMarkdownPlainText(t *testing.T) {
	result := RenderMarkdown("just plain text", 80)
	if result != "just plain text" {
		t.Errorf("expected plain passthrough, got: %q", result)
	}
}

func TestRenderMarkdownCode(t *testing.T) {
	input := "Here is some code:\n```go\nfmt.Println(\"hello\")\n```"
	result := RenderMarkdown(input, 80)
	// Should contain the code somehow (glamour renders it)
	if !strings.Contains(result, "Println") {
		t.Errorf("expected code content in rendered output, got: %q", result)
	}
}

func TestRenderMarkdownNarrowWidth(t *testing.T) {
	input := "## Heading\n- alpha beta gamma delta"
	result := RenderMarkdown(input, 18)

	for _, want := range []string{"Heading", "alpha", "delta"} {
		if !strings.Contains(result, want) {
			t.Fatalf("expected narrow render to contain %q, got %q", want, result)
		}
	}

	if !strings.Contains(result, "\n") {
		t.Fatalf("expected narrow render to wrap, got %q", result)
	}
}

func TestRenderMarkdownWideWidth(t *testing.T) {
	input := "## Heading\n- alpha beta gamma delta"
	result := RenderMarkdown(input, 120)

	for _, want := range []string{"Heading", "alpha beta gamma delta"} {
		if !strings.Contains(result, want) {
			t.Fatalf("expected wide render to contain %q, got %q", want, result)
		}
	}
}

func TestRenderMarkdownEmpty(t *testing.T) {
	result := RenderMarkdown("", 80)
	if result != "" {
		t.Errorf("expected empty string, got: %q", result)
	}
}

func TestRenderMarkdownFallsBackWhenRendererUnavailable(t *testing.T) {
	input := "## Heading"
	width := 777

	markdownRenderersMu.Lock()
	old, existed := markdownRenderers[width]
	markdownRenderers[width] = nil
	markdownRenderersMu.Unlock()

	t.Cleanup(func() {
		markdownRenderersMu.Lock()
		defer markdownRenderersMu.Unlock()
		if existed {
			markdownRenderers[width] = old
			return
		}
		delete(markdownRenderers, width)
	})

	result := RenderMarkdown(input, width)
	if result != input {
		t.Fatalf("expected raw fallback, got %q", result)
	}
}

func TestWrapText(t *testing.T) {
	input := "this is a long line that should be wrapped at some point because it exceeds the width"
	result := WrapText(input, 40)
	lines := strings.Split(result, "\n")
	if len(lines) < 2 {
		t.Errorf("expected wrapped output to have multiple lines, got %d", len(lines))
	}
}

func TestWrapTextShortLine(t *testing.T) {
	input := "short"
	result := WrapText(input, 80)
	if result != "short" {
		t.Errorf("expected no wrapping, got: %q", result)
	}
}

func TestWrapTextMultiline(t *testing.T) {
	input := "line1\nline2\nline3"
	result := WrapText(input, 80)
	if result != input {
		t.Errorf("expected passthrough for short lines, got: %q", result)
	}
}

func TestWrapTextANSIAware(t *testing.T) {
	input := "\x1b[31malpha beta gamma delta epsilon\x1b[0m"
	result := WrapText(input, 12)

	if !strings.Contains(result, "\x1b[31m") || !strings.Contains(result, "\x1b[0m") {
		t.Fatalf("expected ANSI codes to be preserved, got %q", result)
	}

	lines := strings.Split(result, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected wrapped ANSI output, got %q", result)
	}

	for _, line := range lines {
		if w := visibleWidth(line); w > 12 {
			t.Fatalf("expected visible width <= 12, got %d for %q", w, line)
		}
	}
}

func TestWrapTextNarrowWidth(t *testing.T) {
	input := "alpha beta gamma delta"
	result := WrapText(input, 7)
	lines := strings.Split(result, "\n")

	if len(lines) < 3 {
		t.Fatalf("expected narrow wrapping across several lines, got %q", result)
	}

	for _, line := range lines {
		if w := visibleWidth(line); w > 7 {
			t.Fatalf("expected visible width <= 7, got %d for %q", w, line)
		}
	}
}

func visibleWidth(s string) int {
	var b strings.Builder
	b.Grow(len(s))

	inEscape := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case inEscape && ch == 'm':
			inEscape = false
		case inEscape:
			continue
		case ch == 0x1b:
			inEscape = true
		default:
			b.WriteByte(ch)
		}
	}

	return len([]rune(b.String()))
}
