# 🦍 SIGNALS & KEYS 🦍

This scroll documents the target spec for keyboard and OS control, ensuring absolute dominance over the process.

## 1. Core Signals

- **Ctrl+C (Hard Cancel)**: 
  - Cancels the turn context (`ctx`).
  - Terminal transcript is marked `_cancelled_`.
  - In-flight bash subprocesses are killed via `SIGKILL`.
  - A second `Ctrl+C` at an empty input prompt exits the app.

- **Ctrl+Z (Suspend)**:
  - Sends `SIGTSTP` to the process.
  - Redraws the screen upon `fg` resume.

## 2. Graduated Esc Stop (3-Stage)

The `Esc` key provides a "calm panic" mechanism. Actions must occur within a **1.5s window** to graduate.

- **Esc x1**: Close open panel (MP3/Activity) OR cancel the **active tool call only**. The tool returns `"user cancelled"`, and the agent is allowed to react (e.g., pick a different tool).
- **Esc x2**: Full turn cancel (equivalent to `Ctrl+C`).
- **Esc x3**: Full turn cancel **AND** clear the message queue.

## 3. Wiring Rules (`REBUILD_NOTES.md`)

- **Context Derivation**: Each tool call gets a sub-context derived from the turn context. This allows `Esc x1` to kill a specific tool without aborting the entire agent loop.
- **Bash Execution**: Must use `exec.CommandContext(toolCtx, ...)` to ensure subprocesses die cleanly on signal.
- **Select Pattern**: Tool callbacks must `select` on `ctx.Done()` to bail out fast when the user cancels.

APE STRONK TOGETHER. 🦍💪🤝
