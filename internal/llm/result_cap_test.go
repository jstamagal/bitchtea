package llm

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestCapLLMResult_PassThroughWhenUnderCap confirms small results are
// returned unchanged (no truncation, no footer).
func TestCapLLMResult_PassThroughWhenUnderCap(t *testing.T) {
	for _, in := range []string{
		"",
		"short result",
		strings.Repeat("a", llmResultMaxBytes),         // exactly at cap
		strings.Repeat("a", llmResultMaxBytes-1),       // one under
	} {
		got := capLLMResult(in)
		if got != in {
			t.Errorf("len=%d: expected pass-through, got modified output", len(in))
		}
	}
}

// TestCapLLMResult_TruncatesAndAppendsFooter confirms oversized content gets
// truncated near the cap and a footer naming both sizes is appended.
func TestCapLLMResult_TruncatesAndAppendsFooter(t *testing.T) {
	huge := strings.Repeat("a", llmResultMaxBytes*5) // 5x the cap
	got := capLLMResult(huge)
	if got == huge {
		t.Fatal("expected truncation for 5x-cap input")
	}
	if !strings.Contains(got, "[result truncated at LLM boundary:") {
		t.Errorf("expected truncation footer, got: %q", got[len(got)-200:])
	}
	if !strings.Contains(got, "of 512000 total") { // 100*1024 * 5 = 512000
		t.Errorf("footer should name original size, got: %q", got[len(got)-200:])
	}
}

// TestCapLLMResult_RuneBoundary confirms multi-byte UTF-8 characters are
// never split mid-encoding when the cap falls in the middle of one.
func TestCapLLMResult_RuneBoundary(t *testing.T) {
	// Build content that puts a 4-byte rune (😀 = U+1F600) straddling the cap.
	// Padding chosen so the emoji's first byte sits AT the cap byte.
	pad := strings.Repeat("a", llmResultMaxBytes-2)
	content := pad + "😀" + strings.Repeat("b", 100)
	got := capLLMResult(content)
	// The truncated portion (before the footer) must be valid UTF-8.
	footerStart := strings.Index(got, "\n\n[result truncated")
	if footerStart < 0 {
		t.Fatal("expected footer in truncated output")
	}
	body := got[:footerStart]
	if !utf8.ValidString(body) {
		t.Fatalf("truncated body contains invalid UTF-8 — rune was split: byte at boundary = %x", body[len(body)-1])
	}
}

// TestCapLLMResult_FooterFormat pins the footer string format so callers
// (model-side) can rely on it.
func TestCapLLMResult_FooterFormat(t *testing.T) {
	huge := strings.Repeat("x", llmResultMaxBytes+100)
	got := capLLMResult(huge)
	// Should look like: "...xxxxx\n\n[result truncated at LLM boundary: N bytes shown of M total]"
	if !strings.HasSuffix(got, " total]") {
		t.Errorf("footer should end with ' total]', got tail: %q", got[len(got)-50:])
	}
	// The cap is exactly llmResultMaxBytes bytes for ASCII content.
	if !strings.Contains(got, "102400 bytes shown of 102500 total") {
		t.Errorf("expected exact size in footer, got tail: %q", got[len(got)-100:])
	}
}
