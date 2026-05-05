package ui

import (
	"testing"

	"github.com/jstamagal/bitchtea/internal/config"
)

// testModel returns a ready-to-drive Model backed by isolated temp
// directories and a stub API key. All UI tests should use this instead of
// building ad-hoc config + NewModel calls so that directory isolation,
// base config, and future test-only setup are consistent.
func testModel(t *testing.T) Model {
	t.Helper()
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.APIKey = "sk-test-key-12345"
	cfg.WorkDir = dir
	cfg.SessionDir = dir + "/sessions"
	cfg.LogDir = dir + "/logs"
	return NewModel(&cfg)
}