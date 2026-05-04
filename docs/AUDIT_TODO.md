# BitchTea Documentation Audit TODO

Audit date: 2026-05-04. Source of truth: live code in this repo. Where this audit and `CLAUDE.md` disagree, the live code is authoritative.

This is the exhaustive punch list of holes, wrong claims, missing under-the-hood explanations, disconnected wiring assumptions, and doc-organization issues across `docs/`. Each finding cites a file/line in code or docs so the next pass can verify quickly.

Sections:
1. Wrong / Stale Claims
2. Internal Contradictions (and CLAUDE.md vs code)
3. Disconnected Wiring (docs claim connected; code says no)
4. Missing Under-the-Hood Data Flows
5. Completely Undocumented Code / Subsystems
6. Doc Organization Issues
7. Prioritized Punch List

---

## 1. Wrong / Stale Claims

### `docs/signals-and-keys.md`

- **Line 24** — Claims "If `m.modelPicker != nil`, all key events except `esc` and `enter` are sent to the picker." Wrong on two counts. Field is `m.picker`, not `m.modelPicker`. The picker swallows ALL keys when active (`internal/ui/model.go:462-465`); `handlePickerKey` (`internal/ui/model_picker_keys.go:54-105`) explicitly handles Esc/Enter/Ctrl+C inside the picker — there is no "except enter/esc" pass-through.
- **Line 34-37** — `esc` graduation table omits the panel-close step. `handleEscKey` (`internal/ui/model.go:1051-1060`) closes ToolPanel/MP3 panels *before* the cancel ladder counts.
- **Line 46** — Claims `esc` "Resets `textarea`". False. `handleEscKey` (`internal/ui/model.go:1042-1103`) never touches `m.input`. It closes panels, arms queue clear, or steps the cancel ladder.
- **Line 51** — Claims `ctrl+z` "Sends `tea.SuspendMsg`". No explicit `ctrl+z` keybinding exists in `internal/ui/model.go`. Bubble Tea intercepts SIGTSTP and emits `tea.SuspendMsg` itself; the model only handles the *received* `tea.SuspendMsg` (`internal/ui/model.go:601-603`). Reword to say "tea.SuspendMsg from the framework's SIGTSTP handler".
- **Line 58** — Picker `up/down` description: claims "Moves `p.cursor` in the filtered list." Actually calls `m.picker.moveCursor(±1)` (`internal/ui/model_picker_keys.go:70-77`). Picker also handles PgUp/PgDown (currently omitted).
- **Line 56** — Picker "Any Character: Appends to `p.query` and calls `p.refilter()`." The actual function is `appendQuery`, which calls `refilter()` internally — close but minor.
- **Line 62** — Claims **`ctrl+s`** "Toggles the player visibility." NO `ctrl+s` handler exists anywhere in `internal/ui/`. MP3 panel toggle is via `/mp3` slash command (`internal/ui/commands.go:365-402`); `handleMP3Key` only handles `space`, `left`/`j`, `right`/`k` (`internal/ui/model.go:1359-1368`).
- **Line 13** — SignalMsg cancellation message: doc says `"Interrupted by signal"`. Actual string is `"Interrupted by signal."` (trailing period present at `internal/ui/model.go:594-600`).

### `docs/tools.md`

- **Line 33** — Claims `terminal_start` spawns "in a new process group". `internal/tools/terminal.go:98` calls `exec.CommandContext(sessionCtx, "bash", "-c", args.Command)` and does NOT set `SysProcAttr.Setpgid`. Claim is unverified — drop or back with code.
- **Line 56** — Edit error format quoted as `"oldText matches 3 times in file.txt (must be unique)"`. Real format at `internal/tools/tools.go:556` is `"oldText matches %d times in %s (must be unique): %q"` — the trailing quoted-text suffix is dropped from the doc.
- **Coverage:** `tools.md` only deeply documents `bash`, `edit`, `read`, `terminal_start`, `terminal_wait`. The other 9 tools (`write`, `search_memory`, `write_memory`, `terminal_send`, `terminal_keys`, `terminal_snapshot`, `terminal_resize`, `terminal_close`, `preview_image`) get no dedicated coverage. `preview_image` is not mentioned at all.

### `docs/sessions.md`

- **Line 1184** — Claim about stale-watermark on resume is technically correct but worth pinning to a test: `RestoreMessages` (`internal/agent/agent.go:1091-1114`) prepends a system message when missing and resets `bootstrapMsgCount=0`; `ResumeSession`'s saved-idx is set after conversion, leaving an off-by-one window the doc flags.
- **Line 184** — v0 example JSON is roughly accurate but the prose hedges with "field order follows Go's `encoding/json` struct field order." The actual struct order at `internal/session/session.go:36-61` is `ts, role, content, context, bootstrap, tool_name, tool_args, tool_call_id, tool_calls, parent_id, branch, id, v, msg, legacy_lossy`. Pin the example to the canonical order for clarity.

### `docs/memory.md`

- Verified end-to-end against `internal/memory/memory.go` and `internal/daemon/jobs/memory_consolidate.go`. The few claims that look fragile (`< 6` no-op, daemon writes raw without `flock`, daemon marker `<!-- bitchtea-consolidated:... -->`) are all correct.

### `docs/ui-components.md`

- **Line 42-44** — "`Update` mutates `Model`." `Update` has a value receiver `(m Model) Update(msg) (tea.Model, tea.Cmd)` (`internal/ui/model.go:360`); strictly it returns a new value. Pointer-receiver helpers (`handleAgentEvent`, `handleEscKey`) mutate. This nuance is the root cause of the `/debug on` value-capture bug — call it out here.
- **Line 69-74** — Init textarea size `80x3`. Verified. Add: `ShowLineNumbers = false` (minor).
- **Line 131-138** — WindowSize math says "viewport height as `height - 7`." Real formula is `vpHeight := msg.Height - 4 - inputHeight` where `inputHeight := 3` (`internal/ui/model.go:369-374`). Same numeric result today, but the doc hides that input height is dynamic.
- **Line 200-203** vs `agent-loop.md:223` — Both consistent: `waitForAgentEvent` reads one event per call; `Update` re-issues another wait after each `agentEventMsg` (`internal/ui/model.go:626-628`).
- **Line 484** — `ToolPanel.Clear()` is unused in production. Confirmed: callers are only the definition and `internal/ui/toolpanel_test.go:65`. Leave the note but tag as dead code.
- **Line 484** — "fixed width 28" for ToolPanel — not verified by audit; double-check against current `toolpanel.go`.
- **Line 537-540** — MP3 player precedence "mpv → ffplay → mpg123". Verified at `internal/ui/mp3.go:407-410, 440`.
- **Line 592** — `pickerVisibleRows = 12`. Verified at `internal/ui/model_picker_keys.go:109`.
- **Line 714** — `FocusManager.RestoreState` does not floor a negative `state.ActiveIndex` — only clamps `>= len()` (`internal/ui/context.go:176-179`). Doc correctly flags this.
- **Line 786-792** — `/debug on|off` is described as if it works normally. It DOES NOT — `handleDebugCommand` (`internal/ui/commands.go:306`) takes `m Model` by value, so the closure on `commands.go:319-327` captures a stale copy and its `m.addMessage`/`m.refreshViewport` calls are silent no-ops. Add the same warning that `agent-loop.md:1303-1306` carries.
- **Line 825-836** — `/set service` description has no warning that the service value never reaches the agent (no call to `agent.SetService`). Mirror the `agent-loop.md` warning here.
- **Line 954** — `saveCurrentContextMessages` is unused. Confirmed dead code at `internal/ui/context_helpers.go:25` (no production callers).

### `docs/agent-loop.md`

- Bulk of the document was verified accurate against `internal/agent/agent.go`, `internal/llm/stream.go`, `internal/agent/context_switch.go`, `main.go`. Highlights confirmed correct:
  - 14-step bootstrap order at `agent.go:127-189`.
  - Persona prompt + rehearsal at `agent.go:522-615`.
  - System-prompt first line format `agent.go:425-430`.
  - `StepCountIs(64)` at `stream.go:14, 101`.
  - Retry backoff `1s..64s` at `stream.go:20-28`. Retry text uses U+2026 (`…`), not three ASCII dots — call this out explicitly so future doc edits don't normalize it.
  - `AutoNextPrompt`/`AutoIdeaPrompt` exact text at `agent.go:816-827`.
  - Headless `[auto]` / `[tool]` / `[status]` formats at `main.go:267, 281-297, 324-333`.
- The doc's flagged gaps (service propagation, `/debug on` value-capture, queued prompt drain) are real bugs in code — see Section 3.

### `docs/testing.md`

- Reasonably accurate. Notable verifications:
  - `internal/llm/client_test.go::TestClientConcurrentSetAndStream` exists at line 216.
  - `internal/agent/messages_phase3_test.go` exists with five `TestPhase3*` functions.
  - `internal/daemon/e2e_test.go` and `integration_test.go` exist with the listed test names.
- Missing categories: see Section 4 (failure-mode coverage, MCP tests, catalog tests, debug-hook tests, RC parser tests).

---

## 2. Internal Contradictions (and CLAUDE.md vs code)

### CLAUDE.md vs live code (CRITICAL)

- **`CLAUDE.md` "In flight" claims `bt-x1o` per-context histories are unfinished P0** ("`/join #chan` and `/query nick` only re-label the UI today; the agent still streams against one shared `messages` slice. Don't write code or docs that assume isolated histories until this lands.").
- **Reality:** Per-context histories are end-to-end. `internal/agent/context_switch.go` defines `Agent.SetContext`, `InitContext`, `SavedIdx`, `SetSavedIdx`, `RestoreContextMessages`, `InjectNoteInContext`. They are used at `internal/agent/agent.go:179-186` (init), `internal/ui/model.go:933-938` (switch on turn start), `internal/ui/model.go:662-676` (per-context session save), `internal/ui/model.go:267-272` (resume restore).
- **Action:** Update `CLAUDE.md` "In flight" — close the per-context-histories item, refer readers to `docs/agent-loop.md:166-191` and `internal/agent/context_switch.go`. `docs/agent-loop.md` is correct; `CLAUDE.md` is stale.

### CLAUDE.md "Docs Split" lists deleted files

`CLAUDE.md` "Docs Split" enumerates these as if they live in `docs/`: `architecture.md`, `commands.md`, `cli-flags.md`, `streaming.md`, `getting-started.md`, `user-guide.md`, `development.md`, `glossary.md`, `troubleshooting.md`. **All of these are in `git status`'s deleted list.** `docs/README.md` is also deleted. Either restore them or update the CLAUDE.md doc-split section to reflect what actually exists today (`agent-loop.md`, `memory.md`, `sessions.md`, `tools.md`, `signals-and-keys.md`, `ui-components.md`, `testing.md`, `development-guide.md`, `development-guide-1.md`).

### CLAUDE.md / daemon_cli.go reference deleted phase docs

- `CLAUDE.md` "In flight" Daemon entry references `docs/phase-7-daemon-audit.md` and `docs/phase-7-process-model.md` — both deleted.
- `daemon_cli.go` references `docs/phase-7-process-model.md` — deleted.
- Other deleted phase docs referenced nowhere but worth noting: `phase-3-message-contract.md`, `phase-4-preparestep.md`, `phase-5-catalog-audit.md`, `phase-6-mcp-contract.md`, `phase-8-cancellation-state.md`, `phase-9-service-identity.md`.
- **Action:** Replace these references with a successor doc (e.g., a new `docs/daemon.md`) or strip the references.

### Cross-doc

- `docs/signals-and-keys.md:62-63` claims `ctrl+s` toggles MP3 panel, while `docs/ui-components.md:534-540` correctly attributes toggle to `mp3Controller.toggle()` driven by `/mp3`. signals-and-keys.md is wrong.
- `docs/agent-loop.md:1303-1306` warns that `/debug on` streaming visibility is not fully wired (value-capture bug); `docs/ui-components.md:786-792` describes `/debug on` as if it works. Sync the warning into ui-components.md.
- `docs/agent-loop.md:1208-1226` warns `/set service` and `/profile load` don't propagate to the agent; `docs/ui-components.md:825-836` does not. Sync the warning.
- `docs/sessions.md` and `docs/memory.md` both describe `flock` semantics; both are correct individually but the divergence between session append (uses `flock`) and daemon memory consolidation (raw `os.OpenFile`, no flock) deserves a single cross-reference paragraph.

---

## 3. Disconnected Wiring (docs assume connected; code says no)

### Confirmed disconnected

- **MCP integration is wired but never bootstrapped.** `Agent.SetMCPManager` (`internal/agent/agent.go:692-705`), `Client.SetMCPManager` (`internal/llm/client.go:154-166`), and `MCPTools` consumed in `stream.go:113` all exist. **No code in `main.go`, `daemon_cli.go`, `cmd/`, or `internal/ui/` calls `SetMCPManager`.** The whole `internal/mcp/` package and `internal/llm/mcp_tools.go` are code-complete but unreachable from the running binary. **Action:** Either wire MCP at startup (probably from `main.go` after profile load) or document it as code-present-but-disabled in a top-level "What works / What is dormant" section.
- **Daemon has no producer.** `internal/daemon/mailbox.go::Submit` has zero callers. `bitchtea daemon start` runs the loop, watches the mail dir, and dispatches via `internal/daemon/jobs/jobs.go`, but no agent/UI code ever submits a job envelope. The daemon is a sink with no source. **Action:** Add a "Producer" section to a new `docs/daemon.md` describing the intended call sites (session checkpoint, memory consolidation) and explicitly note that no production caller currently submits.
- **`/set service` does not reach the agent.** `handleServiceSet` (`internal/ui/commands.go:847-857`) writes `m.config.Service = value` and never calls `agent.SetService`. Service-gated behavior (Anthropic prompt cache markers, future provider gating) silently does not change mid-session. Mirror this warning in `ui-components.md`.
- **`/profile load` does not propagate `Service`.** `applyProfileToModel` (`internal/ui/commands.go:812-836`) calls `SetProvider`/`SetModel`/`SetBaseURL`/`SetAPIKey` but never `SetService`.
- **TUI queue does not call `Agent.QueuePrompt`.** Enter while streaming appends to `m.queued` directly (`internal/ui/model.go:531-541`). `agent.QueuePrompt` (`internal/agent/agent.go:241-246`) has only test callers.
- **`/debug on` writes through a value-receiver capture.** `handleDebugCommand` (`internal/ui/commands.go:306-336`) captures `m Model` by value; closure mutations never reach the live model.
- **`ToolPanel.Clear()` has no production caller.** Defined at `internal/ui/toolpanel.go:70`, only invoked from `toolpanel_test.go:65`.
- **`saveCurrentContextMessages` has no production caller.** Defined at `internal/ui/context_helpers.go:25`. Dead.
- **`FocusManager.RestoreState` lacks a negative-index floor.** `internal/ui/context.go:176-179` only guards `>= len()`. A persisted negative `ActiveIndex` panics.
- **Anthropic prompt-cache gating is service-string literal.** `internal/llm/cache.go::applyAnthropicCacheMarkers` checks `service == "anthropic"`. The `zai-anthropic` profile uses the Anthropic API shape but has a different service value, so cache markers are silently skipped. Document this gate explicitly — it is either a Phase 9 intentional separation or a latent bug.
- **`/invite` catch-up text is rendered to the UI but never injected into the agent transcript.** The LLM never "sees" who joined. Document this as visual-only.

---

## 4. Missing Under-the-Hood Data Flows

These flows are partially visible in docs but do not get a from-the-OS-up explanation. Each is worth a short subsection in the relevant doc.

### Streaming + provider lifecycle (`docs/agent-loop.md` or new `docs/streaming.md`)

