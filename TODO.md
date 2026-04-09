# TODO

## Exact open state
- `bitchtea-7an` — IRC-style channel and query memory architecture
- `bitchtea-42f` — Tag session events with context identity and persist active joins
- `bitchtea-nyd` — Add channel and query context model plus IRC-style routing commands

## Exact closed state

### Reliability / plumbing done
- `bitchtea-130` — retry with backoff to Anthropic client
- `bitchtea-dhu` — graceful Ctrl+C / Ctrl+Z handling
- `bitchtea-j0m` — agent reliability fixes
- `bitchtea-nno` — session robustness fixes
- `bitchtea-rv9` — turn completion / checkpointing rewrite
- `bitchtea-4o4` — HTTP transport and timeout hardening
- `bitchtea-8ml` — real token usage into cost tracking

### Memory / transcript / context plumbing done
- `bitchtea-0g9` — silent pre-compaction memory flush
- `bitchtea-30e` — memory recall and search
- `bitchtea-8os` — phase 1 markdown memory layout
- `bitchtea-bpl` — background activity behavior
- `bitchtea-ty5` — hide bootstrap messages from visible transcript
- `bitchtea-odr` — session fork/tree tests
- `bitchtea-l2s` — compact regression tests
- `bitchtea-7px` — replay harness

### UI / command / docs work done
- `bitchtea-bqq` — slash command dispatch refactor
- `bitchtea-lb7` — safer undo/commit behavior
- `bitchtea-3t8` — command validation
- `bitchtea-jwv` — /debug command
- `bitchtea-vwi` — input history navigation
- `bitchtea-v4k` — human-readable conversation logs
- `bitchtea-863` — /copy command
- `bitchtea-q98` — headless mode
- `bitchtea-xbb` — CLI help / README risk profiles
- `bitchtea-rml` — README split

### Rendering / theme / sound work done
- `bitchtea-rs3` — collapse to one built-in theme for now
- `bitchtea-g9e` — replace poser themes
- `bitchtea-ic9` — remove placeholder themes
- `bitchtea-58i` — thinking indicator styling fix
- `bitchtea-t73` — markdown width fix
- `bitchtea-dbm` — bounded markdown renderer cache
- `bitchtea-1xp` — rendering snapshot tests
- `bitchtea-2h5` — theme drift tests
- `bitchtea-c9r` — stats double-counting fix
- `bitchtea-u0q` — sound configurability
- `bitchtea-yn2` — /mp3

### Provider / config work done
- `bitchtea-80p` — provider support: Ollama / OpenRouter / Z.ai
- `bitchtea-nyb` — config / perf one-liner fixes

### Tracker cleanup already done
- `bitchtea-7q5` — dependency direction cleanup
- `bitchtea-7hw` — stale embedded lock issue
- `bitchtea-u2y` — broad umbrella safety/reliability issue closed as satisfied

## Locked decisions
- Phase 1 memory is **pure markdown**.
- Restore **open contexts and current focus** on restart.
- A **channel** is a real context.
- A **subchannel** is a child context that reads parent context and writes only to itself.
- A **nickname/persona** is a real direct conversation target.
- **`/query` is routing**, not a separate kind of place.
- Query/direct-message targets do **not** see channels by default.
- They only get channel context when explicitly **invited/bridged**.
- If invited into a channel, they are treated as **actually joined there until parted**.
- The channel keeps its own resident context; inviting another persona does not replace the channel context.
- Daemon / janitor / heartbeat is **Phase 1**.
- Provider strategy is **borrow/fork useful pieces**, not custom work for its own sake.

## What the open items mean in plain English
- `bitchtea-nyd` = build the actual IRC-style routing/focus behavior
- `bitchtea-42f` = make channel/query identity persist in sessions and across restart
- `bitchtea-7an` = finish the broader IRC-style architecture on top of that

## Concrete plan

### Phase 1 — make channel runtime real
1. Add real channel / subchannel / direct-target state.
2. Add routing commands:
   - /join
   - /msg
   - /part
   - /query
3. Make Enter send to the current focus.
4. Keep slash commands working no matter what is focused.

### Phase 2 — persistence
5. Persist:
   - open contexts
   - current focus
   - context identity on messages/events
6. Restore them on restart.

### Phase 3 — channel membership + bridging
7. Let a direct target be invited into a channel.
8. Invited target stays joined until parted.
9. Joined target does not replace the channel’s own resident context.
10. Default rule:
   - direct targets do not see channels unless invited.

### Phase 4 — memory behavior
11. Keep Phase 1 memory pure markdown.
12. Attach memory to context identity.
13. Child context:
   - reads parent
   - writes only to itself
14. Keep compaction / durable summaries working with the new context model.

### Phase 5 — daemon
15. Add Phase 1 daemon / janitor / heartbeat in the simplest useful form.
16. It should help with compaction / hygiene, not invent complexity.

### Phase 6 — provider stack
17. Lock what to borrow/fork for provider support.
18. Improve provider behavior without rewriting everything for purity.

## Immediate build order
1. `bitchtea-nyd`
2. `bitchtea-42f`
3. `bitchtea-7an`

## Current plain-English status
- Most low-level plumbing is already done.
- The real missing thing is the actual **IRC-style runtime behavior**.
