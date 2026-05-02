package llm

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jstamagal/bitchtea/internal/tools"
)

// These tests cover the typed edit wrapper (bt-p2-edit). They use the harness
// helpers from typed_tool_harness_test.go so the assertion shape matches every
// other Phase 2 typed-tool port.
//
// The wrapper is a thin pass-through to internal/tools.Registry.Execute; these
// tests intentionally exercise the *seam*, not the underlying edit semantics
// (those are covered by internal/tools/tools_test.go). What we assert here:
//
//   - The fantasy schema advertises (path, edits) with the right JSON types.
//   - A successful edit returns a text response and mutates the file on disk.
//   - Empty oldText surfaces the bt-z4d error message as a tool error response,
//     not a Go error.
//   - Non-unique matches surface the "must be unique" message as a tool error.
//   - Missing file surfaces a tool error with the path in the message.
//   - A pre-cancelled context surfaces context.Canceled as a tool error.

// --- schema --------------------------------------------------------------

func TestEditTool_SchemaAdvertisesPathAndEdits(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())
	info := editTool(reg).Info()

	if info.Name != "edit" {
		t.Fatalf("info.Name = %q, want edit", info.Name)
	}
	assertSchemaHasField(t, info, "path", "string")
	assertSchemaHasField(t, info, "edits", "array")

	// Required slice must include both fields. Use a set so order changes in
	// fantasy's reflective schema generator don't break this test.
	wantRequired := map[string]bool{"path": true, "edits": true}
	gotRequired := map[string]bool{}
	for _, r := range info.Required {
		gotRequired[r] = true
	}
	for name := range wantRequired {
		if !gotRequired[name] {
			t.Fatalf("Required %v missing %q", info.Required, name)
		}
	}

	// Same anti-nesting guard the harness enforces for dummy_echo: fantasy
	// Parameters is a properties map, not a full JSON Schema object.
	for _, bogus := range []string{"type", "properties", "required"} {
		if _, ok := info.Parameters[bogus]; ok {
			t.Fatalf("typed schema must not include nested key %q at properties root: %+v", bogus, info.Parameters)
		}
	}
}

// --- successful edit -----------------------------------------------------

func TestEditTool_SuccessfulEditMutatesFile(t *testing.T) {
	workDir := t.TempDir()
	reg := tools.NewRegistry(workDir, t.TempDir())

	const fileName = "hello.txt"
	const before = "hello world\n"
	const wantAfter = "hello fantasy\n"
	if err := os.WriteFile(filepath.Join(workDir, fileName), []byte(before), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	resp, err := runTypedTool(t, context.Background(), editTool(reg), editArgs{
		Path: fileName,
		Edits: []editArgsItem{
			{OldText: "world", NewText: "fantasy"},
		},
	})
	assertToolReturnsTextResponse(t, resp, err, "Applied 1 edit(s) to "+fileName)

	got, readErr := os.ReadFile(filepath.Join(workDir, fileName))
	if readErr != nil {
		t.Fatalf("read back edited file: %v", readErr)
	}
	if string(got) != wantAfter {
		t.Fatalf("file content after edit = %q, want %q", string(got), wantAfter)
	}
}

// --- empty oldText -------------------------------------------------------

func TestEditTool_EmptyOldTextReturnsToolError(t *testing.T) {
	workDir := t.TempDir()
	reg := tools.NewRegistry(workDir, t.TempDir())

	const fileName = "empty.txt"
	if err := os.WriteFile(filepath.Join(workDir, fileName), []byte("anything\n"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	resp, err := runTypedTool(t, context.Background(), editTool(reg), editArgs{
		Path: fileName,
		Edits: []editArgsItem{
			{OldText: "", NewText: "irrelevant"},
		},
	})
	// Match wording from internal/tools.execEdit (bt-z4d).
	assertToolReturnsErrorResponse(t, resp, err, "oldText must not be empty")
}

// --- non-unique match ----------------------------------------------------

func TestEditTool_NonUniqueMatchReturnsToolError(t *testing.T) {
	workDir := t.TempDir()
	reg := tools.NewRegistry(workDir, t.TempDir())

	const fileName = "dupes.txt"
	const content = "foo\nfoo\n"
	if err := os.WriteFile(filepath.Join(workDir, fileName), []byte(content), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	resp, err := runTypedTool(t, context.Background(), editTool(reg), editArgs{
		Path: fileName,
		Edits: []editArgsItem{
			{OldText: "foo", NewText: "bar"},
		},
	})
	// internal/tools.execEdit phrases this as "matches N times ... must be unique".
	assertToolReturnsErrorResponse(t, resp, err, "must be unique")
}

// --- missing file --------------------------------------------------------

func TestEditTool_MissingFileReturnsToolError(t *testing.T) {
	workDir := t.TempDir()
	reg := tools.NewRegistry(workDir, t.TempDir())

	resp, err := runTypedTool(t, context.Background(), editTool(reg), editArgs{
		Path: "does_not_exist.txt",
		Edits: []editArgsItem{
			{OldText: "x", NewText: "y"},
		},
	})
	assertToolReturnsErrorResponse(t, resp, err, "does_not_exist.txt")
}

// --- cancelled context ---------------------------------------------------

func TestEditTool_CancelledContextSurfacesAsToolError(t *testing.T) {
	workDir := t.TempDir()
	reg := tools.NewRegistry(workDir, t.TempDir())

	const fileName = "cancel.txt"
	if err := os.WriteFile(filepath.Join(workDir, fileName), []byte("hello\n"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resp, err := runTypedTool(t, ctx, editTool(reg), editArgs{
		Path: fileName,
		Edits: []editArgsItem{
			{OldText: "hello", NewText: "world"},
		},
	})
	assertToolReturnsErrorResponse(t, resp, err, "context canceled")

	// Sanity: cancellation must short-circuit before touching the file.
	got, readErr := os.ReadFile(filepath.Join(workDir, fileName))
	if readErr != nil {
		t.Fatalf("read back file after cancelled edit: %v", readErr)
	}
	if string(got) != "hello\n" {
		t.Fatalf("cancelled edit mutated file: got %q, want %q", string(got), "hello\n")
	}
}
