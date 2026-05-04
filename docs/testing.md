# Testing

This is the test reference for `bitchtea`.

It documents what the suites actually prove, which fakes they use, which behaviors are covered by real failure modes, and which checks are only proving output shape.

## Required Gates

Before closing any code-changing issue, the repo expects all of these to pass:

```bash
go build ./...
go test ./...
go test -race ./...
go vet ./...
```

The main binary build is separate:

```bash
go build -o bitchtea .
```

For focused iteration, the project also documents targeted package runs such as:

```bash
go test ./internal/agent -run TestAgentLoop
go test ./internal/ui
```

Use `t.TempDir()` in tests. Do not write to `/tmp` manually. The suite assumes isolated temp homes, session dirs, and work dirs.

## What The Suite Does Not Do

The test suite is strong on local behavior, serialization, and concurrency, but it is not a substitute for live provider or shell integration:

- there is no real OpenAI or Anthropic network test
- there is no real MCP subprocess or HTTP server beyond fakes
- there is no real audio playback verification
- there is no real terminal rendering snapshot harness
- there is no real IRC network integration

When a test uses a fake streamer, fake server, or fake process, it is proving control flow and state mutation, not the external system.

## Fakes And Harnesses

The following harnesses are the core of the suite.

- `internal/agent/agent_loop_test.go`: `fakeStreamer`
  - `StreamChat` closes the events channel after one scripted response.
  - Each response is a function that writes a deterministic sequence of `llm.StreamEvent` values.
  - This is the main agent-loop harness for tool call, tool result, error, cancel, and follow-up flow.

- `internal/ui/model_turn_test.go`: `stubStreamer` and `singleReplyStreamer`
  - `stubStreamer` closes the stream immediately.
  - `singleReplyStreamer` emits one text event and `done`.
  - These isolate UI turn-boundary logic from provider timing.

- `main_test.go`: `headlessScriptedStreamer`
  - Drives `runHeadlessLoop` with scripted messages.
  - This proves headless turn sequencing without a live provider.

- `internal/agent/replay_test.go`: `fixtureStreamer`
  - Replays a fixed transcript from fixture data.
  - Used to prove replay and tool-loop semantics without network calls.

- `internal/agent/compact_test.go`: `blockingStreamer` and `summaryStreamer`
  - `summaryStreamer` returns a fixed summary turn.
  - `blockingStreamer` holds compaction in flight so cancellation paths can be asserted.

- `internal/llm/client_test.go`: `fakeProvider` and `blockingLanguageModel`
  - `fakeProvider` is only enough to populate the client cache.
  - `blockingLanguageModel` blocks inside `Stream` so the mutex/cache race surface can be exercised under `-race`.

- `internal/mcp/manager_test.go`: `fakeServer`, `recordingAuthorizer`, `recordingAudit`
  - `fakeServer` simulates MCP lifecycle, tool listing, prompts, resources, and per-server failures.
  - `recordingAuthorizer` records authorize-before-dispatch order and can deny a tool.
  - `recordingAudit` records `OnToolStart` and `OnToolEnd`.

- `internal/llm/mcp_tools_test.go`: `fakeMCPServer`
  - Exercises MCP-to-agent-tool assembly without a real MCP daemon.

- `internal/catalog/refresh_test.go`: `fakeClient`
  - Simulates HTTP fetch behavior for refresh/ETag/TTL logic.

- `internal/ui/models_command_test.go`: `stubCatalog`
  - Replaces the live model catalog with a fixture provider list.

- `internal/ui/clipboard_test.go`: `stubClipboard`
  - Stubs OSC52 and external clipboard binaries.

- `internal/ui/mp3_test.go`: `fakeMP3Process` and `stubMP3Globals`
  - Fakes the MP3 process control layer and global helpers.

- `internal/llm/typed_tool_harness_test.go`: `dummyEchoInput`
  - The in-test typed tool used to validate schema generation and error propagation.

- `internal/llm/debug_test.go`: `recordingRoundTripper`
  - Captures request and response bodies for debug redaction tests.

These are not interchangeable. Most of the suite is written around the exact behavior of these harnesses.

## Suite Map

### Root Command Tests

- `daemon_cli_test.go`
  - `TestDaemonStatusReportsNotRunning` and `TestDaemonStopReportsNotRunningWhenAbsent` assert that absent daemon state prints a message containing `not running` and exits 0.
  - `TestDaemonRejectsUnknownSubcommand` checks that an unknown subcommand returns non-zero and writes `unknown daemon subcommand` to stderr.
  - `TestDaemonHelpExitsZero` and `TestDaemonNoArgsPrintsUsage` check that usage text appears and that help exits 0.
  - These are output-contract tests, not daemon lifecycle tests.

- `main_test.go`
  - `TestRunHeadlessLoopFollowsAutoNextAndIdeaFlow` drives the headless loop with a scripted streamer. It verifies control-token redaction and the handoff between auto-next and auto-idea follow-up behavior.
  - `TestApplyStartupConfigRCProfileOverrideClearsProfile` and `TestApplyStartupConfigCLIModelOverrideClearsProfile` prove that startup overrides clear a loaded profile instead of letting stale profile state survive.

### `internal/config`

- `config_test.go`
  - Covers `DefaultConfig`, provider detection, profile save/load/delete, builtin profile resolution, profile listing, API-key emptiness rules, data-path migration, service identity, and lazy service migration by provider/host/fallback.
  - `TestMigrateDataPaths` and `TestMigrateDataPathsSkipsIfNewExists` are the real filesystem migration checks.
  - `TestProfileAllowsEmptyAPIKey` captures the one profile family that may run without a key.

- `rc_test.go`
  - Covers RC parsing, application of `set` commands from RC files, profile overrides, invalid provider handling, and boolean parsing.
  - This suite is about startup config wiring, not runtime command execution.

### `internal/catalog`

- `cache_test.go`
  - Verifies cache round-trip, missing-file behavior, schema rejection, and parent-directory creation.

- `load_test.go`
  - Proves cache-first loading, embedded fallback, empty-envelope fallback, and corrupt-cache fallback.

- `refresh_test.go`
  - Exercises the full refresh policy: 200 replace, 304 bump timestamp, transport error returns stale cache, fresh cache skips HTTP, expired context returns cached data when possible, disabled/no-source cases skip HTTP, missing cache writes fresh, write failures return in-memory results, and soft TTL overrides work.
  - `TestRefresh_AgainstHTTPTestServer_End2End` is the package's live HTTP smoke.

### `internal/session`

- `session_test.go`
  - Covers append-only JSONL creation, loading, listing, forking, tree rendering, latest lookup, checkpoint file writing, info display, context round-trip, focus save/load, missing-file fallback, bootstrap round-trip, and bootstrap filtering from display output.
  - This is the core persistence suite for session files.

- `membership_test.go`
  - Covers membership save/load round-trip and empty-file behavior.

- `session_fantasy_test.go`
  - This is the fantasy-native migration suite.
  - It proves v0 fixture compatibility, fantasy entry conversion, assistant tool-call preservation, multi-text lossiness, reasoning-part lossiness, media and error tool-result lossiness, JSON round-trip for v1 entries, mixed-session loading, skipping legacy tools without IDs, and bootstrap propagation.
  - The important point: some of this suite is intentionally validating lossiness. That is not a bug; it is documenting conversion boundaries.

### `internal/agent`

- `agent_loop_test.go`
  - This is the main turn-loop suite.
  - It proves injected streamers work, tool calls execute without network, context cancellation stops a turn, usage accounting is preserved when reported, thinking events are forwarded, stream errors still emit `done`, bootstrap message count is tracked, the system prompt mentions `search_memory`, live tool definitions are included, restored messages refresh tool definitions, follow-up auto-next and idea loops behave correctly, stale messages are not queued into the next turn, and done tokens are sanitized out of assistant text.
  - The fake streamer here is the canonical unit harness for the agent loop.

- `compact_test.go`
  - Proves compaction thresholds, summary insertion, retention of the system prompt and last four messages, flushing of daily memory before rewriting summary state, preservation of tool metadata, daily-memory path selection, hot-memory injection once per scope, and cancellation before stream start.
  - This suite is not just shape-checking; it is the compaction and memory-write contract.

- `context_test.go`
  - Covers context discovery, load/save memory, daily append files, hot memory search, scoped memory path layout, parent-scope inheritance without leaking child writes, search rendering, and file-ref expansion.
  - This is where memory scope semantics are pinned down.

- `messages_phase3_test.go`
  - This is the fantasy-message migration contract.
  - It proves bootstrap messages are fantasy-shaped, restore refreshes the system prompt, tool-call and tool-result parts survive a restore, turn transcripts survive in fantasy form, done-token sanitization works in fantasy parts, and compaction flushes fantasy messages to memory.

- `replay_test.go`
  - Replays fixed fixture transcripts through the agent loop.
  - It covers simple reply replay, tool-loop replay, and the no-panic path when the fixture is exhausted.

