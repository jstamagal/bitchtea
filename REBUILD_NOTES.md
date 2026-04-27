# Rebuild notes

Stuff to fix / decide during the Charm-stack rebuild. Delete this file when done.

## Signal / key handling is broken

`Ctrl+C`, `Ctrl+Z`, and `Esc` are all in inconsistent / varying / borked states in the pre-rebuild code. The rebuild is the chance to make them work cleanly and predictably.

Target behavior to lock in:

- **Ctrl+C** — one-shot full-turn cancel (`ctx` cancel → fantasy stops at next event boundary → transcript marked `_cancelled_` → input ready). No graduation. Tool side effects up to cancel point are real (can't un-write files); in-flight bash subprocess gets SIGKILL'd via `exec.CommandContext`. A second Ctrl+C at the input prompt = quit.
- **Ctrl+Z** — suspend to shell (SIGTSTP), `fg` resumes cleanly with screen redraw.
- **Esc** — graduated three-stage stop:
  - **Esc x1** = if a panel is open, close it. Else if a tool call is in flight, cancel JUST that tool call. The agent's tool callback returns `"user cancelled"` as the tool result; the agent reacts in its next step (usually picks a different path or asks). The turn keeps going.
  - **Esc x2** = full turn cancel (same end-state as Ctrl+C). If a message is queued, it fires as the next turn — killing the current turn is the natural moment for "do this next."
  - **Esc x3** = full turn cancel **and** clear the queue. The "wait no, the queued message ALSO would've dug into the wrong folder" panic button.
  - x1 → x2 → x3 must occur within a short window (~1.5s) of each other to count as graduation; otherwise Esc resets to x1.

Wiring rules:
- Bash tool MUST use `exec.CommandContext(toolCtx, ...)` with a per-tool-call context so Esc x1 / Ctrl+C kill subprocesses cleanly.
- Each tool call gets its OWN context derived from the turn context. Esc x1 cancels the tool ctx; Esc x2 / Ctrl+C cancels the turn ctx (which transitively cancels the tool ctx).
- Tool callbacks should `select` on `ctx.Done()` early to bail fast.
- After Esc x1 cancels a tool, the agent loop must continue — feed `"user cancelled"` back as the tool_result and let fantasy emit the next step.

Implementation reminders for the rebuild:
- Bubble Tea: `tea.KeyMsg` for keyboard, OS signals come in via `signal.Notify` forwarded as a custom msg (bitchtea did this with `SignalMsg` in `main.go`).
- Fantasy: `agent.Stream(ctx, ...)` cancellation is via `ctx`. Cancel the context = streaming stops at the next event boundary.
- Suspend: `tea.NewProgram(... tea.WithAltScreen())` plus a manual `syscall.Kill(os.Getpid(), syscall.SIGTSTP)` on Ctrl+Z. Test that the alt screen restores on `fg`.
