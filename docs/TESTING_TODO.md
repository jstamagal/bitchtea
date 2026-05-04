# BitchTea Testing Audit TODO

Audit date: 2026-05-04. Source of truth: live test files in this repo. Each finding cites a test name + file:line.

This is the exhaustive punch list of test suites that verify the **shape** of returned values (struct fields, mock-bouncing, canned responses) without exercising **failure modes**, OS-level behavior, real concurrency, or production wiring. Each section also lists the concrete tests that should be written.

Sections:
1. Executive Summary
2. The Six Recurring Shape-Test Anti-Patterns
3. Critical: Missing Test Files Entirely
4. Per-Package Shape-Only Inventory
5. Disconnected-Wiring Tests Nobody Catches
6. Concrete Tests to Write (grouped + named)
7. Prioritized Punch List

Companion to `docs/AUDIT_TODO.md` (which audits the docs); this audits the tests.

---

## 1. Executive Summary

- **71 test files** scanned across the repo.
- **~185 individual tests** identified as shape-only (assert struct shape, mock returns, or substring matches but never exercise the real failure path).
- **~400 uncovered failure modes** spanning concurrency, OS-level errors, malicious input, partial-write recovery, real PTY, real signal delivery, real network.
- **`internal/memory/` has ZERO tests** despite being the flock-sensitive backbone of agent memory persistence.
- **Several severe disconnected-wiring bugs would be caught by trivial integration tests** but are caught by none.

The codebase tests "this function returns a value." It does not test "this system handles failure."

---

## 2. The Six Recurring Shape-Test Anti-Patterns

These anti-patterns recur across the entire suite. Fixing the underlying habit is more leveraged than chasing every individual test.

### A. `tea.Cmd` returned, never executed

UI tests call `m.Update(tea.KeyMsg{...})` and assert on the returned `tea.Cmd` shape (or that `cmd != nil`). They do not run the cmd against a real `tea.Program`, so any mutation that depends on the program loop is invisible. This pattern hides the `/debug on` value-receiver bug, the `m.queued` drain disconnect, and the focus restoration race.

### B. Substring assertions instead of structural assertions

`render_test.go`, `message_test.go`, `themes_test.go`, `set_command_test.go`, and most slash-command tests assert `strings.Contains(out, "expected")`. This passes when the output has anything resembling the expected fragment, even if the surrounding ANSI/layout is broken.

### C. Mocked streamer / pre-canned tool result

`agent_loop_test.go`, `compact_test.go`, `replay_test.go`, `typed_tool_smoke_test.go` all use a `fakeStreamer` that returns events from a fixture. They exercise the dispatcher logic but not real LLM HTTP, real provider error classification, retry backoff, or real tool latency.

### D. Round-trip without contention

`session_test.go`, `membership_test.go`, `cache_test.go`, `pidfile_test.go`, `lock_test.go`, `envelope_test.go`, `mailbox_test.go` write a value and read it back. They do not test concurrent writers, partial writes, disk-full ENOSPC, readonly directories, or corrupted state.

### E. Happy-path provider construction

`providers_test.go`, `client_test.go`, `cache_test.go`, `cost_test.go` assert non-nil/non-error. They do not invoke the constructed provider's HTTP path, do not test concurrent `SetX` + `StreamChat`, and do not test factory panics.

### F. Single-character or single-event input

`mp3_test.go`, `models_command_test.go`, `model_turn_test.go`, `signal_test.go`, picker tests fire one synthetic key, observe one returned model. They do not test rapid key bursts, signal coalescing, or the multi-press cancel ladder under timing pressure.

---

## 3. Critical: Missing Test Files Entirely

### `internal/memory/` — ZERO test coverage

The package implements the flock-sensitive backbone of agent memory persistence (~465 lines: `memory.go`, scope lineage, daily file rotation, search). It has no `*_test.go` file at all.

**Tests that must be written:**

- `TestAppendHotConcurrentFlock` — two goroutines call `AppendHot` on the same scope; both entries present, ordered.
- `TestAppendDailyForScopeFlock` — concurrent `AppendDailyForScope`, atomic writes.
- `TestScopeLineageCircular` — scope with parent pointing to itself; `lineage()` must terminate.
- `TestScopeLineageDepth` — 100-level deep scope tree; `lineage()` finishes promptly.
- `TestScopeRelativePathSanitization` — scope name with spaces, slashes, `..`; verify safe path.
- `TestMemoryBaseDirHashCollision` — two distinct `workDir`s producing the same hash.
- `TestSearchWithZeroLimit` / `TestSearchWithNegativeLimit` — limit defaults to 5.
- `TestSearchInScopeWalksLineage` — channel-scope search includes parent scope hits.
- `TestDailyFileDateBoundary` and `TestDailyFileYearBoundary` — files split correctly across midnight and Dec→Jan.
- `TestRootScopeAlwaysReturnsMemoryMd` — root `HotPath` resolves to `workDir/MEMORY.md`.
- `TestLoadScopedWithMissingFile` — empty string, no error.
- `TestSaveScopedCreatesParentDirs` — nested scope creates full tree.
- `TestMemoryConsolidateRaceWithAppendHot` — `internal/daemon/jobs/memory_consolidate.go` writes `HOT.md` *without* flock while `AppendHot` does use flock; verify what the divergence does under contention.
- `TestInjectedPathsResetSemantics` — currently no reset on `RestoreMessages`; pin the behavior.

### `internal/agent/context_test.go`, `messages_phase3_test.go`, `typed_tool_smoke_test.go`

Per Agent 1: these files appear to be empty / package-decl only. Per `docs/testing.md` they are listed as containing real tests. Either the audit found stripped files or the docs are wrong. **Action:** verify file contents and reconcile.

### `cmd/cpm/`

Empty directory — no source, no tests. Either implement, document the placeholder, or delete.

---

## 4. Per-Package Shape-Only Inventory

Each row pairs a test name with a 1-line reason it's shape-only. File paths are abbreviated (drop `internal/` prefix).

### agent/

| Test (file:line) | Why shape-only |
| --- | --- |
| `TestCompactNoOpWhenFewerThanSixMessages` (`agent/agent_loop_test.go:61`) | Asserts message count boundary; no LLM round-trip. |
| `TestCompactNoOpExactlySixMessages` (`agent_loop_test.go:83`) | Counts streamer calls; doesn't validate summary content. |
| `TestCompactRetainsSystemPromptAndLastFour` (`agent_loop_test.go:99`) | Index/role check; mocks summarization. |
| `TestCompactSummaryInsertedProperly` (`agent_loop_test.go:147`) | Role + prefix check; mocked streamer. |
| `TestCompactPreservesToolMetadata` (`agent_loop_test.go:217`) | Field check on a fabricated tool message. |
| `TestDiscoverContextFilesNone` (`compact_test.go:30`) | Placeholder; reads nothing. |
| `TestLoadSaveMemory` (`compact_test.go:39`) | Round-trip; no concurrent access. |
| `TestAppendDailyMemory` (`compact_test.go:60`) | Substring + heading check; no atomicity test. |
| `TestSearchMemoryFindsHotAndDurableMarkdown` (`compact_test.go:84`) | Mocks file structure; no FS race. |
| `TestScopedMemoryPathsUseChannelAndQueryLayout` (`compact_test.go:128`) | Path string format only. |
| `TestScopedMemorySearchInheritsParentsWithoutLeakingChildWrites` (`compact_test.go:148`) | Result shape; no concurrent scope mutation. |
| `TestRenderMemorySearchResults` (`compact_test.go:202`) | Canned input → string formatting. |
| `TestExpandFileRefs` (`compact_test.go:220`) | Substring check on injection. |
| `TestReplaySimpleReply` (`replay_test.go:69`) | Event type/content match against fixture; no malformed-stream test. |
| `TestReplayToolLoop` (`replay_test.go:104`) | Presence of event types; no ordering or failure injection. |
| `TestReplayExhaustedFixtureDoesNotPanic` (`replay_test.go:156`) | Boundary; no fixture corruption test. |

### llm/

