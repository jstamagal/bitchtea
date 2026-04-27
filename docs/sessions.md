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

## 🧱 TECHNICAL DEEP-DIVE: THE RECORD

### 🧬 JSONL APPEND MECHANICS
- **Executor**: `Session.Append` in `internal/session/session.go:88`.
- **Logic**: 
  - Generates a unique `ID` via `unixNano` if missing.
  - Automatically links to the previous entry via `ParentID` (`line 94`), creating a causal chain.
  - Opens the file with `O_APPEND` for thread-safe, non-destructive commits.
- **Bytes Moving**: Marshals the `Entry` struct to JSON, appends a newline, and flushes to the `.jsonl` file.

### 🌳 THE TREE & FORKING
- **Tree Rationale**: While the storage is linear JSONL, the `ParentID` allows for future-proofing branching. 
- **Forking**: `Session.Fork` (`line 102`) snapshots the current session up to a specific `ID`, writes them to a new `.jsonl` file with a timestamped suffix, and switches the TUI to this new timeline.

### 📍 CHECKPOINT INTERNALS
- **Storage**: `internal/session/session.go:198` -> `SaveCheckpoint`.
- **Verbatim JSON**:
  ```json
  {
    "turn_count": 42,
    "tool_calls": {
      "bash": 12,
      "read": 5
    },
    "model": "gpt-4o",
    "timestamp": "2026-04-27T12:00:00Z"
  }
  ```
- **Churn**: This captures the "ephemeral" stats of the autonomous turn loop, ensuring counters like `TurnCount` survive a TUI restart without polluting the durable `MEMORY.md`.

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝

## 🦴 UNDER THE HOOD: SESSION INTERNALS

### 🧬 THE JSONL CHURN
Every interaction is persisted via `Session.Append` in `internal/session/session.go`.

- **Input**: An `Entry` struct containing role, content, and metadata.
- **The Append Step (line 88)**: 
  - Ststamps the entry with `time.Now()`.
  - Assigns a unique `ID` (nanosecond precision string).
  - **Tree Logic (line 94)**: Sets `entry.ParentID` to the `ID` of the previous entry in the `s.Entries` slice, creating a linear causality chain that can be reconstructed as a tree.
- **The Bytes Move (line 103)**: Opens the `.jsonl` file with `os.O_APPEND`. Marshals the entry to a single JSON line and flushes it to disk.
- **Verbatim Output**: A raw JSON string followed by `\n`.

### 📍 CHECKPOINT.JSON
During autonomous turn sequences, bitchtea writes to a hidden `.bitchtea_checkpoint.json` in the session directory.

- **Data Struct**:
  ```go
  type Checkpoint struct {
      TurnCount int            // Total turns in session
      ToolCalls map[string]int // Count of every tool used
      Model     string         // The brain currently in use
      Timestamp time.Time      // Last update
  }
  ```
- **Logic**: This prevents "amnesia" when restarting the TUI. When a session is resumed, the checkpoint values are loaded back into the `Agent` struct.

