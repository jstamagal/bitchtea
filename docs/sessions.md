# 🦍 THE BITCHTEA SCROLLS: SESSIONS

Bitchtea never forgets a turn. Sessions are the immutable record of the Green Dark.

## 📄 JSONL FORMAT

Every session is a `.jsonl` file in the session directory. Each line is a self-contained JSON `Entry`.

### 🧬 Entry Fields (`internal/session/session.go:16`)
- **`ts`**: RFC3339 timestamp.
- **`role`**: `user`, `assistant`, `system`, or `tool`.
- **`content`**: The message text or tool output.
- **`context`**: The IRC routing label (e.g., `#main`, `buddy`).
- **`bootstrap`**: `true` if injected during startup (hidden from TUI transcript).
- **`tool_calls`**: List of requested tool metadata (for assistant role).
- **`tool_call_id`**: Mapping response to request (for tool role).

## 🌳 RESUME, FORK, & TREE

### 🔄 Resume
When booting with `--resume` or `-r`, bitchtea loads the `latest` or specified JSONL. It replays `DisplayEntries` (skipping bootstrap) into the TUI and restores the full `Messages` into the agent's history.

### 🍴 Fork (`internal/session/session.go:102`)
The `/fork` command clones the current session up to the last message. This creates a new timeline, allowing for experimentation without polluting the original record.

### 🌲 Tree (`internal/session/session.go:138`)
The `/tree` command renders a visual path of the session. It tracks parent-child relationships via `id` and `parent_id` fields, though the current TUI primarily shows a linear view of the active branch.

## 📍 CHECKPOINTS (`internal/session/session.go:198`)
A hidden `.bitchtea_checkpoint.json` tracks turn counts and tool usage statistics. This is used for session resumes to ensure counters don't reset to zero.

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝
