# Work map — fantasy migration parallelization

Status: **draft, pending v2 plan approval.** Once codex signs off the plan, this is the dispatch map. Don't fire the swarm without TJ's go-ahead.

## Dependency graph

```
event-split  ─┐
              ├─→ llm/types.go ─┬─→ providers.go ─┐
              │                  │                 │
              │                  ├─→ cost.go       │
              │                  ├─→ errors.go     │
              │                  ├─→ debug.go      │
              │                  ├─→ convert.go    │
              └──────────────────┴─→ tools.go      ├─→ stream.go ─→ client.go ─→ agent.go rewrite ─→ tests + smoke
                                                   │
                                            (event pkg required)
```

## Two git worktrees, work in parallel

- **WT-A: `event-split`** — move `agent.Event` + `agent.State` to a new `internal/agent/event` subpackage. Mechanical but touches every caller in `internal/ui`, `agent`, the four agent test files, `main.go`. Build green when done.
- **WT-B: `llm-impl`** — all new `internal/llm/*.go`. Pure new files, doesn't conflict with main until merged.

Worktrees can run simultaneously. WT-A merges first (other code references the new event package). Then WT-B merges. Then agent.go rewrite is in main.

## Agent assignment per chunk

Per TJ's swarm formula (Claude=code/orchestration, codex=review/nitpicking/verification/tests, gemini-pro=design synthesis, gemini-flash=mechanics, gemini-flash-lite=inventory).

| Chunk | Worktree | Agent | Why this agent |
|---|---|---|---|
| `event` package split | WT-A | **Claude** | High blast radius (touches every Event consumer); needs careful import + test update |
| `types.go` | WT-B | **Claude** | Public surface contract — can't get the field names/JSON tags wrong |
| `providers.go` | WT-B | **Claude** | D4 routing is the riskiest single decision; baseURL detection logic |
| `client.go` | WT-B | **Claude** | Owns D5 cache invalidation correctness |
| `cost.go` | WT-B | **gemini-3.1-pro** (acpx) | Self-contained catwalk lookup; pro handles the synthesis cleanly |
| `debug.go` | WT-B | **codex** (jstamagal) | Standard RoundTripper pattern; codex writes ok code, Claude reviews |
| `errors.go` | WT-B | **codex** (foreigner) | Mechanical port from old `ErrorHint` + `errors.As(*fantasy.ProviderError)` |
| `convert.go` | WT-B | **codex** (jstamagal — second job after debug.go) | Mechanical part-translation, lots of switch cases |
| `tools.go` | WT-B | **codex** (foreigner — second job after errors.go) | `fantasy.AgentTool` wrapper interface; small + bounded |
| `stream.go` | WT-B | **Claude** | Hardest piece: fantasy callbacks → bitchtea event channel. Codex reviews after. |
| Rewrite `agent.go sendMessage` | main | **Claude** | Persona + `@file` + sanitizer + follow-up state must survive — needs human-grade care |
| Port deleted llm tests | main | **gemini-3.1-flash** (acpx) | Recover from git history, paste back, light signature fixup |
| Cache-invalidation tests (D5) | main | **codex** | Vouching/verification is codex's strength |
| Final code review | main | **`/codex:review`** | Working-tree review pass |
| Doc cross-check | main | Claude + spot-checks | Drift = bug |

## Fanout windows

Two parallel windows where multiple agents run simultaneously:

**Window 1 (after `types.go` lands in WT-B):**
- Claude: `providers.go` → `client.go` (sequential, both Claude)
- codex-jstamagal: `debug.go`
- codex-foreigner: `errors.go`
- gemini-3.1-pro: `cost.go`

→ 4 agents in flight. WT-A's event split runs in parallel the entire time.

**Window 2 (after Window 1 + WT-A merge):**
- codex-jstamagal: `convert.go`
- codex-foreigner: `tools.go` (needs event package from WT-A)
- Claude: holds for results, then writes `stream.go` (needs convert + tools + client)

→ 2 codex agents in flight, Claude integrates after.

**Window 3 (after llm/ complete):**
- Claude: rewrite `agent.go sendMessage`
- gemini-3.1-flash: recover deleted llm tests from git history (parallel, mostly mechanical)
- codex: cache invalidation tests (parallel)

→ 3 agents in flight.

**Window 4 (verification):**
- `/codex:review` on working tree
- Claude: doc cross-check
- Manual smoke across 4 profiles (Claude, can't be parallelized — single binary, single TTY)

## Pre-launch checklist

Before firing any swarm:
1. Plan v2 approved by codex (running now in background)
2. TJ notified via `notify-send` (per his explicit instruction — "notify-send im headin to the hentai workspace")
3. `ps aux` clean of stale codex/acpx jobs
4. Both `.codex-jstamagal` and `.codex-foreigner` confirmed ready (not mid-job)
5. `acpx codex sessions list` shows healthy state

Then launch Window 1.

## Open questions for TJ

- OK with two git worktrees? `git worktree add ../bitchtea-event-split` + `../bitchtea-llm-impl`. Or do you want everything in main with sequential merges?
- Approve the agent assignments above? Anything you want pulled off codex/gemini and onto Claude (or vice versa)?
- Manual smoke: do you have OPENROUTER_API_KEY and a local ollama running tonight, or is OpenAI the only profile we'll smoke-test?
