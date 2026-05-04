# BitchTea Architecture & Foundational Truth

This document serves as the architectural source of truth for the `bitchtea` codebase. It details the precise inner workings, dependency structures, data flows, and critical discrepancies between the stated design and the current repository state.

## 1. Acyclic Dependency Graph

The runtime splits across a set of packages. The theoretical dependency graph dictates an acyclic structure. Based on empirical analysis of imports, the actual dependency graph is:

```text
main
├── cmd/daemon
├── internal/agent
├── internal/catalog
├── internal/config
├── internal/daemon
├── internal/llm
├── internal/session
└── internal/ui

cmd/daemon -> internal/config, internal/daemon, internal/daemon/jobs
cmd/trace -> internal/tools

internal/ui -> internal/agent, internal/catalog, internal/config, internal/llm, internal/session, internal/sound
internal/agent -> internal/config, internal/llm, internal/mcp, internal/memory, internal/tools
internal/daemon/jobs -> internal/daemon, internal/memory, internal/session
internal/session -> internal/llm
internal/llm -> internal/catalog, internal/mcp, internal/tools
internal/mcp -> (external MCP SDKs)
internal/tools -> internal/memory
```

### Disconnected Wiring & Architectural Discrepancies
*   **The Phantom Daemon:** Documentation (e.g., CLAUDE.md) explicitly claims "there is currently no daemon binary, no cmd/daemon, no internal/daemon package." **This is factually incorrect.** `cmd/daemon/main.go` and `internal/daemon/` exist, are fully compiled, and are wired into `main.go` via `if os.Args[1] == "daemon" { os.Exit(runDaemon(...)) }`. The daemon code interacts with memory and session.
*   **Fantasy Migration Incomplete:** The migration to `charm.land/fantasy` is partial. `internal/agent` manages a `[]fantasy.Message` state, but bridges to `[]llm.Message` at the streamer boundary using `llm.FantasySliceToLLM()`. Tool execution remains largely untyped (`Registry.Execute(name, argsJSON)`) rather than using fully integrated typed `fantasy.NewAgentTool` wrappers.
*   **Context Histories Isolation:** The UI mimics IRC (`/join #chan`, `/query nick`), and while `agent.go` manages `contextMsgs` mapping `ContextKey` to `[]fantasy.Message`, the LLM loop conversion and certain session compaction flows still blur the lines, relying on shared pointers or bleeding memory scopes.
*   **Missing `write_memory` Tool:** The prompt explicitly instructs the LLM to call `write_memory` ("call write_memory with a clear title and concise content"), but the tool is unimplemented. Only `search_memory` exists in the registry.

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
2.  **Configuration:** Invokes `config.MigrateDataPaths()` and sets up default configs via `~/.bitchtearc`.
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