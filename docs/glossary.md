# BITCHTEA: GLOSSARY

Terms used across the bitchtea codebase and documentation, with definitions and references to the defining file or package.

- **`APE`**: The LLM agent. The entity that does the work. Strength through action. Defined in the persona anchor in `internal/agent/agent.go`.

- **`KING`**: The user. The alpha who gives commands. Defined in the persona anchor in `internal/agent/agent.go`.

- **`Adapter (typed / legacy)`**: Two paths for tool execution. Typed adapters (`internal/llm/typed_*.go`) wrap tools as `fantasy.NewAgentTool` with schema generation. The legacy generic adapter (`bitchteaTool`) passes raw JSON schemas. `translateTools` in `internal/llm/tools.go` selects the typed wrapper when one exists.

- **`Bootstrap`**: The sequence when a session starts: inject system prompt, context files, persona anchor, and rehearsal turn before the first user message. See `buildSystemPrompt` in `internal/agent/agent.go`.

- **`Bootstrap Context`**: Injected messages at the start of every session: context files (`AGENTS.md`, `CLAUDE.md`), `MEMORY.md`, and the persona anchor. See `buildSystemPrompt` in `internal/agent/agent.go`.

- **`Catalog`**: The model registry. A list of available models from each provider, cached at `~/.bitchtea/catalog/providers.json` with an embedded offline snapshot. `internal/catalog/` manages refresh and query.

- **`Catwalk`**: The external Charm library (`charm.land/catwalk`) for model-catalog discovery and fuzzy-find model picking in the UI. Imported by `internal/catalog`, `internal/llm`, and `internal/ui`.

- **`Checkpoint`**: A daemon-driven snapshot of a session at a point in time, saved by the session checkpoint job in `internal/daemon/jobs/`. Used for crash recovery and long-term session preservation.

- **`Compaction`**: The process of truncating the in-memory message history by flushing older messages to per-day memory files via `internal/memory`, then clearing them from the active slice. Triggered by `/compact` or automatically when context grows too large. See `Compact()` in `internal/agent/agent.go`.

- **`Context (IRC)`**: Routing metadata that mimics IRC channels and queries to keep different tasks separated. Each context has its own message history, stored in `contextMsgs map[ContextKey][]fantasy.Message`. Switched via `/join #chan` and `/query nick`. See `internal/agent/context_switch.go`.

- **`Context Files`**: `AGENTS.md` and `CLAUDE.md` files discovered upward from the working directory, injected into every session bootstrap to ground the agent. See `internal/agent/context.go`.

- **`Context Key`**: The `ContextKey` type that identifies an IRC context — either a channel (`#name`) or a query (`nickname`). Used as the map key in per-context history storage. Defined in `internal/agent/context_switch.go`.

- **`Daily Archive`**: A daily append file in `~/.bitchtea/memory/` where compacted message history is stored, keyed by workspace and memory scope. See `internal/memory`.

- **`Drain`**: The process of collecting and injecting queued user inputs when the LLM finishes a turn. `drainAndMirrorQueuedPrompts` in `internal/agent/agent.go` pulls prompts queued by `QueuePrompt()` and injects them as mid-turn inputs.

- **`Durable Memory`**: Facts extracted during compaction and saved to daily log files for long-term recall. Accessed via the `search_memory` tool and `/memory` command. See `internal/memory`.

- **`Envelope`**: The message framing format used for daemon IPC over the mailbox. Carries message type, payload, and routing metadata. Defined in `internal/daemon/envelope.go`.

- **`Event`**: A typed message emitted by the agent during a turn: `text`, `tool_start`, `tool_result`, `state`, `error`, or `done`. Events flow from the agent to the UI through channels. Defined in `internal/agent/event/`.

- **`Fantasy`**: The internal Go library (`charm.land/fantasy`) providing shared LLM message and tool types used across `internal/agent`, `internal/llm`, and `internal/session`. Replaces raw `llm.Message` types in most of the stack.

- **`Focus`**: An active IRC context — the context that is currently displayed in the UI and receiving the agent's output. Managed by `FocusManager` in `internal/agent/`.

- **`FocusManager`**: The component that tracks which IRC context is currently active (focused). Owns the `Focus` and manages context switching. Defined in `internal/agent/context_switch.go`.

- **`Green Dark`**: The philosophy of minimal friction, raw speed, and no corporate politeness. The tonal foundation of the project.

- **`Hot Memory`**: The `MEMORY.md` file in the workspace root, used for immediate, high-priority context. Consumed via `/memory` and injected during bootstrap. See `internal/agent/context.go`.

- **`IRCContext`**: The data structure representing a single IRC context (channel or query), holding its message history, metadata, and bootstrap state. Defined in `internal/agent/context_switch.go`.

- **`Ladder (cancel)`**: A progressive cancellation system. `Esc` ladder: press 1 cancels the active tool, press 2 cancels the entire LLM turn. `Ctrl+C` ladder: press 1 cancels the turn, press 2 clears queued inputs, press 3 hard-exits the process. See `internal/ui/model.go`.

- **`Mailbox`**: A file-based IPC mechanism for the daemon. Messages (envelopes) are written to a shared directory and consumed by the daemon loop. Defined in `internal/daemon/mailbox.go`.

- **`mcpAgentTool`**: An adapter that bridges MCP (Model Context Protocol) tools from an MCP server into the bitchtea tool registry. Defined in `internal/tools/tools.go`.

- **`Membership`**: Channel/query membership state tracked in `internal/session/membership.go`, persisted alongside the session log.

- **`MemoryScope`**: An enum (`RootScope`, `ChannelScope`, `QueryScope`) that controls where memory reads and writes are routed in the scoped memory store. Used by the `search_memory` and `write_memory` tools and agent compaction. See `internal/memory`.

- **`Persona`**: The in-character identity of the APE agent — a persona anchor message injected at the end of bootstrap. Dictates behavior, tone, "POACHER" avoidance, and constraints. See `personaPrompt` in `internal/agent/agent.go`.

- **`Persona Anchor`**: A synthetic exchange at the end of bootstrap that locks the agent into its APE persona. See `personaPrompt` in `internal/agent/agent.go`.

- **`Profile`**: A named set of provider configuration values (endpoint, API key, model, etc.). Built-in profiles include `ollama`, `openrouter`, `zai-openai`, `zai-anthropic`. Saving and loading via `/profile` command or `--profile` flag. See `internal/config`.

- **`Provider`**: The upstream LLM API (OpenAI-compatible or Anthropic). Inferred from environment variables by `DetectProvider` in `internal/config`. The wire format use `openai` or `anthropic` as values.

- **`Rehearsal Token`**: A synthetic `[User: personaPrompt] -> [Assistant: ready. APES STRONG TOGETHER]` exchange at the end of system injection that anchors the personality before the first user prompt. See `internal/agent/agent.go`.

- **`Sanitizer`**: A stream filter applied to LLM output to intercept autonomous tokens like `AUTONEXT_DONE` and `AUTOIDEA_DONE`, removing them from the visible transcript. See `newFollowUpStreamSanitizer` in `internal/agent/agent.go`.

- **`Scope (memory)`**: The routing dimension for memory operations — `RootScope` (workspace-wide), `ChannelScope` (per-channel), or `QueryScope` (per-query). Determines which storage file is read or written. See `internal/memory`.

- **`Service`**: The upstream identity used for per-service gating. Distinct from `provider` (the wire format). A provider can be `openai` while the service is a specific compatibility layer.

- **`Sidecar (file)`**: A file that lives alongside a session log (same basename, different extension) carrying auxiliary data such as membership state. See `internal/session/membership.go`.

- **`Steering`**: Typing a message while the agent is already working. The input is queued and fires as soon as the agent finishes the current turn. See `QueuePrompt()` in `internal/ui/model.go` and `drainAndMirrorQueuedPrompts` in `internal/agent/agent.go`.

- **`Stream`**: The channel-based flow of LLM response chunks (`text`, `tool_call`, `tool_result`, `done`) from the provider through the agent and into the UI. See `StreamChat` in `internal/llm/stream.go`.

- **`ToolContextManager`**: Manages the lifecycle of tool execution contexts — cancellation, timeouts, and result routing. Coordinates with the escape ladder for progressive cancellation. Defined in `internal/tools/tools.go`.

- **`Turn`**: A single user-to-agent exchange: user sends a prompt, the LLM streams a response and may execute one or more tool calls, then yields back to the user. See `sendMessage` in `internal/agent/agent.go`.

- **`Watermark`**: A position marker tracking how far the agent has read or processed within a stream or message history. Used during compaction to know what is safe to flush.

- **`bitchteaTool`**: The legacy generic tool adapter that wraps arbitrary tool definitions as `fantasy.Tool` for use in the fantasy type system. Falls back when no typed adapter exists. Defined in `internal/llm/tools.go`.
