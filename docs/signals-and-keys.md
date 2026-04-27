# 🦍 THE BITCHTEA SCROLLS: SIGNALS & KEYS

The ape must be responsive. Control is absolute.

## 🛑 CANCEL & QUIT

### **`Ctrl+C`**
- **During Stream**: Triggers `ctx.Cancel()`. The agent stops at the next token, marks the transcript, and returns focus to the input.
- **At Input**: A second `Ctrl+C` exits the program (`tea.Quit`).
- **Subprocesses**: Bash commands receive `SIGKILL` via `exec.CommandContext`.

### **`Ctrl+Z`**
- Suspends the TUI (`SIGTSTP`).
- Redraws the screen upon `fg` resume.

## ⌨️ THE 3-STAGE ESC SPEC

As defined in the `REBUILD_NOTES.md` and implemented in `internal/ui/model.go`:

1. **Esc x1 (The Panel Kill)**:
   - Closes open panels (MP3, Tool Panel).
   - Cancels only the **current in-flight tool call** (if applicable).
   - Returns `"user cancelled"` to the model so it can pivot.

2. **Esc x2 (The Turn Kill)**:
   - Equivalent to `Ctrl+C`.
   - Cancels the entire turn.
   - If a message was queued (steering), it becomes the next active turn.

3. **Esc x3 (The Panic Button)**:
   - Cancels the turn **and** clears the entire steering queue.
   - Resets state to idle.

*Note: Graduation (x1 -> x2 -> x3) requires presses within a short window (~1.5s).*

## 📡 SIGNAL FORWARDING (`main.go:88`)
OS signals (`SIGINT`, `SIGTERM`) are captured in `main.go` and forwarded to the Bubbletea loop as `ui.SignalMsg` to ensure the session is saved before the process dies.

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝
