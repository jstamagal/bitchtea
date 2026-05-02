package llm

import (
	"context"
	"os"
	"strings"
	"testing"

	memorypkg "github.com/jstamagal/bitchtea/internal/memory"
	"github.com/jstamagal/bitchtea/internal/tools"
)

// These tests cover the typed write_memory wrapper (bt-p2-switch). They use
// the harness helpers from typed_tool_harness_test.go so the assertion shape
// matches every other Phase 2 typed-tool port.
//
// The wrapper is a thin pass-through to internal/tools.Registry.Execute; these
// tests intentionally exercise the *seam*, not the underlying memory store
// (those semantics are covered by internal/memory and internal/tools tests).
// What we assert here:
//
//   - The fantasy schema advertises (content, title, scope, name, daily) with
//     the right JSON types and lists content as required.
//   - A successful write under the Registry's default scope creates the hot
//     MEMORY.md file with the content, and the response echoes "Wrote N bytes".
//   - A scope override (scope="root") writes to the explicit root scope's hot
//     file even when the Registry was set to a channel scope.
//   - Missing required content surfaces as a tool error, not a Go error.
//   - A pre-cancelled context surfaces context.Canceled as a tool error and
//     does not touch the memory store.

// --- schema --------------------------------------------------------------

func TestWriteMemoryTool_SchemaAdvertisesAllFields(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())
	info := writeMemoryTool(reg).Info()

	if info.Name != "write_memory" {
		t.Fatalf("info.Name = %q, want write_memory", info.Name)
	}
	assertSchemaHasField(t, info, "content", "string")
	assertSchemaHasField(t, info, "title", "string")
	assertSchemaHasField(t, info, "scope", "string")
	assertSchemaHasField(t, info, "name", "string")
	assertSchemaHasField(t, info, "daily", "boolean")

	gotRequired := map[string]bool{}
	for _, r := range info.Required {
		gotRequired[r] = true
	}
	if !gotRequired["content"] {
		t.Fatalf("Required %v missing %q", info.Required, "content")
	}

	for _, bogus := range []string{"type", "properties", "required"} {
		if _, ok := info.Parameters[bogus]; ok {
			t.Fatalf("typed schema must not include nested key %q at properties root: %+v", bogus, info.Parameters)
		}
	}
}

// --- successful write under default registry scope -----------------------

func TestWriteMemoryTool_SuccessfulWriteUsesRegistryScope(t *testing.T) {
	workDir := t.TempDir()
	sessionDir := t.TempDir()
	reg := tools.NewRegistry(workDir, sessionDir)
	// Registry's default Scope is RootScope().

	const body = "platypus-decision: ship it"
	resp, err := runTypedTool(t, context.Background(), writeMemoryTool(reg), writeMemoryArgs{
		Content: body,
		Title:   "decision",
	})
	assertToolReturnsTextResponse(t, resp, err, "Wrote ")

	// Read back the hot file under the default (root) scope and confirm the
	// content landed there. We don't pin the title format — only that the
	// body bytes are present.
	hotPath := memorypkg.HotPath(sessionDir, workDir, memorypkg.RootScope())
	got, readErr := os.ReadFile(hotPath)
	if readErr != nil {
		t.Fatalf("read back hot memory file %q: %v", hotPath, readErr)
	}
	if !strings.Contains(string(got), body) {
		t.Fatalf("hot memory file missing written body; got %q want substring %q", string(got), body)
	}
}

// --- explicit scope override beats Registry.Scope ------------------------

func TestWriteMemoryTool_RootScopeOverrideBeatsRegistryScope(t *testing.T) {
	workDir := t.TempDir()
	sessionDir := t.TempDir()

	// Configure the Registry to a channel scope; the explicit scope="root"
	// override in the tool args must win.
	root := memorypkg.RootScope()
	channelScope := memorypkg.ChannelScope("scopetest", &root)
	reg := tools.NewRegistry(workDir, sessionDir)
	reg.SetScope(channelScope)

	const needle = "lemur-override-marker"
	resp, err := runTypedTool(t, context.Background(), writeMemoryTool(reg), writeMemoryArgs{
		Content: needle,
		Scope:   "root",
	})
	assertToolReturnsTextResponse(t, resp, err, "Wrote ")

	rootHot := memorypkg.HotPath(sessionDir, workDir, root)
	channelHot := memorypkg.HotPath(sessionDir, workDir, channelScope)

	rootBytes, err := os.ReadFile(rootHot)
	if err != nil {
		t.Fatalf("read root hot memory %q: %v", rootHot, err)
	}
	if !strings.Contains(string(rootBytes), needle) {
		t.Fatalf("root hot file missing override write; got %q", string(rootBytes))
	}

	// Channel hot file should NOT have been touched.
	if _, statErr := os.Stat(channelHot); !os.IsNotExist(statErr) {
		channelBytes, _ := os.ReadFile(channelHot)
		if strings.Contains(string(channelBytes), needle) {
			t.Fatalf("scope override leaked into channel scope %q: %q", channelHot, string(channelBytes))
		}
	}
}

// --- missing required content surfaces as tool error --------------------

func TestWriteMemoryTool_MissingContentReturnsToolError(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())

	resp, err := runTypedTool(t, context.Background(), writeMemoryTool(reg), writeMemoryArgs{
		Content: "   \t\n", // whitespace-only — execWriteMemory rejects after TrimSpace
	})
	assertToolReturnsErrorResponse(t, resp, err, "content is required")
}

// --- cancelled context --------------------------------------------------

func TestWriteMemoryTool_CancelledContextSurfacesAsToolError(t *testing.T) {
	workDir := t.TempDir()
	sessionDir := t.TempDir()
	reg := tools.NewRegistry(workDir, sessionDir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resp, err := runTypedTool(t, ctx, writeMemoryTool(reg), writeMemoryArgs{
		Content: "should not be persisted",
	})
	assertToolReturnsErrorResponse(t, resp, err, "context canceled")

	// Sanity: cancellation must short-circuit before touching the store.
	hotPath := memorypkg.HotPath(sessionDir, workDir, memorypkg.RootScope())
	if _, statErr := os.Stat(hotPath); !os.IsNotExist(statErr) {
		t.Fatalf("cancelled write_memory created hot file at %q; should have short-circuited", hotPath)
	}
}
