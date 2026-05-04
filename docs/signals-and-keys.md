# bitchtea Signals and Key Bindings

This document provides a comprehensive reference for the signal handling and key bindings implemented in the `bitchtea` TUI.

## Signal Handling

`bitchtea` handles OS signals gracefully to ensure sessions are saved and terminal states are restored.

| Signal | Logic |
| :--- | :--- |
| `SIGINT` (Ctrl+C) | Handled by a "graduation ladder" in the TUI (see below). If sent from the OS (e.g., `kill -2`), it cancels the active turn and shuts down. |
| `SIGTERM` | Cancels any active agent turns, finishes transcript logging, and quits gracefully. |
| `SIGWINCH` | Handled automatically by the `tea.WindowSizeMsg` to recalculate the layout (topbar, viewport, statusbar, and panels). |

## Key Bindings (TUI)

### Navigation & Viewport
- **`PgUp` / `PgDn`**: Scroll the chat history viewport.
- **`Up` / `Down`**: 
    - Navigate input history if the cursor is at the top/bottom of the textarea.
    - If the input is empty and there are queued messages, `Up` will "unqueue" the last message back into the input area.
- **`Ctrl+P` / `Ctrl+N`**: Explicitly navigate history (Previous/Next) regardless of cursor position.
- **`Mouse Wheel`**: Scroll the viewport (delta 3 lines).

### Agent Interaction
- **`Enter`**: Submit the current input. If the agent is already working, the message is **queued** (steering).
- **`Esc`**: 
    - **Stage 0**: Close active panels (Tool panel, MP3 panel).
    - **Stage 1**: Cancel the **active tool call** only (leaving the turn alive for the agent to recover).
    - **Stage 2**: Cancel the **entire turn** (within a 1.5s window of the first Esc).
    - **Queue Clearing**: If armed after a cancel, another Esc will clear any queued messages.
- **`Ctrl+C`**:
    - **Stage 1**: Cancel the active turn (or prompt for confirmation if idle).
    - **Stage 2**: Clear all queued messages.
    - **Stage 3**: Quit the application (within a 3s window).
- **`Ctrl+T`**: Toggle the Tool Panel (shows active tool stats, token usage, and elapsed time).

### MP3 Controller (When Panel Visible)
*Note: Only active when input area is empty.*
- **`Space`**: Play/Pause.
- **`Left` / `j`**: Previous track.
- **`Right` / `k`**: Next track.

### Model Picker (`/models` overlay)
- **`Up` / `Down` / `k` / `j`**: Navigate model list.
- **`Enter`**: Select and switch to the highlighted model.
- **`Esc` / `Ctrl+C`**: Close the picker without changing the model.
- **Any other keys**: Fuzzy search the model list.

## Internal Message Loop

The UI communicates with the Agent via several custom Bubble Tea messages:

- **`SignalMsg`**: Bridges OS signals into the `Update` loop.
- **`agentEventMsg`**: Streams events (`text`, `tool_start`, `tool_result`, etc.) from the background agent goroutine into the viewport.
- **`agentDoneMsg`**: Signals the completion of an agent turn, triggering session saves and processing of queued messages.
- **`tea.SuspendMsg`**: Triggered by `Ctrl+Z`, allows the TUI to suspend and return to the shell.
