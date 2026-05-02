package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeMCPFile is a tiny helper: lays down a workspace dir that mimics
// the production layout (<workDir>/.bitchtea/mcp.json) and writes the
// supplied JSON body. Returns the workDir to feed to LoadConfig.
func writeMCPFile(t *testing.T, body string) string {
	t.Helper()
	work := t.TempDir()
	dir := filepath.Join(work, ".bitchtea")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ConfigFileName), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return work
}

// Layer 1 of disable-by-default: no file, no MCP, no error.
func TestLoadConfig_MissingFile_Disabled(t *testing.T) {
	work := t.TempDir()
	cfg, err := LoadConfig(work)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cfg.Enabled {
		t.Fatalf("expected disabled when file missing, got enabled")
	}
	if len(cfg.Servers) != 0 {
		t.Fatalf("expected zero servers, got %d", len(cfg.Servers))
	}
}

// Layer 2 of disable-by-default: file present but the master switch is
// off. The file is still parsed for syntax (so the user gets feedback
// when they later flip it on), but no servers come back.
func TestLoadConfig_TopLevelDisabled(t *testing.T) {
	work := writeMCPFile(t, `{
  "enabled": false,
  "servers": [
    {"name":"fs","transport":"stdio","command":"echo","enabled":true}
  ]
}`)
	cfg, err := LoadConfig(work)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cfg.Enabled {
		t.Fatalf("expected disabled when top-level enabled is false")
	}
	if len(cfg.Servers) != 0 {
		t.Fatalf("expected no servers in disabled config, got %d", len(cfg.Servers))
	}
}

// Layer 3 of disable-by-default: per-server toggle filters out specific
// entries even when the master switch is on.
func TestLoadConfig_PerServerEnabledFilter(t *testing.T) {
	work := writeMCPFile(t, `{
  "enabled": true,
  "servers": [
    {"name":"on","transport":"stdio","command":"echo","enabled":true},
    {"name":"off","transport":"stdio","command":"echo","enabled":false},
    {"name":"defaulted","transport":"stdio","command":"echo"}
  ]
}`)
	cfg, err := LoadConfig(work)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !cfg.Enabled {
		t.Fatalf("expected enabled")
	}
	if _, ok := cfg.Servers["on"]; !ok {
		t.Errorf("expected server 'on' to be present")
	}
	if _, ok := cfg.Servers["defaulted"]; !ok {
		t.Errorf("expected server 'defaulted' (enabled omitted -> default true) to be present")
	}
	if _, ok := cfg.Servers["off"]; ok {
		t.Errorf("expected server 'off' to be filtered out")
	}
}

// ${env:VARNAME} interpolation: present env var substitutes, missing
// env var is a load-time error (the contract forbids substituting "").
func TestLoadConfig_EnvInterpolation(t *testing.T) {
	t.Setenv("BT_MCP_TEST_VAR", "hello")
	work := writeMCPFile(t, `{
  "enabled": true,
  "servers": [
    {"name":"fs","transport":"stdio","command":"echo","env":{"GREETING":"${env:BT_MCP_TEST_VAR}"}}
  ]
}`)
	cfg, err := LoadConfig(work)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	got := cfg.Servers["fs"].Env["GREETING"]
	if got != "hello" {
		t.Fatalf("expected interpolated env value 'hello', got %q", got)
	}
}

func TestLoadConfig_EnvInterpolationMissing(t *testing.T) {
	// Make sure the var really is unset for this test.
	os.Unsetenv("BT_MCP_TEST_MISSING")
	work := writeMCPFile(t, `{
  "enabled": true,
  "servers": [
    {"name":"fs","transport":"stdio","command":"echo","env":{"GREETING":"${env:BT_MCP_TEST_MISSING}"}}
  ]
}`)
	_, err := LoadConfig(work)
	if err == nil {
		t.Fatalf("expected error for missing env var, got nil")
	}
	if !strings.Contains(err.Error(), "BT_MCP_TEST_MISSING") {
		t.Fatalf("expected error to mention missing var, got: %v", err)
	}
}

// Inline-secret refusal: if the user pastes a token literal into
// mcp.json, the loader refuses to honor the file. The error mentions
// the offending field so the user can fix it.
func TestLoadConfig_RefusesInlineSecret(t *testing.T) {
	work := writeMCPFile(t, `{
  "enabled": true,
  "servers": [
    {"name":"issues","transport":"http","url":"https://example/","headers":{"Authorization":"Bearer sk-abc123"}}
  ]
}`)
	_, err := LoadConfig(work)
	if err == nil {
		t.Fatalf("expected refusal for inline secret pattern, got nil")
	}
	if !strings.Contains(err.Error(), "inline secret") {
		t.Fatalf("expected error to mention inline secret, got: %v", err)
	}
}

// Bad transport is a load-time failure with a useful message rather
// than a silent skip — the contract says these errors should surface.
func TestLoadConfig_UnknownTransport(t *testing.T) {
	work := writeMCPFile(t, `{
  "enabled": true,
  "servers": [
    {"name":"x","transport":"websocket","url":"wss://nope/"}
  ]
}`)
	_, err := LoadConfig(work)
	if err == nil {
		t.Fatalf("expected error for unknown transport")
	}
}

// DisallowUnknownFields: a typo in the JSON schema fails loudly.
func TestLoadConfig_UnknownFieldFails(t *testing.T) {
	work := writeMCPFile(t, `{
  "enabled": true,
  "serverz": []
}`)
	_, err := LoadConfig(work)
	if err == nil {
		t.Fatalf("expected error for unknown top-level field")
	}
}
