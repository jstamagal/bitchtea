# Model Catalog & Offline Behavior

bitchtea ships with a compiled-in model catalog snapshot from
[catwalk](https://charm.land/catwalk). This embedded catalog is the
**always-available floor** — you can use `/models` and get cost estimates
without ever touching the network.

## How the catalog works

On startup, bitchtea walks a three-level fallback chain:

1. **On-disk cache** at `~/.bitchtea/catalog/providers.json` — if present and
   readable, this is used immediately. The cache stores catwalk's full provider
   + model payload along with freshness metadata (fetch time, ETag, source URL).
2. **Compiled-in embedded snapshot** — if no cache file exists (or it's
   corrupt), bitchtea falls back to the catwalk snapshot that was current when
   the binary was built. This is always available.
3. **Empty envelope** — only happens if both the cache and the embedded snapshot
   are unavailable (should never occur in practice).

## Autoupdate (opt-in network refresh)

By default, the catalog is **offline**. No network calls are made unless you
explicitly opt in with environment variables:

```bash
export BITCHTEA_CATWALK_AUTOUPDATE=true
export BITCHTEA_CATWALK_URL=https://catwalk.charm.sh
```

With these set, bitchtea will:

- On startup, fire a single ETag-conditional `GET /v2/providers` (timeout: 5s).
- Write the fresh result to `~/.bitchtea/catalog/providers.json`.
- On subsequent runs, skip the network call if the cache is less than 24 hours
  old (the soft TTL).

If the server returns `304 Not Modified`, only the `last_checked` timestamp is
updated — the cached data stays in place.

## What happens when the network fails

Refresh failures are **silent and non-fatal**:

- Transport errors, timeouts, and HTTP errors leave the disk cache untouched.
- The stale cache continues to serve `/models` and cost estimates exactly as
  before.
- The error is logged to the debug transcript only (not shown to the user).

The cache is **never evicted due to a failed refresh**. A stale cache is always
preferred to no cache.

## What happens when the cache is corrupt

If the cache file is unparseable JSON or has an unrecognized `schema_version`,
bitchtea ignores it and falls back to the embedded snapshot. The corrupt file
is not deleted (you can inspect it manually if needed).

Next time a successful refresh occurs, the file is overwritten with valid data.

## `/models` command

The `/models` fuzzy picker uses the catalog to list available model IDs for
your active service:

- If no `BITCHTEA_CATWALK_AUTOUPDATE` environment variables are set, you see
  models from the embedded catalog that was current when the binary was built.
- If the autoupdate ran successfully, you see the latest model list from
  catwalk.
- If your service has no catalog entry (e.g., the active service doesn't match
  any catwalk provider), you get an error listing the available services.

The picker is a simple **substring filter** (not a ranked fuzzy finder). Type
to narrow the list, arrow keys to move the cursor, Enter to select, Esc to
cancel. The selected model takes effect immediately (like `/set model`).

## Cost tracking and the catalog

The `CostTracker` uses the same catalog chain for pricing:

- By default, it uses the embedded snapshot for cost-per-token lookups.
- When the autoupdate cache is available and wired (via `CatalogPriceSource`),
  catalog prices take precedence over embedded prices.
- If a model is missing from the catalog (or has zero pricing), the embedded
  snapshot serves as the backup.
- An unknown model (present in neither source) costs **$0.00** — no error.

The "join" between your active profile and catalog pricing is by service
identity (e.g., `"openai"`, `"openrouter"`). Models that appear under multiple
providers with different prices resolve correctly — the service key disambiguates.

## Cache file location

```
~/.bitchtea/catalog/providers.json
```

The directory is created lazily on first refresh. No migration from old XDG
paths is needed — there is no prior catalog data format.

## Freshness tuning

| Env var | Default | Effect |
|---|---|---|
| `BITCHTEA_CATWALK_AUTOUPDATE` | unset (off) | Must be `true` to enable network refresh |
| `BITCHTEA_CATWALK_URL` | unset (off) | catwalk base URL (e.g. `https://catwalk.charm.sh`) |

The soft TTL is currently 24 hours and is not user-configurable at runtime. If
you need a shorter window, delete the cache file manually and restart.
