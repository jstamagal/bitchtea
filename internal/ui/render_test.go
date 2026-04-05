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
		{"use `code` here", true},
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

func TestRenderMarkdownEmpty(t *testing.T) {
	result := RenderMarkdown("", 80)
	if result != "" {
		t.Errorf("expected empty string, got: %q", result)
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
