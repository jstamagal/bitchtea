# HANDOFF — bitchtea fantasy migration

Read this first. You are a fresh Claude with clean context. Previous Claude got compacted too many times and was told not to touch code.

## What's already done

- `internal/llm/` was deleted earlier this session. Build is red.
- `MIGRATION_PLAN.md` (v3, 883 lines) — the canonical plan. Verified against fantasy v0.17.1 source in `/home/admin/go/pkg/mod/charm.land/fantasy@v0.17.1/`. Already passed two rounds of codex review.
- `WORK_MAP.md` — dispatch map. 2 git worktrees, 4 fanout windows, agent assignments per chunk.
- `.subagents/precheck.sh` — sanity check before firing anything.
- `.subagents/migrate-window-1.sh` — Window 1 fanout (background, non-blocking). Fires codex + gemini in parallel.

## What you do

1. **Read these three files first, in order:**
   - `/home/admin/bitchtea/MIGRATION_PLAN.md`
   - `/home/admin/bitchtea/WORK_MAP.md`
   - `/home/admin/bitchtea/CLAUDE.md`

2. **(Optional but recommended) — fresh codex review of v3.**
   v2 was reviewed; v3 added the v2 fixes but hasn't been independently reviewed. Fire both default profiles in parallel, background:
   ```bash
   ps aux | grep -E "codex|acpx" | grep -v grep   # should be empty-ish
   # If clean, fire both in parallel as background bash tasks:
   CODEX_HOME=/home/admin/.codex-jstamagal codex exec --skip-git-repo-check "$(cat /home/admin/bitchtea/.subagents/v3-review-prompt.txt)"
   CODEX_HOME=/home/admin/.codex-foreigner codex exec --skip-git-repo-check "$(cat /home/admin/bitchtea/.subagents/v3-review-prompt.txt)"
   ```
   Foreigner may still be quota-dead. Don't restart it if it dies.

3. **Run the precheck:**
   ```bash
   bash /home/admin/bitchtea/.subagents/precheck.sh
   ```
   It verifies: clean ps aux, fantasy module present, codex profiles reachable, working tree state, plan files exist.

4. **Set up worktrees** (per WORK_MAP):
   ```bash
   git -C /home/admin/bitchtea worktree add ../bitchtea-event-split
   git -C /home/admin/bitchtea worktree add ../bitchtea-llm-impl
   ```
   You work in `bitchtea-llm-impl` for the new `internal/llm/` files. Event-split worktree is for moving `agent.Event` + `agent.State` into a new `internal/agent/event` subpackage.

5. **Fire Window 1** (parallel fanout, non-blocking):
   ```bash
   bash /home/admin/bitchtea/.subagents/migrate-window-1.sh
   ```
   This kicks off codex-jstamagal on `debug.go` and gemini-3.1-pro on `cost.go` in the background. While they run, **you** (Claude) write `types.go` first (other files depend on it), then `providers.go`, then `client.go` in `bitchtea-llm-impl/internal/llm/`. Plan §D2/D4/D5/D7 has the exact contracts.

6. **Window 1 gate:** `go build ./...` in `bitchtea-llm-impl` (will still fail until stream.go lands — that's window 2). But each individual file should compile to its own definitions cleanly.

7. **Window 2:** after Window 1 lands AND event-split worktree merges, fire `migrate-window-2.sh` (TODO — write it after window 1 lands; it depends on event package paths). That window does `convert.go`, `tools.go`, then Claude writes `stream.go` last.

8. **Window 3:** rewrite `agent.go sendMessage` (Claude — high care, persona/sanitizer/follow-up state must survive), recover deleted llm tests from git history, write cache-invalidation tests. Parallelize where independent.

9. **Window 4:** `/codex:review` working tree, doc cross-check, manual smoke across 4 profiles (`OPENAI_API_KEY`, `OPENROUTER_API_KEY`, ollama local, zai-openai).

## Verification gates (every window, all four must pass)

```bash
go build -o bitchtea .
go build ./...
go test ./...
go test -race ./...
go vet ./...
```

## Hard rules

- **Don't fire codex profiles other than `.codex-jstamagal` or `.codex-foreigner`** unless TJ explicitly tells you a different profile for a specific task. `.codexinfinity` is NOT a default — TJ has corrected this multiple times.
- **Always `ps aux | grep -E "codex|acpx" | grep -v grep` before firing codex.** Don't stack 20 copies.
- **Subagents are NONBLOCKING.** Fire `run_in_background: true` and do other work while they run. Don't sit idle.
- **If a codex review is running, don't kill it.** TJ: "let codex run dont touch it if u touch it i scream."
- **Don't downgrade the daemon's `compactModel`** — see comment in `internal/daemon/daemon.go`. (Daemon is in `_attic/` for now; this matters when it comes back.)
- **Append-only session JSONL** at `~/.bitchtea/sessions/`. Don't rewrite the format.
- **Acyclic deps:** main → config/session/ui; ui → agent/config/session/sound; agent → config/llm/tools/memory. Never add an upward edge.
- **Notify TJ before final smoke test:** `notify-send "bitchtea ready for smoke"`. He explicitly asked for this.

## Critical details from plan v3 (so you don't have to grep)

- `fantasy.AgentTool.Run` returns `fantasy.ToolResponse`, NOT a Go error for normal tool failures. Returning a Go `error` aborts the whole stream. Use `fantasy.NewTextErrorResponse(content)` for "this tool call failed but keep going."
- Transcript built by walking `result.Steps[].Messages` AFTER `Stream` returns. Do NOT mutate `a.messages` from inside a tool's `Run` — race with fantasy's tool dispatcher goroutine.
- Anthropic `DefaultURL = "https://api.anthropic.com"` (no `/v1`!). zai-anthropic config has `/v1` baked in — watch for double-prefix.
- Provider routing via `net/url` parse + comparison against `openai.DefaultURL`/`openrouter.DefaultURL`/`vercel.DefaultURL`/`anthropic.DefaultURL` constants — NOT substring match.
- Cache invalidation: `Set*` methods take a mutex, nil out cached `provider` and `model`. `SetDebugHook` only nils debugHook.
- `AgentStreamCall.Prompt` is `string`. Prior messages go in `Messages []fantasy.Message`.
- Stop conditions: `fantasy.WithStopConditions(fantasy.StepCountIs(n))`. There is no `WithMaxSteps`.
- `ToolInfo`: `Name, Description, Parameters map[string]any, Required []string, Parallel bool`. NOT `InputSchema`.
- Event-split: `internal/agent/event` subpackage holds `Event` + `State`. `internal/agent` keeps `type Event = event.Event` aliases for gradual migration.

## Files you should NOT change

- `internal/daemon/*` — archived in `_attic/`, out of scope.
- `internal/tools/tools.go` — leave alone tonight; the new `bitchteaTool` wrapper adapts its existing output.
- Session JSONL format — append-only, migrate deliberately if you ever need to.

## When in doubt

- Plan v3 is the source of truth. If your read of the code disagrees, plan wins (it was reviewed twice). If you find a real plan bug, update the plan file before coding around it.
- TJ has TBI. Half-size English. No hedging. No "shall we?" — just act. Push back when he's wrong.
- TJ is on Claude Max 5 only. Save Claude tokens by delegating: codex=review/tests, gemini=docs, gemini-flash-lite=inventory, Claude=code+orchestration.
