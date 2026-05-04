# Testing

This file is the test map for the current checkout. It says what each suite actually proves, what it fakes, which behaviors are pinned to exact strings, and where the coverage stops at a seam instead of a real external failure.

Cross-reference the behavior docs in `docs/agent-loop.md`, `docs/memory.md`, `docs/sessions.md`, and `docs/ui-components.md` when you need the runtime story behind these tests.

## Required Gates

Run these before closing any code-changing issue:

- `go build ./...`
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build -o bitchtea .`

While iterating, narrow the scope first. `go test ./internal/ui` is the usual UI loop. `CLAUDE.md` still mentions `go test ./internal/agent -run TestAgentLoop`, but there is no test with that name in the current tree. The agent coverage now lives in `agent_loop_test.go`, `compact_test.go`, `replay_test.go`, `messages_phase3_test.go`, and `typed_tool_smoke_test.go`.

## Fake Streamers and Other Seams

The test suite relies on a small set of deterministic fakes. These are load-bearing because they let the suite exercise the event loop without real provider calls, shell sessions, or OS devices.

- `headlessScriptedStreamer` in `main_test.go` records the last prompt text and returns canned replies. It drives the headless auto-next / auto-idea path and proves the control tokens are hidden from stdout while follow-up labels are written to stderr.
- `fakeStreamer` in `internal/agent/agent_loop_test.go`, `compact_test.go`, and `replay_test.go` closes the `events` channel itself, increments a call counter under a mutex, and emits a canned sequence per turn. When it runs out of responses it emits `done`.
- `fixtureStreamer` in `internal/agent/replay_test.go` loads turn data from `internal/agent/testdata/*.json` and replays the listed `llm.StreamEvent` values turn by turn.
- `summaryStreamer` in `internal/agent/compact_test.go` is a `fakeStreamer` with two turns: the first is a throwaway reply, the second is the compaction summary.
- `blockingStreamer` in `internal/agent/compact_test.go` blocks on `ctx.Done()` so the compaction test can prove canceled contexts short-circuit before a stream starts.
- `readReplyLanguageModel` in `internal/agent/typed_tool_smoke_test.go` is a fake `fantasy.LanguageModel` wrapped by a real `llm.Client`. It emits a `read` tool call on the first stream and a final `ack` on the second.
- `capturingLanguageModel` and `blockingLanguageModel` in `internal/llm/cache_test.go` and `client_test.go` record fantasy calls so tests can inspect prompt shape, cache markers, and concurrency behavior without network traffic.
- `recordingRoundTripper` in `internal/llm/debug_test.go` captures request bodies and headers, then returns a canned response. It is how the debug hook proves replayability and redaction.
- `fakeMCPServer` in `internal/llm/mcp_tools_test.go` and `fakeServer` in `internal/mcp/manager_test.go` stand in for real MCP servers. They do not spawn subprocesses or listeners.
- `fakeMP3Process` in `internal/ui/mp3_test.go` counts pause, resume, and stop calls without playing audio.
- `stubClipboard` in `internal/ui/clipboard_test.go` swaps the clipboard backends for pure test seams.

Every one of these fakes is a seam test, not an integration with a real provider, clipboard, or player binary.

## Exact Strings Pinned By Tests

The suite locks down a number of visible strings. These are not examples; they are the exact values the tests assert.

- `Unknown command: /definitely-not-real. Try /help, genius.`
- `not running`
- `Subcommands:`
- `Commands:`
- `/activity [clear]`
- `Copied last assistant response via OSC 52.`
- `Copied assistant response 1 via xclip.`
- `Now playing: alpha`
- `Exit code: 17`
- `timed out after 1s`
- `context canceled`
- `tool cancelled`
- `past end of file`
- `content is required`
- `must be unique`
- `mkdir`
- `Memory matches for "IRC metaphor":`
- `No memory matches found`
- `image preview tiny.png (png, 2x2)`
- `terminal session <id> (50x8) running`
- `matched terminal text "ready"`
- `Applied 1 edit(s) to <resolved path>`
- `Wrote N bytes to <resolved path>`
- `Press Ctrl+C again to clear`
- `Press Esc again to cancel the turn`
- `twice more`
- `provider = openai`
- `baseurl = https://api.openai.com/v1`
- `apikey = sk-t...2345`
- `model = gpt-4o`
- `Provider set to: ...`
- `Base URL set to: ...`
- `API key set: ...`
- `Model switched to: ...`
- `Service set to: ...`
- `service = ...`
- `models for <service> (<n> total) — type to filter`
- `no active service`
- `models: catalog is empty`
- `models: no catalog data for service ...`
- `available services`
- `If this server is OpenAI-compatible, switch with /set provider openai.`
- `anthropic transport sends requests to /messages`
- `requests -> http://127.0.0.1:3456/messages`

## Package Inventory

### `main_test.go`

- `TestRunHeadlessLoopFollowsAutoNextAndIdeaFlow` drives `runHeadlessLoop` with `headlessScriptedStreamer`. It proves that stdout hides `AUTONEXT_DONE` and `AUTOIDEA_DONE`, stderr carries the follow-up labels, and the prompt history is ordered `fix it` -> auto-next prompt -> auto-idea prompt.
- `TestApplyStartupConfigRCProfileOverrideClearsProfile` and `TestApplyStartupConfigCLIModelOverrideClearsProfile` prove that a loaded profile is cleared when manual connection settings are applied from rc or CLI flags. These tests do not hit the network; they only assert startup state and rc command extraction.

### `daemon_cli_test.go`

- `TestDaemonStatusReportsNotRunning` and `TestDaemonStopReportsNotRunningWhenAbsent` pin the absent-daemon case: `status` and `stop` both print `not running` and exit 0.
- `TestDaemonRejectsUnknownSubcommand` checks the hard error path and the `unknown daemon subcommand` message.
- `TestDaemonHelpExitsZero` and `TestDaemonNoArgsPrintsUsage` pin the usage block headed `Subcommands:`.

### `internal/config`

- `config_test.go` covers defaults, provider detection from env, profile save/load/delete, builtin profiles, the ollama empty-key exception, data-path migration, service identity, and `ApplyProfile` / `ApplySet` behavior.
- `rc_test.go` covers rc parsing and rc `set` application, including profile selection, manual connection overrides clearing profiles, invalid provider handling, and bool parsing.

These are local filesystem and env tests. They do not talk to providers.

### `internal/session`

- `session_test.go` covers `New`, `Append`, `Load`, `List`, `Fork`, `Tree`, `Latest`, checkpoint writing, info formatting, focus save/load, bootstrap filtering, and parent-chain behavior when forking from the middle of a transcript.
- `membership_test.go` covers membership save/load round trips, the missing-file path, and nil-channel handling.
- `session_fantasy_test.go` covers the v0 legacy reader and the v1 fantasy-native writer. It pins the fact that some fantasy shapes are intentionally lossy when projected back to the old JSONL fields: multi-text messages, reasoning parts, and media or error tool results are marked `LegacyLossy`.

The session tests are mostly JSONL and path-shape tests. They do not simulate every kind of crash-corrupted file.

### `internal/agent`

- `agent_loop_test.go` uses `fakeStreamer` to prove the core agent loop: offline text replies, real tool-call execution through `read`, context cancellation producing `state:idle` plus `context.Canceled` plus `done`, and usage accounting when the stream reports token counts.
- `replay_test.go` uses `fixtureStreamer` and JSON fixtures under `internal/agent/testdata/` to replay scripted turns. It covers a simple reply, a tool loop, and the exhausted-fixture case that should still end in `done` rather than panic.
- `compact_test.go` proves compaction boundaries and memory flush behavior: fewer than 6 messages is a no-op, exactly 6 compacts, the summary is inserted as a user message with the `[Previous conversation summary]:` prefix, the system prompt is preserved, the last four messages survive, daily memory is flushed before the history is rewritten, scoped daily files go to scoped paths, tool metadata survives, and a canceled context returns `context.Canceled` without starting a stream.
- `messages_phase3_test.go` pins the fantasy-native history contract. Bootstrap messages are fantasy messages, restore rebuilds the live system prompt instead of trusting stale text, tool calls and tool results survive round-trip, and follow-up sanitization strips the done token from assistant text while leaving the tool call part intact.
- `typed_tool_smoke_test.go` is the only agent-level end-to-end smoke for the Phase 2 typed-tool migration. It wires a real `llm.Client` to a fake fantasy LM, exercises the typed `read` wrapper against a real workspace, and proves the result comes back as a tool event and lands in agent history.
- `context_test.go` covers context-file discovery, memory load/save/search, daily append, scoped path layout, inheritance across root/channel/query scopes, rendered search output, and file-reference expansion. `TestDiscoverContextFilesNone` is intentionally weak because the upward walk can see host files outside the temp tree.

The agent tests still use fake language models almost everywhere. They prove the loop and transcript wiring, not provider-specific streaming semantics.

### `internal/llm`

- `client_test.go` and `tool_context_test.go` are the race-heavy suites. `client_test.go` proves `SetAPIKey`, `SetBaseURL`, `SetModel`, `SetProvider`, and `SetDebugHook` invalidate cached provider/model state, and it hammers concurrent `Set*` plus `StreamChat` with a blocking fake model. `tool_context_test.go` proves tool contexts cancel independently, inherit turn cancellation, clean up, and survive concurrent `NewToolContext`, `CancelTool`, and `CancelAll` traffic.
- `providers_test.go` covers provider routing and URL parsing: `buildProvider`, `routeOpenAICompatible`, `routeByService`, `hostOf`, `hostOfMust`, `stripV1Suffix`, and the service-vs-host precedence rules.
- `convert_test.go` locks in the fantasy/llm bridge. `splitForFantasy` only peels off a tail user prompt when the transcript actually ends in a user turn; assistant-tail and tool-tail transcripts keep the earlier messages intact. `fantasyToLLM` preserves text, tool calls, tool results, pointer parts, ordering, and nil-safety, and it ignores unknown fantasy part types like reasoning and files. `LLMToFantasy` round-trips the current agent message shapes.
- `stream_test.go` proves `safeSend` blocks until delivery or context cancellation, and `toolResultText` only extracts text from the supported fantasy tool-result shapes.
- `tools_test.go` covers tool-schema translation and the fantasy streaming bridge. `translateTools` must flatten schemas into a properties map, `bitchteaTool.Run` must return fantasy error responses rather than Go errors, `splitSchema` must drop malformed and empty required entries, and `TestStreamChatSendsValidToolSchemaAndExecutesToolCall` proves the real `StreamChat` call sends the correct `read` schema, forwards the registry tools, replays the assistant tool-call transcript, and emits `tool_call`, `tool_result`, `text`, and `done` in the expected order.
- `typed_tool_harness_test.go` plus `typed_read_test.go`, `typed_write_test.go`, `typed_edit_test.go`, `typed_search_memory_test.go`, `typed_write_memory_test.go`, and `typed_bash_test.go` are the Phase 2 contract tests for typed fantasy wrappers. The harness asserts flat schemas and the rule that wrappers return text or text-error responses, not Go errors. The per-tool tests pin success, failure, and cancellation behavior:
  - `typed_read` - `path`, `offset`, `limit`; successful file reads; `past end of file`; missing file; canceled context.
  - `typed_write` - `path`, `content`; create and overwrite; invalid parent path yields `mkdir`; canceled context short-circuits before filesystem writes.
  - `typed_edit` - `path`, `edits`; successful edit; empty `oldText`; non-unique match; missing file; canceled context.
  - `typed_search_memory` - `query`, `limit`; root and scoped memory hits; empty result returns `No memory matches found`; canceled context.
  - `typed_write_memory` - `content`, `title`, `scope`, `name`, `daily`; root-scope default; explicit `scope="root"` override; missing content yields `content is required`; canceled context.
  - `typed_bash` - `command`, `timeout`; stdout/stderr capture; non-zero exit preserves output and the `Exit code: N` suffix; parent-cancel vs timeout wording; UTF-8-safe truncation with a `(truncated)` marker.
- `mcp_tools_test.go` covers the MCP adapter layer. It checks schema conversion, namespaced tool names, manager dispatch, Go errors becoming fantasy text errors, `Result.IsError` becoming fantasy text errors, nil-manager fallback to local tools, coexistence of local and MCP tools, local-wins name collisions, first-wins duplicate handling inside a server, and the end-to-end dispatch path through a real `mcp.Manager`.
- `cache_test.go` covers Anthropic cache-marker placement on the bootstrap boundary and the bootstrap index math. It explicitly excludes `openai` and `zai-anthropic` from cache markers. This is a request-shape test, not a live Anthropic capture test.
- `cost_test.go` covers token accumulation, cached price source selection, catalog-vs-embedded precedence, service joins, unknown-model fallback, `SetPriceSource`, and concurrent setter/estimator safety.
- `errors_test.go` covers `ErrorHint` mapping for nil, canceled, provider status codes, context-too-large, wrapped provider errors, dial errors, DNS, TLS, refused, and timeout cases.
- `debug_test.go` covers the debug hook: request capture without consuming the body, response capture for JSON and plain text, SSE skip behavior, and redaction of sensitive headers while leaving benign headers alone.

The llm suite is broad, but much of it is still seam work. It proves routing, conversion, and wrapper contracts more than real provider I/O.

### `internal/tools`

- `TestDefinitions` pins the current tool surface at 14 tools: `read`, `write`, `edit`, `search_memory`, `write_memory`, `bash`, `terminal_start`, `terminal_send`, `terminal_keys`, `terminal_snapshot`, `terminal_wait`, `terminal_resize`, `terminal_close`, and `preview_image`.
- `read` tests cover full-file reads, offset/limit slicing, past-EOF rejection, empty-file behavior, and UTF-8-safe truncation. The important failure mode is the `past end of file` error that mentions both the requested offset and the file length.
- `write` and `edit` tests pin resolved-path success messages and actual on-disk mutation. `write` reports `Wrote N bytes to <resolved path>`. `edit` reports `Applied 1 edit(s) to <resolved path>`. The failure cases cover empty `oldText`, non-unique replacements, missing paths, and invalid parents that fail during `mkdir`.
- `bash` tests cover stdout/stderr capture, non-zero exit handling, parent-canceled vs timeout wording, inner command-not-found without panic, and rune-safe truncation. Non-zero exit is not an error; it returns output plus `Exit code: N`. The truncation tests assert the output stays valid UTF-8 and ends with `... (truncated)`.
- `search_memory` and `write_memory` tests prove the root and channel memory scopes, daily append behavior, empty-content rejection, and the scoped path layout. `search_memory` renders the `Memory matches for "<query>":` banner and source metadata.
- `terminal_start`, `terminal_send`, `terminal_keys`, `terminal_snapshot`, `terminal_wait`, `terminal_resize`, and `terminal_close` exercise the PTY family. The tests pin `terminal session <id> (WxH) running`, the `matched terminal text "ready"` wait path, `exited` snapshots, resize output, and the fact that `terminal_start` uses `bash -c` rather than a login shell. The login-shell test deliberately avoids `terminal_close` in a defer because the comments call out a known `vt.SafeEmulator` close-vs-read race.
- `preview_image` reports `image preview tiny.png (png, 2x2)` for a tiny PNG fixture.
- `TestExecuteCancelledContextReturnsError` proves `Execute` returns a `tool cancelled` error early for canceled contexts.

These tests are the closest thing the repo has to true behavior specs for file, shell, PTY, and memory tools.

### `internal/ui`

- `commands_test.go` and `routing_commands_test.go` cover the slash-command registry and the IRC-style routing commands. They pin `/help` and `/h` aliasing, the removed root commands (`/auto-next`, `/auto-idea`, `/sound`, `/apikey`, `/baseurl`, `/provider`, `/model`), `/set` forwarding, `/join`, `/part`, `/query`, `/msg`, `/channels`, and focus persistence.
- `command_validation_test.go` covers the settings surface. `/set provider`, `/set baseurl`, `/set apikey`, and `/set model` accept arbitrary values verbatim. The no-arg forms show the current values, including the masked API key `sk-t...2345`. It also checks the endpoint mismatch warnings, profile hints, debug toggles, activity output, and the effective-endpoint status strings.
- `models_command_test.go` covers the model picker. It uses a stubbed `loadModelCatalog` seam, so it proves catalog shaping and picker behavior, not the live upstream catalog. The tests pin unset-service, empty-catalog, and unknown-service errors, case-insensitive service matching, selection applying `SetModel` and clearing the profile, Esc cancellation, filtering, and the default-large-model ordering.
- `model_turn_test.go` is the strict turn-boundary suite. It proves `sendToAgent` cancels the previous context, Up unqueues the last queued message, Ctrl+C and Esc use multi-stage ladders, stale `agentDoneMsg` values are ignored after channel replacement, panel closure has priority over cancellation, background activity messages do not dirty the viewport, and queue draining only happens on the right stage transitions. The exact prompts `Press Ctrl+C again to clear`, `Press Esc again to cancel the turn`, and `twice more` are pinned here.
- `resume_v1_test.go` and `startup_rc_test.go` cover restoration and startup command behavior. They prove v0 and v1 session files both restore into the viewport, mixed-version JSONL files are accepted, tool nicks are reconstructed, bootstrap entries stay hidden from display, startup commands run silently, and resumed messages stay visible while startup focus changes apply.
- `clipboard_test.go` covers OSC 52 and fallback clipboard routing. It proves `/copy` chooses the last assistant response by default, supports numbered selection, errors when there are no assistant messages, and falls back to `xclip` when stdout is not a terminal. The exact status strings above are pinned here.
- `mp3_test.go` covers the MP3 panel. It proves track sorting, panel visibility, start/pause/next controls, process shutdown, track-progress rendering, and help text. It is fully seam-driven: the player process and duration lookups are fakes.
- `transcript_test.go` covers the daily transcript logger. It writes human-readable logs, streams assistant chunks into one line, finalizes before tool output, omits thinking messages, and keeps the agent stream uninterrupted when thinking messages are ignored.
- `toolpanel_test.go` covers start/finish/error display, hidden rendering, truncation, session resume state, and the disabled theme command.
- `render_test.go` covers markdown detection, markdown/plain/code rendering, narrow and wide widths, renderer fallback, cache eviction, cache refresh, and ANSI-aware wrapping. It is a renderer-shape suite, not a Bubble Tea viewport integration test.
- `themes_test.go` is a style guard. It checks that theme colors and styles are populated and non-zero, but it does not prove visual layout.
- `signal_test.go` covers `tea.SuspendMsg` and `tea.QuitMsg` handling, including canceling active streaming on quit.
- `membership_test.go` and `invite_test.go` cover membership persistence, invite/kick idempotence, channel catchup building, direct-context refusal, and the channel list output.
- `message_test.go` covers message formatting shapes and panic-free rendering for long and empty content.

The UI tests are heavy on state and string checks. They do not drive a real terminal, clipboard daemon, or audio player.

### `internal/sound`

- `sound_test.go` captures writes to `Output` and proves the bell patterns: `bell`, `done`, `success`, and the default case each write one bell (`\a`), while `error` writes three bells (`\a\a\a`).

This is a pure output test. It does not try to validate terminal or audio semantics.

### `internal/daemon`

- `run_test.go` covers the daemon core loop in process: jobs without handlers are rejected into `failed/`, lock contention returns `ErrLocked`, and the pid file appears during the run and is removed on shutdown.
- `integration_test.go` covers stale-lock recovery, two-daemon contention, crash-recovery quarantine of pre-existing mail, and the dispatch hook path. These tests use real files and lock semantics, but they stay in-process.
- `e2e_test.go` is the only subprocess smoke. It builds `cmd/daemon`, runs it with an isolated `HOME`, submits a `session-checkpoint` job, waits for the `done/` result, and SIGTERMs the process to prove graceful shutdown and pidfile cleanup. It is Linux-only and skipped under `-short`.
- `mailbox_test.go`, `pidfile_test.go`, `lock_test.go`, `envelope_test.go`, and `ulid_test.go` cover filesystem mailbox behavior, pidfile read/write/remove, lock acquire/release, envelope serialization, and ULID ordering.
- `jobs/checkpoint_test.go`, `jobs/memory_consolidate_test.go`, `jobs/dispatch_test.go`, and `jobs/jobs_test.go` cover the job handlers. The checkpoint tests prove sibling-file writes, envelope-path acceptance, missing-path rejection, idempotence, and cancellation. The memory-consolidation tests prove unique appends, cutoff respect, idempotence, envelope-scope fallback, required dirs, and cancellation. Dispatch tests cover unknown kinds and timestamp filling.

The daemon suite has the best process-level coverage in the tree, but only one test actually crosses the process boundary.

### `internal/catalog`

- `cache_test.go` covers cache read/write round trips, missing-file behavior, schema rejection, and parent-directory creation.
- `load_test.go` covers cache-vs-embedded precedence, cache-miss fallback, corrupt-cache fallback, and the empty-envelope path when the embedded snapshot is skipped.
- `refresh_test.go` covers ETag handling, 304 behavior, stale-cache fallback on transport errors, fresh-cache TTL skipping, disabled refresh skipping, expired-context short-circuiting, loopback end-to-end refresh against a local HTTP server, missing-source-url behavior, fresh-writes, timeout handling, write failure fallback, and soft-TTL override.

These are HTTP fixture and state-machine tests. They do not hit the live catalog service.

### `internal/mcp`

- `config_test.go` covers config loading, disabled states, enabled-server filtering, env interpolation, missing-env errors, inline-secret rejection, unknown transport rejection, and unknown field rejection.
- `redact_test.go` covers secret scrubbing in server configs and string redaction.
- `permission_test.go` covers the allow-all authorizer and the noop audit hook.
- `manager_test.go` covers start/stop, tool/resource/prompt listing, resource reads, prompt retrieval, authorization ordering, denial blocking dispatch, unknown server handling, bad name handling, disabled config, namespaced tool naming, duplicate handling, and exclusion of unhealthy servers.

The MCP tests use fake servers. They prove manager wiring and policy, not a real MCP transport.

## Gaps and Missing Failure Modes

- Most suites are seam tests. The repo proves the control flow, data shape, and file-system effects much more than it proves real provider, terminal, clipboard, or audio integrations.
- The stale `TestAgentLoop` gate in `CLAUDE.md` is a real documentation bug. The current agent tests are all named differently.
- `internal/llm/cache_test.go` intentionally excludes `zai-anthropic` from cache markers until a live capture proves the provider should get them. That is an explicit coverage gap, not a mistake.
- `internal/ui/clipboard_test.go`, `internal/ui/mp3_test.go`, `internal/ui/render_test.go`, and `internal/ui/themes_test.go` are mostly shape checks. They confirm state transitions and string output, not actual device behavior.
- `internal/ui/models_command_test.go` stubs `loadModelCatalog`, so it does not prove the live catalog content or the remote picker experience.
- `internal/ui/model_turn_test.go` uses synthetic Bubble Tea key messages and synthetic agent events. It is good at turn boundaries and cancellation ladders, but it does not prove raw TTY timing.
- `internal/daemon/e2e_test.go` is Linux-only and skipped in `-short`. Everything else in `internal/daemon` is in-process.
- `internal/catalog/refresh_test.go` exercises the live HTTP path only against a local `httptest.Server`, not the real upstream service.
- `internal/mcp/manager_test.go` and `internal/llm/mcp_tools_test.go` use fake MCP servers, so they do not prove subprocess startup, transport auth, or long-lived remote server behavior.
- `internal/agent/context_test.go` has one intentionally weak test, `TestDiscoverContextFilesNone`, because the upward walk can see files outside the temp tree. It documents the limitation but does not pin a perfect negative case.
- `internal/tools/tools_test.go` has a known PTY comment about `vt.SafeEmulator` close-vs-read behavior. The test suite avoids the race by letting the process exit naturally instead of forcing `terminal_close` in a defer.
- I did not find a dedicated crash-corrupted JSONL test for session files that are truncated mid-line. The session suite is strong on valid legacy and v1 shapes, but it does not obviously fuzz partial-file corruption.

## What This Means In Practice

- If you touch the agent loop, run the agent, llm, and tools suites together. The fake streamer tests, typed-tool smoke, and conversion tests are the real contract.
- If you touch UI turn handling or slash commands, run `go test ./internal/ui` before the full tree. The cancel ladders, picker flow, session resume, clipboard routing, and transcript logger all live there.
- If you touch provider routing, cache markers, or tool schema translation, the llm suite is the load-bearing one.
- If you touch file, PTY, memory, or shell behavior, `internal/tools/tools_test.go` is the closest thing to a spec.
- If you touch daemon recovery or job handling, the daemon integration and e2e tests are the only ones that exercise the lock and mailbox model for real.
