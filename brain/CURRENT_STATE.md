# Current State

- The user wants a local, file-based memory system inside the repo.
- The user prefers chat-first interaction over browser-based workflows.
- The user wants tentative notes written continuously while talking.
- The user wants later sessions to resume by reading the repo, not by trusting volatile session memory.
- The user is frustrated by excessive task spawning and Kanban overreach.
- The desired end state is small tasks later, after the conversation has clarified the vision.
- `brain/PROJECT_MAP.md` is now the main repo map for future restarts.
- The current worktree is dirty, but `internal/ui/commands.go` is clean against `HEAD`; the invite/kick handlers live in `internal/ui/invite.go`, and the earlier duplicate-functions note was stale repo memory.
- The `/set` command and rc-file startup behavior are aligned on the current tested paths.
- `main.go` startup wiring now goes through a shared startup-config path; rc `/set` semantics and CLI `--model` semantics both clear stale loaded profiles, and focused tests cover config loading plus resume/startup-command behavior.
- `auto-next` / `auto-idea` are a known risk area because the agent can continue automatically after a finished turn.
- `auto-next` / `auto-idea` follow-up flow and headless parity were tightened and the full Go test suite passes after the rc-startup follow-up work.
- Untracked scratch artifacts (`codex.diff`, `foo`, `thought.txt`, `HELP.TXT`, `internal/agent/agent.bak`, and the Kate swap file) were removed as safe junk; `.gitignore` now ignores `*.kate-swp`, and the remaining dirty state is tracked in-progress repo work.
