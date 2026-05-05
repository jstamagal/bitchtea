package config

// Failure-mode tests asserting exact error wraps and sentinels for the config
// package. See bd issue bt-test.16.

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestErrorMessage_LoadProfile_MalformedJSON verifies that LoadProfile on a
// file with invalid JSON returns an error wrapping a *json.SyntaxError with
// the "parse profile %q:" prefix that includes the profile name.
func TestErrorMessage_LoadProfile_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	origDir := ProfilesDir
	ProfilesDir = func() string { return dir }
	t.Cleanup(func() { ProfilesDir = origDir })

	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := LoadProfile("broken")
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !strings.HasPrefix(err.Error(), `parse profile "broken":`) {
		t.Fatalf("error should be prefixed with 'parse profile \"broken\":', got %q", err.Error())
	}
	var syntaxErr *json.SyntaxError
	if !errors.As(err, &syntaxErr) {
		t.Fatalf("expected wrapped *json.SyntaxError, got %T: %v", err, err)
	}
}

// TestErrorMessage_LoadProfile_NotFound verifies that LoadProfile on a missing
// path returns an error wrapping fs.ErrNotExist with the "read profile %q:"
// prefix. The CLI distinguishes "no such profile" from "parse failure" by
// checking errors.Is(err, fs.ErrNotExist), so this contract must hold.
func TestErrorMessage_LoadProfile_NotFound(t *testing.T) {
	dir := t.TempDir()
	origDir := ProfilesDir
	ProfilesDir = func() string { return dir }
	t.Cleanup(func() { ProfilesDir = origDir })

	_, err := LoadProfile("ghost")
	if err == nil {
		t.Fatal("expected error for missing profile, got nil")
	}
	if !strings.HasPrefix(err.Error(), `read profile "ghost":`) {
		t.Fatalf("error should be prefixed with 'read profile \"ghost\":', got %q", err.Error())
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("error should wrap fs.ErrNotExist, got %v", err)
	}
}

// TestErrorMessage_ApplyRCSetCommands_MalformedLines documents the contract
// that ApplyRCSetCommands silently passes malformed/non-set lines through
// to the "remaining" return value rather than erroring. The TUI then runs
// those remaining lines as slash commands at startup. A future refactor that
// promotes malformed-line handling to a hard error would silently break
// every user's bitchtearc, so this test pins the current behaviour.
func TestErrorMessage_ApplyRCSetCommands_MalformedLines(t *testing.T) {
	cfg := &Config{}
	lines := []string{
		"set",                                 // missing key — too few fields
		"set unknown-key value",               // unrecognised set key
		"   set provider openai   ",           // well-formed; whitespace tolerated
		"profile load custom",                 // not a 'set' line — passes through
		"# comment-looking-line not stripped", // comment-stripping happens in ParseRC, not here
		"join #dev",                           // arbitrary slash-style line
	}

	remaining := ApplyRCSetCommands(cfg, lines)

	// Lines that don't start with "set" or that don't apply cleanly are
	// returned verbatim. The well-formed `set provider openai` is applied
	// and dropped from remaining.
	wantRemain := []string{
		"set",
		"set unknown-key value",
		"profile load custom",
		"# comment-looking-line not stripped",
		"join #dev",
	}
	if len(remaining) != len(wantRemain) {
		t.Fatalf("remaining length: got %d, want %d (%v)", len(remaining), len(wantRemain), remaining)
	}
	for i, want := range wantRemain {
		if remaining[i] != want {
			t.Fatalf("remaining[%d] = %q, want %q", i, remaining[i], want)
		}
	}

	// The valid `set provider openai` line should have been applied even
	// though it had leading/trailing whitespace.
	if cfg.Provider != "openai" {
		t.Fatalf("Provider after apply: got %q, want %q", cfg.Provider, "openai")
	}
	// Setting provider via /set marks the service as custom (see rc.go).
	if cfg.Service != "custom" {
		t.Fatalf("Service after apply: got %q, want %q", cfg.Service, "custom")
	}
}

// TestErrorMessage_ApplyRCSetCommands_UnknownKeyKeptAsRemaining is a focused
// counterpart: a well-formed `set <key> <value>` line whose key is not in
// SetKeys() is *not* applied and is *not* dropped — it surfaces in remaining
// so the user gets a slash-command error at startup rather than silent loss.
func TestErrorMessage_ApplyRCSetCommands_UnknownKeyKeptAsRemaining(t *testing.T) {
	cfg := &Config{}
	remaining := ApplyRCSetCommands(cfg, []string{"set bogus-key bogus-value"})
	if len(remaining) != 1 || remaining[0] != "set bogus-key bogus-value" {
		t.Fatalf("malformed set line should be returned in remaining, got %v", remaining)
	}
}

// TestErrorMessage_MigrateDataPaths_TargetExists verifies that when the new
// (target) path already exists, MigrateDataPaths returns nil — it does NOT
// produce a spurious error, and it does NOT clobber the existing target.
// This is the on-disk safety contract for users who downgrade and then
// upgrade again across releases.
func TestErrorMessage_MigrateDataPaths_TargetExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	old := filepath.Join(home, ".local", "share", "bitchtea", "memory")
	newP := filepath.Join(home, ".bitchtea", "memory")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatalf("mkdir old: %v", err)
	}
	if err := os.MkdirAll(newP, 0o755); err != nil {
		t.Fatalf("mkdir new: %v", err)
	}
	if err := os.WriteFile(filepath.Join(old, "stale.md"), []byte("old"), 0o644); err != nil {
		t.Fatalf("seed old: %v", err)
	}
	if err := os.WriteFile(filepath.Join(newP, "fresh.md"), []byte("new"), 0o644); err != nil {
		t.Fatalf("seed new: %v", err)
	}

	if err := MigrateDataPaths(); err != nil {
		t.Fatalf("MigrateDataPaths returned error when target already exists: %v", err)
	}

	// The new file must remain untouched.
	got, err := os.ReadFile(filepath.Join(newP, "fresh.md"))
	if err != nil {
		t.Fatalf("read new: %v", err)
	}
	if string(got) != "new" {
		t.Fatalf("target was clobbered: got %q, want %q", got, "new")
	}
	// The old dir must still exist (not moved over the existing target).
	if _, err := os.Stat(filepath.Join(old, "stale.md")); err != nil {
		t.Fatalf("old data should be preserved when target exists: %v", err)
	}
}
