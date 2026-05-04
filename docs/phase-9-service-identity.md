> **Status:** SHIPPED

# Phase 9: Service Identity Field

Status: design only. Implementation tracked under epic `bt-p9`.

## Problem

Today `Config.Provider` and `Profile.Provider` (see `internal/config/config.go:21`,
`internal/config/config.go:115`) carry the **wire format** — only `"openai"` or
`"anthropic"`. Every OpenAI-compatible upstream (Ollama, OpenRouter, z.ai,
Vercel, xAI, copilot, aihubmix, etc.) ships with `Provider: "openai"`.

That is not enough information to decide:

- whether OpenRouter `reasoning` params should be forwarded,
- whether to suppress Ollama's `stream_options.include_usage`,
- whether Anthropic `cache_control` blocks are safe (native Anthropic vs a
  proxy that merely speaks the wire format, e.g. `zai-anthropic`),
- whether an empty `APIKey` is allowed (today: URL-prefix sniff for
  `localhost:11434`, see `ProfileAllowsEmptyAPIKey` in
  `internal/config/config.go:334`),
- which fantasy provider package to instantiate (today: host-of-baseURL match
  in `internal/llm/providers.go:49`).

Every one of those decisions currently relies on URL sniffing. URL sniffing
breaks the moment a user sets a custom `BaseURL` for a known service, and
makes per-service feature gates implicit.

## Service field semantics

Add `Service string` to both `Config` and `Profile`. Compared to `Provider`:

| Field      | Meaning                  | Example values                                                                 |
|------------|--------------------------|--------------------------------------------------------------------------------|
| `Provider` | Wire format / dialect    | `openai`, `anthropic`                                                          |
| `Service`  | Upstream service identity| `openai`, `anthropic`, `ollama`, `openrouter`, `vercel`, `zai-openai`, `zai-anthropic`, `xai`, `copilot`, `aihubmix`, `avian`, `cortecs`, `huggingface`, `ionet`, `nebius`, `synthetic`, `venice`, `custom` |

Rules:

- `Service` is the source of truth for per-service behavior gates
  (`ProfileAllowsEmptyAPIKey`, reasoning forwarding, cache blocks, usage
  suppression).
- `Provider` keeps its current meaning — it picks the wire dialect and is
  consumed by `buildProvider` in `internal/llm/providers.go`. Eventually
  `buildProvider` should switch on `Service` first and use `Provider` only as
  a fallback for `Service: "custom"`.
- `Service` is lowercased ASCII, matches built-in profile keys when one
  applies, and is `"custom"` for everything else.

## Defaults for built-in profiles

Update `builtinProfileSpec` (`internal/config/config.go:142`) to carry a
`Service` field. Mapping:

| Profile name    | Provider    | Service         |
|-----------------|-------------|-----------------|
| `ollama`        | `openai`    | `ollama`        |
| `openrouter`    | `openai`    | `openrouter`    |
| `aihubmix`      | `openai`    | `aihubmix`      |
| `avian`         | `openai`    | `avian`         |
| `copilot`       | `openai`    | `copilot`       |
| `cortecs`       | `openai`    | `cortecs`       |
| `huggingface`   | `openai`    | `huggingface`   |
| `ionet`         | `openai`    | `ionet`         |
| `nebius`        | `openai`    | `nebius`        |
| `synthetic`     | `openai`    | `synthetic`     |
| `venice`        | `openai`    | `venice`        |
| `vercel`        | `openai`    | `vercel`        |
| `xai`           | `openai`    | `xai`           |
| `zai-openai`    | `openai`    | `zai-openai`    |
| `zai-anthropic` | `anthropic` | `zai-anthropic` |

For env-detected configs (`DetectProvider` in `internal/config/config.go:77`):

| Env var present     | Provider    | Service     |
|---------------------|-------------|-------------|
| `ANTHROPIC_API_KEY` | `anthropic` | `anthropic` |
| `OPENROUTER_API_KEY`| `openai`    | `openrouter`|
| `ZAI_API_KEY`       | `openai`    | `zai-openai`|
| `OPENAI_API_KEY`    | `openai`    | `openai`    |

The default `Config` (no env, no profile) gets `Provider: "openai"`,
`Service: "openai"` to match `BaseURL: https://api.openai.com/v1`.

## JSON profile migration

Old profiles on disk look like:

```json
{ "name": "myrouter", "provider": "openai", "base_url": "https://...",
  "api_key": "...", "model": "..." }
```

Strategy: **lazy default-on-load, no rewrite**.

1. Add `Service string \`json:"service,omitempty"\`` to `Profile`.
2. After `json.Unmarshal` in `loadSavedProfile` (and `LoadProfile`), if
   `p.Service == ""`, derive a default:
   - if `p.Name` matches a built-in key, use that built-in's `Service`;
   - else if `p.BaseURL` host matches a known built-in's host, use that
     built-in's `Service`;
   - else fall back to `"custom"`.
3. Do **not** rewrite the file. The next `/profile save` (which always
   marshals the in-memory `Profile`) will persist the field naturally.
4. `omitempty` on the JSON tag means freshly-saved profiles whose `Service`
   somehow ends up empty still round-trip cleanly through old binaries.

`SaveProfile` writes the field whenever it is set. `/profile save` in
`internal/ui/commands.go:629` must be updated to copy `m.config.Service` into
the new `Profile`.

`ApplyProfile` (`internal/config/config.go:312`) gets one extra clause:

```
if p.Service != "" { cfg.Service = p.Service }
```

## Env override behavior

Precedence stays as it is today, with `Service` riding alongside `Provider`
at every layer:

1. **Built-in defaults** (`DefaultConfig`) — `Service: "openai"`.
2. **Env detection** (`DetectProvider`) — sets both `Provider` and `Service`
   per the table above.
3. **`~/.bitchtearc`** (`ApplyRCSetCommands`) — `set profile <name>` resolves
   a profile and applies `Service`. A future `set service <id>` would
   override directly; not in scope for this phase.
4. **CLI flags / `--profile`** — `ApplyProfile` overwrites `Service` when the
   loaded profile carries one.
5. **Slash commands** — `/profile load` overwrites `Service`. `/set provider`
   and `/set baseurl` keep their current behavior and **do not** change
   `Service`; the user is opting into a custom transport, so `Service`
   should be set to `"custom"` whenever either of those commands runs to
   avoid stale gating. (Document this in `commands.md` when the change
   ships.)

There is no `BITCHTEA_SERVICE` env var in this phase. If one is added later
it slots in at layer 2.

## Compatibility & rollback

- **Old binary, new on-disk profile**: extra `service` JSON field is ignored
  by `encoding/json` (no `DisallowUnknownFields`). Behavior unchanged from
  today.
- **New binary, old on-disk profile**: `Service` defaults via the lazy
  derivation above. No file rewrite, no migration step at startup.
- **`/profile save` round-trip**: `omitempty` keeps the file diff minimal
  for profiles that legitimately have no service (none in practice once the
  defaulter runs).
- **Rollback**: deleting the `Service` field from `Config`/`Profile`
  reverts cleanly. Saved profiles with `service: "..."` survive the
  downgrade because old binaries ignore unknown fields. The only loss is
  the per-service gates, which fall back to today's URL-sniffing paths.
- **TODO comments** in `config.go:123` and `config.go:333` mentioning
  "Phase 6" should be retargeted at `bt-p9` once the field lands; the
  `ProfileAllowsEmptyAPIKey` body becomes `cfg.Service == "ollama"`.

## Out of scope

- Rewriting `buildProvider` to dispatch on `Service` instead of host. That is
  `bt-1p6`'s test surface and a follow-up implementation issue.
- Adding `BITCHTEA_SERVICE` env var.
- A `set service` rc / slash command.
- Migrating the per-service feature gates (cache_control, reasoning,
  include_usage) — those are separate issues under `bt-p9`.

## Open questions

- Should `/set baseurl` and `/set provider` clobber `Service` to `"custom"`
  unconditionally, or only when the new value no longer matches a known
  built-in's host? Proposal above takes the simpler unconditional path; revisit
  if it surprises users editing a known-service URL in place.
- For env detection, when the user has both `OPENAI_API_KEY` and a custom
  `OPENAI_BASE_URL` pointing at, e.g., a local proxy: `Service` will still
  be `"openai"`. Acceptable for now; a future heuristic could downgrade to
  `"custom"` when the host doesn't match `api.openai.com`.
