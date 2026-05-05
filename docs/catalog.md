# Model Catalog System

> For the user-facing `/models` and refresh walkthrough, see
> [user-guide.md](user-guide.md) and [providers.md](providers.md). This document
> covers the code architecture, data flow, and implementation contracts.

## Overview

The model catalog is a three-tier fallback chain that supplies model metadata
(model IDs, pricing, context windows, capabilities) to the `/models` picker and
the `CostTracker`. Catalog reads are always synchronous and never fail; network
refresh is an opt-in best-effort background operation.

**Package:** `internal/catalog/`

## Data structure

**File:** `internal/catalog/cache.go:36-43` (`Envelope`)

```go
type Envelope struct {
    SchemaVersion int                `json:"schema_version"` // 1
    FetchedAt     time.Time          `json:"fetched_at"`
    LastChecked   time.Time          `json:"last_checked"`
    ETag          string             `json:"etag"`
    Source        string             `json:"source"`
    Providers     []catwalk.Provider `json:"providers"`
}
```

- `SchemaVersion` — bumped on breaking envelope changes. Current value: `1`
  (constant `SchemaVersion`, `cache.go:31`).
- `Providers` — verbatim `[]catwalk.Provider` from catwalk, same type used by
  the embedded snapshot.
- `ETag` — opaque validator for HTTP conditional requests.
- `Source` — the upstream URL that produced this payload.

