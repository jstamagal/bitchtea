# 🦍 THE AGENT LOOP 🦍

This scroll describes the "meat" of `bitchtea`: the autonomous turn-taking cycle.

## 1. Entry Points

- **`SendMessage`**: Processes a raw user message (`internal/agent/agent.go:182`).
- **`SendFollowUp`**: Continues a chain after a tool result or auto-prompt (`internal/agent/agent.go:187`).

Both converge on the private **`sendMessage`** loop (`internal/agent/agent.go:195`).

## 2. The Autonomous Loop (`for {}`)

The `sendMessage` function contains an infinite loop that only breaks when the LLM produces a final response without tool calls.

1. **Thinking Phase**: 
   - State becomes `StateThinking`.
   - `a.streamer.StreamChat` is called in a goroutine (`internal/agent/agent.go:213`).
   - Tokens stream back via the `streamEvents` channel.

2. **Parsing Phase**:
   - The agent accumulates text and tool calls.
   - It looks for "finality tokens" (e.g., `AUTONEXT_DONE`).

3. **Tool Execution Phase**:
   - If the LLM requested tools, state becomes `StateToolCall` (`internal/agent/agent.go:269`).
   - The agent iterates over `toolCalls` and calls `a.tools.Execute` (`internal/agent/agent.go:276`).
   - Tool results are added as messages with the `tool` role.

4. **Continuation Phase**:
   - The loop restarts. The LLM is given the tool results and asked to proceed.
   - If no more tools are called, the loop exits.

## 3. Tool Contexts & Cancellation

- Every tool execution uses a context derived from the turn context.
- If the user presses `Esc` (Stage 1), the specific tool context is canceled, but the turn context remains active.
- If the turn is canceled (`Ctrl+C`), the entire loop aborts and state returns to `StateIdle`.

## 4. Expansion & Injection

Before the loop starts, the agent performs:
- **`ExpandFileRefs`**: Converts `@path/to/file` into the file's content (`internal/agent/context.go:130`).
- **Persona Injection**: Ensures the agent's core instructions are anchored at the top of the history.

APE STRONK TOGETHER. 🦍💪🤝
