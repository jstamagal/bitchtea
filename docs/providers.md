# Provider Routing, Profiles, and Detection

## Overview

bitchtea supports two wire-format providers (`openai`, `anthropic`) and a
growing list of upstream service identities. The **Provider** field selects the
wire dialect; the **Service** field identifies the upstream endpoint for
per-service behavior gates. A default `Config` points at OpenAI:

- `internal/config/config.go:49-77` — `DefaultConfig()` reads
  `OPENAI_API_KEY`, `OPENAI_BASE_URL`, `BITCHTEA_MODEL`, and
  `BITCHTEA_PROVIDER` from the environment, defaulting to
  `https://api.openai.com/v1`, `gpt-4o`, and `openai` respectively.
- `Service` defaults to `"openai"` in step with the base URL.

## Provider detection (`DetectProvider`)

**File:** `internal/config/config.go:80-112`

`DetectProvider` runs after `DefaultConfig` and before rc/CLI overrides. It
walks a fixed env-var precedence — the **first** env var that is set and whose
corresponding `APIKey` is still empty wins:

| Priority | Env var set          | `Provider`    | `Service`      | `Profile` label | Notes |
|----------|----------------------|---------------|----------------|-----------------|-------|
| 1        | `ANTHROPIC_API_KEY`  | `anthropic`   | `anthropic`    | (none)          | Default model becomes `claude-sonnet-4-20250514` if still `gpt-4o`. |
| 2        | `OPENROUTER_API_KEY` | `openai`      | `openrouter`   | `openrouter`    | Base URL and model pulled from the built-in openrouter profile. |
| 3        | `ZAI_API_KEY`        | `openai`      | `zai-openai`   | `zai-openai`    | Base URL and model pulled from the built-in zai-openai profile. |
| 4        | `OPENAI_API_KEY`     | `openai`      | `openai`       | (none)          | Uses the existing DefaultConfig base URL and model. |

Each detected service sets `cfg.Profile` to the matching built-in profile name
(except pure OpenAI and pure Anthropic), which the UI displays in the topbar.

**Precedence note:** `OPENAI_BASE_URL` is read by `DefaultConfig` at line 59
*before* `DetectProvider` runs. If `ANTHROPIC_API_KEY` is later detected, the
base URL is overridden to `ANTHROPIC_BASE_URL` or the Anthropic default. If
none of the detection env vars are set, the config keeps whatever `DefaultConfig`
produced (OpenAI).

## Built-in profiles

**File:** `internal/config/config.go:150-255` (`builtinProfiles`)

bitchtea ships 15 built-in profiles. Each is a `builtinProfileSpec` carrying
`Provider` (wire format), `Service` (upstream identity), `BaseURL`, default
`Model`, and a list of env vars that supply the API key.

| Profile name     | Provider    | Service         | Base URL                                    | API key env vars                          |
|------------------|-------------|-----------------|---------------------------------------------|-------------------------------------------|
| `ollama`         | `openai`    | `ollama`        | `http://localhost:11434/v1`                 | (none — local)                            |
| `openrouter`     | `openai`    | `openrouter`    | `https://openrouter.ai/api/v1`              | `OPENROUTER_API_KEY`                      |
| `aihubmix`       | `openai`    | `aihubmix`      | `https://aihubmix.com/v1`                   | `AIHUBMIX_API_KEY`                        |
| `avian`          | `openai`    | `avian`         | `https://api.avian.io/v1`                   | `AVIAN_API_KEY`                           |
| `copilot`        | `openai`    | `copilot`       | `https://api.githubcopilot.com`             | `GITHUB_TOKEN`, `COPILOT_API_KEY`         |
| `cortecs`        | `openai`    | `cortecs`       | `https://api.cortecs.ai/v1`                 | `CORTECS_API_KEY`                         |
| `huggingface`    | `openai`    | `huggingface`   | `https://router.huggingface.co/v1`          | `HUGGINGFACE_API_KEY`                     |
| `ionet`          | `openai`    | `ionet`         | `https://api.intelligence.io.solutions/api/v1` | `IONET_API_KEY`                       |
| `nebius`         | `openai`    | `nebius`        | `https://api.tokenfactory.nebius.com/v1`    | `NEBIUS_API_KEY`                          |
| `synthetic`      | `openai`    | `synthetic`     | `https://api.synthetic.new/openai/v1`       | `SYNTHETIC_API_KEY`                       |
| `venice`         | `openai`    | `venice`        | `https://api.venice.ai/api/v1`              | `VENICE_API_KEY`                          |
| `vercel`         | `openai`    | `vercel`        | `https://ai-gateway.vercel.sh/v1`           | `VERCEL_API_KEY`                          |
| `xai`            | `openai`    | `xai`           | `https://api.x.ai/v1`                       | `XAI_API_KEY`                             |
| `zai-openai`     | `openai`    | `zai-openai`    | `https://api.z.ai/api/coding/paas/v4`       | `ZAI_API_KEY`                             |
| `zai-anthropic`  | `anthropic` | `zai-anthropic` | `https://api.z.ai/api/anthropic`            | `ZAI_API_KEY`                             |

