package llm

import (
	"context"
	"strings"
	"testing"
	"time"

	memorypkg "github.com/jstamagal/bitchtea/internal/memory"
	"github.com/jstamagal/bitchtea/internal/tools"
)

// These tests cover the typed search_memory wrapper (bt-p2-bash-memory). They
// use the harness helpers from typed_tool_harness_test.go so the assertion
// shape matches every other Phase 2 typed-tool port.
//
// The wrapper is a thin pass-through to internal/tools.Registry.Execute; these
// tests intentionally exercise the *seam*, not the underlying memory store
// (those semantics are covered by internal/memory and internal/tools tests).
// What we assert here:
//
//   - The fantasy schema advertises (query, limit) with the right JSON types
//     and lists query as required.
//   - A successful query against a seeded hot memory file returns the matching
//     entry as a text response, including the rendered "Memory matches for ..."
//     banner from RenderSearchResults.
//   - The Registry's per-turn scope (Registry.SetScope) is honored: a query
//     run under ChannelScope sees that channel's memory and not a sibling's.
//     This is the closest thing to "scope override works" that the current
//     search_memory tool surface supports — the JSON schema does NOT accept a
//     scope arg today (see typed_search_memory.go for the rationale).
//   - An empty result set returns a text response with the "no matches"
//     banner, not a tool error response.

// --- schema --------------------------------------------------------------

func TestSearchMemoryTool_SchemaAdvertisesQueryAndLimit(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())
	info := searchMemoryTool(reg).Info()

	if info.Name != "search_memory" {
		t.Fatalf("info.Name = %q, want search_memory", info.Name)
	}
	assertSchemaHasField(t, info, "query", "string")
	assertSchemaHasField(t, info, "limit", "integer")

	// Required slice must include query (limit is optional).
	gotRequired := map[string]bool{}
	for _, r := range info.Required {
		gotRequired[r] = true
	}
	if !gotRequired["query"] {
		t.Fatalf("Required %v missing %q", info.Required, "query")
	}

	// Same anti-nesting guard the harness enforces for dummy_echo: fantasy
	// Parameters is a properties map, not a full JSON Schema object.
	for _, bogus := range []string{"type", "properties", "required"} {
		if _, ok := info.Parameters[bogus]; ok {
			t.Fatalf("typed schema must not include nested key %q at properties root: %+v", bogus, info.Parameters)
		}
	}
}

// --- successful query against root scope --------------------------------

func TestSearchMemoryTool_SuccessfulQueryReturnsHit(t *testing.T) {
	workDir := t.TempDir()
	sessionDir := t.TempDir()
	reg := tools.NewRegistry(workDir, sessionDir)
	// Default Scope on a fresh Registry is the zero Scope (RootScope).

	// Seed one entry into hot memory under the root scope. Use a distinctive
	// noun so the search query can't accidentally match anything else.
	const needle = "antelope-decision"
	if err := memorypkg.AppendHot(sessionDir, workDir, memorypkg.RootScope(), time.Now(), "test entry", "remember the "+needle+" outcome"); err != nil {
		t.Fatalf("seed hot memory: %v", err)
	}

	resp, err := runTypedTool(t, context.Background(), searchMemoryTool(reg), searchMemoryArgs{
		Query: needle,
		Limit: 5,
	})
	assertToolReturnsTextResponse(t, resp, err, needle)
	if !strings.Contains(resp.Content, "Memory matches for") {
		t.Fatalf("expected RenderSearchResults banner; got %q", resp.Content)
	}
}

// --- scope respected via Registry.SetScope ------------------------------

func TestSearchMemoryTool_RespectsRegistryScope(t *testing.T) {
	workDir := t.TempDir()
	sessionDir := t.TempDir()

	root := memorypkg.RootScope()
	channelScope := memorypkg.ChannelScope("scopetest", &root)

	// Seed a needle into the channel scope ONLY. If the wrapper somehow
	// dispatched against a different scope, the search would miss.
	const needle = "marmoset-marker"
	if err := memorypkg.AppendHot(sessionDir, workDir, channelScope, time.Now(), "channel entry", needle+" lives in the channel"); err != nil {
		t.Fatalf("seed channel memory: %v", err)
	}

	// First: a Registry whose scope is root MUST NOT find the channel needle.
	rootReg := tools.NewRegistry(workDir, sessionDir)
	rootResp, err := runTypedTool(t, context.Background(), searchMemoryTool(rootReg), searchMemoryArgs{
		Query: needle,
	})
	if err != nil {
		t.Fatalf("Go error: %v", err)
	}
	if rootResp.IsError {
		t.Fatalf("root-scope query should not error; got %q", rootResp.Content)
	}
	if strings.Contains(rootResp.Content, needle) && !strings.Contains(rootResp.Content, "No memory matches") {
		// channel-scope memory leaks up only when the lineage walk explicitly
		// includes child scopes. Today's SearchInScope walks parents, not
		// children, so a root-scope search for a channel-only needle must miss.
		t.Fatalf("root-scope search unexpectedly saw channel-scope content: %q", rootResp.Content)
	}

	// Second: a Registry whose scope is the channel MUST find the needle.
	channelReg := tools.NewRegistry(workDir, sessionDir)
	channelReg.SetScope(channelScope)
	chResp, err := runTypedTool(t, context.Background(), searchMemoryTool(channelReg), searchMemoryArgs{
		Query: needle,
	})
	assertToolReturnsTextResponse(t, chResp, err, needle)
}

// --- empty result -> text response, not error ---------------------------

func TestSearchMemoryTool_EmptyResultReturnsTextResponse(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())

	const needle = "nothing-here-zzz-impossible-token"
	resp, err := runTypedTool(t, context.Background(), searchMemoryTool(reg), searchMemoryArgs{
		Query: needle,
	})
	// Acceptance: empty results are normal output (RenderSearchResults emits
	// "No memory matches found for query ..."), NOT a tool error.
	assertToolReturnsTextResponse(t, resp, err, "No memory matches found")
	if !strings.Contains(resp.Content, needle) {
		t.Fatalf("expected query echoed in no-match banner; got %q", resp.Content)
	}
}

// --- cancelled context --------------------------------------------------

func TestSearchMemoryTool_CancelledContextSurfacesAsToolError(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resp, err := runTypedTool(t, ctx, searchMemoryTool(reg), searchMemoryArgs{Query: "anything"})
	assertToolReturnsErrorResponse(t, resp, err, "context canceled")
}
