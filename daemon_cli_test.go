package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestDaemonStatusReportsNotRunning is the smoke test required by bt-p7-cli's
// acceptance criteria: with a fresh HOME (no daemon ever started), `bitchtea
// daemon status` must print "not running" and exit 0.
func TestDaemonStatusReportsNotRunning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := runDaemon([]string{"status"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("status exit code = %d, want 0 (stderr=%q)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "not running") {
		t.Fatalf("stdout = %q, want 'not running'", stdout.String())
	}
}

// TestDaemonStopReportsNotRunningWhenAbsent mirrors the status case for stop:
// a missing pid file should report "not running" with exit 0 so scripted
// callers can chain stop+start without conditionals.
func TestDaemonStopReportsNotRunningWhenAbsent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := runDaemon([]string{"stop"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("stop exit code = %d, want 0 (stderr=%q)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "not running") {
		t.Fatalf("stdout = %q, want 'not running'", stdout.String())
	}
}

func TestDaemonRejectsUnknownSubcommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := runDaemon([]string{"banana"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit for unknown subcommand")
	}
	if !strings.Contains(stderr.String(), "unknown daemon subcommand") {
		t.Fatalf("stderr = %q, want 'unknown daemon subcommand'", stderr.String())
	}
}

func TestDaemonHelpExitsZero(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := runDaemon([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("--help exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "Subcommands:") {
		t.Fatalf("stdout = %q, want usage block", stdout.String())
	}
}

func TestDaemonNoArgsPrintsUsage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := runDaemon(nil, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("no-args should fail with usage hint, got exit 0")
	}
	if !strings.Contains(stderr.String(), "Subcommands:") {
		t.Fatalf("stderr = %q, want usage block", stderr.String())
	}
}