13 of 15 profiles are `Provider: "openai"`. Only `zai-anthropic` uses the
Anthropic wire format. `ollama` is the only profile with no API key env var — it
is designed for local-only access.

Built-in profiles are resolved via `ResolveProfile` (`config.go:356-369`):
disk-backed JSON profile is tried first (`loadSavedProfile`), then the built-in
map (`builtinProfile`). A built-in's API key is populated from the first
matching env var found in `APIKeyEnv` (`config.go:400-408`, `builtinProfile()`).

## Profile persistence

**File:** `internal/config/config.go:264-328`

Profiles are stored as individual JSON files under `~/.bitchtea/profiles/`:

```json
{
  "name": "my-openrouter",
  "provider": "openai",
  "service": "openrouter",
  "base_url": "https://openrouter.ai/api/v1",
  "api_key": "sk-or-...",
  "model": "anthropic/claude-sonnet-4"
}
```

- `SaveProfile` (`config.go:264-279`) — marshals to JSON and writes to
  `~/.bitchtea/profiles/<name>.json`.
- `LoadProfile` (`config.go:282-298`) — reads and unmarshals; if `Service` is
  missing (old-format profile), derives it via `deriveService` (`config.go:418-430`).
- `ListProfiles` (`config.go:300-322`) — merges built-in profile names with
  on-disk `.json` files.
- `DeleteProfile` (`config.go:324-327`) — removes the file.
- `ApplyProfile` (`config.go:330-347`) — copies `Provider`, `Service`,
  `BaseURL`, `APIKey`, and `Model` from the profile into `Config`. Each field
  is only applied when non-empty, so a partial profile can set only the fields
  it cares about.

### Service identity migration on load

Profiles saved before the `Service` field existed (Phase 9) lack the `service`
key. `deriveService` (`config.go:418-430`, tested at `config_test.go:415-478`)
fills the gap:

1. If the profile name matches a built-in key, use that built-in's `Service`.
2. If the base URL host matches a known built-in's host, use that built-in's `Service`.
3. Otherwise fall back to `"custom"`.

The field is not written back; the next `/profile save` call persists it.

## Empty API key policy

**File:** `internal/config/config.go:352-354`

```go
func ProfileAllowsEmptyAPIKey(cfg Config) bool {
    return cfg.Service == "ollama"
}
```

Only the `ollama` service (local-only) is allowed to start without an API key.
This gate is checked in `main.go:65` — if the final config has an empty API key
and the service is not `ollama`, bitchtea prints an error and exits.

## Loading a profile at startup

**File:** `main.go:132-158` (`applyStartupConfig`)

1. `config.ParseRC()` reads `~/.bitchtea/bitchtearc`.
2. `ApplyRCSetCommands` processes embedded `set` lines (including `set profile <name>`).
3. CLI flags are parsed. `--profile <name>` (`config.go:190-195`) calls
   `config.ResolveProfile(name)` then `config.ApplyProfile(cfg, p)`.
4. CLI flags are re-parsed so explicit `--model` etc. override profile defaults
   using the same manual-override semantics as `/set`.

## Loading a profile at runtime (`/profile`)

**File:** `internal/ui/commands.go:612-689` (`handleProfileCommand`)

The `/profile` slash command supports five forms:

| Form | Behavior |
|------|----------|
| `/profile` (bare) | Lists all profiles (built-in + saved). |
| `/profile show <name>` | Resolves and prints profile details. |
| `/profile save <name>` | Snapshots current `Config` fields into a new saved profile. |
| `/profile load <name>` | Resolves and applies the profile, updating the agent connection. |
| `/profile delete <name>` | Removes the saved profile file. |
| `/profile <name>` | Shorthand for `load` — applies the profile silently. |

`applyProfileToModel` (`commands.go:815-839`) wires the profile's fields
through the agent's cached setters (`SetModel`, `SetBaseURL`, `SetAPIKey`,
`SetProvider`), which invalidate the cached `fantasy.Provider` so the next
stream call rebuilds with the new config. It also prints a summary message and,
if the profile has no API key, a reminder.

## Manual overrides and profile clobbering

**File:** `internal/config/rc.go:120-170`

When the user sets a connection parameter directly (not through a profile), the
current profile tag is cleared to indicate the config is now in a
manually-overridden state. This is enforced by `applySetToConfig`:

| `/set` key | Effect on `cfg.Profile` | Effect on `cfg.Service` |
|------------|------------------------|-------------------------|
| `provider` | cleared to `""`        | set to `"custom"`       |
| `model`    | cleared to `""`        | unchanged               |
| `apikey`   | cleared to `""`        | unchanged               |
| `baseurl`  | cleared to `""`        | set to `"custom"`       |

The Service → `"custom"` clobber is intentional (implemented at `rc.go:123-128`,
`140-146`): when the user manually points at a different wire format or a
non-standard base URL, the previous service identity may no longer be accurate,
and per-service behavior gates (cache markers, reasoning forwarding) should
fall back to a safe default.

## Wire format routing (`buildProvider`)

**File:** `internal/llm/providers.go:30-41`

`buildProvider` turns the user's `Provider` + `Service` + `BaseURL` + `APIKey`
into a `fantasy.Provider`. The dispatch is:

```
cfg.provider == "anthropic"    → buildAnthropicProvider
cfg.provider == "openai" / ""  → routeByService (if service set)
                              → routeOpenAICompatible (if no service)
```

### Service-based routing (`routeByService`)

**File:** `internal/llm/providers.go:58-86`

When `cfg.service` is non-empty, routing is by named service identity:

| Service           | fantasy provider package | Notes                                    |
|-------------------|--------------------------|------------------------------------------|
| `openrouter`      | `openrouter`             | No baseURL override — SDK uses DefaultURL |
| `vercel`          | `vercel`                 | No baseURL override                      |
| `openai`, `ollama`, `zai-openai`, `aihubmix`, `copilot`, `xai`, `custom`, and everything else | `openaicompat` | BaseURL passed verbatim. Empty-baseURL OpenAI is NOT handled here — see below. |

### Host-based routing (`routeOpenAICompatible`)

**File:** `internal/llm/providers.go:90-144`

When `cfg.service` is empty (pre-Phase-9 configs), routing falls back to
parsing the host of `cfg.baseURL`:

| Host pattern                     | fantasy provider | Base URL        |
|----------------------------------|------------------|-----------------|
| Empty baseURL                    | `openai`         | upstream OpenAI |
| `api.openai.com`                 | `openai`         | as-is           |
| `openrouter.ai`                  | `openrouter`     | as-is           |
| `ai-gateway.vercel.sh`           | `vercel`         | as-is           |
| Everything else (ollama, z.ai, localhost, custom proxies) | `openaicompat` | as-is |

Host comparison is via `hostOf` (`providers.go:148-154`), which parses the URL
and lower-cases the host. `hostOfMust` (`providers.go:158-163`) panics on
malformed compile-time constants.

### Anthropic base URL cleanup (`stripV1Suffix`)

**File:** `internal/llm/providers.go:170-172`

The `anthropic` fantasy provider's SDK appends `/v1` internally, so a
user-supplied `ANTHROPIC_BASE_URL` that already ends in `/v1` would double it.
`stripV1Suffix` trims a trailing `/v1` or `/v1/` before passing it to the SDK.

## Per-service behavior gates

**File:** `internal/llm/cache.go:28-43`

The `Service` field is the gate for prompt-cache marker placement:

- Only `Service == "anthropic"` (native Anthropic) enables cache markers.
- `zai-anthropic` is explicitly excluded until proxy round-trip tests confirm
  `cache_control` works through the z.ai proxy.

Other planned gates (reasoning forwarding, include_usage suppression) are
tracked under epic `bt-p9`.

## Price source wiring

**File:** `internal/llm/cost.go`

The `CostTracker` uses a `PriceSource` abstraction to look up model pricing.
Two sources exist:

- **`EmbeddedPriceSource`** (`cost.go:152-156`) — the default. Uses
  `embedded.GetAll()` from the catwalk snapshot compiled into the binary.
- **`CatalogPriceSource`** (`cost.go:162-174`) — backed by the on-disk
  `~/.bitchtea/catalog/providers.json` envelope from the catalog refresh
  system. Falls through to embedded on any per-model miss.

`main.go:57` sets the default to `CatalogPriceSource(catalog.Load(...))`, which
prefers the on-disk cache and falls back to embedded. This is the path used by
all `CostTracker` instances created thereafter, unless overridden per-tracker
via `SetPriceSource`.

The lookup join key (`cost.go:82`) is `modelID + service`. When `service` is
non-empty, only the matching `InferenceProvider`'s models are searched,
disambiguating model IDs that appear under multiple providers.

## Config and env var reference

| Env var                     | Where read                               | Effect |
|-----------------------------|------------------------------------------|--------|
| `OPENAI_API_KEY`            | `DefaultConfig` / `DetectProvider`       | OpenAI/fallback key |
| `OPENAI_BASE_URL`           | `DefaultConfig`                          | Base URL override |
| `ANTHROPIC_API_KEY`         | `DetectProvider`                         | Enables Anthropic provider |
| `ANTHROPIC_BASE_URL`        | `DetectProvider`                         | Anthropic base URL override |
| `OPENROUTER_API_KEY`        | `DetectProvider` / `builtinProfile`      | Enables OpenRouter profile |
| `ZAI_API_KEY`               | `DetectProvider` / `builtinProfile`      | Enables z.ai profiles |
| `BITCHTEA_MODEL`            | `DefaultConfig`                          | Default model |
| `BITCHTEA_PROVIDER`         | `DefaultConfig`                          | Default provider (`openai` / `anthropic`) |

| CLI flag     | Config field affected | Profile clobber? |
|--------------|-----------------------|------------------|
| `--model`    | `Model`               | Yes (clears `Profile`) |
| `--profile`  | All connection fields | Yes (sets `Profile`) |

| Slash command           | Config field(s) affected | Profile clobber? |
|------------------------|--------------------------|------------------|
| `/set provider`        | `Provider`, `Service`→`custom` | Yes (clears `Profile`) |
| `/set model`           | `Model`                  | Yes (clears `Profile`) |
| `/set apikey`          | `APIKey`                 | Yes (clears `Profile`) |
| `/set baseurl`         | `BaseURL`, `Service`→`custom` | Yes (clears `Profile`) |
| `/set service`         | `Service` only           | No                |
| `/profile load <name>` | All connection fields    | Yes (sets `Profile`) |

## Design rationale

Originally documented in `archive/phase-9-service-identity.md` (archived).

**Why `Service` is separate from `Provider`.** `Provider` is the *wire
format* — `openai` or `anthropic`. Every OpenAI-compatible upstream
(Ollama, OpenRouter, z.ai, Vercel, xAI, copilot, aihubmix, etc.) ships
with `Provider: "openai"`. That single field is not enough to decide
whether OpenRouter `reasoning` params should be forwarded, whether to
suppress Ollama's `stream_options.include_usage`, whether Anthropic
`cache_control` blocks are safe (native vs proxy that merely speaks the
wire format like `zai-anthropic`), whether an empty `APIKey` is allowed,
or which fantasy provider package to instantiate. `Service` is the source
of truth for those gates; `Provider` keeps its wire-dialect role.

**URL sniffing breaks the moment a user sets a custom BaseURL.** Pre-
Phase-9 code inferred service from the host of `BaseURL` (e.g.
`localhost:11434` → ollama, `openrouter.ai` → openrouter). That works
until a user runs an Ollama instance on a non-standard port, fronts
OpenRouter with a corporate proxy, or points `OPENAI_BASE_URL` at a
local development stub. Carrying `Service` explicitly makes the gate
intentional rather than a coincidence of hostname matching. The
host-based `routeOpenAICompatible` path is kept as a fallback for
pre-Phase-9 configs only.

**Lazy default-on-load, no rewrite.** Profiles saved before `Service`
existed lack the field. On `LoadProfile`, an empty `Service` is
back-filled by `deriveService`: built-in name match wins, then base URL
host match against known built-ins, then fall back to `"custom"`. The
file is *not* rewritten — the next deliberate `/profile save` persists
the field naturally. This keeps disk diffs minimal and avoids the class
of bugs that come with implicit-write-on-read.

**`/set baseurl` and `/set provider` clobber `Service` to `"custom"`.**
The user is opting into a custom transport — the previous service
identity may no longer be accurate, and per-service behavior gates
(cache markers, reasoning forwarding) should fall back to a safe
default rather than apply stale gating against the new endpoint. The
unconditional clobber was chosen over a conditional "only when host no
longer matches" rule because the simpler rule is easier to reason about;
revisit if it ever surprises users editing a known-service URL in place.
`/set service` is the explicit knob for users who want to keep the new
URL but assert a known service identity.

**`Service: "ollama"` is the only allowed empty-API-key case.**
`ProfileAllowsEmptyAPIKey` simply tests `cfg.Service == "ollama"` — no
URL prefix sniffing, no special-case for `localhost`. Ollama is the only
shipped service designed for fully local access without auth; every
other service requires a key and is rejected at startup if one is
missing.

**Rollback is clean.** Deleting the `Service` field from `Config` and
`Profile` reverts cleanly: saved profiles with `service: "..."` survive
the downgrade because old binaries ignore unknown JSON fields (no
`DisallowUnknownFields` on profiles). The only loss is per-service
gates, which fall back to the URL-sniffing paths that are still
present.
