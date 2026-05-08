# BitchTea Architecture & Foundational Truth

This document serves as the architectural source of truth for the `bitchtea` codebase. It details the precise inner workings, dependency structures, data flows, and critical discrepancies between the stated design and the current repository state.

## 1. Acyclic Dependency Graph

The runtime splits across a set of packages plus two external Charm libraries (`charm.land/fantasy` for LLM message/tool types, `charm.land/catwalk` for the model catalog). The graph is acyclic: every internal arrow points downward in the ordering shown below, and the two external libraries are pure leaves (they import nothing from this repo). Based on empirical analysis of `go list -f '{{.Imports}}'` across `./...`, the dependency graph is:

```text
Binaries (cmd/*):
  main (./)        -> internal/{agent, catalog, config, daemon, daemon/jobs, llm, session, ui}
  cmd/daemon       -> internal/{config, daemon, daemon/jobs}
  cmd/trace        -> internal/tools          (developer scratchpad; not shipped)

Internal packages (top-down; arrows point at dependencies):
  internal/ui          -> internal/{agent, catalog, config, llm, session, sound}
                          + charm.land/catwalk
  internal/agent       -> internal/{agent/event, config, llm, mcp, memory, tools}
                          + charm.land/fantasy
  internal/agent/event -> (leaf; no internal deps)
  internal/session     -> internal/llm
                          + charm.land/fantasy
  internal/daemon/jobs -> internal/{daemon, memory, session}
  internal/llm         -> internal/{catalog, mcp, tools}
                          + charm.land/fantasy, charm.land/catwalk
  internal/catalog     -> internal/config
                          + charm.land/catwalk
  internal/tools       -> internal/memory
  internal/mcp         -> (leaf; external github.com/modelcontextprotocol/go-sdk)
  internal/config      -> (leaf; stdlib only)
  internal/daemon      -> (leaf; stdlib + golang.org/x/sys)
  internal/memory      -> (leaf; stdlib only)
  internal/sound       -> (leaf; stdlib only)

External critical edges:
  charm.land/fantasy   <- internal/{agent, llm, session}        (LLM message + tool types)
  charm.land/catwalk   <- internal/{catalog, llm, ui}           (model registry / picker)
```

### Wiring Notes & Architectural State
*   **Daemon (shipped):** `cmd/daemon/main.go` is the binary entry point; `internal/daemon/` holds the implementation (`run.go` main loop, `mailbox.go` file-based IPC, `envelope.go` framing, `lock.go`, `pidfile.go`, `jobs/` dispatch registry for session checkpoint and memory consolidation). `main.go` traps `os.Args[1] == "daemon"` and forks into `runDaemon(...)` before any TUI setup.
*   **`write_memory` (shipped):** Live as a typed `fantasy.NewAgentTool` wrapper in `internal/llm/typed_write_memory.go`, wired through `translateTools` in `internal/llm/tools.go` and exposed in the registry alongside `search_memory`.
*   **Fantasy Migration (partial, Phase 2 in flight):** Six tools have typed wrappers in `internal/llm/typed_*.go` — `read`, `write`, `edit`, `bash`, `search_memory`, `write_memory`. Eight tools still flow through the generic `bitchteaTool` compatibility adapter: `terminal_start`, `terminal_send`, `terminal_keys`, `terminal_snapshot`, `terminal_wait`, `terminal_resize`, `terminal_close`, `preview_image`. `translateTools` selects the typed wrapper when one exists; both paths bottom out in `Registry.Execute(name, argsJSON)`. Phase 3 is complete: agent history and session JSONL are fantasy-native (`messages []fantasy.Message`); the remaining `llm.Message` glue is at the `Client.StreamChat` boundary only.
*   **Per-Context Histories (shipped):** `internal/agent/context_switch.go` keeps a `contextMsgs map[ContextKey][]fantasy.Message` so `/join #chan` and `/query nick` swap whole histories instead of relabeling one shared slice. `SetContext` saves the active slice back into the map and loads (or seeds from bootstrap) the target context; `InitContext` pre-creates a context from the bootstrap prefix without switching; `RestoreContextMessages` rehydrates a context from a session log while forcing the bootstrap system prompt to match the current build.

## 2. Superficial Testing ("Testing the Shape")

A significant portion of the test suite validates that code *does not crash*, rather than validating correct behavioral failure modes.
*   `TestReplayExhaustedFixtureDoesNotPanic` (in `internal/agent/replay_test.go`)
*   `TestBashNonexistentCommandDoesNotPanic` (in `internal/tools/tools_test.go`)
*   `TestFantasyToLLMEmptyPartsNoPanic`, `TestLLMToFantasyEmptyUserNoPanic` (in `internal/llm/convert_test.go`)
*   `TestChatMessageFormatLongContentNoPanic`, `TestChatMessageFormatEmptyContentNoPanic` (in `internal/ui/message_test.go`)
*   `TestRapidEscInputDoesNotPanic` (in `internal/ui/model_turn_test.go`)

These tests assert that a `panic()` is not triggered, ignoring whether the resulting state, error message, or data transformation is structurally sound.

## 3. Under the Hood: Core Execution Flows