- `typed_tool_smoke_test.go`
  - A single end-to-end smoke for the phase-2 typed-tool migration.
  - It wires a real `llm.Client` to a fake fantasy language model, uses a real workspace file, and proves the typed `read` wrapper dispatches through the production path into the real registry and back into the agent as a tool result.

### `internal/llm`

- `client_test.go`
  - Proves `NewClient` copies fields without eagerly building a provider or model.
  - Proves each setter invalidates the cache when it should.
  - Proves `SetDebugHook(nil)` is a no-op only when the hook was already nil.
  - `TestClientConcurrentSetAndStream` is the important concurrency test: it runs multiple streamers and multiple setters at the same time to catch real mutex/race bugs.

- `convert_test.go`
  - Validates fantasy <-> LLM conversion invariants: splitting the system prompt, keeping assistant/tool tails intact, preserving tool-call order, handling nil/pointer parts, ignoring unknown part types, and round-tripping ordinary user/assistant/system/tool messages.
  - This suite is mostly structural, but it is the structural contract for the provider boundary.

- `stream_test.go`
  - Covers `safeSend` under open, buffered, and canceled-channel cases, plus `toolResultText` extraction from pointer/value/error/media variants.

- `providers_test.go`
  - Covers provider construction and routing across OpenAI-compatible, Anthropic, OpenRouter, ZAI, Ollama, and custom hosts.
  - Also covers URL parsing, `hostOf`, `hostOfMust`, service-based routing, and `stripV1Suffix`.

- `errors_test.go`
  - Exercises error-hint classification for nil, cancellation, provider status codes, context-too-large, wrapped provider errors, dialing failures, message fallbacks, and unknown errors.

- `cost_test.go`
  - Covers token-usage accumulation, cache isolation, cost estimation, and formatting.

- `debug_test.go`
  - Verifies request-body capture without consumption, JSON/plain response body capture, event-stream skipping, sensitive-header redaction, transport error handling, nil-response behavior, nil-hook no-op behavior, unknown content-type skipping, default transport selection, and a real HTTP server smoke.

- `tool_context_test.go`
  - Covers tool cancellation by turn, cancellation propagation, already-cancelled turns, cancel-all, cleanup, nonexistent-tool cancellation, concurrent access, concurrent cancel/new races, and cancel-during-use behavior.

- `cache_test.go`
  - Applies Anthropic cache markers and verifies the bootstrap index helper used by cache-boundary logic.

- `mcp_tools_test.go`
  - Proves MCP tool schema conversion, dispatch through the manager, error conversion, local-vs-MCP tool coexistence, shadowing rules, duplicate-name first-wins behavior, and end-to-end assembly.

- `tools_test.go`
  - Proves untyped tool translation preserves fantasy schema shape, the generic wrapper returns tool errors rather than Go errors, schema splitting handles malformed and already-split inputs, filtered required fields collapse to `nil` when appropriate, typed wrappers are used for ported tools, the real registry definitions produce valid schemas, and a streamed tool call executes with a valid schema.

- `typed_read_test.go`
  - Schema advertisement, successful read, EOF error, missing-file error, and cancellation for the typed `read` wrapper.

- `typed_write_test.go`
  - Schema advertisement, create/overwrite success, invalid path failure, and cancellation for the typed `write` wrapper.

- `typed_edit_test.go`
  - Schema advertisement, successful edit, empty-old-text failure, non-unique match failure, missing-file failure, and cancellation for the typed `edit` wrapper.

- `typed_bash_test.go`
  - Schema advertisement, successful run, non-zero exit reporting, parent-cancel reporting, running-process cancel reporting, timeout reporting, and UTF-8-safe truncation.

- `typed_search_memory_test.go`
  - Schema advertisement, successful query, registry-scope respect, empty-result behavior, and cancellation for typed `search_memory`.

- `typed_write_memory_test.go`
  - Schema advertisement, successful write using registry scope, root-scope override, missing content failure, and cancellation for typed `write_memory`.

- `typed_tool_harness_test.go`
  - This is the shared harness for typed wrappers.
  - It asserts schema generation, successful response conversion, tool-level error conversion, and the current cancellation contract: cancellation becomes a text error response, not a Go error.
  - The file comment explicitly says this contract is expected to change with the later cancellation redesign.

### `internal/tools`

