> **Status:** SHIPPED for client; resources/prompts/sampling future

# Phase 6: MCP Transport, Config & Security Contract

Status: design only. Implementation tracked under epic `bt-p6` (`bt-p6-client`,
`bt-p6-tools`, `bt-p6-resources`, `bt-p6-security`, `bt-p6-verify`). This doc
is the contract those tasks must satisfy. Sibling task `bt-p6-security` owns
the deeper audit/permission detail; this doc only stakes out the interface.

## Security Checklist

Read this first. Every item is a hard rule, enforced by `internal/mcp` or
the MCP client manager. If a future PR breaks one of these, the change is
wrong even if tests pass.

- **Disabled by default.** No `mcp.json` in the workspace = zero MCP code
  paths exercised. A user who has never heard of MCP must never see an
  MCP-flavored error, tool name, or transcript line.
- **Three opt-in layers, all required.** (1) `<WorkDir>/.bitchtea/mcp.json`
  exists, (2) top-level `enabled: true`, (3) per-server `enabled: true`
  (default). Failing any layer silences the corresponding scope.
- **Per-workspace, never global.** Global profiles and `~/.bitchtearc`
  must never enable MCP implicitly. The only enable site is the workspace
  file.
- **No inline secrets.** `mcp.json` is loaded with a screen for known
  token prefixes (`sk-`, `ghp_`, `github_pat_`, `gho_`, `sk-ant-`,
  `xoxb-`, `xoxp-`, `AKIA`, `AIza`). A literal hit anywhere in
  `command`, `args`, `url`, `env`, or `headers` refuses the whole file.
  This is a heuristic, not a vault — but it catches every obvious paste.
- **Env-only secret resolution.** The single sanctioned form is
  `${env:VARNAME}`. No `$VAR`, no shell expansion, no defaulting.
  A reference to an unset variable is a load-time error and the server
  is skipped (we do NOT silently substitute the empty string).
- **Tool args and results flow through transcript unredacted.** Same
  treatment as `bash` output. If a user passes a token *as a tool
  argument*, it shows up — that is expected behavior, not a bug.
- **Resolved env maps and HTTP headers NEVER reach transcript or
  session JSONL.** Use `mcp.RedactServer` / `mcp.RedactString` at every
  emit site. Connection config is structurally invisible to logging.
- **Tool side effects are real.** MCP tools can write files, run
  commands, hit remote APIs, charge money, page on-call humans, and
  destroy data. Built-in tools at least live in this repo and have been
  audited; a third-party MCP server is *its author's* code running with
  *your* environment. Treat enabling a server as installing software.
- **Permission hook is consulted on every call.** Even reconnect
  retries. Built-in tools never go through it. v1 default is allow-all
  (the trust gate is enabling MCP at all); a stricter `Authorizer`
  lands in a follow-up task.
- **A failing MCP server cannot crash the agent loop.** Connect
  timeouts, mid-turn server death, schema errors, and HTTP non-2xx all
  surface as a tool error returned to the model — never a Go panic and
  never a silent hang.

## Goals

1. Let the user point bitchtea at one or more Model Context Protocol servers
   and have their tools show up alongside the built-in registry without
   colliding with it.
2. Keep MCP **disabled by default** and **per-workspace** — global profile
   and `~/.bitchtearc` never enable it implicitly.
3. Don't leak secrets passed to MCP servers into session JSONL or the
   transcript.
4. Survive a flaky/crashing MCP server without taking the agent loop down.

Non-goals for v1: prompts (the third MCP primitive), sampling-back-into-the-
host, dynamic server install, GUI server picker.

## Transports

bitchtea v1 supports **two** transports, both via
`github.com/modelcontextprotocol/go-sdk`:

| Transport | When to use                                     | v1 |
|-----------|-------------------------------------------------|----|
| `stdio`   | Local subprocess, the dominant MCP shape today  | yes (default) |
| `http`    | Streamable HTTP, for remote/shared servers      | yes |
| `websocket` | Rare in the wild, no compelling user           | no |

Wire format follows the MCP spec
(<https://modelcontextprotocol.io/specification>) — JSON-RPC 2.0 framing for
both. The go-sdk handles framing; we don't reimplement it.

Both up front because stdio covers everything `npm install`-able locally,
and HTTP covers the homelab-shared-server case real for solo-dev setups.
Adding HTTP later would force a config-format migration.

## Config format

MCP server declarations live in a **per-workspace** file:

```
<WorkDir>/.bitchtea/mcp.json
```

Per-workspace because the useful set of MCP servers is a property of *this
checkout*, not the user. Global config is intentionally not a location for MCP.

JSON shape:

```json
{
  "enabled": false,
  "servers": [
    {
      "name": "fs",
      "transport": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "."],
      "env": { "FOO": "${env:FOO}" },
      "enabled": true
    },
    {
      "name": "issues",
      "transport": "http",
      "url": "https://mcp.example.local/v1",
      "headers": { "Authorization": "Bearer ${env:ISSUES_TOKEN}" },
      "enabled": true
    }
  ]
}
```

Field rules:

- `enabled` (top-level): master switch. Defaults `false` if file is missing
  or field is absent. **No MCP traffic happens unless this is `true`.**
- `servers[].name`: lowercase ASCII `[a-z0-9_-]+`, unique within file. Used
  as namespace prefix (see "Tool naming"). Reserved: `bitchtea`, `mcp`, `local`.
- `servers[].transport`: `"stdio"` or `"http"`. Anything else → load error,
  server skipped, surfaced in transcript once.
- `servers[].enabled`: per-server toggle, defaults `true`.
- stdio: `command` (required), `args` (optional), `env` (added to inherited).
- http: `url` (required, https/http), `headers` (optional).

Env-var interpolation: any string value in `env`, `headers`, `args`, `url`,
or `command` may contain `${env:VARNAME}`. At load time we substitute from
the bitchtea process env. A missing variable is a load-time error for that
server (server skipped, message logged once); we do not substitute empty
string. This is the only interpolation form — no `$VAR`, no shell.

`mcp.json` parses with `DisallowUnknownFields` so a typo'd key fails loudly.

## Trust model & secrets

Three rules:

1. **Secrets live in the user's environment, not in `mcp.json`.** The file
   may be git-tracked; raw tokens never go in it. The `${env:...}` rule
   above is the only sanctioned way to get a secret to a server. Plain
   strings aren't blocked (we can't tell), but docs warn against them.
2. **Tool args and results are not redacted by default.** MCP calls flow
   through the same transcript path as built-in tools. If the user passes
   a token *as a tool argument*, it appears in transcript — same as `bash`.
3. **Configured secrets (env vars, headers) never appear in transcripts or
   session JSONL.** The MCP client logs server name + tool name + args +
   result. It does *not* log the resolved env map or HTTP headers. The
   `tool_start` UI line shows `mcp__<server>__<tool>` and call args; it
   never renders the server's connection config.

Per-workspace permission state (allow/deny per server, per tool) is owned
by `bt-p6-security` and lives in
`<WorkDir>/.bitchtea/mcp-permissions.json`. This doc just guarantees the
client manager exposes the hook before any tool call dispatches.

## Tool naming

MCP tools are exposed to the LLM as:

```
mcp__<server>__<tool>
```

Double-underscore separators, matching the convention Claude Code's MCP
plugins already use — keeps prompts portable. Examples: `mcp__fs__read_file`,
`mcp__issues__create_ticket`.

Resolves the collision case: a server exposing `read` becomes `mcp__fs__read`,
distinct from the built-in `read`. Built-in names stay unprefixed.

Implementation lands in `internal/tools` as a thin wrapper that:

1. Holds the live MCP client connections (managed by `bt-p6-client`).
2. Appends discovered MCP tools to `Definitions()` after the built-in list,
   prefixed and with the server's JSON schema passed through as-is.
3. In `Execute()`, recognizes the `mcp__` prefix and routes to the matching
   client connection instead of the built-in switch.

Name validation: a server-reported tool whose name contains characters
outside `[A-Za-z0-9_]` is logged and skipped. The agent never sees an
invalid identifier.

## Resource exposure

MCP defines three primitives: **tools**, **resources**, **prompts**.

| Primitive  | bitchtea v1 |
|------------|-------------|
| Tools      | yes — full surface, exposed as `mcp__<server>__<tool>` |
| Resources  | read-only inline via `@mcp:<server>/<uri>` (stretch) |
| Prompts    | no — out of scope for v1 |

Tools is the must-have. Resources are the natural extension of bitchtea's
`@file` token: `@mcp:fs/path/to/file` would resolve via the named server's
resource API and inline content the same way `@file` does. Implemented by
`bt-p6-resources`; if syntax turns out awkward, v1 ships tools-only and
resources land in v1.1.

Prompts skipped: templated-prompt-injection overlaps badly with bitchtea's
slash commands and per-workspace `AGENTS.md`/`CLAUDE.md` discovery.

Sampling (server-asks-host-to-call-an-LLM) is not supported. Servers that
require it will see "method not found" and should degrade.

## Failure behavior

A misbehaving MCP server must never take down the agent loop:

- **Slow startup**: each stdio server gets a 5s connect deadline. On timeout,
  server is marked failed, listed once (`mcp: server <name> failed to start:
  timeout`), skipped for the session.
- **Server crash mid-turn**: in-flight tool call returns synthetic error
  `mcp: server <name> died` to the model (same shape as cancellation result
  in `phase-8-cancellation-state.md`). Manager attempts a one-shot
  reconnect on the *next* call to that server; if reconnect fails, server
  is marked failed for the session.
- **Schema error**: an unparseable tool definition (bad JSON schema, name
  collision after sanitization) drops *that one tool* at registration; the
  rest of the server's tools load normally.
- **HTTP non-2xx**: returned to the model as the tool error string. No retry.
- **All servers failed or none configured**: bitchtea behaves exactly as
  today — built-in tools only.

A failed-for-session server can be retried with a future `/mcp reload`
slash command (not in this phase).

## Disabled by default

Three layers of opt-in, evaluated in order:

1. `<WorkDir>/.bitchtea/mcp.json` exists.
2. Top-level `enabled: true` in that file.
3. Per-server `enabled: true` (default) on each entry.

If any layer fails, that server is silent — no startup, no transcript line,
no schema injection. A user who has never heard of MCP must never see
MCP-flavored errors or tool names.

A future `/set mcp on|off` and `/mcp <server> on|off` pair will toggle
layers 2 and 3 in-memory for the running session. Persisting those toggles
requires a write-back to `mcp.json`; that is a follow-on task.

## Permission boundaries (interface only)

The MCP client manager dispatches every tool call through a single
`Authorize(server, tool, args) (bool, reason)` hook before forwarding. v1
wires this hook to "always allow" when MCP is enabled —
`bt-p6-security` replaces it with a real per-server, per-tool allow/deny
store backed by `<WorkDir>/.bitchtea/mcp-permissions.json` and a
prompt-on-first-use UX.

The contract this doc nails down:

- Hook is consulted *for every MCP tool call*, including reconnect retries.
  Built-in tools do not go through it.
- A `false` result returns synthetic error `mcp: <server>:<tool> denied by
  policy` to the model. Model may retry with different args; fresh
  authorize call.
- Authorize runs *with resolved args* (after JSON parsing) but before they
  reach the wire — lets `bt-p6-security` implement argument-shape gates.

## Compatibility & rollback

- **No `mcp.json`**: identical to today. No code path runs.
- **`mcp.json` present, `enabled: false`**: file parsed for syntax errors
  only (so user gets feedback when they enable it later). No connections.
- **Rollback**: deleting the MCP wrapper and client manager package reverts
  cleanly. `mcp.json` files become inert. No on-disk migration needed —
  MCP additions are purely additive over the built-in registry.
- **Old binary, new `mcp.json`**: file is ignored. No errors.

## Open questions

- Lifecycle: spin servers at session start, or lazy-start on first tool
  call? Leaning eager-start so schema is available when the system prompt
  is built; revisit if startup latency becomes a problem with many servers.
- Path: `<WorkDir>/.bitchtea/mcp.json` vs workspace-root `.bitchtea-mcp.json`?
  Picked the former for symmetry with the existing per-workspace memory
  layout; revisit if users want a flatter root.
- Resource URI prefix: `@mcp:<server>/<path>` vs `@<server>:<path>`. Punted
  to `bt-p6-resources`.
- Should `Authorize` also receive the *result* for an audit log post-call?
  `bt-p6-security` decides; manager interface should leave room for a
  `Record(server, tool, args, result)` callback.
- Expose `/mcp list` in this phase or wait for `bt-p6-verify`? Leaning
  verify — keeps this phase pure-design.

## Status

Client, tools, security shipped; resources read with cap; sampling deferred;
prompts deferred indefinitely. Design rationale (per-workspace, three-layer
opt-in, env-only interpolation, namespace convention, secret-redaction
boundary, no-prompts/no-sampling reasoning) was ported into `docs/mcp.md`
under the Design rationale section. This document is retained for
historical context.
