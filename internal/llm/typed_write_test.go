package llm

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jstamagal/bitchtea/internal/tools"
)

// These tests cover the typed write wrapper (bt-p2-read-write). They use the
// harness helpers from typed_tool_harness_test.go so the assertion shape
// matches every other Phase 2 typed-tool port.
//
// The wrapper is a thin pass-through to internal/tools.Registry.Execute; these
// tests intentionally exercise the *seam*, not the underlying write semantics
// (those are covered by internal/tools/tools_test.go). What we assert here:
//
//   - The fantasy schema advertises (path, content) with the right JSON types
//     and lists both as required.
//   - A successful write creates a new file with the exact bytes.
//   - A successful write to an existing path overwrites the prior contents.
//   - A write into a path whose parent cannot be created (a regular file
//     posing as a directory) surfaces the mkdir error as a tool error.
//   - A pre-cancelled context surfaces context.Canceled as a tool error and
//     does not touch the filesystem.

// --- schema --------------------------------------------------------------

func TestWriteTool_SchemaAdvertisesPathAndContent(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())
	info := writeTool(reg).Info()

	if info.Name != "write" {
		t.Fatalf("info.Name = %q, want write", info.Name)
	}
	assertSchemaHasField(t, info, "path", "string")
	assertSchemaHasField(t, info, "content", "string")

	// Required slice must include both fields. Use a set so order changes in
	// fantasy's reflective schema generator don't break this test.
	wantRequired := map[string]bool{"path": true, "content": true}
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

// --- successful create ---------------------------------------------------

func TestWriteTool_SuccessfulWriteCreatesFile(t *testing.T) {
	workDir := t.TempDir()
	reg := tools.NewRegistry(workDir, t.TempDir())

	const fileName = "sub/created.txt"
	const content = "fresh contents\n"

	resp, err := runTypedTool(t, context.Background(), writeTool(reg), writeArgs{
		Path:    fileName,
		Content: content,
	})
	// execWrite reports byte count + the (relative) path arg.
	assertToolReturnsTextResponse(t, resp, err, "Wrote ")

	got, readErr := os.ReadFile(filepath.Join(workDir, fileName))
	if readErr != nil {
		t.Fatalf("read back created file: %v", readErr)
	}
	if string(got) != content {
		t.Fatalf("file content after write = %q, want %q", string(got), content)
	}
}

// --- successful overwrite ------------------------------------------------

func TestWriteTool_SuccessfulWriteOverwritesFile(t *testing.T) {
	workDir := t.TempDir()
	reg := tools.NewRegistry(workDir, t.TempDir())

	const fileName = "existing.txt"
	const before = "old contents\n"
	const after = "new contents\n"
	if err := os.WriteFile(filepath.Join(workDir, fileName), []byte(before), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	resp, err := runTypedTool(t, context.Background(), writeTool(reg), writeArgs{
		Path:    fileName,
		Content: after,
	})
	assertToolReturnsTextResponse(t, resp, err, "Wrote ")

	got, readErr := os.ReadFile(filepath.Join(workDir, fileName))
	if readErr != nil {
		t.Fatalf("read back overwritten file: %v", readErr)
	}
	if string(got) != after {
		t.Fatalf("file content after overwrite = %q, want %q", string(got), after)
	}
}

// --- invalid path (parent is a regular file) ----------------------------

func TestWriteTool_InvalidPathReturnsToolError(t *testing.T) {
	workDir := t.TempDir()
	reg := tools.NewRegistry(workDir, t.TempDir())

	// Create a regular file where execWrite would try to mkdir a parent
	// directory. MkdirAll fails ("not a directory") and execWrite surfaces
	// the error.
	if err := os.WriteFile(filepath.Join(workDir, "blocker"), []byte("x"), 0644); err != nil {
		t.Fatalf("seed blocker file: %v", err)
	}

	resp, err := runTypedTool(t, context.Background(), writeTool(reg), writeArgs{
		Path:    "blocker/child.txt",
		Content: "should fail",
	})
	assertToolReturnsErrorResponse(t, resp, err, "mkdir")

	// Sanity: blocker file must not have been clobbered.
	got, readErr := os.ReadFile(filepath.Join(workDir, "blocker"))
	if readErr != nil {
		t.Fatalf("read back blocker file: %v", readErr)
	}
	if string(got) != "x" {
		t.Fatalf("invalid write clobbered blocker file: %q", string(got))
	}
}

// --- cancelled context ---------------------------------------------------

func TestWriteTool_CancelledContextSurfacesAsToolError(t *testing.T) {
	workDir := t.TempDir()
	reg := tools.NewRegistry(workDir, t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	const fileName = "cancel.txt"
	resp, err := runTypedTool(t, ctx, writeTool(reg), writeArgs{
		Path:    fileName,
		Content: "should not be written\n",
	})
	assertToolReturnsErrorResponse(t, resp, err, "context canceled")

	// Sanity: cancellation must short-circuit before touching the filesystem.
	if _, statErr := os.Stat(filepath.Join(workDir, fileName)); !os.IsNotExist(statErr) {
		t.Fatalf("cancelled write created file (stat err = %v); should have short-circuited", statErr)
	}
}
