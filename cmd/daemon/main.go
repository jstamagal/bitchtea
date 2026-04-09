package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/jstamagal/bitchtea/internal/daemon"
)

func main() {
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--install":
			execPath, _ := os.Executable()
			if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
				execPath = resolved
			}
			if err := daemon.Install(execPath); err != nil {
				fmt.Fprintf(os.Stderr, "bitchtea-daemon: install failed: %v\n", err)
				os.Exit(1)
			}
			return
		case "--uninstall":
			if err := daemon.Uninstall(); err != nil {
				fmt.Fprintf(os.Stderr, "bitchtea-daemon: uninstall failed: %v\n", err)
				os.Exit(1)
			}
			return
		case "--help", "-h":
			printUsage()
			return
		}
	}

	cfg := daemon.DefaultConfig()

	// BITCHTEA_DAEMON_MODEL overrides the heartbeat model (legacy env var).
	if m := os.Getenv("BITCHTEA_DAEMON_MODEL"); m != "" {
		cfg.HeartbeatModel = m
	}

	// Write PID file for process management / health checks.
	pidPath := filepath.Join(cfg.DataDir, "daemon.pid")
	_ = os.MkdirAll(cfg.DataDir, 0755)
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0644); err != nil {
		cfg.Logger.Printf("warning: could not write PID file: %v", err)
	}
	defer os.Remove(pidPath)

	d := daemon.New(cfg)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := d.Start(ctx); err != nil && err != context.Canceled {
		cfg.Logger.Fatalf("daemon exited with error: %v", err)
	}
}

func printUsage() {
	fmt.Print(`bitchtea-daemon — background heartbeat and janitor

Usage: bitchtea-daemon [--install | --uninstall]

Flags:
  --install     Write systemd user unit and enable the service
  --uninstall   Disable and remove the systemd user unit

Environment:
  ANTHROPIC_API_KEY        Required for HOT.md compaction (always uses claude-opus-4-6)
  BITCHTEA_COMPACT_MODEL   Override compact model (default: claude-opus-4-6 — do not set to cheap model)
  BITCHTEA_HEARTBEAT_MODEL Cheap model for heartbeat pings
  BITCHTEA_DAEMON_MODEL    Alias for BITCHTEA_HEARTBEAT_MODEL (legacy)
  OPENAI_API_KEY           Used for heartbeat if Anthropic key is not set

Behaviour:
  - Startup:       run janitor immediately
  - Every 25 min:  heartbeat — ping cheap model to verify API connectivity
  - Every 30 min:  janitor — prune tool blocks from HOT.md files;
                   compact files >8 KB using claude-opus-4-6 (Anthropic only)

PID file:  ~/.local/share/bitchtea/daemon.pid
Unit file: ~/.config/systemd/user/bitchtea-daemon.service (after --install)
`)
}