### 3.1. `main.go`: The Boot Sequence
1.  **Daemon Trap:** Intercepts `os.Args[1] == "daemon"` to bypass the TUI entirely.
2.  **Configuration:** Invokes `config.MigrateDataPaths()` and sets up default configs via `~/.bitchtea/bitchtearc`.
3.  **Catalog Refresh:** Kicks off `maybeStartCatalogRefresh()` asynchronously if `BITCHTEA_CATWALK_AUTOUPDATE` is set.
4.  **Headless vs. UI:**
    *   **Headless (`--headless`):** Boots an `agent.Agent` and routes `runHeadlessLoop()`. Output is explicitly piped to `stdout`/`stderr`.
        *   *LLM Output:* Direct print to `stdout`.
        *   *Tool Start:* `[tool] <name> args=<truncated_json>` to `stderr`.
        *   *Tool Result:* `[tool] <name> result=<truncated_output>` to `stderr`.
        *   *Status:* `[status] thinking` / `[status] tool_call` to `stderr`.
    *   **TUI:** Passes control to Bubble Tea `tea.NewProgram(ui.NewModel(...))`.

### 3.2. `internal/agent/agent.go`: The LLM Engine (Churning & Firing)
The `Agent` struct is the heart of the REPL.

*   **Prompt Injection (`buildSystemPrompt`)**: The exact payload sent to the LLM upon bootstrap includes:
    1.  **Environment Header:** `System: <OS> | Host: <hostname> | User: <user> | Time: <time> | CWD: <pwd>`
    2.  **Tool Definitions:** `writeToolPrompt` dumps JSON schemas mapped to descriptions.
    3.  **Memory Instructions:** `writeMemoryPrompt` details exactly how the LLM should interact with context (even mentioning the non-existent `write_memory` tool).
    4.  **Persona Anchor (`personaPrompt`):** The massive "SHE APE" injection block dictating the model's behavior, tone, "POACHER" avoidance, and constraints.
    5.  **Context Loading:** Loads `AGENTS.md`, `CLAUDE.md`, and workspace `MEMORY.md`.
    6.  **Rehearsal:** Ends the system injection with `[User: personaPrompt] -> [Assistant: ready. APES STRONG TOGETHER]` to anchor the personality before the user's first prompt.

*   **Turn Lifecycle (`sendMessage`)**:
    1.  Transforms `userMsg` -> expands file refs (`@file`) -> prepends `PerMessagePrefix`.
    2.  Appends `Event{Type: "state", State: StateThinking}` to UI.
    3.  Fires `a.streamer.StreamChat` in a goroutine.
    4.  Selects over `streamEvents` and `ctx.Done()`:
        *   **`text`**: Flows through `newFollowUpStreamSanitizer` (to intercept autonomous tokens like `AUTONEXT_DONE`). Yields to UI.
        *   **`tool_call`**: Registers `ToolCallID`, spins up UI loaders.
        *   **`tool_result`**: Yields back to the UI loop.
        *   **`done`**: Collapses the turn. If tokens like `AUTONEXT_DONE` are present, they are stripped and replaced with `Done.` in the transcript.

*   **Queueing / Mid-turn Inputs (`drainAndMirrorQueuedPrompts`)**:
    If a user types while the LLM is churning, the UI calls `QueuePrompt()`. When the LLM calls `PrepareStep`, it pulls these queued inputs and injects them dynamically as `[queued prompt X]: <text>` to prevent input loss.

### 3.3. `internal/ui/model.go`: TUI Management
Manages raw `tea.Msg` events.

*   **Render Loop (`View`)**:
    Constructs the TopBar (Provider/Model/Context), Viewport (chat), Status Bar (Token usage, tools stats, MP3 status), and Textarea.
*   **Key Intercepts**:
    *   `Esc` Ladder:
        1. Press 1: Cancels the *active tool* (`agent.CancelTool`) but lets the LLM turn continue.
        2. Press 2: Cancels the entire LLM turn (`cancelActiveTurnWithQueueArm`).
    *   `Ctrl+C` Ladder:
        1. Press 1: Cancels active turn.
        2. Press 2: Clears queued inputs.
        3. Press 3: Hard process exit.
*   **Event Handling (`handleAgentEvent`)**:
    *   Translates internal agent state to visible UI.
    *   If `tool_result` is larger than 20 lines, it is strictly truncated on screen: `<first 20 lines> ... (X more lines)`. The LLM still sees the full output (within its context limits), but the human viewport obscures it.
    *   If `thinking` placeholders are present, they are overwritten character-by-character as `text` events arrive.

## 4. Autonomous Follow-ups (`MaybeQueueFollowUp`)
If enabled via flags (`--auto-next-steps`, `--auto-next-idea`), when a turn completes, the agent evaluates the assistant's last raw response.
*   If it does *not* contain the `AUTONEXT_DONE` or `AUTOIDEA_DONE` magic tokens, the agent injects a silent follow-up user prompt.
*   **Exact input:** `"What are the next steps? If there is remaining work, do it now instead of just describing it..."`
*   This creates an internal loop where the LLM continuously pushes actions until it explicitly signals fatigue using the done tokens.

---
*Note: This architecture file describes the exact empirical wiring of the repository, ignoring theoretical aspirations that have not been fully merged or deprecations that were incompletely executed.*