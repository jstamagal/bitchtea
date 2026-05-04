# Daemon Entry Points

bitchtea has two daemon entry points with different use cases:

## `bitchtea daemon start` (in-process)

Entry point: `daemon_cli.go` in the root package (function `runDaemon` / `daemonStart`).

This runs the daemon code in-process within the main `bitchtea` binary. The terminal blocks until SIGINT/SIGTERM. This is the user-facing path for v1 -- manual launch only, no detach. Users who want backgrounding use `nohup bitchtea daemon start &` or wire systemd/launchd.

Subcommands:
- `start` -- Run the daemon in the foreground
- `status` -- Check lock file and report pid
- `stop` -- Read pid file and send SIGTERM

## `cmd/daemon/main.go` (standalone binary)

A thin shim around `internal/daemon.Run`. Builds as a separate binary (`cmd/daemon`). This is a dedicated executable for init systems (systemd, launchd) that want a daemon binary without dragging the TUI in.

The standalone binary produces a smaller binary and avoids importing any TUI packages. It writes to the same lock file, pid file, and mailbox directories as the in-process path -- they are interchangeable.

## Relationship

Both entry points call the same `daemon.Run()` with the same `jobs.Handle` dispatcher. The on-disk state (lock, pidfile, mailbox) is identical regardless of which entry point was used. You can start the daemon via `bitchtea daemon start`, kill it, and restart via `cmd/daemon` with no difference in behavior.

The `cmd/daemon` binary is not shipped by default; it is an alternative for packaging or init-system integration.
