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
- The Anthropic nil-content crash path is now tightened in runtime code: message content is sanitized before alternation merge, nil and whitespace-only text blocks are stripped before Anthropic requests are built, empty Anthropic messages are dropped instead of being sent, multiple non-empty system messages are now preserved in order when building Anthropic requests, blank system-message segments are ignored, tool-call-only assistant messages are preserved, start-event-only `tool_use.input` is now preserved in streamed tool calls, Anthropic SSE parsing now handles multiline `data:` frames correctly, Anthropic thinking blocks now propagate through llm -> agent -> UI instead of being dropped, transcript persistence now intentionally omits transient thinking placeholder/chunk messages so stale `thinking...` text cannot leak into logs, session history still persists only agent/tool/user messages rather than UI thinking state, the client errors early if Anthropic would otherwise receive an empty non-system payload, and focused tests now cover the no-network empty-payload guard plus both mixed text/tool_use/tool_result request ordering and interleaved text/tool_use streaming event ordering.
- Untracked scratch artifacts (`codex.diff`, `foo`, `thought.txt`, `HELP.TXT`, `internal/agent/agent.bak`, and the Kate swap file) were removed as safe junk; `.gitignore` now ignores `*.kate-swp`, and the remaining dirty state is tracked in-progress repo work.
