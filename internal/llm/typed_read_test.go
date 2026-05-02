package llm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jstamagal/bitchtea/internal/tools"
)

// These tests cover the typed read wrapper (bt-p2-read-write). They use the
// harness helpers from typed_tool_harness_test.go so the assertion shape
// matches every other Phase 2 typed-tool port.
//
// The wrapper is a thin pass-through to internal/tools.Registry.Execute; these
// tests intentionally exercise the *seam*, not the underlying read semantics
// (those are covered by internal/tools/tools_test.go). What we assert here:
//
//   - The fantasy schema advertises (path, offset, limit) with the right JSON
//     types.
//   - A successful read returns the file contents as a text response.
//   - A past-EOF offset surfaces the bt-hnh error message as a tool error
//     response, not a Go error.
//   - A nonexistent path surfaces a tool error with the path in the message.
//   - A pre-cancelled context surfaces context.Canceled as a tool error.

// --- schema --------------------------------------------------------------

func TestReadTool_SchemaAdvertisesPathOffsetLimit(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())
	info := readTool(reg).Info()

	if info.Name != "read" {
		t.Fatalf("info.Name = %q, want read", info.Name)
	}
	assertSchemaHasField(t, info, "path", "string")
	assertSchemaHasField(t, info, "offset", "integer")
	assertSchemaHasField(t, info, "limit", "integer")

	// Required slice must include path (offset/limit are optional).
	gotRequired := map[string]bool{}
	for _, r := range info.Required {
		gotRequired[r] = true
	}
	if !gotRequired["path"] {
		t.Fatalf("Required %v missing %q", info.Required, "path")
	}

	// Same anti-nesting guard the harness enforces for dummy_echo: fantasy
	// Parameters is a properties map, not a full JSON Schema object.
	for _, bogus := range []string{"type", "properties", "required"} {
		if _, ok := info.Parameters[bogus]; ok {
			t.Fatalf("typed schema must not include nested key %q at properties root: %+v", bogus, info.Parameters)
		}
	}
}

// --- successful read -----------------------------------------------------

func TestReadTool_SuccessfulReadReturnsContents(t *testing.T) {
	workDir := t.TempDir()
	reg := tools.NewRegistry(workDir, t.TempDir())

	const fileName = "hello.txt"
	const content = "hello fantasy\n"
	if err := os.WriteFile(filepath.Join(workDir, fileName), []byte(content), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	resp, err := runTypedTool(t, context.Background(), readTool(reg), readArgs{
		Path: fileName,
	})
	assertToolReturnsTextResponse(t, resp, err, "hello fantasy")
}

// --- past-EOF offset (bt-hnh) -------------------------------------------

func TestReadTool_PastEOFOffsetReturnsToolError(t *testing.T) {
	workDir := t.TempDir()
	reg := tools.NewRegistry(workDir, t.TempDir())

	const fileName = "short.txt"
	if err := os.WriteFile(filepath.Join(workDir, fileName), []byte("only-line\n"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	resp, err := runTypedTool(t, context.Background(), readTool(reg), readArgs{
		Path:   fileName,
		Offset: 9999,
	})
	// Match wording from internal/tools.execRead (bt-hnh).
	assertToolReturnsErrorResponse(t, resp, err, "past end of file")
}

// --- missing file --------------------------------------------------------

func TestReadTool_MissingFileReturnsToolError(t *testing.T) {
	workDir := t.TempDir()
	reg := tools.NewRegistry(workDir, t.TempDir())

	resp, err := runTypedTool(t, context.Background(), readTool(reg), readArgs{
		Path: "does_not_exist.txt",
	})
	assertToolReturnsErrorResponse(t, resp, err, "does_not_exist.txt")
}

// --- cancelled context ---------------------------------------------------

func TestReadTool_CancelledContextSurfacesAsToolError(t *testing.T) {
	workDir := t.TempDir()
	reg := tools.NewRegistry(workDir, t.TempDir())

	const fileName = "cancel.txt"
	const content = "should not be read\n"
	if err := os.WriteFile(filepath.Join(workDir, fileName), []byte(content), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resp, err := runTypedTool(t, ctx, readTool(reg), readArgs{Path: fileName})
	assertToolReturnsErrorResponse(t, resp, err, "context canceled")

	// Sanity: cancellation must short-circuit before reading the file. If the
	// wrapper still leaked the contents into the response, this test should
	// also catch that regression.
	if strings.Contains(resp.Content, "should not be read") {
		t.Fatalf("cancelled read leaked file contents into response: %q", resp.Content)
	}
}
