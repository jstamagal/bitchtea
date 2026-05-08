package llm

import (
	"fmt"
	"unicode/utf8"
)

// llmResultMaxBytes caps tool result text at the LLM boundary as a final
// safety net. Tools that have their own internal truncation (bash, read via
// truncateWithOverflow in internal/tools) are typically already well under
// this cap; this exists for tools that DON'T cap themselves — terminal_*,
// search_memory, write_memory, preview_image, edit, write, AND every MCP
// tool. A misbehaving MCP server returning megabytes of JSON would
// otherwise blast the model's context window and run up the bill before
// fantasy ever sees the response.
//
// Closes audit finding LOW #12 (bead bt-dxi). 100 KiB is roomy enough that
// well-behaved tools and MCP responses pass through unchanged, while still
// hard-stopping pathological output.
const llmResultMaxBytes = 100 * 1024

// capLLMResult returns content truncated to at most llmResultMaxBytes,
// walking back to a UTF-8 rune boundary so multi-byte characters are not
// split mid-encoding. When truncation occurs, appends a footer naming both
// the cap and the original size so the model knows what it lost.
func capLLMResult(content string) string {
	if len(content) <= llmResultMaxBytes {
		return content
	}
	original := len(content)
	cut := llmResultMaxBytes
	// utf8.RuneStart returns true for ASCII bytes and the leading byte of a
	// multi-byte sequence; walk back until we find one. Same pattern as
	// truncateUTF8 in internal/tools/tools.go.
	for cut > 0 && !utf8.RuneStart(content[cut]) {
		cut--
	}
	return fmt.Sprintf(
		"%s\n\n[result truncated at LLM boundary: %d bytes shown of %d total]",
		content[:cut], cut, original,
	)
}
