package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"syscall"

	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/daemon"
	"github.com/jstamagal/bitchtea/internal/daemon/jobs"
)

// runDaemon implements `bitchtea daemon <subcommand>`. It dispatches before
// the normal flag parser sees `daemon`, which keeps the existing CLI surface
// untouched: `daemon` is a subcommand, not a flag, and never gets confused
// with --resume or friends.
//
// v1 design choices:
//   - `start` runs the daemon code in-process (no detach) — the user's
//     terminal blocks until SIGINT/SIGTERM. Detached spawning is a future
//     enhancement; users who want a backgrounded daemon today should use
//     `nohup bitchtea daemon start &` or wire systemd / launchd.
//   - `status` checks the lock file (authoritative) and reports the pid
//     from the informational pid file. Exits 0 in both running and
//     not-running cases — this is a query, not a probe.
//   - `stop` reads the pid file and sends SIGTERM. Missing pid file or
//     dead process is reported as "not running" with exit 0 so scripted
//     callers can chain `daemon stop && daemon start`.
func runDaemon(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printDaemonUsage(stderr)
		return 2
	}
	switch args[0] {
	case "start":
		return daemonStart(stderr)
	case "status":
		return daemonStatus(stdout)
	case "stop":
		return daemonStop(stdout)
	case "-h", "--help", "help":
		printDaemonUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "bitchtea: unknown daemon subcommand: %s\n", args[0])
		printDaemonUsage(stderr)
		return 2
	}
}

func daemonStart(stderr io.Writer) int {
	base := config.BaseDir()
	paths := daemon.Layout(base)

	logFile, err := daemon.OpenLog(paths.LogPath)
	if err != nil {
		fmt.Fprintf(stderr, "bitchtea: cannot open daemon log: %v\n", err)
		return 1
	}
	defer logFile.Close()
	logger := log.New(io.MultiWriter(stderr, logFile), "daemon: ", log.LstdFlags)

	err = daemon.Run(context.Background(), daemon.RunOptions{
		BaseDir:  base,
		Logger:   logger,
		Dispatch: jobs.Handle,
	})
	if err == nil {
		return 0
	}
	if errors.Is(err, daemon.ErrLocked) {
		// Try to surface the existing pid for better operator feedback.
		if pid, perr := daemon.ReadPid(paths.PidPath); perr == nil {
			fmt.Fprintf(stderr, "bitchtea: daemon already running (pid %d)\n", pid)
		} else {
			fmt.Fprintln(stderr, "bitchtea: another daemon is already running")
		}
		return 0
	}
	if isPermissionError(err) {
		fmt.Fprintf(stderr, "bitchtea: cannot create daemon directories under %s: %v\n", base, err)
		return 1
	}
	fmt.Fprintf(stderr, "bitchtea: daemon failed: %v\n", err)
	return 1
}

func daemonStatus(stdout io.Writer) int {
	paths := daemon.Layout(config.BaseDir())
	locked, err := daemon.IsLocked(paths.LockPath)
	if err != nil {
		fmt.Fprintf(stdout, "unknown (probe error: %v)\n", err)
		return 0
	}
	if !locked {
		fmt.Fprintln(stdout, "not running")
		return 0
	}
	if pid, perr := daemon.ReadPid(paths.PidPath); perr == nil {
		fmt.Fprintf(stdout, "running (pid %d)\n", pid)
		return 0
	}
	fmt.Fprintln(stdout, "running (pid unknown)")
	return 0
}

func daemonStop(stdout io.Writer) int {
	paths := daemon.Layout(config.BaseDir())
	pid, err := daemon.ReadPid(paths.PidPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(stdout, "not running")
			return 0
		}
		fmt.Fprintf(stdout, "not running (pidfile error: %v)\n", err)
		return 0
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Fprintf(stdout, "not running (pid %d not found)\n", pid)
		return 0
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		// ESRCH means the process is gone; treat as already-stopped.
		if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
			fmt.Fprintln(stdout, "not running")
			_ = daemon.RemovePid(paths.PidPath)
			return 0
		}
		fmt.Fprintf(stdout, "stop failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "stop signal sent (pid %d)\n", pid)
	return 0
}

func isPermissionError(err error) bool {
	return errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM)
}

func printDaemonUsage(w io.Writer) {
	fmt.Fprintln(w, `bitchtea daemon — manage the background daemon

Usage: bitchtea daemon <subcommand>

Subcommands:
  start    Run the daemon in the foreground (Ctrl-C to stop). Manual launch
           only — for backgrounding, use nohup/systemd/launchd.
  status   Print "running (pid N)" or "not running". Exit 0 either way.
  stop     Send SIGTERM to the running daemon. "not running" if absent.

The daemon is opt-in. The TUI works fine without it. See
docs/phase-7-process-model.md for the full design.`)
}
