# Phase 3: Fantasy-Native Message and Storage Contract

Status: design only. Implementation tracked under epic `bt-p3`. Sibling tasks
`bt-p3-model-types` (in-memory swap behind adapters), `bt-p3-session-compat`
(JSONL reader/writer), `bt-p3-agent-boundary` (agent loop flip), and
`bt-p3-ui-replay` (resume/fork hardening) implement and test the pieces
described here. This doc is the contract those tasks must satisfy.

## What changes

Today the bitchtea agent boundary, session log, and provider conversion all
funnel through `internal/llm.Message` — a flat `{role, content, tool_calls,
tool_call_id}` struct (`internal/llm/types.go:9-14`). `splitForFantasy`
(`internal/llm/convert.go:16-80`) translates that into `fantasy.Message` /
`fantasy.MessagePart` per call. `splitForFantasy` is lossy in both directions:
multi-part assistant messages collapse to one text blob plus tool calls;
`fantasyToLLM` (`convert.go:84-147`) drops `ReasoningPart`, `FilePart`,
`SourceContent`, and `ProviderOptions` outright.

Phase 3 makes `fantasy.Message` (`charm.land/fantasy@v0.17.1/content.go:143`)
the canonical in-memory message type. `splitForFantasy` and `fantasyToLLM`
shrink to identity passes. The session log gains a versioned envelope so old
JSONL keeps loading and new JSONL keeps replaying after a downgrade.

## Affected packages

Every package that touches `llm.Message` today changes; nothing else does.

| Package | Change |
|---------|--------|
| `internal/llm` | `Message`, `ToolCall`, `FunctionCall` retired from public surface; the type alias `type Message = fantasy.Message` lives here for one release as a compatibility shim. `convert.go` reduces to provider-options stamping. `StreamEvent.Messages` becomes `[]fantasy.Message`. `ChatStreamer.StreamChat` takes `[]fantasy.Message`. |
| `internal/agent` | `Agent.messages` becomes `[]fantasy.Message`. `RestoreMessages`, `MessageCount`, `Compact`, `flushCompactedMessagesToDailyMemory`, the persona anchor (`buildPersonaAnchor`), and the system-prompt seed all switch to `fantasy.Message` constructors. `injectPerMessagePrefix` operates on a `TextPart`. |
| `internal/session` | `Entry` gains `SchemaVersion` and `Parts` (see "Persisted JSONL shape"). `EntryFromMessage` / `MessagesFromEntries` become `EntryFromFantasy` / `FantasyFromEntries` with old-entry shims. `DisplayEntries` keeps working — it filters on `Bootstrap`, not on shape. |
| `internal/ui` | `model.go:198` (resume) and `model.go:580-588` (incremental save) use the new conversion functions. `ToolPanel` reads tool calls from the new entry shape. `invite.go` re-classifies entries from `Parts` instead of `Role`+`ToolName`. |
| `internal/tools` | No change to the `Registry.Execute` boundary (still `(name, argsJSON) -> string`). Phase 2 owns typed tool wrappers; Phase 3 only changes how *results* are wrapped into a message — `ToolResultPart` with `ToolResultOutputContentText` for the text path, `ToolResultOutputContentMedia` for `preview_image` (which today round-trips as `Content: "<base64>"`). |
| `main.go` | `RestoreMessages` call site swaps to `session.FantasyFromEntries`. |

`internal/memory`, `internal/config`, `internal/sound`, and `cmd/trace` do
**not** import `llm.Message` and are untouched.

## Canonical in-memory message type

The type is `fantasy.Message` (`content.go:143`):

```
type Message struct {
    Role            MessageRole     // "system" | "user" | "assistant" | "tool"
    Content         []MessagePart   // ordered, heterogeneous
    ProviderOptions ProviderOptions // per-message provider hints
}
```

`MessagePart` is the discriminated union of `TextPart`, `ReasoningPart`,
`FilePart`, `ToolCallPart`, `ToolResultPart` (`content.go:182-272`).

What we *gain* over today's `llm.Message`:

- **Multi-part content per turn.** Assistant text + tool calls live in one
  ordered slice instead of being smeared across `Content` + `ToolCalls`.
- **Reasoning preserved across turns.** `ReasoningPart` survives the round
  trip; today it's emitted as a `thinking` event and dropped.
- **First-class file/media parts.** `preview_image` results stop being
  base64-stuffed text. Anthropic image input becomes addressable.
- **Per-message `ProviderOptions`.** Phase 4 cache markers and Phase 9
  service gates attach without smuggling state through the message text.
- **`ToolResultPart.Output` typed (text/error/media).** Today every tool
  result is a string; an error result is indistinguishable from a successful
  one whose body happens to start with `error:`.

What we *lose*:

- **Shape simplicity.** A flat string field is gone — UI rendering, session
  display filters, and tests that grep `msg.Content` need helpers (a
  `firstText(msg) string` accessor stays in `internal/llm` for that).
- **Trivial JSON.** Parts are interface values; serialization needs the
  versioned envelope (next section).

## Persisted JSONL shape

Append-only is non-negotiable (CLAUDE.md "Sessions and context discovery";
`session.go:107` — `Append` is the only writer, no rewrites). The new entry
keeps every existing field and adds two:

```jsonc
{
  "ts": "2026-05-01T12:34:56Z",
  "id": "1714567890123456789",
  "parent_id": "1714567890000000000",
  "branch": "",
  "context": "#main",
  "bootstrap": false,

  // NEW — schema version. Absent or 0 means "legacy v0".
  "v": 1,

  // NEW — fantasy.Message marshalled with one envelope per part.
  "msg": {
    "role": "assistant",
    "content": [
      { "type": "text", "text": "Sure. Reading the file." },
      { "type": "tool-call", "tool_call_id": "call_42",
        "tool_name": "read", "input": "{\"path\":\"x\"}" }
    ],
    "provider_options": {}
  },

  // LEGACY mirror — populated by the writer for one release so a
  // downgraded binary can still read sessions written by a new binary.
  "role": "assistant",
  "content": "Sure. Reading the file.",
  "tool_calls": [ { "id": "call_42", "type": "function",
                    "function": { "name": "read", "arguments": "{\"path\":\"x\"}" } } ],
  "tool_call_id": "",
  "tool_name": "",
  "tool_args": ""
}
```

Rules:

- The writer always sets `v: 1` and always populates `msg`.
- The writer **also** populates the legacy fields for any message that the
  legacy projection can losslessly represent (text-only assistant, simple
  user text, single text tool result). This is the dual-write window.
- For messages the legacy projection cannot represent (multi-part assistant
  with reasoning, media tool result, multi-text user paste), the writer
  populates `content` with the concatenated text projection — same as
  `fantasyToLLM` does today — and sets a flag `legacy_lossy: true`. A
  downgraded binary loads what it can; replay quality degrades but doesn't
  crash.
- Reader precedence: if `v >= 1` and `msg` is present, use `msg`. Otherwise
  fall back to legacy fields (today's behavior).
- Unknown future fields are ignored (`encoding/json` default). Unknown
  `MessagePart` `type` values become a `TextPart` with the raw JSON as text
  and a stderr warning, not a load failure.
- Append-only is preserved end-to-end: new fields only ever extend an entry,
  and `Fork` (`session.go:147`) keeps working because it copies entries
  verbatim without inspecting their shape.

## Compatibility rules

- **Forward (new binary, old session file).** `v` is absent → reader runs
  the legacy projection (same as `MessagesFromEntries` today, lifted into
  fantasy types). Sessions written before Phase 3 keep loading forever; we
  do not date-cut them out.
- **Backward (old binary, new session file).** Old binary ignores `v` and
  `msg`, reads legacy fields. For losslessly-representable messages this is
  perfect. For lossy ones (`legacy_lossy: true`), the old binary sees the
  text projection and the user notices missing reasoning / media but the
  session loads. This is the explicit acceptance-criterion guarantee.
- **Mid-file mixing.** A session may contain a mix of v0 and v1 entries
  (e.g., resumed across an upgrade). The reader handles each entry on its
  own merits; there is no whole-file version check.
- **Old binary writing into a v1 session.** Allowed. The old binary appends
  v0-shape entries; the new binary on next load treats those as v0 and
  promotes them in memory. No migration step, no rewrite.
- **`session.Checkpoint` and `session.FocusState`** are not part of this
  contract — they're per-session state files (`.bitchtea_checkpoint.json`,
  `.bitchtea_focus.json`), not the JSONL log. They stay v0 in Phase 3.

## Migration rollback strategy

Rollback means: the user installs Phase 3, hits a regression, downgrades
the binary, and expects their session history to keep working.

- **Dual-write is the rollback.** Because every v1 entry also carries the
  legacy fields, a downgraded binary loses no log lines — at worst it loses
  fidelity on entries flagged `legacy_lossy: true`.
- **No format migration step ships in Phase 3.** We never rewrite an
  existing JSONL file. A user who wants a "clean" v1-only file can
  `/fork` after upgrading; the fork copies entries verbatim, then new
  appends are v1.
- **Kill switch.** `BITCHTEA_DISABLE_FANTASY_MESSAGES=1` env var (read once
  at agent construction in `internal/agent`) forces the writer back to
  legacy-only output and the reader back to legacy-only parsing. Intended
  for the "Phase 3 broke something, I need to keep working until it's
  fixed" case. Removed in Phase 5.
- **Dual-write removal.** Drops in the release **after** Phase 3 lands and
  the kill switch retires. Until then the cost is one extra `content`
  field per entry — a few hundred bytes — which is negligible vs. the
  `msg` envelope itself.

## Adapter boundaries

The migration flips one boundary at a time. To allow that, three adapters
live in `internal/llm` for the duration of the transition:

- `func ToFantasy(msgs []llm.Message) []fantasy.Message` — wraps the
  current `splitForFantasy` minus the prompt-extraction quirk; used by any
  call site that still has legacy messages in hand.
- `func FromFantasy(msgs []fantasy.Message) []llm.Message` — wraps
  `fantasyToLLM`; used by any call site that needs to display or persist
  messages through the legacy path during the transition.
- `type Message = fantasy.Message` — type alias declared once
  `bt-p3-model-types` lands. Lets `agent.messages` switch type without
  touching every call site in one commit.

Adapters live in `internal/llm` (not `internal/session`) because that's
already where conversion lives and it keeps the dependency graph (CLAUDE.md
"High-Level Architecture") acyclic: `agent → llm`, `session → llm`,
`ui → agent, llm, session`. Adding adapters to `session` would require
`session → llm → fantasy`, which is fine, but the symmetry of "all
fantasy↔bitchtea conversion lives in `internal/llm`" is worth more.

The agent loop flips atomically: `bt-p3-agent-boundary` swaps `Agent.messages`
and updates `sendMessage` / `Compact` / `RestoreMessages` in one commit,
deleting `ToFantasy`/`FromFantasy` call sites as it goes. Until then both
adapters compile and the agent runs on the legacy type.

## Open questions

- **`ProviderOptions` persistence.** Cache markers are per-step, not
  per-message — should `ProviderOptions` be persisted at all, or stripped
  before write? Leaning strip on write; PrepareStep re-stamps on read.
- **Reasoning visibility on resume.** If a user resumes a session, do we
  show the assistant's prior reasoning in the transcript or hide it the
  way we do live? Probably hide, but the v1 envelope keeps the data
  available so `/show reasoning` becomes possible.
- **Schema version cadence.** `v: 1` covers Phase 3. If Phase 5+ needs to
  change the part-encoding (e.g. richer `FilePart` with external blob
  refs), is that `v: 2` or a part-level discriminator? Defer; cross that
  bridge when a concrete second change shows up.
- **`legacy_lossy` granularity.** A single boolean is coarse. Worth
  splitting into `lossy_reasoning`, `lossy_media`, `lossy_parts`? Probably
  not for v1 — the field exists to warn the downgrade reader, not to
  drive UI.
