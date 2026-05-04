# 🦍 BITCHTEA: COMMAND REFERENCE

Slash commands control the TUI. Use them to bend the session to your will.

## 🏛️ CONTEXT & NAVIGATION

- **`/join <#channel>`**: Switch focus to a channel. Creates it if it doesn't exist.
- **`/part [#channel]`**: Leave the current or named context.
- **`/query <nick>`**: Open a direct persistent conversation with a persona/nick.
- **`/msg <nick> <text>`**: Send a one-shot message to a nick without changing focus.
- **`/channels`** (or **`/ch`**): List all open contexts and their active members.

## 🧠 MODEL & CONFIG

- **`/set <key> <value>`**: Single source of truth for connection settings. Keys: `provider`, `model`, `baseurl`, `apikey`, `service`, `nick`, `profile`, `sound`, `auto-next`, `auto-idea`. Examples: `/set provider anthropic`, `/set model gpt-4o`, `/set apikey sk-...`. See "Provider vs Service" below.
- **`/profile [load|save|show|delete] <name>`**: Manage saved connection profiles. `show` previews a profile (provider, service, model, baseurl, masked key) without loading it. Bare `/profile <name>` (no subcommand) loads the named profile.
- **`/models`**: Open a fuzzy-find picker over the catwalk model catalog for the active `service`. Type to filter (case-insensitive substring), `↑/↓` move, `PgUp/PgDn` page, `Enter` selects (routes through `agent.SetModel` and clears the loaded profile tag, mirroring `/set model`), `Esc` cancels. Reads `~/.bitchtea/catalog/providers.json` first and falls back to the catwalk-embedded snapshot offline. If the active service has no catalog entry the command surfaces the available service IDs as a hint.

### Provider vs Service

`provider` and `service` are two distinct fields on every config and profile:

- **`provider`** is the *wire format* the client speaks. Today it is always one of `openai` or `anthropic`. It controls request body shape, auth header style, and stream parsing. Setting `/set provider` or `/set baseurl` clobbers `service` to `custom`, because the per-service behavior gates can no longer trust the previous identity once you've redirected the transport.
- **`service`** is the *upstream identity*: which actual API you are talking to (`openai`, `anthropic`, `ollama`, `openrouter`, `zai-openai`, `zai-anthropic`, `vercel`, ... or `custom`). It exists so per-service quirks — Anthropic prompt caching, OpenRouter reasoning forwarding, Ollama empty-key allowance, etc. — can be gated on identity rather than fragile URL sniffing. Built-in profiles populate it; legacy profiles missing the field are derived lazily on load (by name, then by base-URL host, then `custom`). `/set service <value>` accepts any string verbatim and is treated as a metadata relabel — your active profile name is preserved.

Concrete example: the `openrouter` built-in is `provider=openai service=openrouter` because OpenRouter speaks the OpenAI wire format but is not OpenAI; behavior gates that fire only for native OpenAI must check `service`, not `provider`. Full migration notes live in [`phase-9-service-identity.md`](phase-9-service-identity.md).

## 💾 SESSION & MEMORY

- **`/sessions`** (or **`/ls`**): List saved sessions in the session directory.
- **`/tree`**: Show the branch structure of the current session.
- **`/fork`**: Create a new session file from the current state.
- **`/compact`**: Summarize history to save tokens and durable knowledge.
- **`/memory`**: View the contents of `MEMORY.md` and scoped `HOT.md`.

## 🛠️ UTILITIES

- **`/copy [n]`**: Copy the last (or nth) assistant response to the clipboard.
- **`/tokens`**: Show estimated token usage and session cost.
- **`/debug [on|off]`**: Toggle verbose HTTP logging for API calls.
- **`/mp3 [cmd]`**: Control the built-in MP3 player (rescan, play, next, prev).
- **`/clear`**: Clear the scrollback buffer from the TUI.
- **`/help`** (or **`/h`**): Show the quick help menu.
- **`/quit`** (or **`/q`**): Exit bitchtea.

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝

## 🧱 TECHNICAL DEEP-DIVE: THE REPL HANDSHAKE

### 🤝 REPL-TO-AGENT ORCHESTRATION
When a command or message is committed (Enter), the model undergoes a transition:
1. **Input Capture**: `internal/ui/model.go` captures `tea.KeyMsg` for `enter`.
2. **Dispatch**:
   - **Slash Commands**: Routed via `handleCommand` to handlers in `internal/ui/commands.go`.
   - **User Messages**: Routed via `m.sendToAgent(input)`.
3. **The Turn Context**: `startAgentTurn` (`internal/ui/model_turn_test.go` / `model.go`) creates a fresh `context.WithCancel` and an `agent.Event` channel.
4. **Asynchronous Execution**: `go m.agent.SendMessage(ctx, input, ch)` fires.
5. **Event Loop**: The TUI stays responsive, processing `agentEventMsg` (wrapping `agent.Event`) to update spinners and stream text until a `done` event arrives.

