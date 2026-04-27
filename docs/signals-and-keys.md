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

## 🧱 TECHNICAL DEEP-DIVE: THE INTERRUPTS

### 🧬 THE CTRL+C PIPELINE
Control signals propagate from the user's fingertips down to the OS process tree.

1.  **Signal Capture**: `main.go:88` traps `syscall.SIGINT` and `SIGTERM`. It forwards these to the Bubbletea program via `p.Send(ui.SignalMsg{Signal: sig})`.
2.  **TUI Dispatch**: `internal/ui/model.go` handles both the raw `ctrl+c` key and the `SignalMsg`.
3.  **State Graduation**: `handleCtrlCKey` (`line 856`) manages a 3-stage exit:
    - **Stage 1 (The Snip)**: Calls `cancelActiveTurn`. This executes the `m.cancel()` context cancellation.
    - **Stage 2 (The Wipe)**: Clears the steering queue (`m.queued = nil`).
    - **Stage 3 (The Kill)**: Returns `tea.Quit` to the Bubbletea runtime.
4.  **Context Propagation**: 
    - The `context.CancelFunc` was created in `startAgentTurn` (`line 821`). 
    - This context is passed into `agent.SendMessage` -> `streamer.StreamChat` -> `fantasy`.
    - **Crucial**: The `bash` tool uses `exec.CommandContext(ctx, "bash", "-c", ...)` (`internal/tools/tools.go:325`). When the context cancels, Go sends `SIGKILL` to the bash process group, ensuring no orphaned processes.

### 🧬 THE 3-STAGE ESC PROTOCOL
Implemented in `handleEscKey` (`internal/ui/model.go:923`), this uses a graduation window of ~1.5s (`escGraduationWindow`).

- **Logic Hook**:
  ```go
  if now.Sub(m.escLast) > escGraduationWindow {
      m.escStage = 0
  }
  ```

- **Stage 1: Tool/Panel Cancel**
  - If a panel is open, it closes.
  - If `m.activeToolName` is set, it interrupts the tool. *Note: Current implementation falls back to turn-cancel as the per-tool context shim is still being hardened.*

- **Stage 2: Turn Cancel**
  - Triggers `cancelActiveTurn`.
  - Marks `m.queueClearArmed = true`, priming the system for the "Stage 3" panic button.

- **Stage 3: Queue Clear**
  - Checked at the very start of the function:
    ```go
    if m.queueClearArmed && len(m.queued) > 0 {
        m.queued = nil
        // ...
    }
    ```
  - This ensures that if the agent was about to pivot to the next task in the queue, KING can stop the entire chain with one final tap.

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝

## 🧱 TECHNICAL DEEP-DIVE: CONTROL INTERNALS

### 🧬 THE CTRL+C PIPELINE
The signal chain for `Ctrl+C` is a multi-layered propagation from the OS to the in-flight process tree.

1.  **Signal Capture**: `main.go:88` traps `syscall.SIGINT` and `SIGTERM`. It forwards these to the Bubbletea program via `p.Send(ui.SignalMsg{Signal: sig})`.
2.  **TUI Dispatch**: `internal/ui/model.go` handles both the raw `ctrl+c` key and the `SignalMsg`.
3.  **The Snip (`cancelActiveTurn`)**: 
    - The model calls `m.cancel()`, which is the `context.CancelFunc` generated during `startAgentTurn` (`line 821`).
    - This context is shared with `agent.SendMessage` -> `streamer.StreamChat` -> `fantasy`.
    - **Crucial**: The `bash` tool uses `exec.CommandContext(ctx, "bash", "-c", ...)` (`internal/tools/tools.go:325`). When the context is cancelled, Go immediately sends `SIGKILL` to the bash process group, ensuring no orphaned shells.
4.  **Verbatim Viewport Output**:
    - Stage 1 (During Stream): `Interrupted. Press Ctrl+C again to clear queued messages...`
    - Stage 2 (Idle with Queue): `Cleared X queued message(s). Press Ctrl+C again to quit.`
    - Stage 3: The process exits.

---

### 🧬 THE 3-STAGE ESC PROTOCOL
Implemented in `handleEscKey` (`internal/ui/model.go:923`), this protocol provides a graduated "brakes" system using a state machine and a timer.

#### ⚙️ Timer Logic
- **Window**: 1.5 seconds (`escGraduationWindow`).
- **Logic Hook**:
  ```go
  if !m.escLast.IsZero() && now.Sub(m.escLast) > escGraduationWindow {
      m.escStage = 0 // Reset graduation if user waited too long
  }
  m.escLast = now
  m.escStage++
  ```

#### ⚙️ Stage Transitions
1.  **Stage 1: Tool Interrupt**
    - **Logic**: If `m.activeToolName != ""` and the agent is streaming.
    - **Output**: `Tool-only cancel is not wired yet; cancelled the current turn while [tool] was running.`
    - **Handshake**: Triggers `cancelActiveTurn`. *Development Note: Future hardening will allow Stage 1 to only cancel the tool while letting the agent "pivot" based on a "user cancelled" string.*
2.  **Stage 2: Turn Interrupt**
    - **Logic**: If the agent is streaming but no specific tool is active.
    - **Handshake**: Calls `cancelActiveTurn`, which invokes the turn-level `context.Cancel()`.
    - **Side Effect**: Sets `m.queueClearArmed = true`.
    - **Output**: `Interrupted by Esc. Press Esc again to clear queued messages.`
3.  **Stage 3: Queue Wipe (The Panic Button)**
    - **Logic**: Checked at the very entry of `handleEscKey`. If `m.queueClearArmed` is true and a second Esc arrives within the window.
    - **Mutation**: `m.queued = nil`.
    - **Output**: `Cleared queued messages.`

---

### 🧬 THE CTRL+Z SUSPEND
Bitchtea uses a native OS pass-through for suspension.

1.  **TUI Capture**: `model.go` receives `tea.KeyMsg` for `ctrl+z`.
2.  **State Save**: It returns `tea.Suspend`. 
3.  **OS Handshake**: Bubbletea executes the suspension, allowing the terminal to return to the shell.
4.  **Resume**: Upon `fg`, the TUI receives a resize event and triggers `m.refreshViewport()` to restore the canopy state verbatim.

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝
