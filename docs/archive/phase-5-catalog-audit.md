> **Status:** SHIPPED

# Phase 5: Catwalk Catalog Audit

Status: design only. Implementation tracked under epic `bt-p5`
(`bt-p5-autoupdate`, `bt-p5-models-fuzzy`, `bt-p5-picker-ui`, `bt-p5-verify`,
`bt-p5-cost-sync`). This doc nails the catwalk surface, on-disk cache shape,
TTL, and offline behavior the autoupdate task (`bt-p5-autoupdate`) must
satisfy before any picker UI work begins.

## Goals

1. Pull a fresh model catalog from charm.land's catwalk service so the model
   picker (`bt-p5-picker-ui`) and fuzzy finder (`bt-p5-models-fuzzy`) see
   today's models, not whatever was embedded the day we built the binary.
2. Stay 100% functional offline — the embedded catalog is always the floor.
3. One round trip per refresh window, ETag-conditional. We are not a polling
   client.

Non-goals for this phase: pricing reconciliation with `CostTracker`
(`bt-p5-cost-sync`, P3), background refresh via daemon (deferred to Phase 7),
catalog editing / user-defined providers, write-back to catwalk.

## Catwalk in `go.mod`

Already pinned: `charm.land/catwalk v0.35.1`. Used today only by
`internal/llm/cost.go` via `embedded.GetAll()` for cost lookups. The HTTP
client surface has never been wired up.

## Catwalk APIs to consume

All from `charm.land/catwalk/pkg/catwalk` unless noted.

| Symbol | Purpose |
|---|---|
| `catwalk.New() *Client` | HTTP client honoring `CATWALK_URL` env (default `http://localhost:8080`). We will use `NewWithURL` with a real upstream — see Open Questions. |
| `catwalk.NewWithURL(url) *Client` | Pin our own base URL, ignoring env. |
| `(*Client).GetProviders(ctx, etag) ([]Provider, error)` | Sole network call. `GET /v2/providers`, ETag-conditional. Returns full provider+models payload. |
| `catwalk.ErrNotModified` | Sentinel returned when server-side ETag matches — keep current cache, bump `last_checked`. |
| `catwalk.Etag(data []byte) string` | Compute ETag locally for embedded fallback so refresh logic uniformly stores one. |
| `catwalk.Provider` | Top-level catalog row: `Name`, `ID` (`InferenceProvider`), `APIEndpoint`, `Type`, `DefaultLargeModelID`, `DefaultSmallModelID`, `Models`, `DefaultHeaders`. |
| `catwalk.Model` | `ID`, `Name`, `CostPer1MIn`, `CostPer1MOut`, `CostPer1MInCached`, `CostPer1MOutCached`, `ContextWindow`, `DefaultMaxTokens`, `CanReason`, `ReasoningLevels`, `DefaultReasoningEffort`, `SupportsImages` (JSON `supports_attachments`), `Options`. |
| `catwalk.Type`, `catwalk.InferenceProvider` constants | String enums for transport family and provider id. Use them in profile mapping (`bt-p5-picker-ui`) instead of inventing parallel strings. |
| `catwalk.KnownProviders()` / `KnownProviderTypes()` | Whitelist for sanity-checking unknown payload entries. |
| `catwalk/pkg/embedded.GetAll() []catwalk.Provider` | The compiled-in floor. Already imported by `cost.go`. We continue to lean on this when the cache is missing or unreadable. |

No other public types exist in the package — `Client` carries no other
methods, and the `embedded` package exposes only `GetAll`.

## Cache shape

Single JSON file. Catwalk already returns a `[]Provider` JSON document; we
wrap it with a thin envelope that records freshness so the loader doesn't
need to stat the file separately:

```json
{
  "schema_version": 1,
  "fetched_at":     "2026-05-01T18:42:11Z",
  "last_checked":   "2026-05-02T02:38:00Z",
  "etag":           "\"5f1c…\"",
  "source":         "https://catwalk.charm.sh",
  "providers":      [ /* []catwalk.Provider passed through verbatim */ ]
}
```

Field rules:

- `schema_version` (int): bump on any breaking envelope change. v1 loaders
  reject unknown versions and fall back to embedded.
- `fetched_at` (RFC3339 UTC): when `providers` last *changed* (200 response).
- `last_checked` (RFC3339 UTC): when we last asked the server, regardless of
  200 vs 304. Drives TTL math.
- `etag`: opaque string from the upstream response, fed back on next request.
- `source`: the base URL that produced this payload — lets `/models refresh`
  warn if the user's `CATWALK_URL` changed under a stale cache.
- `providers`: `[]catwalk.Provider` exactly as returned. Decoding back into
  `catwalk.Provider` keeps the in-memory shape identical to the embedded
  fallback so callers (`bt-p5-picker-ui`, `bt-p5-models-fuzzy`,
  `bt-p5-cost-sync`) only ever touch one type.

Per-field model data (context window, supports_images, can_reason, pricing)
comes for free because we pass `catwalk.Model` through unchanged. We do
**not** project a smaller shape — the picker needs the raw fields, and the
extra bytes are negligible (catwalk's full payload is well under 1 MiB).

Loader is `DisallowUnknownFields`-strict on the envelope, lenient on the
inner `providers` (let catwalk add fields without breaking us).

## Cache location

```
~/.bitchtea/catalog/providers.json
```

One file, one fetch, one ETag. Per-service files were considered (a la
`~/.bitchtea/catalog/<provider>.json`) but rejected: catwalk only exposes
the bulk endpoint, and splitting on disk would force us to fan out a single
HTTP response anyway. The single-file shape also matches how catwalk itself
ships the embedded payload.

`~/.bitchtea/catalog/` (not `~/.bitchtea/models.json`) so future companions —
e.g. a per-user model-overrides file or a future `pricing.json` snapshot —
have a directory to live in without polluting the `~/.bitchtea/` root.

`config.MigrateDataPaths()` is **not** modified for Phase 5: there is no
prior XDG location for catalog data to migrate from. The directory is
created lazily on first refresh by the autoupdate code.

## TTL & refresh policy

Two clocks, one tunable:

- **Soft TTL: 24h.** On any agent boot or `/models` invocation, if
  `now - last_checked >= 24h`, kick a single ETag-conditional refresh in a
  goroutine. Result is written through to disk. Failure is silent (logged
  to transcript at debug only).
- **Hard floor: embedded.** `loadCatalog` returns embedded providers if the
  cache file is missing, unparseable, or older than 30 days *and* the last
  network attempt also failed. The picker is never empty.

Manual override: `/models refresh` forces a fetch ignoring TTL. Returns a
one-line summary (`catalog: 32 providers, 412 models, etag=…`) or the
network error.

No background daemon refresh in Phase 5. The hook for "kick refresh on
boot" is intentionally synchronous-launch + async-resolve: a future
Phase 7 daemon can take over by writing the same file out-of-band, and the
agent process picks up the new payload on next read.

## Offline / failure behavior

Strict precedence on every catalog read (picker, fuzzy finder, cost
lookup):

1. **Cache file present & parseable & schema_version matches** → use it.
2. **Cache absent / corrupt** → embedded (`embedded.GetAll()`).
3. Never error out to the user from a *read* path. Catalog reads are best-
   effort.

On *refresh* failure:

| Failure mode | Behavior |
|---|---|
| DNS / dial error | `last_checked` not updated, cache untouched, `/models refresh` prints one-line error. Boot path stays silent. |
| HTTP non-2xx (other than 304) | Same as dial error. |
| `ErrNotModified` (304) | `last_checked` updated, `etag` unchanged, `providers` untouched. Counts as success. |
| Decode error | Treat as fetch failure; cache untouched. Logged once. |
| Disk write failure | Refresh result kept in memory for the session; next boot retries. Log once. |

A stale cache is always preferred to no cache. We never delete the cache
file as a recovery step — corruption is handled by parse-failure → embedded
fallback.

## Built-in cohabitation

Catwalk providers and bitchtea's hardcoded `builtinProfiles`
(`internal/config/config.go`) live **side by side**:

- `builtinProfiles` continues to own *connection identity* — `Provider`
  (wire format), `Service`, `BaseURL`, `APIKeyEnv`, default `Model`. This is
  the only thing that lets `--profile zai-anthropic` work without a network
  trip.
- The catwalk catalog owns *model metadata* — full model lists per provider,
  pricing, context window, capabilities. The picker enumerates from catwalk
  first; built-ins pin the *active* model and connection wiring.
- Mapping: catwalk's `InferenceProvider` ID maps to our `Service` field
  one-for-one for the providers we ship (`openai`, `anthropic`, `openrouter`,
  `zai`, `zai-anthropic` ↔ `anthropic`, `vercel`, etc.). The mapping table
  belongs in `bt-p5-picker-ui`; this audit only commits to keeping the
  shapes joinable on `Service`.
- Conflict resolution: when both sources name the same model id, catwalk
  wins for *metadata* (pricing, context window, supports_images), built-in
  wins for *defaults* (which model loads when you select the profile cold).

Profiles are not rewritten from the catalog. Profile load remains an
offline operation.

## Out of scope for this phase

- Picker UI / keybindings — `bt-p5-picker-ui`.
- Fuzzy finder ranking and `/models <query>` syntax — `bt-p5-models-fuzzy`.
- Pushing catwalk pricing into `CostTracker` so live runs use refreshed
  costs — `bt-p5-cost-sync` (P3).
- Daemon-driven background refresh — punted to Phase 7 (`bt-p7`).
- User-supplied catalog override file (e.g. `~/.bitchtea/catalog/local.json`
  to inject custom models). Possibly v1.1.

## Open questions

- **Real catwalk URL.** The embedded `defaultURL` is `http://localhost:8080`.
  The README points at the project but never names a production endpoint;
  Crush presumably ships one. Action item before `bt-p5-autoupdate` codes
  the fetcher: confirm the canonical hostname (likely `https://catwalk.charm.sh`)
  and pin it as the bitchtea default, with `CATWALK_URL` honored as override.
  Until confirmed, autoupdate should be **off by default** with refresh
  reachable only via `/models refresh`.
- **Cache directory permissions.** `0700` for symmetry with `sessions/`, or
  `0755`? Sessions are 0700 today; recommend matching.
- **Schema migration.** If catwalk ships v3 of its `Provider` shape mid-
  Phase-5, do we bump our `schema_version` or rely on JSON tolerance? Lean
  tolerance — don't bump unless our envelope changes.
- **Telemetry.** Should `/models` show last refresh time and source URL by
  default? Useful for debugging stale data; trivial to add when picker
  lands.
- **Embedded staleness.** `embedded.GetAll()` is whatever was current when
  catwalk v0.35.1 was tagged. Document a version-bump cadence so we don't
  drift more than a few months from upstream even when the user is fully
  offline.

## Status

All work shipped. Picker UI, autoupdate, fuzzy finder, and cost-sync landed.
Design rationale (single-file shape, cache-dir layout, soft TTL + embedded
floor, built-in cohabitation) was ported into `docs/catalog.md` under the
Design rationale section. This document is retained for historical context.