| Test | Why shape-only |
| --- | --- |
| `TestNewClientCopiesFields` (`client_test.go:41`) | Field-copy assertion only. |
| `TestSetAPIKey/BaseURL/Model/Provider/DebugHookInvalidatesCache` (`client_test.go:51-122`) | All check cache nil after `Set`; no concurrent `Set + Stream` during invalidation. |
| `TestSetDebugHookNilNoOpKeepsCache` (`client_test.go:124`) | Cache shape only. |
| `TestClientConcurrentSetAndStream` (`client_test.go:216`) | Mutex smoke; uses blocking fake — no real network errors or factory panics. |
| All `TestSplitForFantasy*` (`convert_test.go:10-116`) | Array length / role order; no malformed input. |
| All `TestFantasyToLLM*` and `TestLLMToFantasy*` (`convert_test.go:118-688`) | Round-trip with valid input; no nil parts, no >100KB content. |
| All `TestCostTracker*` and `TestCatalogPriceSource*` (`cost_test.go:14-344`) | Numeric assertions on canned input; no negative tokens, NaN, division by zero. |
| All `TestDebugRoundTripper_*` (`debug_test.go:60-413`) | Body/header captured against canned content; no streaming-large-body, hook panic, concurrent RT. |
| All `TestErrorHint*` (`errors_test.go:14-99`) | Substring rules tested with canned inputs; no nested wraps, no non-Canceled context error, no status 0/600+. |
| All `TestMCPTools_*` and `TestAssembleAgentTools_*` (`mcp_tools_test.go:108-471`) | Shape of dispatch; no manager panic, no oversized schema, no concurrent dispatch. |
| All `TestBuildProvider_*` and `TestRouteOpenAI_*` (`providers_test.go:10-340`) | Non-nil / no-error assertions; no real HTTP path, no IPv6/auth/port edge cases. |
| All `TestSafeSend_*` and `TestToolResultText_*` (`stream_test.go:23-209`) | Canned event flow; no nil channel, closed channel, concurrent send, custom subtypes. |
| All `TestToolContextManager_*` (`tool_context_test.go:11-247`) | State assertion; no double-cleanup, nil parent, deadline-vs-cancel race. |
| `TestTypedToolHarness_*` (`typed_tool_harness_test.go:166-222`) | Schema shape + canned echo; no panic recovery, oversized input, missing required field. |
| `TestReadTool_*` (`typed_read_test.go:31-133`) | Content substring + cancel; no path traversal, no symlink escape, no TOCTOU. |
| `TestWriteTool_*` (`typed_write_test.go:31-165`) | File-existence + content; no traversal, ENOSPC, hard-link, atomic-rename. |
| `TestEditTool_*` (`typed_edit_test.go:30-182`) | Mutation/cancel; no overlapping edits, CRLF, Unicode normalization, recursion when `newText` contains `oldText`. |
| `TestBashTool_*` (`typed_bash_test.go:36-190`) | Output substring + cancel; no PATH miss, no zombie reaping, no SIGTERM-ignored→SIGKILL fallback, no embedded null bytes. |
| `TestSearchMemoryTool_*` (`typed_search_memory_test.go:37-160`) | Hit assertion; no malformed memory file, no regex DOS, no concurrent scope change. |
| `TestWriteMemoryTool_*` (`typed_write_memory_test.go:34-161`) | Scope file existence; no concurrent writers, no path-traversal scope name, no >100MB content. |
| `TestApplyAnthropicCacheMarkers_*` (`cache_test.go:141-200`) | Marker-presence check; no `SetService` mid-stream invalidation, no negative `bootstrapMsgCount`, no real Anthropic 400. |
| `TestBootstrapPreparedIndex` (`cache_test.go:205`) | Truth table for index math; no live marker placement test. |
| `TestTranslateTools*` and `TestSplitSchema*` (`tools_test.go:17-296`) | Schema shape; no >1000 properties, no circular refs, no concurrent registry mutation. |
| `TestStreamChatSendsValidToolSchemaAndExecutesToolCall` (`tools_test.go:504`) | End-to-end shape only; no tool panic, no registry mutation mid-call. |

### ui/

| Test | Why shape-only |
| --- | --- |
| `TestCopyCommand*` (`clipboard_test.go`) | Stubs `writeOSC52Clipboard` and `runClipboardCommand`; no real terminal escape, no fallback chain. |
| `TestLookupSlashCommandSupportsAliases`, `TestHandleCommandUsesAliasRegistry` (`commands_test.go`) | Direct handler call; no `tea.NewProgram`, no event pump. |
| `TestProvider/BaseURL/APIKey/ModelAcceptsAnyValueVerbatim` (`command_validation_test.go`) | Asserts `model.config.X == wantStored`; never reads `model.agent.Provider()` etc. — would catch the `/set` non-propagation bug. |
| `TestChannel/Subchannel/Direct` and `TestFocusManager_ActiveIndex_clamped` (`context_test.go`) | Field asserts; only positive overflow clamp tested. Negative index unverified — known bug. |
| `TestInviteCommandJoinsPersonaToCurrentChannel`, `TestBuildChannelCatchup_*` (`invite_test.go`) | `IsJoined` boolean + substring presence in catch-up text; no persistence error path. |
| `TestMembershipManager_*` (`membership_test.go`) | Round-trip; no concurrent save/load, no corrupted JSON. |
| `TestChatMessageFormat`, `TestAllMsgTypesFormatNonEmpty` (`message_test.go`) | Substring presence; no width-overflow, no embedded ANSI, no long-nick alignment. |
| `TestModelsCommandOpensPicker` (`models_command_test.go`) | `model.picker != nil`; no key routing through `Update`. |
| `TestSendToAgentCancelsPreviousContext` (`model_turn_test.go`) | Mock `m.cancel` and check call; no real ctx propagation to streamer. |
| `TestUpUnqueuesLastQueuedMessage` (`model_turn_test.go`) | Slice membership; no input-field side effects, no event ordering. |
| `TestCtrlCCancelsTurnWithoutClearingQueue` (`model_turn_test.go`) | Stubbed cancel; no real `context.Context` deadline check. |
| `TestMP3CommandStartsPlaybackAndShowsPanel`, `TestMP3KeybindingsControlPlayback` (`mp3_test.go`) | Mock `mp3ProcessStarter`; no SIGSTOP/SIGCONT delivery, no real PTY backpressure. |
| `TestLooksLikeMarkdown`, `TestRenderMarkdown*`, `TestWrapTextANSIAware` (`render_test.go`) | Substring + truth table; no max-visible-width verification across widths 5–200. |
| `TestRestartCommandClearsHistoryAndDisplay` (`restart_command_test.go`) | Length asserts; no bootstrap-leak check, no on-disk reset. |
| `TestResumeFromV0FixtureFile`, `TestResumeFromV1Fixture`, `TestResumeV1ToolCallPopulatesPanelStats` (`resume_v1_test.go`) | Length + nick assertion; no tool-call-ID mismatch, no fork-mid-session test. |
| `TestHandleJoinCommand_switchesFocus`, `TestRouteMessage_directFocusPrefixesDisplay` (`routing_commands_test.go`) | Direct call; comments admit "can't easily mock sendToAgent". |
| `TestSetCommand*` (`set_command_test.go`) | `model.config.X` checks only; never reads `model.agent.X` — would catch the propagation bug. |
| `TestSuspendMsgHandling`, `TestQuitMsgCancelsStreaming` (`signal_test.go`) | `cmd != nil` + `cancelCalled == true`; no real SIGINT delivery, no handler restoration check. |
| `TestExecuteStartupCommandRunsSilently` (`startup_rc_test.go`) | Single command; no rc-parse-error path, no read-error path. |
| `TestBuiltInThemeUpdatesStyles`, `TestThemeAllColorsNonZero` (`themes_test.go`) | Non-empty assertion; no ANSI-validity, no narrow-width render. |
| `TestToolPanelStartFinish`, `TestToolPanelRender`, `TestResumeSessionRestoresAgentMessagesAndToolNick` (`toolpanel_test.go`) | Status-string checks; no UTF-8 truncation, no tool-name overflow. |
| `TestTranscriptLogger*` (`transcript_test.go`) | Substring presence; no rotation across midnight, no concurrent-writer corruption. |

### tools/

| Test | Why shape-only |
| --- | --- |
| `TestReadFile` (`tools_test.go`) | Offset/limit shape; no permission denied, no deletion mid-read, no symlink escape. |
| `TestBash` (`tools_test.go`) | Output equality; no PTY allocation failure, no signal-to-child, no large-output rune-boundary. |
| `TestTerminalSessionEcho` (`tools_test.go`) | Mocked PTY echo; no real PTY exhaustion, no resize race, no TTY mode setup. |
| `TestBashCancelledContextReportsCancelNotTimeout` (`tools_test.go`) | Strong test for cancel-vs-timeout wording, but doesn't verify SIGTERM delivery to child. |

### daemon/, daemon/jobs/

| Test | Why shape-only |
| --- | --- |
| `TestDaemonStatusReportsNotRunning`, `TestDaemonStop*`, `TestDaemonRejectsUnknownSubcommand`, `TestDaemonHelpExitsZero`, `TestDaemonNoArgsPrintsUsage` (`daemon_cli_test.go:9-78`) | Output-string checks; no real subprocess, no concurrent start/stop, no signal handling. |
| `TestJobRoundTrip`, `TestUnmarshalJobRejectsUnknownFields`, `TestMarshalJobRequiresKind`, `TestResultRoundTrip` (`envelope_test.go:10-82`) | JSON shape; no expired Deadline, no future SubmittedAt, no malformed Args RawMessage. |
| `TestAcquireSucceedsOnFreshLock`, `TestIsLockedReportsHeld`, `TestIsLockedAfterReleaseReturnsFalse` (`lock_test.go:9-76`) | Lock-acquired/released boolean; no fd-closed-before-release, no parent-dir-missing. |
| `TestSubmitAndList`, `TestListSortedByULID`, `TestComplete`, `TestFail`, `TestListSkipsTmpAndDirs` (`mailbox_test.go:23-160`) | Round-trip in-process; no readonly baseDir, no done/-collision, no corrupted-envelope. |
| `TestWriteAndReadPid`, `TestReadPidMissing`, `TestReadPidGarbage`, `TestRemovePidIdempotent` (`pidfile_test.go:10-55`) | Round-trip; no negative PID, no overflow, no parent-dir-missing. |
| `TestRunRejectsJobsWithoutHandler`, `TestRunFailsWhenLockHeld`, `TestRunWritesPidFile` (`run_test.go:19-139`) | Job→failed/ shape; no missing-mail/-auto-creation, no logger-panic, no drainBudget=0. |
| `TestNewULIDLength`, `TestNewULIDAlphabet`, `TestULIDsAreOrderedByTime` (`ulid_test.go:9-31`) | 26-char + alphabet + two-call ordering; no concurrent uniqueness across 1000+ generations, no time-jump-backward test. |
| `TestCheckpoint*` (`jobs/checkpoint_test.go:51-188`) | Sidecar JSON shape; no growing-session-during-handler, no malformed entries, no readonly workDir. |
| `TestHandleUnknownKindReturnsNoHandlerError`, `TestHandleFillsTimestamps` (`jobs/jobs_test.go:16-47`) | Error wrapping + timestamp backfill; no nil job, no error-chain wrapping. |
| `TestMemoryConsolidate*` (`jobs/memory_consolidate_test.go:75-278`) | Counts/idempotency/scope override — but **does not test flock**, and `memory_consolidate.go` writes `HOT.md` *without* flock, diverging from `AppendHot`. This is a real race; the test does not exercise it. |

### session/, config/, catalog/, mcp/, sound/

| Test | Why shape-only |
| --- | --- |
| `TestSaveMembership_*`, `TestLoadMembership_missingFileReturnsEmpty` (`session/membership_test.go:7-63`) | Round-trip; no corruption, no readonly dir. |
| `TestNewAndAppend`, `TestLoadSession`, `TestFork`, `TestTree`, `TestInfo` (`session/session_test.go:11-321`) | Round-trip + counts; no append-after-delete, no readonly-dir, no Fork-with-invalid-ID, no Tree structural verification (Tree() is linear; not asserted). |
| `TestEntryFromFantasy*`, `TestV1EntryRoundTrip*`, `TestMixedSessionFile` (`session/session_fantasy_test.go:94-408`) | Lossless flag + presence of fields; no missing-Role v0 entry, no malformed `tool_calls` JSON, no `BranchTag` propagation (Fork ignores BranchTag — never tested). |
| `TestDefaultConfig`, `TestDetectProvider*`, `TestProfileSaveLoadDelete` (`config/config_test.go:11-150`) | Field shape; no env-var precedence test, no `MigrateDataPaths`, no `ProfileAllowsEmptyAPIKey` enforcement, no readonly ProfilesDir. |
| `TestParseRCFile`, `TestApplyRCSetCommands*`, `TestParseBoolSetting` (`config/rc_test.go:9-165`) | Comment/blank filtering; no quoted values, no env expansion, no unknown-command behavior. |
| `TestWriteCacheReadCacheRoundTrip`, `TestReadCache*`, `TestWriteCacheCreatesParentDir` (`catalog/cache_test.go:13-82`) | Round-trip + schema reject; no partial-JSON, no perms-changed-mid-read. |
| `TestLoad_*` (`catalog/load_test.go:11-66`) | Selection of cache vs embedded; no missing-and-skip-embedded combination. |
| All `TestRefresh_*` (`catalog/refresh_test.go:37-452`) | Strong HTTP+ETag+TTL coverage — but no malformed-200 body, no partial-body, no `If-None-Match` header verification, no 301/302 redirect. |
| `TestLoadConfig_*` (`mcp/config_test.go:27-174`) | Round-trip + env interpolation; no malformed JSON, no cyclic env vars. |
| `TestManager_*` (`mcp/manager_test.go:174-605`) | Strong lifecycle/dispatch coverage — but no concurrent `CallTool`, no oversized args, no cancellation-mid-resource-fetch, no duplicate tool name across servers. |
| `TestAllowAllAuthorizer_AlwaysNil`, `TestDefaultAuthorizer_IsAllowAll`, `TestNoopAuditHook_DoesNotPanic` (`mcp/permission_test.go:12-36`) | Trivial; pin-only future stricter authorizer policy gap. |
| `TestRedactServer_*`, `TestRedactString_*` (`mcp/redact_test.go:12-86`) | Substring redaction; no regex-special-char in secret, no perf with very long secret. |
| `TestPlayWritesExpectedBellPattern`, `TestHelpersWriteToOutput` (`sound/sound_test.go:8-63`) | Bell-byte counts; no invalid soundType, no nil Output. |

### root: main_test.go, daemon_cli_test.go

| Test | Why shape-only |
| --- | --- |
| `TestRunHeadlessLoopFollowsAutoNextAndIdeaFlow` (`main_test.go:43`) | Output stream check; no real agent loop iteration, no signal mid-stream. |
| `TestApplyStartupConfigRCProfileOverrideClearsProfile`, `TestApplyStartupConfigCLIModelOverrideClearsProfile` (`main_test.go:93-176`) | `cfg.X` field check; no `MigrateDataPaths` invocation, no `ProfileAllowsEmptyAPIKey` enforcement, no RC-and-CLI both-set ordering. |

### Flagged HIGH-quality (still incomplete)

These suites do real integration work but still leave important failure modes uncovered.

- `internal/daemon/e2e_test.go::TestDaemonE2E` — real subprocess + envelope round-trip. **Missing:** crash-recovery variant.
- `internal/daemon/integration_test.go::{TestStaleLockRecovered, TestLockContentionTwoDaemons, TestCrashRecoveryQuarantinesPreExistingMail, TestRunDispatchesViaHook}` — real lock/dispatch plumbing. **Missing:** dispatcher timeout, write-failure recovery, lock-file permission denied.
- `internal/daemon/jobs/dispatch_test.go::TestRunDispatchesCheckpointJob` — real envelope→done/. **Missing:** dispatcher panic, oversize output, dispatcher hang.
- `internal/catalog/refresh_test.go` — broad HTTP/TTL/ETag coverage. **Missing:** malformed-JSON 200, partial body, redirect.
- `internal/mcp/manager_test.go` — broad lifecycle. **Missing:** concurrent dispatch, oversized args, duplicate tool name.

