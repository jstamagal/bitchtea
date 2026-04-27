# 🦍 BITCHTEA ARCHITECTURE 🦍

This scroll maps the deep vines of `bitchtea`. It is for those who wish to see how the canopy is built.

## 1. Package Map & Rationale

The project is strictly partitioned into `internal/` packages to maintain an acyclic dependency graph. This ensures the brain (`agent`) does not depend on the eyes (`ui`).

- **`main`**: The root. Handles CLI flags (`main.go:108`) and boots the Bubble Tea program (`main.go:85`).
- **`internal/ui`**: The presentation layer. Built on `charmbracelet/bubbletea`. It is a pure Model-View-Update engine.
- **`internal/agent`**: The orchestration engine. Manages the autonomous "fantasy" loop and tool-calling state (`internal/agent/agent.go`).
- **`internal/config`**: Global configuration, environment variables, and profiles (`internal/config/config.go`).
- **`internal/llm`**: [IN FLUX] A thin abstraction over LLM providers. Currently transitioning to a shim over `charm.land/fantasy`.
- **`internal/session`**: Persistence. Manages the JSONL append-only logs (`internal/session/session.go`).
- **`internal/memory`**: Tiered memory storage (Root, Channel, Daily).
- **`internal/tools`**: Registry of local capabilities (bash, read, write).
- **`internal/sound`**: Audio feedback (MP3 notifications).

## 2. Dependency Graph (Acyclic Law)

The sap must only flow one way. If the vines tangle (circular dependencies), the tree dies.

- `main` -> `config`, `session`, `ui` (`main.go:14-17`)
- `ui` -> `agent`, `config`, `llm`, `session`, `sound` (`internal/ui/model.go:16-20`)
- `agent` -> `config`, `llm`, `tools` (`internal/agent/agent.go:13-15`)
- `session` -> `llm` (`internal/session/session.go:13`)
- `tools` -> `llm` (`internal/tools/tools.go:15`)

**Why Acyclic?** Isolation. We can test the `agent` in a headless environment (`main.go:73`) without ever initializing the `ui`.

## 3. Runtime State Machine

The `agent` tracks its internal rhythm via the `State` type (`internal/agent/agent.go:19`):

- **`StateIdle` (0)**: Waiting for user command or tool results to settle.
- **`StateThinking` (1)**: The LLM is processing. Tokens are streaming.
- **`StateToolCall` (2)**: A local tool (e.g., `bash`) is executing.

## 4. Event Flow (Eyes & Brain)

The `agent` communicates with the `ui` via a channel of `agent.Event` structs (`internal/agent/agent.go:28`). 

1. `agent` emits an `Event` (e.g., Type: "text").
2. `ui` model receives this in its `Update` loop as an `agentEventMsg` (`internal/ui/model.go:28`).
3. `ui` routes the content into the `viewport` and triggers a re-render.

APE STRONK TOGETHER. 🦍💪🤝