- `tools_test.go`
  - This is the executor suite for the built-in tools.
  - It covers `read`, `write`, `edit`, `bash`, `search_memory`, `write_memory`, the terminal PTY family, `preview_image`, tool-definition listing, unknown-tool errors, and UTF-8 truncation boundaries.
  - It is the closest thing to a real tool integration test in the repo.

  - Exact output contracts that are pinned here:
    - `read` and `edit` success paths report resolved paths in their success message.
    - `bash` distinguishes command failure from timeout and cancellation.
    - truncation tests ensure output stays valid UTF-8 even at rune boundaries.

### `internal/mcp`

- `config_test.go`
  - Covers missing-file disablement, top-level disablement, per-server enablement filters, environment interpolation, missing env values, inline secret rejection, unknown transport rejection, and unknown-field rejection.

- `manager_test.go`
  - Covers startup success, partial startup failure, startup timeout, parallel stop, namespaced tool listing, invalid-name filtering, authorize-before-dispatch ordering, denial blocking dispatch, unknown-server errors, malformed-name errors, disabled config, resource listing, resource reads, read-size limits, prompt listing, prompt fetches, and unhealthy-server exclusion.
  - This suite is real failure-mode coverage, not just shape checking.

- `permission_test.go`
  - Proves the allow-all authorizer always returns nil, the default authorizer is allow-all, and the no-op audit hook is safe to call.

- `redact_test.go`
  - Verifies redaction of environment values and headers, scrubbing of resolved secrets, and empty-secret no-ops.

### `internal/ui`

The UI suite is large because it covers command parsing, state persistence, turn boundaries, rendering, and visible output. It is also the place where a lot of tests are substring-based rather than exact golden snapshots.

- `commands_test.go`
  - Alias lookup, command routing, unknown-command failure, removed root commands, `/set` compatibility routing, and channel-switching commands.

- `routing_commands_test.go`
  - Join/part/query/msg/channel routing and display-prefix behavior for direct messages.

- `command_validation_test.go`
  - Provider, base URL, API key, and model commands accept verbatim values.
  - Provider/base URL mismatches trigger warnings.
  - Profile/load commands suggest the provider command when the user typed a provider name.
  - `debug` and `activity` commands are covered here too.

- `set_command_test.go`
  - Shows all settings, shows a single setting, sets provider/model/base URL/API key/nick/service, handles unknown keys, masks API keys, preserves service labels, and verifies profile display/load/save semantics.

- `models_command_test.go`
  - Covers `/models` registration and help output, unset-service handling, empty catalog, unknown service, picker opening, case-insensitive service matching, picker selection, escape cancelation, filtering, empty-filter state, service enumeration, cursor and page movement, and default-large model deduplication.

- `invite_test.go`
  - Covers invite/join behavior, named-channel invite, missing-arg errors, DM refusal, idempotency, visible join notice and catch-up text, membership persistence, kick behavior, and channel-member listing.
  - The catch-up tests prove only the visible catch-up path, not agent-context injection.

- `membership_test.go`
  - Covers membership invite/part, cleanup of empty channels, sorted member lists, state round-trip, save/load, missing-file fallback, and context-key derivation.

- `context_test.go`
  - Covers channel/direct/subchannel context constructors, focus management, removal shifting, refusal to remove the last context, snapshotting, restore/load behavior, missing-file fallback, and active-index clamping.
  - The restore test only clamps oversized indexes. Negative indexes are not a covered failure mode.

- `resume_v1_test.go`
  - Covers resume from v0, resume from v1, mixed-format sessions, forking v1 sessions, tool-call panel stats on resume, legacy lossy entries, and multi-text flattening.

- `startup_rc_test.go`
  - Covers silent startup RC execution and the interaction between resume and startup commands.

- `signal_test.go`
  - Covers suspend handling and the Ctrl+C quit ladder.

- `restart_command_test.go`
  - Covers history/display clearing and help registration for `/restart`.

- `clipboard_test.go`
  - Covers copying the last assistant response, copying a selected assistant response, refusing non-assistant selections, and rejecting invalid or out-of-range selection numbers.

- `mp3_test.go`
  - Covers starting playback and showing the panel, playback keybindings, status text shape, and help text.
  - This is a fake-process suite, not a real audio integration test.

- `render_test.go`
  - Covers markdown detection, plain-text rendering, code blocks, narrow/wide widths, empty input, renderer-unavailable fallback, cache eviction, cache refresh, and ANSI-aware wrapping.
  - This is mostly substring and width behavior, not byte-exact terminal snapshots.

- `message_test.go`
  - Covers chat-message formatting, all message types producing non-empty output, long/empty content not panicking, and agent markdown rendering.