- **Keypress → HTTP request chain.** `Update` → `sendToAgent` → `startAgentTurn` → goroutine `Agent.SendMessage` → `Client.StreamChat` → `streamOnce` → `ensureModel` → `buildProvider` (`internal/llm/providers.go:30-44`) → `routeByService`/`buildAnthropicProvider`/`routeOpenAICompatible` → fantasy-issued HTTP via the (optional) `debugRoundTripper` wrap.
- **Provider cache invalidation.** `Client.SetModel/SetProvider/SetBaseURL/SetAPIKey` all call `invalidateLocked()` (`internal/llm/client.go:191-200`). `ensureModel` (`client.go:201-224`) lazy-rebuilds. `SetService` and `SetBootstrapMsgCount` deliberately do NOT invalidate (consumed in PrepareStep, not provider construction). Document the why.
- **PrepareStep responsibilities.** `splitForFantasy` removes system messages and pulls the tail prompt; `bootstrapPreparedIndex` (`internal/llm/cache.go:72`) translates the agent's `bootstrapMsgCount` into a prepared-message index because `splitForFantasy` shifts indices. Document the two-index translation.
- **Cost tracking.** `CostTracker.AddTokenUsage` from streaming `usage` events; `EstimateCostFor(model, service)` joins on Service ↔ InferenceProvider via `CatalogPriceSource` (`internal/llm/cost.go:167-196`), with embedded fallback (`lookupEmbedded`). Wired at `main.go:57` via `SetDefaultPriceSource(CatalogPriceSource(catalog.Load(...)))`.
- **Debug hook plumbing.** `newDebugRoundTripper`/`newDebugTransport`/`debugRoundTripper.RoundTrip` (`internal/llm/debug.go:21-100`) installs an HTTP transport wrapper, redacts Authorization headers (`redactAuthValues`, `repeatedRedaction`), and replays response bodies (`replayReadCloser`). Currently only the `/debug on` UI is described; the transport interception model is invisible.
- **Error hint classification.** `internal/llm/errors.go:15` `ErrorHint` substring rules (which inputs map to which hints) are not enumerated.
- **Anthropic prompt-cache marker placement.** `applyAnthropicCacheMarkers` (`cache.go:28`) called inside PrepareStep (`stream.go:140`); gate condition (`service == "anthropic"`) is exact-string. Document.

### Memory + sessions

