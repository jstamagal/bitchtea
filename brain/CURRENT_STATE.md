# Current State

- The user wants a local, file-based memory system inside the repo.
- The user prefers chat-first interaction over browser-based workflows.
- The user wants tentative notes written continuously while talking.
- The user wants later sessions to resume by reading the repo, not by trusting volatile session memory.
- The user is frustrated by excessive task spawning and Kanban overreach.
- The desired end state is small tasks later, after the conversation has clarified the vision.
- `brain/PROJECT_MAP.md` is now the main repo map for future restarts.
- The current worktree is dirty and `internal/ui/commands.go` still has an unstaged local edit that duplicates `handleInviteCommand` and `handleKickCommand`.
- The `/set` command work is still part of the active surface area and needs to stay aligned with `internal/config/rc.go`.
- `main.go` currently handles CLI flags, headless mode, resume, and TUI startup; rc-file startup wiring still needs verification against the config helpers.
- `auto-next` / `auto-idea` are a known risk area because the agent can continue automatically after a finished turn.