### 🌀 COMMAND TRACES

#### `/compact` (The Context Shrinker)
- **Viewport**: Prints `Compacted: ~X -> ~Y tokens`.
- **Under the Hood**: `agent.Compact(ctx)` (`internal/agent/agent.go`) runs three phases in this order:
  1. **Memory Extraction (first)**: Calls `flushCompactedMessagesToDailyMemory` on `a.messages[1:end]` (everything except the system prompt and the last 4 messages). That sends a hidden "Extract durable memory from this conversation slice before it is compacted" prompt to the LLM via `streamer.StreamChat`. If the response is non-empty and not exactly `NONE`, it is appended to the current scope's daily markdown log via `AppendScopedDailyMemory`.
  2. **Summary (second)**: Builds a separate "Summarize the following conversation concisely…" prompt over the same `a.messages[1:end]` slice and streams it through `streamer.StreamChat`.
  3. **Rewrite (third)**: `a.messages` is truncated. Index 0 (System) remains, followed by a new user message `[Previous conversation summary]:\n…`, then an assistant ack (`Got it, I have the context from the summary.`), then the last 4 messages of the original history appended back on.

#### `/fork` (Timeline Splitting)
- **Viewport**: Prints `Forked to new session: [path]`.
- **Under the Hood**:
  1. Identifies the `lastID` of the current session entries.
  2. Calls `session.Fork(lastID)` (`internal/session/session.go:102`).
  3. **Atomic Copy**: A new `.jsonl` file is created with a timestamped suffix. All entries up to `lastID` are marshaled and written in a single pass.
  4. **State Swap**: The `Model.session` pointer is updated to the new file, making all subsequent `Append` calls target the fork.

#### `/msg` (Targeted Routing)
- **Viewport**: Prints `→nick: text`.
- **Agent Handshake**: Sends `[to:nick] text` to `agent.SendMessage`.
- **Logic**: This bypasses the persistent `/query` focus but tells the agent exactly who is being addressed via the `[to:...]` prefix, which the agent's system prompt (Persona) is trained to understand as a routing hint.

#### `/join <#channel>`
- **Immediate Handshake**: `handleJoinCommand` (`internal/ui/commands.go`) calls `m.focus.SetFocus(ctx)` and `m.focus.Save(m.config.SessionDir)`. Nothing else fires synchronously — the agent is not touched.
- **Deferred (next agent turn)**: `startAgentTurn` (`internal/ui/model.go`) reads `m.focus.Active()`, then calls `m.agent.InitContext`, `m.agent.SetContext`, and `m.agent.SetScope(ircContextToMemoryScope(...))`. Any scoped `HOT.md` injection happens through that `SetScope` path on the *next* user message, not at `/join` time.
- **Viewport Output**: `Joined #channel`.

#### `/query <persona>`
- **Immediate Handshake**: `handleQueryCommand` (`internal/ui/commands.go`) calls `m.focus.SetFocus(Direct(persona))` and `m.focus.Save`. The agent's context and scope are unchanged at this point.
- **Deferred (next agent turn)**: Same lazy swap as `/join` — `startAgentTurn` picks up the new active context and calls `InitContext` / `SetContext` / `SetScope` then.
- **Viewport Output**: `Query open: @persona`.

#### `/part [target]`
- **Immediate Handshake**: `handlePartCommand` (`internal/ui/commands.go`) calls `m.focus.Remove(target)` (defaulting to the active context) and persists via `m.focus.Save`. Refuses to part the last remaining context. The agent is not notified inline.
- **Deferred (next agent turn)**: If the part changed the active focus, the next `startAgentTurn` is what actually swaps `agent.SetContext` + `agent.SetScope` to match.
- **Viewport Output**: `Parted <label> — now in <new active label>`.

### 📜 COMMAND VERBATIM MAP

| Command | Viewport Output (sysMsg) | Agent Handshake |
| :--- | :--- | :--- |
| `/set model <m>` | `*** Model switched to: <m>` | `a.SetModel(<m>)` (Immediate config sync) |
| `/tokens` | `~X tokens \| $Y \| Z msgs` | None (Local stat read) |
| `/join #ch` | `Joined #ch` | None inline — `focus.SetFocus` + `focus.Save`; `agent.SetContext`/`SetScope` deferred to next `startAgentTurn` |
| `/query nick` | `Query open: @nick` | None inline — `focus.SetFocus(Direct("nick"))` + `focus.Save`; agent swap deferred to next `startAgentTurn` |
| `/part [target]` | `Parted <label> — now in <new>` | None inline — `focus.Remove` + `focus.Save`; agent swap deferred to next `startAgentTurn` |
| `/debug on` | `Debug mode: ON` | `a.SetDebugHook(fn)` (Wraps HTTP transport) |
| `/set k v` | `K set to: V` | Syncs `a.SetProvider/Model/etc` if profile-related |

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝
