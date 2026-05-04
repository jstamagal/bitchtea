# TESTING_TODO.md — Test Quality Audit & Missing Tests

Generated 2026-05-04. Companion files: `DOC_TODO.md`, `WIRING_TODO.md`.

659 test functions across 43 test files. This audit classifies tests as
**shape-only** (doesn't panic, returns non-nil, compiles) vs **behavioral**
(correct output, correct error, correct state transitions). Then lists every
missing test that should exist.

---

## A. SHAPE-ONLY TESTS (testing "doesn't crash" instead of correctness)

These tests pass if the code doesn't panic but make no assertions about
whether the output, state, or error message is correct.

### A.1 Explicit panic-recovery tests

| Test | File | What it should also test |
|------|------|------------------------|
| `TestReplayExhaustedFixtureDoesNotPanic` | `agent/replay_test.go:156` | Should verify the agent returns a done event with empty text (graceful degradation) |
| `TestBashNonexistentCommandDoesNotPanic` | `tools/tools_test.go:287` | Should verify the error output contains "command not found" or similar |
| `TestFantasyToLLMEmptyPartsNoPanic` | `llm/convert_test.go:178` | Should verify the output message has empty content, not just "didn't crash" |
| `TestLLMToFantasyEmptyUserNoPanic` | `llm/convert_test.go:621` | Should verify the output fantasy message structure |
| `TestDebugRoundTripper_NilHookDoesNotPanic` | `llm/debug_test.go:340` | Should verify the request still passes through |
| `TestNoopAuditHook_DoesNotPanic` | `mcp/permission_test.go:32` | Should verify no side effects |
| `TestChatMessageFormatLongContentNoPanic` | `ui/message_test.go:82` | Should verify the output is truncated correctly |
| `TestChatMessageFormatEmptyContentNoPanic` | `ui/message_test.go:101` | Should verify the output is a valid empty message format |
| `TestRapidEscInputDoesNotPanic` | `ui/model_turn_test.go:773` | Should verify the Esc state machine is in a valid state after rapid input |

**Action:** Each test should keep the panic guard AND add behavioral assertions.

---

## B. PACKAGES WITH ZERO DIRECT TESTS

### B.1 internal/memory/ — NO TESTS

`internal/memory/memory.go` has zero test files. All memory semantics are
tested indirectly through:
- `internal/agent/compact_test.go` (compaction flushes)
- `internal/tools/tools_test.go` (search_memory, write_memory tools)
- `internal/llm/typed_search_memory_test.go` (typed wrapper)
- `internal/llm/typed_write_memory_test.go` (typed wrapper)
- `internal/daemon/jobs/memory_consolidate_test.go` (consolidation)

**Missing tests that MUST be written directly in `internal/memory/`:**

1. **`TestScopeName`** — verify FNV hash + sanitization for representative paths
2. **`TestMemoryBaseDir`** — verify derivation from sessionDir
3. **`TestHotPath_root`** — verify returns `workDir/MEMORY.md`
4. **`TestHotPath_channel`** — verify returns scoped path
5. **`TestHotPath_nestedQueryUnderChannel`** — verify full nested path
6. **`TestDailyPathForScope_root`** — verify date-based path
7. **`TestDailyPathForScope_channel`** — verify scoped daily path
8. **`TestAppendHot_createsParentDirs`** — verify mkdir behavior
9. **`TestAppendHot_flockExclusive`** — verify concurrent appends don't interleave
10. **`TestAppendHot_emptyContentNoOp`** — verify no file created
11. **`TestAppendHot_headingFormat`** — verify exact markdown heading shape
12. **`TestAppendDailyForScope_headingAlwaysSaysPreCompaction`** — verify the misleading heading
13. **`TestSearchInScope_allTermsRequired`** — verify AND semantics
14. **`TestSearchInScope_preferFullQueryMatch`** — verify snippet anchoring
15. **`TestSearchInScope_lineageOrder`** — verify child→parent→root search order
16. **`TestSearchInScope_childDoesNotLeakToRoot`** — verify isolation
17. **`TestSearchInScope_oneResultPerFile`** — verify dedup
18. **`TestSearchInScope_readErrorReturnsError`** — verify non-missing read errors propagate
19. **`TestSearchResult_snippetEllipsis`** — verify `...` at start/end boundaries
20. **`TestLoad_permissionErrorReturnsEmpty`** — verify swallowed errors
21. **`TestSaveScoped_createsParentDirs`** — verify mkdir
22. **`TestSanitizeSegment`** — verify character replacement and edge cases

---

### B.2 internal/agent/event/ — NO TESTS

`internal/agent/event/event.go` defines the `Event` struct and `State` type.
No tests. Low priority since it's just types, but state transitions should
be tested.

---

### B.3 internal/ui/art.go — NO TESTS

Startup ASCII art rendering. Low priority.

---

### B.4 internal/ui/styles.go — NO TESTS

Style definitions. Low priority (visual).

---

## C. TEST FILES WITH WEAK BEHAVIORAL COVERAGE

### C.1 internal/llm/convert_test.go — tests shape, not round-trip fidelity

**What it tests well:**
- Fantasy-to-LLM conversion for various message types
- Tool call round-trip preservation
- Multi-part message handling

**What it misses:**
- **No round-trip fidelity test:** Convert fantasy→LLM→fantasy and verify equivalence
- **No lossy-conversion detection:** When LLM→fantasy loses data (e.g., reasoning parts), is it flagged?
- **No test for ToolResponse conversion accuracy** — only tests that it doesn't crash
- **No test for system message conversion** with provider-specific content blocks

**Tests to write:**
1. `TestFantasyToLLMRoundTrip` — convert both ways, verify no data loss
2. `TestConvertPreservesToolResponseContent` — verify tool result text is preserved exactly
3. `TestConvertHandlesMultipleAssistantToolCalls` — verify ordering preserved

---

### C.2 internal/llm/stream_test.go — fake server, limited failure modes

**What it tests well:**
- Basic streaming flow via fake fantasy agent

**What it misses:**
- **No test for mid-stream errors** (provider returns error during tool execution)
- **No test for stream interruption** (context cancelled mid-stream)
- **No test for malformed stream events** (unexpected event types)
- **No test for usage event accumulation** across multiple chunks

**Tests to write:**
1. `TestStreamChat_midStreamError` — provider error during tool loop
2. `TestStreamChat_contextCancelledDuringTool` — cancellation mid-tool
3. `TestStreamChat_usageAccumulation` — verify token counts across multiple usage events
4. `TestStreamChat_emptyToolResult` — tool returns empty string

---

### C.3 internal/llm/tools_test.go — TranslateTools coverage

**What it tests well:**
- Typed vs generic tool detection
- MCP tool namespacing

**What it misses:**
- **No test for AssembleAgentTools collision resolution** (local wins over MCP)
- **No test for MCP tool with missing namespace prefix** (should be dropped)
- **No test for duplicate MCP tool names** (first wins)

**Tests to write:**
1. `TestAssembleAgentTools_localWinsOverMCP` — verify collision resolution
2. `TestAssembleAgentTools_dropsUnprefixedMCP` — verify defense-in-depth
3. `TestAssembleAgentTools_nilManager` — verify graceful nil handling

---

### C.4 internal/tools/tools_test.go — good behavioral tests but gaps

**What it tests well:**
- read, write, edit, bash tools with exact output matching
- Error messages for bad inputs
- Cancellation vs timeout distinction for bash

**What it misses:**
- **No tests for terminal_* family** (start, send, keys, snapshot, wait, resize, close)
- **No test for preview_image** — the image tool
- **No test for search_memory scope inheritance** at registry level
- **No test for write_memory with explicit scope override** to a non-existent channel
- **No test for bash with very large output** (50KB truncation boundary)
- **No test for edit with multiple edits in one call** (order matters)
- **No test for read with binary files** (should they be handled?)

**Tests to write:**
1. `TestTerminalStartAndSnapshot` — basic PTY lifecycle
2. `TestTerminalSendAndKeys` — input handling
3. `TestTerminalWaitWithTimeout` — wait-for-text behavior
4. `TestTerminalCloseCleanup` — resource cleanup verification
5. `TestPreviewImage_validPNG` — basic image rendering
6. `TestPreviewImage_missingFile` — error path
7. `TestBashOutputTruncation` — verify 50KB cap
8. `TestEditMultipleEditsOrdering` — verify edits applied in order
9. `TestReadBinaryFile` — behavior on non-text files
10. `TestWriteMemoryExplicitChannelScope` — scope override correctness

---

### C.5 internal/agent/compact_test.go — tests happy path only

**What it tests well:**
- Compaction flushes to daily memory before summary
- Scoped compaction writes to correct path
- Fantasy-native compaction works

**What it misses:**
- **No test for compaction when messages < 6** (should be no-op)
- **No test for compaction with LLM returning "NONE"** (no memory written)
- **No test for compaction failure** (LLM error during summary)
- **No test for bootstrap boundary enforcement** (compaction must not cross bootstrapMsgCount)
- **No test for session save watermark after compaction** (known gap)

**Tests to write:**
1. `TestCompact_fewMessagesIsNoOp` — verify threshold check
2. `TestCompact_noneResponseSkipsMemory` — verify "NONE" handling
3. `TestCompact_streamerErrorPropagates` — verify error handling
4. `TestCompact_bootstrapBoundaryRespected` — verify system prompt preserved
5. `TestCompact_rebuildsAsSystemSummaryLast4` — verify exact message structure

---

### C.6 internal/ui/resume_v1_test.go — extensive but missing edge cases

**What it tests well:**
- v1 session resume with various message types
- Multi-context resume
- Lossy migration detection

**What it misses:**
- **No test for resume of session with corrupted JSONL** (partial write, truncated line)
- **No test for resume of v0 session with tool calls** (legacy format)
- **No test for resume overwriting pre-resume injected memory** (the known bug in memory.md)

**Tests to write:**
1. `TestResumeCorruptedJSONL` — verify graceful degradation
2. `TestResumeV0SessionWithToolCalls` — verify legacy tool call restoration
3. `TestResumeOverwritesPreInjectedMemory` — reproduce the known injection bug

---

### C.7 internal/ui/model_turn_test.go — strong behavioral but missing flows

**What it tests well:**
- Esc ladder (3-stage cancel)
- Ctrl+C ladder (3-stage)
- Cancel/queue interaction
- Stale event handling
- Follow-up prompt injection

**What it misses:**
- **No test for sendToAgent context switching** — verify SetContext+SetScope called
- **No test for per-context session save** at turn boundary
- **No test for queued message staleness discard** timing
- **No test for agent turn during model switch** (race condition)

**Tests to write:**
1. `TestSendToAgent_switchesAgentContext` — verify InitContext+SetContext+SetScope
2. `TestTurnBoundary_savesPerContextMessages` — verify session append per context
3. `TestQueuedMessageStaleness_60sDiscard` — verify staleness window

---

## D. MISSING INTEGRATION / E2E TESTS

### D.1 No end-to-end agent turn test with real tool execution

The agent loop tests use `fakeStreamer` which pre-programs responses. There
is no test that exercises the full path: user message → system prompt → tool
call → tool execute → tool result → next LLM call → final response.

The `replay_test.go` tests come closest but use fixture files, not actual
tool execution through the full stack.

**Tests to write:**
1. `TestAgentTurnWithRealToolExecution` — full round-trip with bash tool
2. `TestAgentTurnWithMultipleToolCalls` — verify sequential tool execution
3. `TestAgentTurnWithToolError` — verify error propagation through full stack

---

### D.2 No test for /join → message → session save → resume → verify context

The per-context system has no integration test that proves the full cycle:
join a channel, send a message, verify it's saved with context label, resume
the session, verify the message is in the right context.

**Tests to write:**
1. `TestContextRoundTrip_joinSendSaveResume` — full lifecycle

---

### D.3 No test for MCP tool execution through agent

MCP tools have unit tests for the manager and adapter, but no test exercises
an MCP tool call through the full agent loop (agent → fantasy → mcpAgentTool →
manager → server).

**Tests to write:**
1. `TestAgentMCPToolExecution` — full MCP path with fake server

---

### D.4 No test for headless mode follow-up loop

`runHeadlessLoop` in main.go supports `MaybeQueueFollowUp` but there's no
test that exercises the auto-follow-up loop in headless mode.

**Tests to write:**
1. `TestHeadlessFollowUpLoop` — verify multi-turn auto-follow-up

---

### D.5 No test for concurrent /invite and agent turn

The membership system has no concurrency test to verify that /invite during
an active agent turn doesn't corrupt state.

---

## E. MISSING FAILURE MODE TESTS

### E.1 Error message accuracy

Many tests verify that an error occurs but not that the error message is
correct or useful. Specific gaps:

| Package | Missing |
|---------|---------|
| tools | edit with empty file, write to read-only dir, read of directory |
| session | Load of truncated JSONL, Append to read-only file |
| memory | Search with unreadable daily dir, AppendHot with full disk |
| config | ParseRC with malformed lines, MigrateDataPaths when target exists |
| daemon | Run with already-held lock (tested), Stop with zombie process |

---

### E.2 Boundary condition tests

| Area | Missing test |
|------|-------------|
| tools/read | offset=0 (should that be line 1?), limit=0 |
| tools/edit | edit that creates an empty file |
| tools/bash | command that writes to stderr only |
| tools/bash | timeout of exactly 0 seconds |
| agent/compact | exactly 6 messages (minimum threshold) |
| session/fork | fork with 0 entries |
| session/tree | tree with no forks |
| memory/search | query with only whitespace |
| memory/search | limit of exactly 1 |
| llm/cost | model not in price table |

---

### E.3 Concurrency tests

The `-race` flag catches data races, but there are no explicit concurrency
tests for:

| Area | Missing test |
|------|-------------|
| agent | Concurrent SendMessage calls (should be prevented) |
| agent | QueuePrompt during active turn drain |
| tools/terminal | Concurrent terminal_send on same session |
| session | Concurrent Append from multiple goroutines |
| memory | Concurrent AppendHot from TUI and daemon |
| mcp/manager | Concurrent CallTool during server restart |

---

## F. TEST INFRASTRUCTURE GAPS

### F.1 No test helper for creating a full Model with agent

Many UI tests need a `Model` with a working agent. Currently each test builds
this ad-hoc. A shared `testModel(t)` helper that creates a Model with
fakeStreamer, temp dirs, and default config would reduce boilerplate and
ensure consistent setup.

### F.2 No test fixtures for session resume

Resume tests create entries inline. A set of fixture `.jsonl` files (v0
format, v1 format, multi-context, corrupted) would make resume testing more
thorough and less fragile.

### F.3 No benchmarks

Zero benchmark tests (`func Benchmark*`) in the entire codebase. Key areas
that would benefit:
- `memory.SearchInScope` with large daily files
- `session.FantasyFromEntries` with large session files
- `llm.FantasySliceToLLM` conversion cost
- `tools.execBash` startup latency

---

## G. SUMMARY OF HIGHEST-PRIORITY MISSING TESTS

| # | Priority | What | Why |
|---|----------|------|-----|
| 1 | **P0** | `internal/memory/` package tests | Zero direct tests for core store; all testing is indirect |
| 2 | **P0** | Terminal tool tests | 7 tools, zero tests |
| 3 | **P0** | preview_image tool tests | Implemented, zero tests |
| 4 | **P1** | Compaction edge cases (< 6 msgs, NONE, error, bootstrap boundary) | Happy path only |
| 5 | **P1** | Convert round-trip fidelity | Shape tests only |
| 6 | **P1** | Stream error/cancellation during tool execution | Not tested |
| 7 | **P1** | AssembleAgentTools collision resolution | Not tested |
| 8 | **P1** | Context round-trip integration (join→send→save→resume) | Not tested |
| 9 | **P2** | MCP tool execution through agent | Not tested |
| 10 | **P2** | Headless follow-up loop | Not tested |
| 11 | **P2** | Resume with corrupted JSONL | Not tested |
| 12 | **P2** | Concurrent memory writes (TUI + daemon) | Not tested |
| 13 | **P2** | sendToAgent context switch verification | Not tested |
| 14 | **P3** | Bash output truncation at 50KB | Not tested |
| 15 | **P3** | Edit with multiple edits ordering | Not tested |
| 16 | **P3** | Benchmarks for search, resume, conversion | Zero benchmarks |