- `toolpanel_test.go`
  - Covers tool start/finish/error state, render behavior, clear, hidden render, result truncation, restore-session agent/tool-nick reconstruction, bootstrap-entry hiding, and the disabled theme command.
  - The render assertions are shape-based.

- `transcript_test.go`
  - Covers daily transcript logging, streaming assistant message buffering, finalizing the stream before tool output, omitting thinking messages, and not breaking the agent stream when thinking is ignored.

- `themes_test.go`
  - Covers built-in theme updates, non-empty style values, current theme name, thinking-bar colors, separator non-emptiness, and non-zero theme colors.

- `model_turn_test.go`
  - This is the strict turn-boundary suite.
  - It covers canceling the previous context when starting a new send, unqueuing with Up, the Ctrl+C ladder, the Esc ladder, stale agent-event suppression after channel replacement, checkpoint vs memory-file writes on agent completion, follow-up prompt selection, background-activity reporting, viewport cleanliness, thinking placeholders, queue draining behavior, and panel-close precedence over cancel ladders.
  - This suite is the best documentation of the current UI turn model.

### `internal/daemon`

- `envelope_test.go`
  - Round-trips job and result envelopes and rejects unknown fields / missing kind.

- `lock_test.go`
  - Covers fresh lock acquisition, lock contention, lock status, and release behavior.

- `pidfile_test.go`
  - Covers write/read, missing-file fallback, garbage rejection, and idempotent removal.

- `mailbox_test.go`
  - Covers submit/list, ULID ordering, completion, failure, and skipping temporary files and directories.

- `run_test.go`
  - Covers missing-handler rejection, lock-held failure, and pidfile writing.

- `jobs/jobs_test.go`
  - Covers unknown-job-kind errors and timestamp filling.

- `jobs/dispatch_test.go`
  - Covers a real checkpoint dispatch path and the unknown-kind path through the dispatcher.

- `jobs/checkpoint_test.go`
  - Covers sibling checkpoint file writes, session-path acceptance from the envelope, missing session path, idempotency, and cancellation.

- `jobs/memory_consolidate_test.go`
  - Covers unique-entry append behavior, since-cutoff filtering, idempotency, cancellation, envelope-scope fallback, and workdir/sessiondir requirements.

- `integration_test.go`
  - Covers stale lock recovery, lock contention between two daemons, crash recovery quarantining pre-existing mail, and the hook-based dispatch path.

- `e2e_test.go`
  - The full daemon smoke.
  - Builds `cmd/daemon`, starts it in an isolated `HOME`, submits a real checkpoint job, waits for completion, and SIGTERMs the process.
  - It is Linux-only and skipped in `-short`.

- `ulid_test.go`
  - Covers ULID length, alphabet, and time ordering.

### `internal/sound`

- `sound_test.go`
  - This is one of the few exact-byte suites.
  - `bell`, `done`, `success`, and default output are exactly `\a`.
  - `error` is exactly `\a\a\a`.
  - The helpers are verified against the global `Output` writer.

## Exact Output And Shape Checks

Most tests in the repo use substring checks, role checks, and state assertions instead of byte-for-byte snapshots. That is intentional.

Exact output is asserted most often in:

- `daemon_cli_test.go`
- `internal/sound/sound_test.go`
- JSON/file persistence tests in `internal/session`, `internal/daemon`, and `internal/catalog`
- the tool-executor tests when they care about resolved paths or error text

Shape-only or mostly-shape tests include:

- `internal/ui/message_test.go`
- `internal/ui/render_test.go`
- `internal/ui/toolpanel_test.go`
- `internal/ui/themes_test.go`
- `internal/ui/mp3_test.go`
- `internal/ui/clipboard_test.go`
- `internal/llm/convert_test.go`
- parts of `internal/agent/messages_phase3_test.go`
- parts of `internal/session/session_fantasy_test.go`

That does not make them useless. It means they pin layout, invariants, and conversion shape, not the exact terminal raster or exact provider transcript.

## Missing Failure Modes And Gaps

The important missing edges are:

- no provider-network integration test
- no real shell/PTY integration test beyond the tool harness
- no real audio renderer integration test
- no full terminal snapshot suite
- no external MCP server process test
- no negative-index test for UI focus restore
- no live agent-context injection test for `/invite` catch-up
- no direct test for the now-unused `saveCurrentContextMessages` helper
- no live assertion that `ToolPanel.Clear()` is wired into the running turn loop
- no full-provider serialization test across `llm.Client.StreamChat` with a real upstream service

The test suite is still good. It just needs a human to remember which parts are contract tests and which parts are only harness tests.