`catwalk.Provider` (`charm.land/catwalk/pkg/catwalk`) is the per-service row:
`ID` (maps to bitchtea's `Service` field), `Name`, `APIEndpoint`, `Type`,
`DefaultLargeModelID`, `DefaultSmallModelID`, `Models []catwalk.Model`,
`DefaultHeaders`.

`catwalk.Model` carries `ID`, `Name`, `CostPer1MIn`, `CostPer1MOut`,
`CostPer1MInCached`, `CostPer1MOutCached`, `ContextWindow`, `DefaultMaxTokens`,
`CanReason`, `ReasoningLevels`, `DefaultReasoningEffort`, `SupportsImages`,
`Options`.

## On-disk cache

**File:** `internal/catalog/cache.go:47-116`

The cache lives at a single file:

```
~/.bitchtea/catalog/providers.json
```

`CachePath(baseDir)` (`cache.go:47-49`) builds the absolute path. Directory
path: `~/.bitchtea/catalog/` (shared namespace for potential future companions
like a user-override file).

### Read path

`ReadCache(path)` (`cache.go:54-67`) loads and validates the envelope:

1. Read file bytes.
2. JSON-unmarshal into `Envelope`. Returns error on decode failure or
   `SchemaVersion != 1`.
3. Missing file returns `(zero, fs.ErrNotExist)` — callers fall through.

No errors are fatal; callers always have a fallback (embedded → empty).

### Write path

`WriteCache(path, env)` (`cache.go:72-109`) uses an atomic write:

1. Marshal envelope to indented JSON.
2. Create `os.CreateTemp` in the same directory (`providers-*.json.tmp`).
3. Write + `Sync()` + `Close()`.
4. `os.Rename(tmpPath, path)` — atomic on POSIX.
5. Creates parent directory with `0o700` on first write.

## Three-level read chain

**File:** `internal/catalog/load.go:33-45` (`Load`)

```
Load(opts):
  1. ~/.bitchtea/catalog/providers.json   ← on-disk cache (if present & valid)
  2. embedded.GetAll()                     ← compiled-in catwalk snapshot
  3. Envelope{SchemaVersion: 1}           ← empty (providers list = nil)
```

`Load` never returns an error. It is synchronous, safe to call at startup.
The chain is wired in `main.go:57`:

```go
llm.SetDefaultPriceSource(llm.CatalogPriceSource(catalog.Load(catalog.LoadOptions{})))
```

### Embedded floor

**File:** `internal/catalog/load.go:47-61` (`embeddedEnvelope`)

Wraps `catwalk/pkg/embedded.GetAll()` in an `Envelope` with `Source:
"embedded"` and a computed `ETag`. This is the fallback when no cache file
exists or the cache file is corrupt/schema-mismatched. The embedded snapshot
is what was current when the `charm.land/catwalk` dependency was pinned in
`go.mod`.

## Background refresh

**File:** `internal/catalog/refresh.go`

`Refresh` is an async, best-effort network operation that updates the cache
file in place. It is fired from `main.go` via `maybeStartCatalogRefresh`
(`main.go:348-364`).

### Activation

Refresh is **off by default**. The env vars:

| Env var | Required | Effect |
|---------|----------|--------|
| `BITCHTEA_CATWALK_AUTOUPDATE` | `true` / `1` / `yes` / `on` | Gates whether a background goroutine is spawned at all. |
| `BITCHTEA_CATWALK_URL`        | A valid URL | Catwalk base URL. If unset, no network call is made even when AUTOUPDATE is true. |

Both must be set for refresh to run. If either is missing, `maybeStartCatalogRefresh`
returns immediately.

### Timing

- Fired as a goroutine during `main()` before Bubble Tea boots.
- Time-bound: `context.WithTimeout(ctx, 5 * time.Second)` (`catalog.DefaultRefreshTimeout`,
  `refresh.go:19`). A slow or hung catwalk endpoint cannot block startup.
- Errors are silently swallowed — the only effect is a stale cache.

### Refresh decision tree

**File:** `internal/catalog/refresh.go:92-169` (`Refresh`)

```
1. Read existing cache from opts.CachePath (missing/corrupt → zero Envelope).
2. If !opts.Enabled || opts.SourceURL == "" → return cached verbatim.
3. If ctx already cancelled → return cached with ctx.Err().
4. If cache.LastChecked is recent (within SoftTTL, default 24h) → return cached.
5. Call client.GetProviders(ctx, cached.ETag).
   - 200 OK → replace all providers, update FetchedAt + LastChecked + ETag,
              write new envelope to disk.
   - 304 NotModified → update LastChecked only, write envelope (same data).
   - Error → return cached with Err set; cache file untouched.
```

### ETag handling

- The cached ETag is sent as the `If-None-Match` header via catwalk's
  `GetProviders(ctx, etag)` method.
- On `304 NotModified` (`catwalk.ErrNotModified`), the provider list stays
  unchanged but `LastChecked` is bumped so the TTL resets.
- On `200 OK`, the ETag is recomputed from the response body via
  `computeETag` (`refresh.go:174-180`) and stored for the next conditional
  request.
- `computeETag` marshal-hashes the provider list with `catwalk.Etag()`.

### Result type

**File:** `internal/catalog/refresh.go:60-73` (`RefreshResult`)

```go
type RefreshResult struct {
    Envelope    Envelope
    Updated     bool        // providers/etag actually changed on disk
    HitNetwork  bool        // HTTP round trip was attempted
    NotModified bool        // server returned 304
    FromCache   bool        // result was served from disk (no network)
    Err         error       // non-fatal transport/refresh error
}
```

All fields are informational. The caller (currently `main.go:355-363`) ignores
the result entirely — refresh is fire-and-forget.

## Soft TTL

**File:** `internal/catalog/refresh.go:12-20`

```go
const DefaultSoftTTL = 24 * time.Hour
```

If `now - cache.LastChecked < SoftTTL`, Refresh skips the network call and
returns `FromCache: true`. The TTL is not currently user-configurable.

## Models command integration

**File:** `internal/ui/commands.go:943-993` (`handleModelsCommand`)

`/models` opens a substring-filter picker of model IDs for the active service.

The command:

1. Resolves the active `Service` from `m.config.Service`.
2. Loads the catalog via `loadModelCatalog()` (`commands.go:22-24`), a
   package-level seam that defaults to `catalog.Load(catalog.LoadOptions{})`.
3. Calls `modelsForService(env.Providers, service)` (`model_picker.go:50-79`)
   to extract the model IDs for the matching `InferenceProvider`.
4. Opens a `modelPicker` overlay (`model_picker.go:36-43`).

The picker is a **substring filter** (not ranked fuzzy). Typing narrows the
list; arrow keys move the cursor; Enter selects (applies via `agent.SetModel`
and clears the profile tag); Esc cancels.

`modelsForService` (`model_picker.go:50-79`) performs a case-insensitive match
of `service` against each `catwalk.Provider.ID`. When found, it extracts all
model IDs, floating `DefaultLargeModelID` to the front. If no provider matches,
`availableServices` (`model_picker.go:83-93`) provides a hint listing all known
provider IDs.

### Empty catalog at startup

If the catalog is empty (no cache, no embedded snapshot), `/models` prints an
error suggesting `BITCHTEA_CATWALK_AUTOUPDATE=true`.

## Catalog → price-source bridge

**File:** `internal/llm/cost.go:162-174` (`CatalogPriceSource`)

`CatalogPriceSource(env Envelope) PriceSource` adapts the catalog `Envelope`
to the `PriceSource` interface consumed by `CostTracker`:

```go
func CatalogPriceSource(env catalog.Envelope) PriceSource {
    return PriceSourceFunc(func(modelID, service string) *catwalk.Model {
        if m := lookupEnvelope(env, modelID, service); m != nil {
            return m
        }
        return lookupEmbedded(modelID)
    })
}
```

The lookup join (`cost.go:179-193`, `lookupEnvelope`) uses `service` as a
join key against `catwalk.Provider.ID`. When `service` is non-empty, only the
matching provider's models are searched. On any miss (empty envelope, wrong
service, model not present, zero pricing), it falls through to `lookupEmbedded`
— the compiled-in floor — so a refreshed-but-incomplete catalog never loses
pricing for known models.

