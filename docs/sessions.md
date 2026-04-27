# 🦍 SESSIONS 🦍

This scroll documents the persistence of time in `bitchtea`. 

## 1. JSONL Format (`internal/session/session.go:17`)

Sessions are stored as append-only JSONL (JSON Lines) files in `~/.bitchtea/sessions/`. Each line is an independent `session.Entry` object.

### Fields
- **`ts`**: RFC3339 timestamp.
- **`role`**: `user`, `assistant`, `system`, or `tool`.
- **`content`**: The message text or tool output.
- **`context`**: IRC routing label (e.g., `#main`).
- **`bootstrap`**: Boolean. True if the entry was injected during startup (e.g., `CLAUDE.md`).
- **`tool_name`**: Name of the tool called.
- **`tool_args`**: Arguments passed to the tool.
- **`tool_call_id`**: Unique ID for the tool call (matching provider requirements).
- **`tool_calls`**: List of tool calls (for providers that support multiple in one turn).
- **`parent_id`**: ID of the previous message (enables branching/tree).
- **`branch`**: Label for the branch.
- **`id`**: Unique identifier for this entry (nanosecond timestamp).

## 2. Resume & List

- **Resume**: `bitchtea --resume [path]` loads a session by reading every line and reconstructing the `llm.Message` history via `MessagesFromEntries` (`session.go:327`).
- **List**: `bitchtea --sessions` (or `/ls`) lists all `.jsonl` files in the session directory, sorted newest first (`session.go:214`).

## 3. Fork & Tree

- **Fork**: `/fork` creates a new session file. It copies all entries from the current session up to the active message, allowing the user to explore a different path without mutating the original history (`session.go:130`).
- **Tree**: `/tree` provides a text representation of the session history, showing timestamps, roles, and truncated content summaries (`session.go:169`).

## 4. State Persistence

- **Checkpoint**: Autonomous-turn state is periodically saved to `.bitchtea_checkpoint.json` (`session.go:273`).
- **Focus**: The IRC context list and active focus are saved to `.bitchtea_focus.json` (`session.go:348`) to ensure your workspace looks the same when you return.

APE STRONK TOGETHER. 🦍💪🤝
