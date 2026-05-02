// Command daemon is the bitchtea background process. It is a thin shim
// around internal/daemon.Run; the real lifecycle, locking, and IPC live
// there. See docs/phase-7-process-model.md.
//
// This binary is intentionally separate from the main `bitchtea` binary:
// `bitchtea daemon start` runs the same code in-process for v1 (manual
// launch only, no detach), but having `cmd/daemon` lets users wire init
// systems against a dedicated executable without dragging the TUI in.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/daemon"
	"github.com/jstamagal/bitchtea/internal/daemon/jobs"
)

func main() {
	if err := config.MigrateDataPaths(); err != nil {
		fmt.Fprintf(os.Stderr, "bitchtea-daemon: data migration warning: %v\n", err)
	}

	base := config.BaseDir()
	paths := daemon.Layout(base)

	logFile, err := daemon.OpenLog(paths.LogPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bitchtea-daemon: %v\n", err)
		os.Exit(1)
	}
	defer logFile.Close()

	// Tee stderr to the log file so an operator can `tail -F daemon.log`
	// while still seeing fatal startup errors on the terminal.
	logger := log.New(io.MultiWriter(os.Stderr, logFile), "daemon: ", log.LstdFlags)

	if err := daemon.Run(context.Background(), daemon.RunOptions{
		BaseDir:  base,
		Logger:   logger,
		Dispatch: jobs.Handle,
	}); err != nil {
		if err == daemon.ErrLocked {
			fmt.Fprintln(os.Stderr, "bitchtea-daemon: another daemon is already running")
			os.Exit(0) // design: locked = exit 0 with stderr message
		}
		logger.Printf("fatal: %v", err)
		os.Exit(1)
	}
}