Embedded-model matching (`cost.go:249-260`, `lookupEmbedded`) is exact-match
first, then prefix match both directions (known-ID-prefixes-query and
query-prefixes-known-ID) as a coarse fallback because catwalk provider IDs do
not always agree with suffix conventions.

### Default price source

`main.go:57` swaps the package-level default:

```go
llm.SetDefaultPriceSource(llm.CatalogPriceSource(catalog.Load(catalog.LoadOptions{})))
```

`SetDefaultPriceSource` (`cost.go:229-237`) replaces the global default so
every `CostTracker` constructed after startup uses the catalog-backed source.
A `nil` argument resets to `EmbeddedPriceSource()`.

## Test seams

**File:** `internal/catalog/load.go:13-20`, `internal/ui/commands.go:22-24`

- `catalog.LoadOptions.BaseDir` — overrides the base directory for cache
  lookup in tests.
- `catalog.LoadOptions.SkipEmbedded` — disables the embedded fallback so
  tests can assert empty-tail behavior.
- `loadModelCatalog` (`commands.go:22`) — package-level var seam in the UI
  package; tests substitute a fixture instead of reading from disk.

## Refresh test hooks

**File:** `internal/catalog/refresh.go:23-28`

```go
type Provider interface {
    GetProviders(ctx context.Context, etag string) ([]catwalk.Provider, error)
}
```

A one-method interface lets tests inject an `httptest.Server` or a fake
provider instead of the real catwalk HTTP client.

## Design rationale

Originally documented in `archive/phase-5-catalog-audit.md` (archived).

**Single-file cache, not per-service.** Per-service files
(`~/.bitchtea/catalog/<provider>.json`) were considered and rejected because
catwalk only exposes the bulk `GET /v2/providers` endpoint. Splitting on disk
would force us to fan out a single HTTP response into many files for no
benefit, and the single-file shape matches how catwalk itself ships the
embedded payload.

**Cache directory, not a single file at the root.** `~/.bitchtea/catalog/`
(rather than `~/.bitchtea/models.json`) leaves room for future companions —
e.g. a per-user model-overrides file or a future `pricing.json` snapshot —
without polluting the `~/.bitchtea/` root. `config.MigrateDataPaths()` was
not modified for this phase: there is no prior XDG location to migrate from,
so the directory is created lazily on first write.

**Soft TTL with embedded hard floor.** Two clocks, one tunable: the soft TTL
(default 24h) drives whether a refresh fires; the embedded snapshot is the
floor that guarantees the picker is never empty. A stale cache is *always*
preferred to no cache. We never delete the cache file as a recovery step —
corruption is handled by parse-failure falling through to embedded.

**Pass `[]catwalk.Provider` through verbatim.** The picker, fuzzy finder, and
cost source need raw fields (context window, supports_images, can_reason,
pricing). Projecting a smaller shape would force every consumer to learn two
types and re-import lost fields later. The size cost is negligible (catwalk's
full payload is well under 1 MiB).

**Built-in profiles cohabit with catwalk.** `builtinProfiles` continues to own
*connection identity* (`Provider`, `Service`, `BaseURL`, `APIKeyEnv`, default
`Model`) — that is the only thing that lets `--profile zai-anthropic` work
without a network trip. Catwalk owns *model metadata* (full model lists per
provider, pricing, context window, capabilities). Conflict resolution: when
both sources name the same model id, catwalk wins for *metadata*, built-in
wins for *defaults* (which model loads when you select the profile cold).
Profiles are never rewritten from the catalog; profile load remains an
offline operation.

**Refresh failure is silent on the boot path.** Errors only surface from a
manual `/models refresh`. The boot path is fire-and-forget so a slow or
broken catwalk endpoint cannot block startup or spam the transcript on every
launch.
