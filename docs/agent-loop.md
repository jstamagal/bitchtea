# 🦍 THE BITCHTEA SCROLLS: THE AGENT LOOP

The meat of bitchtea is the `SendMessage` loop. It is designed to be autonomous, resilient, and noisy only when it matters.

## 🔄 THE SENDMESSAGE LOOP

Located in `internal/agent/agent.go:167`, the loop follows this path:

1. **Expansion**: User message is scanned for `@file` references and expanded (see `ExpandFileRefs`).
2. **Bootstrap**: History is initialized with:
   - System Prompt (Environment + Persona).
   - Project Context Files (Discovered in `internal/agent/context.go`).
   - Durable Memory (from working directory).
   - Persona Anchor (Synthetic user/assistant exchange to lock voice).
3. **Streaming**: `streamer.StreamChat` is called. It emits `llm.StreamEvent`s.
4. **Event Handling**:
   - **`text`**: Tokens are accumulated in a `strings.Builder` and sanitized for follow-up tokens.
   - **`tool_call`**: Tool execution is dispatched.
   - **`usage`**: Token counts are updated for cost tracking.
5. **Finalization**: The transcript is rebuilt and saved to `a.messages`.

## 🛠️ TOOL EXECUTION FLOW

Bitchtea tools are not just commands; they are capabilities.

1. **Request**: LLM decides a tool is needed (e.g., `bash` to run tests).
2. **Dispatch**: `agent` emits `tool_start` to the UI.
3. **Registry**: `internal/tools/Registry.Execute` matches the tool name.
4. **Action**:
   - `read`/`write`/`edit`: Files are touched via standard `os` calls.
   - `bash`: Executed via `exec.CommandContext` with a 30s timeout (`internal/tools/tools.go:343`).
   - `terminal`: Persistent PTYs managed by `terminalManager` for interactive sessions.
5. **Feedback**: Results are returned to the LLM. The LLM then continues thinking or finishes.

## ⏭️ AUTONOMOUS FOLLOW-UPS

Bitchtea doesn't wait for permission to be useful.

### The Mechanism (`MaybeQueueFollowUp` in `internal/agent/agent.go:509`):
If `AutoNextSteps` or `AutoNextIdea` is enabled:
- The agent appends a follow-up prompt after a turn finishes.
- **`AutoNextSteps`**: "What are the next steps? ... If everything is done, start with AUTONEXT_DONE."
- **`AutoNextIdea`**: "Pick the next highest-impact improvement... If nothing worthwhile left, start with AUTOIDEA_DONE."

### Sanitization:
The `followUpStreamSanitizer` (internal to `agent.go`) ensures these control tokens (`AUTONEXT_DONE`) are stripped from the TUI display so KING only sees the actual work.

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝
