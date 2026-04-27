# 🦍 THE BITCHTEA SCROLLS: ARCHITECTURE

Bitchtea is a terminal-native agent harness built for power and speed. It follows the **Green Dark** philosophy: minimal friction, maximal action.

## 🗺️ PACKAGE MAP

### 🏛️ Core Brain & Orchestration
- **`main.go`**: The anchor. Handles CLI flags, profile loading, and boots either the TUI or Headless mode.
- **`internal/agent`**: The soul. Manages conversation history (`llm.Message`), system prompts, and the autonomous loop.
  - `agent.go`: The `Agent` struct and `SendMessage` orchestration.
  - `context.go`: Discovers project-specific context files (`AGENTS.md`, `ARCHITECTURE.md`).

### 🖥️ User Interface
- **`internal/ui`**: The body. A [Bubbletea](https://github.com/charmbracelet/bubbletea) TUI.
  - `model.go`: Top-level TUI state.
  - `commands.go`: Slash-command handlers (`/model`, `/compact`, etc.).
  - `render.go`: Styled viewport rendering.

### 🛠️ Capabilities & Infrastructure
- **`internal/llm`**: The connection. Wraps the `fantasy` library to talk to OpenAI, Anthropic, etc.
- **`internal/tools`**: The hands. Implementation of file manipulation, bash execution, and persistent terminals.
- **`internal/session`**: The memory bank. Persistence of chat history as JSONL.
- **`internal/memory`**: The long-term archive. Manages `MEMORY.md` and daily memory logs.
- **`internal/config`**: The settings. Global defaults and profile management.

## 📐 DEPENDENCY GRAPH RATIONALE

\`\`\`mermaid
graph TD
    main --> ui
    main --> agent
    ui --> agent
    ui --> session
    agent --> llm
    agent --> tools
    tools --> memory
    agent --> config
\`\`\`

1. **Separation of Concerns**: The \`agent\` is agnostic of the TUI. It communicates via channels (\`agent.Event\`), allowing it to run in \`headless\` mode (see \`runHeadless\` in \`main.go:210\`).
2. **Persistence Layer**: \`session\` and \`memory\` are decoupled. \`session\` tracks the immediate conversation; \`memory\` tracks durable facts across sessions.
3. **Lazy Tooling**: \`tools.Registry\` is initialized by the \`agent\` but tools are only executed when the LLM requests them.

## 🌀 RUNTIME STATE MACHINE

The system operates in three primary states defined in \`internal/agent/agent.go:218\`:

1. **\`StateIdle\`**: Waiting for KING (user) input.
2. **\`StateThinking\`**: LLM is processing the prompt. TUI shows a spinner and "thinking..." placeholder.
3. **\`StateToolCall\`**: A tool is executing. TUI displays the tool name and arguments.

### Turn Lifecycle:
- **\`turnStateIdle\`**: Default state.
- **\`turnStateCompleted\`**: Successful response received.
- **\`turnStateErrored\`**: API or Tool failure.
- **\`turnStateCanceled\`**: User hit \`Ctrl+C\`.

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝
