package llm

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jstamagal/bitchtea/internal/tools"
)

// These tests cover the typed bash wrapper (bt-p2-bash-memory). They use the
// harness helpers from typed_tool_harness_test.go so the assertion shape
// matches every other Phase 2 typed-tool port.
//
// The wrapper is a thin pass-through to internal/tools.Registry.Execute; these
// tests intentionally exercise the *seam*, not the underlying subprocess
// semantics (those are covered by internal/tools/tools_test.go). What we
// assert here:
//
//   - The fantasy schema advertises (command, timeout) with the right JSON types.
//   - A successful echo returns the output as a text response.
//   - A non-zero inner exit returns a TEXT response (not an error response)
//     containing the stdout/stderr and the "Exit code: N" suffix that execBash
//     emits — this is the bt-p2 contract for "command ran but failed".
//   - Parent-context cancellation surfaces "command cancelled" as a tool error
//     (bt-91b: distinct from timeout).
//   - The tool-level `timeout` arg surfaces "command timed out after Ns" as a
//     tool error (bt-91b: distinct from cancel).
//   - Output that crosses the 50KiB truncation boundary on a multi-byte rune
//     comes back as valid UTF-8 (bt-71q regression guard).

// --- schema --------------------------------------------------------------

func TestBashTool_SchemaAdvertisesCommandAndTimeout(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())
	info := bashTool(reg).Info()

	if info.Name != "bash" {
		t.Fatalf("info.Name = %q, want bash", info.Name)
	}
	assertSchemaHasField(t, info, "command", "string")
	assertSchemaHasField(t, info, "timeout", "integer")

	// Required slice must include command (timeout is optional).
	gotRequired := map[string]bool{}
	for _, r := range info.Required {
		gotRequired[r] = true
	}
	if !gotRequired["command"] {
		t.Fatalf("Required %v missing %q", info.Required, "command")
	}

	// Same anti-nesting guard the harness enforces for dummy_echo: fantasy
	// Parameters is a properties map, not a full JSON Schema object.
	for _, bogus := range []string{"type", "properties", "required"} {
		if _, ok := info.Parameters[bogus]; ok {
			t.Fatalf("typed schema must not include nested key %q at properties root: %+v", bogus, info.Parameters)
		}
	}
}

// --- successful echo -----------------------------------------------------

func TestBashTool_SuccessfulRunReturnsOutput(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())

	resp, err := runTypedTool(t, context.Background(), bashTool(reg), bashArgs{
		Command: "echo hello && echo world",
	})
	assertToolReturnsTextResponse(t, resp, err, "hello\nworld\n")
}

// --- nonzero exit returns TEXT response, not error ----------------------

func TestBashTool_NonzeroExitReturnsTextResponseWithExitCode(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())

	resp, err := runTypedTool(t, context.Background(), bashTool(reg), bashArgs{
		Command: "echo failmsg >&2; exit 17",
	})
	// Acceptance: nonzero exit is "command ran" — must surface as a normal
	// text response so the model sees stdout/stderr and the exit code.
	assertToolReturnsTextResponse(t, resp, err, "Exit code: 17")
	if !strings.Contains(resp.Content, "failmsg") {
		t.Fatalf("response missing stderr content; got %q", resp.Content)
	}
}

// --- parent-context cancellation (bt-91b) -------------------------------

func TestBashTool_ParentContextCancellationSurfacesAsToolError(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel before the wrapper runs — the wrapper's ctx.Err() guard
	// short-circuits with the same kind of text error response that the
	// post-spawn cancel path produces. Either way the contract is "cancel
	// surfaces as a tool error, never aborts the stream".
	cancel()

	resp, err := runTypedTool(t, ctx, bashTool(reg), bashArgs{
		Command: "echo should not run",
	})
	assertToolReturnsErrorResponse(t, resp, err, "context canceled")
}

// TestBashTool_RunningProcessCancelReportsCancelNotTimeout exercises the
// post-spawn cancel path in execBash (bt-91b): the parent ctx is cancelled
// while sleep is in flight, and the wrapper must surface "command cancelled"
// as a tool error (NOT "timed out"). This covers the wording boundary that
// bt-91b fixed.
func TestBashTool_RunningProcessCancelReportsCancelNotTimeout(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel ~50ms after we start the test — by then the wrapper has cleared
	// its pre-spawn ctx.Err() guard and execBash has started the sleep, so
	// the cancellation hits the post-spawn path. Mirrors
	// TestBashCancelledContextReportsCancelNotTimeout in tools_test.go.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	resp, err := runTypedTool(t, ctx, bashTool(reg), bashArgs{
		Command: "sleep 5",
		Timeout: 30,
	})
	assertToolReturnsErrorResponse(t, resp, err, "cancel")
	if strings.Contains(strings.ToLower(resp.Content), "timed out") || strings.Contains(strings.ToLower(resp.Content), "timeout") {
		t.Fatalf("cancel error must not mention timeout (bt-91b): %q", resp.Content)
	}
}

// --- tool-level timeout (bt-91b) ----------------------------------------

func TestBashTool_TimeoutArgSurfacesTimeoutError(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())

	resp, err := runTypedTool(t, context.Background(), bashTool(reg), bashArgs{
		Command: "sleep 5",
		Timeout: 1,
	})
	assertToolReturnsErrorResponse(t, resp, err, "timed out after 1s")
	if strings.Contains(strings.ToLower(resp.Content), "cancel") {
		t.Fatalf("timeout error must not mention cancel (bt-91b): %q", resp.Content)
	}
}

// --- UTF-8 truncation safety (bt-71q) -----------------------------------
//
// execBash truncates output that exceeds 50KiB at a UTF-8 rune boundary. We
// emit far more than 50KiB of a 3-byte UTF-8 rune (U+00B5 MICRO SIGN is 2
// bytes in UTF-8; use U+1F600 which is 4 bytes — printf encoded as \xXX
// escapes). The wrapper itself doesn't re-truncate; it must pass execBash's
// already-rune-safe output through unchanged.
func TestBashTool_OutputTruncatedAtRuneBoundaryIsValidUTF8(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())

	// 4-byte rune (U+1F4A9 PILE OF POO) repeated to overflow 50KiB.
	// 60000 / 4 = 15000 runes; at >50KiB the UTF-8-aware truncator in execBash
	// trims back to a rune boundary. printf %s expansion of $(yes ... | head)
	// is hard to control inside bash -c quoting, so just emit a direct
	// repeated literal via printf with a count. Use perl if printf %s repeat
	// syntax is too fragile, but `printf` + `%.0s` trick works in bash.
	//
	// Command: print the 4-byte UTF-8 sequence for U+1F4A9 (\xF0\x9F\x92\xA9)
	// 15000 times.
	cmd := `printf '\xF0\x9F\x92\xA9%.0s' $(seq 1 15000)`

	resp, err := runTypedTool(t, context.Background(), bashTool(reg), bashArgs{
		Command: cmd,
	})
	if err != nil {
		t.Fatalf("Go error from typed wrapper: %v", err)
	}
	if resp.IsError {
		t.Fatalf("did not expect error response; got %q", resp.Content)
	}
	if !utf8.ValidString(resp.Content) {
		t.Fatalf("truncated bash output is not valid UTF-8 (bt-71q regression)")
	}
	// Sanity: must actually have been truncated, otherwise we didn't exercise
	// the code path. Pattern 3 uses "[TRUNCATED" marker instead of "(truncated)".
	if !strings.Contains(resp.Content, "[TRUNCATED") {
		t.Fatalf("expected TRUNCATED marker; output too small to exercise path (len=%d)", len(resp.Content))
	}
}
