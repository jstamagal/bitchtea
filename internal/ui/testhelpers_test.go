package ui

import (
	"path/filepath"
	"testing"

	"github.com/jstamagal/bitchtea/internal/config"
)

// sessionFixturePath returns the absolute path to a session-resume fixture
// stored under internal/session/testdata. UI resume tests share the session
// package's fixtures rather than carrying their own copies — both layers
// exercise the same on-disk JSONL format.
func sessionFixturePath(t *testing.T, name string) string {
	t.Helper()
	// Tests run with cwd == internal/ui, so the session testdata lives one
	// directory up. Resolve to absolute so failure messages stay readable.
	abs, err := filepath.Abs(filepath.Join("..", "session", "testdata", name))
	if err != nil {
		t.Fatalf("resolve fixture path %q: %v", name, err)
	}
	return abs
}

// testModel returns a fresh Model wired to ephemeral per-test directories,
// ready to drive in unit tests that need a real (but disposable) Model.
//
// It uses config.DefaultConfig() so the resulting Config carries the same
// defaults a freshly-launched binary would (UserNick, AgentNick, MaxTokens,
// Service, etc.) — the only fields overridden are the on-disk locations,
// which are pinned to t.TempDir() values so tests cannot stomp on each
// other or on the real ~/.bitchtea state.
//
// The returned *config.Config is the same one held by the Model. Callers
// that need to write fixtures into SessionDir or read flags back out can
// keep a reference instead of re-deriving them.
//
// Co-located in package ui (rather than a testutil subpackage) because the
// resume-test contract pokes at unexported Model fields like
// model.lastSavedMsgIdx and model.messages — moving the helper into a
// separate package would force a wider refactor to expose those.
func testModel(t *testing.T) (Model, *config.Config) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()
	cfg.LogDir = t.TempDir()
	return NewModel(&cfg), &cfg
}