---

## 5. Disconnected-Wiring Tests Nobody Catches

Each row is a real bug from `docs/AUDIT_TODO.md` that no test suite catches. Each can be caught by a single targeted integration test.

| Bug | Where | Test that would catch it |
| --- | --- | --- |
| `/set service` doesn't call `agent.SetService` | `internal/ui/commands.go:847-857` | `TestSetServiceUpdatesAgent` — assert `model.agent.Service() == "openai"` after `/set service openai`. |
| `/set provider`, `/set baseurl`, `/set apikey` don't propagate to agent | `internal/ui/commands.go` (handleProvider/BaseURL/APIKeySet) | `TestSetProviderPropagatesToAgent`, `TestSetBaseURLPropagatesToAgent`, `TestSetAPIKeyPropagatesToAgent`. |
| `/profile load` doesn't call `agent.SetService` | `internal/ui/commands.go::applyProfileToModel:812-836` | `TestProfileLoadPropagatesService`. |
| `/debug on` captures `m Model` by value | `internal/ui/commands.go:306-336` | `TestDebugOnViaTeaProgram` — run `/debug on` through `tea.NewProgram`; assert internal model state mirrors returned model. |
| `m.queued` doesn't call `Agent.QueuePrompt` | `internal/ui/model.go:531-541` | `TestQueueDrainCallsAgentQueuePrompt` — fire Enter while streaming; assert `agent.QueuePrompt` invoked. |
| MCP integration wired but never bootstrapped | `Agent.SetMCPManager`/`Client.SetMCPManager` have zero production callers | `TestMCPManagerBootstrappedAtStartup` — start the binary with an `mcp.json`; assert `Agent.MCPManager()` non-nil. (Currently fails — confirms the disconnect.) |
| Daemon `Mailbox.Submit` has zero production callers | `internal/daemon/mailbox.go::Submit` | `TestSessionCheckpointTriggersDaemonSubmit` — checkpoint a session in-process; assert envelope appears in `mail/`. (Currently fails — confirms the disconnect.) |
| Anthropic cache markers skipped for `zai-anthropic` profile | `internal/llm/cache.go::applyAnthropicCacheMarkers` (string-equal `"anthropic"`) | `TestApplyAnthropicCacheMarkers_ZaiAnthropicGap` — assert markers placed on zai-anthropic if intended, or assert absence + add doc note. |
| `FocusManager.RestoreState` floors negative `ActiveIndex` only when `>= len()`, not `< 0` | `internal/ui/context.go:176-179` | `TestFocusManagerNegativeIndexFloored` — set `ActiveIndex = -5`; assert no panic, `active == 0`. |
| `ToolPanel.Clear()` has no production caller (dead code) | `internal/ui/toolpanel.go:70` | `TestNoProductionCallerOfToolPanelClear` — grep gate or static linter; either remove method or wire it. |
| `saveCurrentContextMessages` has no production caller (dead code) | `internal/ui/context_helpers.go:25` | Same — remove or wire. |
| `Session.Fork` ignores marshal errors and never sets `BranchTag` | `internal/session/session.go:202-205` | `TestForkPropagatesBranchTag` — call `Fork(ID, "experiment")`; assert resulting session has `BranchTag == "experiment"`. (Currently absent.) |

---

## 6. Concrete Tests to Write (grouped + named)

These are de-duplicated, prioritized, and grouped by failure-mode family. Names are concrete; each line states the failure mode being proved.

### 6.1 OS-level + filesystem failure (P0)

- `TestReadToolPathTraversal` — `read` of `../../etc/passwd` rejected.
- `TestReadToolSymlinkEscape` — symlink pointing outside `workDir` rejected.
- `TestReadToolFileRemovedMidRead` — TOCTOU between stat and read.
- `TestWriteToolPathTraversal` — `write` of `../../etc/passwd` rejected.
- `TestWriteToolDiskFull` — ENOSPC during write.
- `TestWriteToolAtomicityViaRename` — process dies mid-write; recovery leaves file consistent.
- `TestEditToolNewTextContainsOldText` — recursion / infinite-loop guard.
- `TestEditToolUnicodeNormalization` — combining-character mismatch.
- `TestEditToolCRLFvsLF` — newline conventions.
- `TestBashToolCommandNotFound` — bash not in PATH.
- `TestBashToolSignalDeliveryToChild` — SIGTERM kills child, not just parent.
- `TestBashToolEmbeddedNullBytes` — null-byte safety in truncation.
- `TestBashToolLargeOutputRuneBoundaryMidEmoji` — 1MB multibyte output truncated cleanly.
- `TestTerminalPTYExhaustion` — try to allocate 100 sessions; verify graceful error.
- `TestTerminalSessionResizeRace` — resize while pump active.
- `TestSessionAppendToDeletedFile` — load, delete, append; verify behavior.
- `TestSessionAppendReadonlyDir` — readonly dir; append errors gracefully.
- `TestSessionForkInvalidID` — fork with ID not in entries; error.
- `TestPidfileWriteParentDirMissing` — autocreate or error cleanly.
- `TestPidfileNegativeOrZero` — file contains `-1` or `0`; ReadPid errors.
- `TestLockReleaseClosedFD` — close fd before Release; error path.
- `TestLockAcquireMissingParentDir` — autocreate or error.
- `TestMailboxReadonlyBaseDir` — Submit errors instead of corrupting state.
- `TestMailboxCompleteCollision` — pre-existing `done/<id>.json` when another job tries to Complete.

### 6.2 Concurrency + races (P0/P1)

- `TestMemoryAppendHotConcurrentFlock` — verify both writers' lines present, ordered.
- `TestMemoryConsolidateRaceWithAppendHot` — daemon raw-write vs `AppendHot`'s flock.
- `TestSessionConcurrentAppend` — two goroutines append; verify file integrity.
- `TestMembershipConcurrentInviteAndSave` — race detector clean.
- `TestModelPickerRapidKeyPresses` — 50 rapid keys; verify no panic.
- `TestCtrlCSequenceUnderRealProgram` — triple-tap window via `tea.NewProgram` synthetic events.
- `TestConcurrentJoinCommands` — 5 goroutines `/join`; no corruption.
- `TestClientConcurrentSetAndBuildProviderPanic` — provider factory panics under contention.
- `TestSafeSendConcurrentSenders` — multiple writers to same channel.
- `TestToolContextManagerCancelThenCleanup` — ordering race.
- `TestULIDUniquenessUnderConcurrency` — 100 goroutines × 10 ULIDs; all unique.
- `TestCatalogPriceSourceConcurrentRefresh` — refresh during `Lookup`.

### 6.3 Production wiring (P0)

- `TestSetServiceUpdatesAgent` — `/set service`.
- `TestSetProviderUpdatesAgent`, `TestSetBaseURLUpdatesAgent`, `TestSetAPIKeyUpdatesAgent`.
- `TestProfileLoadPropagatesService`.
- `TestDebugOnViaTeaProgram` — value-receiver bug.
- `TestQueueDrainCallsAgentQueuePrompt`.
- `TestMCPManagerBootstrappedAtStartup` — wire `Agent.SetMCPManager` from `main.go`.
- `TestSessionCheckpointTriggersDaemonSubmit` — connect daemon producer.
- `TestRestartSetsNewSessionPath`.
- `TestRestartBootstrapMessagesIntact`.
- `TestForkPropagatesBranchTag`.
- `TestNoProductionCallerOfToolPanelClear` (lint or static).
- `TestNoProductionCallerOfSaveCurrentContextMessages` (lint or static).

### 6.4 Malformed input + adversarial (P1)

- `TestRefreshMalformedJSONResponse` — HTTP 200 + invalid body.
- `TestRefreshPartialBody` — connection cut mid-stream.
- `TestRefreshETagHeaderSent` — verify `If-None-Match` actually sent.
- `TestLoadConfigMalformedJSON` — MCP config invalid.
- `TestLoadConfigCyclicEnvVars` — cycle detection.
- `TestSessionEntryMissingRoleField` — v0 entry missing `role`.
- `TestSessionV0EntryMalformedToolCalls` — invalid `tool_calls` JSON.
- `TestParseRCFileQuotedValues` — `set model "name with space"`.
- `TestParseRCFileUnknownCommand` — pin behavior.
- `TestSearchMemoryMalformedFile` — invalid YAML/JSON.
- `TestSearchMemoryRegexDOS` — pathological pattern.
- `TestRedactStringRegexSpecialChars` — secret with `.+*?[]` chars.
- `TestErrorHintNilProviderErrorMessage` — nil Message field.
- `TestErrorHintDeepWrapping` — 5+ levels of `errors.Wrap`.
- `TestProvidersHostOfIPv6` — bracketed IPv6.
- `TestProvidersHostOfWithAuthority` — `user:pass@host`.

### 6.5 Numeric + edge case (P2)

- `TestCostTrackerNegativeTokens` — reject or clamp.
- `TestCostTrackerOverflow` — > MaxInt64.
- `TestFormatCostWithInfinityAndNaN`.
- `TestPer1MDivisionByZero`.
- `TestCatalogPriceSourceServiceCaseSensitivity` — `"OpenAI"` vs `"openai"`.
- `TestStripV1SuffixPartialMatch` — `/v10` not stripped to `/`.
- `TestBuildProviderCaseInsensitiveProvider` — `"OPENAI"` upper.

### 6.6 Daemon resource + recovery (P1)

- `TestDispatcherPanicRecovery` — panic in handler → job marked failed.
- `TestDispatcherTimeout` — slow handler → marked failed.
- `TestDispatcherOversizeOutput` — Result.Output > limit.
- `TestRunMissingMailDirAutoCreated`.
- `TestRunWithDrainBudgetZero`.
- `TestLockFilePermissionDenied`.
- `TestRunCompleteEnvelopeWriteFailure` — readonly `done/`.
- `TestCheckpointMalformedEntries`.
- `TestCheckpointReadonlyWorkDir`.
- `TestCheckpointConcurrentAppend`.

### 6.7 Headless + startup (P1)

- `TestCollectHeadlessPromptMergesStdinAndFlag` — `\n\n` join order.
- `TestCollectHeadlessPromptEOFOnEmptyStdin`.
- `TestApplyStartupConfigRCParseFailureSurfaces`.
- `TestApplyStartupConfigMigrateDataPathsMovesFiles`.
- `TestApplyStartupConfigMigrateDataPathsConflict`.
- `TestProfileAllowsEmptyAPIKeyEnforcement` — Anthropic + empty key.
- `TestHeadlessLoopSIGTERMDuringStream`.
- `TestHeadlessLoopBudgetExhaustion`.
- `TestApplyStartupConfigCLIVsRCOrdering` — both set; verify CLI wins.

### 6.8 UI integration (P1)

- `TestJoinCommandViaTeaProgram` — focus survives event pump.
- `TestSetServiceViaTeaProgram` — pair with §6.3.
- `TestQuitMsgViaTeaProgram` — real cancel propagation.
- `TestCtrlCSequenceAcrossRealProgram`.
- `TestRealSIGINTToProcess` — `syscall.Kill(os.Getpid(), syscall.SIGINT)`; app responds.
- `TestThemeRendersValidANSI`.
- `TestRenderMarkdownExactWidth` — for widths 5–200, no line exceeds width.
- `TestTranscriptLoggerRotatesAtMidnight`.
- `TestTranscriptLoggerConcurrentWrites`.

---

## 7. Prioritized Punch List

P0 — fix before anything else; correctness/wiring bugs that pass today's tests:

1. Add `internal/memory/*_test.go` covering all 14 cases in §3.
2. Add `TestMemoryConsolidateRaceWithAppendHot` and decide: either daemon must use flock (fix), or the divergence is intentional and documented + tested.
3. Add the wiring tests in §5: `TestSetServiceUpdatesAgent`, `TestProfileLoadPropagatesService`, `TestQueueDrainCallsAgentQueuePrompt`, `TestDebugOnViaTeaProgram`, `TestForkPropagatesBranchTag`. These are the smallest tests that catch real bugs.
4. Add `TestMCPManagerBootstrappedAtStartup` and `TestSessionCheckpointTriggersDaemonSubmit`. They will fail today — that is the point. Pair with the wiring fix.
5. Add `TestFocusManagerNegativeIndexFloored`. One-line test, prevents a panic.

P1 — substantive coverage gaps with concrete failure modes:

6. OS-level filesystem tests for `read`, `write`, `edit`, `bash`, `terminal_*` (path traversal, ENOSPC, signal-to-child, PTY exhaustion, UTF-8 mid-emoji truncation).
7. Concurrency tests on session append, membership save, model picker, ULID uniqueness.
8. Daemon resource tests: dispatcher panic/timeout/oversize, readonly `done/`, missing `mail/`.
9. Catalog refresh malformed-200 + partial-body.
10. RC parser quoted values + unknown commands.
11. Headless flag+stdin merge and SIGTERM-mid-stream.

P2 — numeric/edge case + adversarial:

12. Cost tracking negative tokens, overflow, NaN/Inf.
13. Provider routing case-insensitivity, IPv6, auth in URL, /v10 partial match.
14. Error hint deep wrapping, status 0/600+, custom context error.
15. Memory search regex DOS, malformed file.
16. MCP redaction with regex special chars.

P3 — UI integration via real `tea.NewProgram`:

17. Replace direct-handler tests with `tea.NewProgram` synthetic-event tests for all `/set`, `/join`, `/profile`, `/debug`, `/restart`, `/fork`, `/models`, `/mp3`. This single architectural shift catches the largest class of value-receiver / closure-capture bugs that no current test catches.
18. Real SIGINT delivery via `syscall.Kill(os.Getpid(), syscall.SIGINT)`.

P4 — hygiene:

19. Reconcile `internal/agent/context_test.go`, `messages_phase3_test.go`, `typed_tool_smoke_test.go` — agents reported these as empty; `docs/testing.md` says they have tests. Investigate and fix.
20. Fate of `cmd/cpm/` (empty) — implement, document placeholder, or delete.

---

## Appendix: Audit Method

Three parallel audits scanned all 71 `*_test.go` files under `internal/`, `cmd/`, and the repo root. Each auditor was briefed on the disconnected-wiring catalog from `docs/AUDIT_TODO.md` so they could cross-reference shape-only tests against known production bugs. Findings were de-duplicated, grouped by failure-mode family, and attached to specific test names + file:line refs so the next pass can verify in place. Where two auditors flagged the same gap (e.g., `internal/memory/` having no tests, the `/set service` non-propagation), it is rated higher priority.