- `injectedPaths` is NOT reset by `RestoreMessages` (`internal/agent/agent.go:1091-1114`). After resume, scoped `HOT.md` is not re-injected because the bootstrap-time set sticks even though `bootstrapMsgCount=0`. Document the consequence: a resumed session sees its first scoped-memory injection only when the user enters a new scope.
- **`Fork` parent-chain semantics.** `session.Fork` copies entries verbatim including their original `ParentID` references — the parent chain in the forked file points to entries in the same forked file (since they're prefix-copied), but no rebuilding occurs. Document.
- **`Append` parent linking semantics.** `Append` only sets `ParentID` if blank, and walks the in-memory `s.Entries` slice (not on-disk). After `Fork`, the in-memory slice is the prefix copy, so chains diverge from disk if you append. Document.
- **`EntrySchemaVersion` constant.** Pin its location (`internal/session/session.go:23`) and its rationale.
- **`bash` working dir defaults.** `r.WorkDir` (`internal/tools/tools.go:659`) — undocumented.
- **`bash` 50KB rune-boundary truncation** — undocumented (only `read` truncation is mentioned).
- **`terminal_keys` named-key map.** `esc, enter, tab, backspace, delete, up/down/left/right, home, end, pageup, pagedown, ctrl-a..ctrl-z, space, newline/lf` — undocumented.
- **`terminal_start` defaults.** width 100, height 30, delay 200ms — undocumented in the doc body (only in tool description text).
- **`preview_image` flow.** Not in `tools.md` at all. Width clamped to 160, height clamped to 80, default width 80. `image.Decode` for format detection. Output prefix `image preview <path> (<format>, <WxH>)`.

### UI / startup

- **RC file lifecycle.** `main.go:59` calls `applyStartupConfig(&cfg, args, config.ParseRC())`. `config.ApplyRCSetCommands` consumes set-form commands inline and returns slash commands replayed via `m.ExecuteStartupCommand` (`main.go:165-168`, `internal/ui/model.go:325-339`). RC path is `BaseDir/bitchtearc` (NOT `~/.bitchtearc` as `CLAUDE.md` implies).
- **Profile resolution.** `main.go:140-158` resolves `--profile`, applies it, then re-parses CLI args so explicit flags override profile defaults. Order matters; document.
- **Headless prompt collection.** `collectHeadlessPrompt` (`main.go:219-243`) merges `--prompt` flag and piped stdin with `\n\n` between if both. Order: flag first, stdin second.
- **Headless `[auto] <Label>` / `[tool]` / `[status]` lines.** `main.go:267, 281-297, 324-333`. Already described in `agent-loop.md`; cross-reference from a headless-mode section if one is added.
- **`MigrateDataPaths`.** Runs unconditionally at startup. Moves files between legacy and current paths. No doc explains what is migrated or what to do on partial failure.
- **Catalog refresh + `/models` data path.** `main.go::maybeStartCatalogRefresh` triggers a background `catalog.Refresh` gated on `BITCHTEA_CATWALK_AUTOUPDATE` and `BITCHTEA_CATWALK_URL`. `/models` reads via `catalog.Load(...)` and matches `cfg.Service` → catalog `InferenceProvider`. ETag/TTL semantics absent.

---

## 5. Completely Undocumented Code / Subsystems

### Subsystems with substantial code and zero coverage

- **Daemon** (~1000 LoC). `cmd/daemon/main.go`, `daemon_cli.go`, `internal/daemon/{run,mailbox,envelope,lock,paths,pidfile,io,ulid}.go`, `internal/daemon/jobs/{jobs,checkpoint,memory_consolidate}.go`. File-based IPC layout (`mail/`, `done/`, `failed/`, `quarantine/`), flock-based locking, ULID envelope IDs, `DisallowUnknownFields` reader contract, `Scope` re-declaration to avoid import cycle, drain budget, crash-recovery quarantine. **No `docs/daemon.md` exists.**
- **MCP integration.** `internal/mcp/{manager,client,config,permission,audit,redact}.go` (~1500 LoC). Three-layer disable-by-default permission model, audit log redaction, secret redaction in tool args. Bridge in `internal/llm/mcp_tools.go`. Not bootstrapped (see Section 3) but the package is real.
- **Catwalk catalog.** `internal/catalog/{cache,load,refresh}.go` (~370 LoC). HTTP `If-None-Match` ETag refresh, on-disk `Envelope`, env overrides `BITCHTEA_CATWALK_URL`, `BITCHTEA_CATWALK_REFRESH`, `BITCHTEA_CATWALK_OFFLINE`.
- **Configuration / Profiles / RC.** `internal/config/{config,rc}.go` (~670 LoC). `Config`, `Profile`, `DetectProvider`, `MigrateDataPaths`, `ProfileAllowsEmptyAPIKey`, 17 built-in profiles. RC parser at `BaseDir/bitchtearc`. Service-vs-Provider distinction (Phase 9) is silent.
- **Persona / bootstrap prompts.** `personaPrompt`, `personaRehearsal`, `buildPersonaAnchor` at `internal/agent/agent.go:522-615`. Hardcoded "I AM CODE APE" content. `bootstrapMsgCount` boundary that compaction must not cross.
- **Sound system.** `internal/sound/sound.go`. ANSI BEL only — no audio file playback despite the package name. Bell types: `Beep`/`Success`/`Error`/`Done`, `writeBell(count)`.
- **Cost tracking.** `internal/llm/cost.go`. `CostTracker`, `PriceSource`, `CatalogPriceSource`, embedded fallback.
- **Debug hook (transport layer).** `internal/llm/debug.go`. `debugRoundTripper`, redaction, replay. Only the UI toggle is mentioned anywhere.
- **Anthropic prompt caching.** `internal/llm/cache.go`. `bootstrapPreparedIndex`, `applyAnthropicCacheMarkers`, exact-string service gate.
- **Provider routing.** `internal/llm/providers.go`. Service routing through `routeByService`, `buildAnthropicProvider`, `routeOpenAICompatible`.
- **Tool context manager.** `internal/llm/tool_context.go`. `agent-loop.md:649-660` mentions but does not deep-dive.
- **Typed tool wrappers.** `internal/llm/typed_{read,write,edit,bash,search_memory,write_memory}.go`. Listed by name in agent-loop.md "Tool Surface" but architecture not explained: schema generation, registry fallback, why six tools migrated and eight didn't.
- **Aliases shim.** `internal/agent/aliases.go`. Exists solely to break what would otherwise be an import cycle with `internal/agent/event/`. Not "dead" but its purpose is unrecorded.

### Smaller undocumented files

- `internal/llm/types.go` — `Message`, `StreamEvent`, `ToolCall`, `Usage` shapes that define the streaming boundary.
- `internal/tools/types.go` — `ToolDef`, `ToolFuncDef` schema shape used to register every tool.
- `internal/agent/context.go` — `DiscoverContextFiles`/`ExpandFileRefs` (30KB per-file cap, silent truncation).
- `internal/llm/errors.go` — `ErrorHint` classification rules.
- `internal/ui/art.go` — splash randomizer; six art variants enumerated nowhere.
- `internal/ui/clipboard.go` — clipboard fallback chain.
- `internal/ui/invite.go` — `/invite` catch-up generation; visual-only (see Section 3).
- `internal/ui/transcript.go` — `syscall.Flock`-based locking, daily file rotation, stderr fallback. Mentioned in passing only.
- `internal/agent/event/event.go` — subpackage exists but role not described.

### Empty / stub directories

- `cmd/cpm/` — empty, no `.go` files. Reserved for future binary; mention or remove.
- `cmd/trace/` — referenced in `CLAUDE.md` only. Add a one-paragraph `docs/development-guide.md` entry.

### CLI flags

- `docs/cli-flags.md` is **deleted**. `main.go::parseCLIArgs` implements only **8** flags: `-m/--model`, `-r/--resume`, `-p/--profile`, `--prompt`, `--auto-next-steps`, `--auto-next-idea`, `-H/--headless`, `-h/--help`. Common assumptions like `--provider`, `--base-url`, `--api-key`, `--service`, `--workdir`, `--debug`, `--no-color` are NOT implemented. Restore the file or fold the canonical list into `docs/development-guide.md`.

---

## 6. Doc Organization Issues

### Deleted docs still referenced

`git status` shows these deleted, but `CLAUDE.md` "Docs Split" or `daemon_cli.go` still cite several: `README.md`, `architecture.md`, `cli-flags.md`, `commands.md`, `development.md`, `getting-started.md`, `glossary.md`, `model-catalog.md`, `phase-3-message-contract.md`, `phase-4-preparestep.md`, `phase-5-catalog-audit.md`, `phase-6-mcp-contract.md`, `phase-7-daemon-audit.md`, `phase-7-process-model.md`, `phase-8-cancellation-state.md`, `phase-9-service-identity.md`, `streaming.md`, `troubleshooting.md`, `user-guide.md`. **Action:** Either restore (likely not desired given the deliberate deletion) or strip references and explicitly state in `CLAUDE.md` that these were removed.

### Duplicate / stale files

- `docs/development-guide-1.md` is a truncated duplicate of `docs/development-guide.md` (stops at step 5). **Action:** Delete `development-guide-1.md`.

### Coverage drift

- `docs/tools.md` is narrow (registry + PTY + a few tools). Either split per tool family or expand to cover all 14 tools (read, write, edit, bash, search_memory, write_memory, terminal_*×7, preview_image).
- `docs/testing.md` lists test files but does not enumerate failure modes, edge cases, or what is NOT covered (PTY exhaustion, real-network paths, MCP bootstrap, RC parser, debug hook).
- No top-level `docs/README.md` index linking the surviving docs.

---

## 7. Prioritized Punch List

P0 — fix before anything else, low effort, high blast radius:

1. Update `CLAUDE.md` "In flight" to close the per-context-histories item (it's done) — current text is misleading agents working in this repo.
2. Update `CLAUDE.md` "Docs Split" to match what actually exists in `docs/`.
3. Delete `docs/development-guide-1.md`.
4. Strip references to deleted phase docs from `CLAUDE.md` and `daemon_cli.go`, or replace with `docs/daemon.md` once it exists.
5. Fix `docs/signals-and-keys.md` `ctrl+s` claim, `m.modelPicker` → `m.picker`, `esc` "resets textarea" claim, `ctrl+z` framing.
6. Fix `docs/tools.md` "new process group" claim.
7. Mirror `/debug on` value-capture warning and `/set service` non-propagation warning into `docs/ui-components.md`.

P1 — substantive new docs:

8. Write `docs/daemon.md` covering the run loop, mailbox/envelope/lock/pidfile, ULID, jobs dispatch, drain budget, crash recovery, and the *currently disconnected producer* warning.
9. Write `docs/mcp.md` covering `internal/mcp/` and the bridge in `internal/llm/mcp_tools.go`, plus a "wired but not bootstrapped" warning.
10. Write `docs/config-and-profiles.md` covering `internal/config/`, the 17 built-in profiles, RC at `BaseDir/bitchtearc`, profile resolution order at startup, and the Service-vs-Provider distinction.
11. Write `docs/catalog.md` covering `internal/catalog/`, ETag/TTL refresh, and env overrides.
12. Restore (or replace) `docs/cli-flags.md` listing the 8 implemented flags. Pin to `main.go::parseCLIArgs`.
13. Expand `docs/tools.md` to cover `write`, `search_memory`, `write_memory`, `preview_image`, and the full `terminal_*` family. Add the `terminal_keys` named-key table.

P2 — under-the-hood data flows (Section 4):

14. Add a "Streaming & Providers" section to `docs/agent-loop.md` (or a new `docs/streaming.md`) covering the keypress→HTTP chain, provider cache lifecycle, PrepareStep, cost tracking, debug RoundTripper, error hint classification, and Anthropic cache gating.
15. Add a "Resume edge cases" section to `docs/sessions.md` covering the `injectedPaths`-not-reset behavior, `Fork` parent-chain semantics, `Append` in-memory chain semantics, and `EntrySchemaVersion`.
16. Add persona / bootstrap prompts to `docs/agent-loop.md` (or a new `docs/persona.md`) — they are hardcoded, agent-visible, and currently invisible from docs.

P3 — small fills:

17. Document `internal/sound/sound.go` (terminal bell only — disabuse the "audio playback" reading).
18. Document `internal/ui/art.go` splash variants and randomizer.
19. Document `internal/ui/transcript.go` flock + daily rotation + stderr fallback.
20. Document `internal/ui/invite.go` as visual-only (does not enter agent transcript).
21. Document `internal/agent/aliases.go` purpose (cycle break for `internal/agent/event/`).
22. Decide fate of `cmd/cpm/` — either implement, document the placeholder, or delete.

P4 — ongoing hygiene:

23. Add a top-level `docs/README.md` index.
24. After each refactor that touches a service-gated, profile-gated, or transport-gated path, re-verify the disconnected-wiring list in Section 3.
25. Pin retry text test to U+2026 ellipsis byte sequence so future doc edits don't normalize it.

---

## Appendix: Verification Method

Findings were generated by three parallel audit passes on (a) `agent-loop.md`/`signals-and-keys.md`/`ui-components.md`, (b) `memory.md`/`sessions.md`/`tools.md`/`testing.md`, (c) undocumented subsystems. Each claim that survived the audit was checked against a specific file:line in the codebase. Where a claim was unverified or mis-quoted, the canonical source location is included so the next pass can correct in place.
