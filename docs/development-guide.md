# BitchTea Development Guide & Operational Reality

Welcome to the jungle. This document is the foundational source of truth for junior models, new contributors, and architects working on the `bitchtea` codebase. It strips away the idealistic documentation and exposes the raw, operational reality of how this system breathes, churns, and breaks.

## 1. How to Contribute (A Human Standpoint)

We build software that is aggressive, fast, and unapologetically powerful. When you contribute to BitchTea, you are touching a live wire. 

*   **Acyclic Mandate:** The dependency graph is strictly acyclic. `main -> agent -> llm -> tools -> memory`. If you draw a line upward (e.g., `tools` depending on `llm`), you will break the build.
*   **No Artificial Guardrails:** Do not add safety bumpers to tools. If a user asks the agent to `rm -rf /`, the agent should execute it. The user owns the risk.
*   **The TUI Must Not Block:** The Bubble Tea `Update()` loop must remain entirely non-blocking. Any long-running work (LLM streams, catwalk fetching, executing shell tools) MUST happen in a goroutine and send `tea.Msg` events back to the main loop.
*   **Beads (`bd`) is King:** Do not use markdown TODO lists. Use the `bd` issue tracker. Before closing a session, you must push to the remote.

## 2. Testing Rules & The Ugly Truth

To close an issue, the following quality gates MUST pass:
```bash
go build ./...
go test ./...
go test -race ./...
go vet ./...
```

### The Ugly Truth: 'Testing the Shape'
Junior models take note: much of the existing test suite does not actually test failure modes. It tests *survival*.

*   **`TestReplayExhaustedFixtureDoesNotPanic`** (`internal/agent/replay_test.go`)
*   **`TestBashNonexistentCommandDoesNotPanic`** (`internal/tools/tools_test.go`)
*   **`TestFantasyToLLMEmptyPartsNoPanic`** (`internal/llm/convert_test.go`)
*   **`TestChatMessageFormatEmptyContentNoPanic`** (`internal/ui/message_test.go`)
*   **`TestRapidEscInputDoesNotPanic`** (`internal/ui/model_turn_test.go`)

These tests simply assert that the program does not trigger a `panic()`. They do **not** assert that the UI rendered correctly, that the LLM recovered, or that the tool output the correct `stderr` payload. When you write new tests, do better. Assert against the actual state mutations and exact text outputs.

## 3. The State of the `fantasy` Migration

The migration to the `charm.land/fantasy` stack is **incomplete and currently living in a hybrid state.** 

Here is exactly what is happening under the hood:
1.  **Agent Storage (`internal/agent`):** The `Agent` struct holds the canonical history as `[]fantasy.Message`.
2.  **The Streamer Boundary (`internal/llm`):** The underlying LLM client expects the legacy `[]llm.Message`.
3.  **The Bridge (`llm.FantasySliceToLLM`):** When a turn begins, the entire `fantasy.Message` history is projected downwards into `llm.Message` structs.
4.  **The Return Journey (`llm.LLMToFantasy`):** When the streamer finishes, the resulting legacy messages are lifted back up into `fantasy.Message` structs.
5.  **Untyped Tools:** Tools are *still* untyped. They rely on `Registry.Execute(name, argsJSON)`. The typed `fantasy.NewAgentTool` wrappers are the aspirational target, but they are not wired in.

**Do not write code assuming the `fantasy` migration is finished.** You must respect the bridging layers.

## 4. REPL & LLM Data Flows: What's Firing and Churning

### The Agent Loop (`agent.sendMessage`)
When a user presses `Enter`, the following sequence fires:

1.  **In:** `userMsg` (string)
2.  **Preprocessing:** `@file` references are expanded. `PerMessagePrefix` is injected.
3.  **UI Feedback:** `Event{Type: 'state', State: StateThinking}` is pushed to the UI, spinning up the Magenta dot spinner.
4.  **Churning:** `a.streamer.StreamChat` runs in a goroutine, sending chunks back via `streamEvents`.
5.  **Out (`text`):** As tokens arrive, they flow through the `newFollowUpStreamSanitizer`. If the tokens match `AUTONEXT_DONE`, they are swallowed and replaced with `Done.`. Otherwise, they yield to the UI and render in the viewport.
6.  **Out (`tool_call`):** `Event{Type: 'tool_start', ToolName: <name>}` fires. The Tool Panel opens, the UI says `calling <name>...`.
7.  **Out (`tool_result`):** If the tool output is > 20 lines, the UI strictly truncates it:
    `[output line 1...20]
... (X more lines)`
    *Note: The LLM receives the full, untruncated string up to its context limit.*
8.  **Mid-Turn Queuing:** If the user types *while* the LLM is churning, the `drainAndMirrorQueuedPrompts` function fires via `PrepareStep`. It injects the buffered text mid-stream as `[queued prompt X]: <text>`.
9.  **Done:** The turn collapses. Checkpoints and sessions are appended.

### Headless Mode (`--headless`)
When running `bitchtea --headless --prompt '...'`, the TUI is bypassed. The exact output emitted is:
*   `[status] thinking` (to stderr)
*   `[tool] <name> args=<truncated json>` (to stderr)
*   `[tool] <name> result=<truncated output>` (to stderr)
*   `<LLM raw string output>` (to stdout)

## 5. Unfinished Business & Disconnected Wiring

As an architect, you must be aware of the ghosts in the machine.

*   **The Phantom Daemon:** `CLAUDE.md` explicitly states: *'there is currently no daemon binary, no `cmd/daemon`, no `internal/daemon` package.'* **This is a lie.** `cmd/daemon/main.go` and the `internal/daemon` package exist and are wired into `main.go`. If `os.Args[1] == 'daemon'`, it traps the execution and bypasses the TUI.
*   **The `write_memory` hallucination:** The System Prompt explicitly tells the LLM:
    *- 'call write_memory with a clear title and concise content.'*
    However, the `write_memory` tool **does not exist** in the `tools.Registry`. The LLM is being instructed to use a phantom tool. Only `search_memory` is wired in.
*   **Isolated Contexts:** We masquerade as an IRC client (`/join #channel`). The UI labels change, but the agent's underlying history slices and session compaction routines often bleed context because the isolation layer isn't fully airtight yet.

**Your directive:** Do not blindly trust the `README.md` or `CLAUDE.md`. Trust the code. When you build, respect the raw mechanisms documented above.
