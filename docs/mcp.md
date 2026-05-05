# MCP Integration Reference

bitchtea supports the [Model Context Protocol (MCP)](https://modelcontextprotocol.io/)
for connecting external tool servers. This document covers configuration, server
lifecycle, tool registration, security, and failure modes from both user and
maintainer perspectives.

## What MCP Provides

MCP servers extend bitchtea with additional tools the LLM can call. Each MCP
server exposes:

- **Tools** — executable functions the LLM can invoke (e.g., filesystem access,
  database queries, API calls).
- **Resources** — read-only data the LLM can reference (e.g., documentation,
  schemas, logs).
- **Prompts** — pre-written prompt templates the LLM can use.

MCP is entirely opt-in. No MCP server starts unless the user creates a config
file and enables it.

## Configuration

### File Location

MCP servers are configured in `<workdir>/.bitchtea/mcp.json`. The file path
resolves against the working directory. Config is loaded by
`mcp.LoadConfig(workDir)` in `internal/mcp/config.go`.

### Three-Layer Disable-by-Default

MCP is OFF unless the user opts in at three levels:

1. **File must exist** — no `mcp.json` means MCP is disabled.
2. **Top-level `enabled` must be `true`** — a kill switch for all servers.
3. **Per-server `enabled` must be `true`** — individual server toggles.

If any layer is missing or false, that server (or all servers) is disabled.

### Config Schema

```json
{
  "enabled": true,
  "servers": [
    {
      "name": "my-server",
      "transport": "stdio",
      "enabled": true,
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "."],
      "env": {
        "MY_API_KEY": "${env:MY_API_KEY}"
      }
    },
    {
      "name": "remote-api",
      "transport": "http",
      "enabled": true,
      "url": "https://mcp.example.com",
      "headers": {
        "Authorization": "Bearer ${env:REMOTE_MCP_TOKEN}"
      }
    }
  ]
}
```

### Transport Types

| Transport | Fields Required | Description |
|-----------|----------------|-------------|
| `stdio` | `command`, optional `args`, `env` | Launches a subprocess speaking MCP over stdio. |
| `http` | `url`, optional `headers` | Connects to an HTTP(S) SSE endpoint. |

### Environment Variable Interpolation

Values in `command`, `url`, `args`, `env` values, and `headers` values can
reference environment variables with the `${env:VARNAME}` syntax:

```json
{
  "env": {
    "GITHUB_TOKEN": "${env:GITHUB_TOKEN}"
  }
}
```

An unset environment variable is a **load-time error** — the server will not
start. The empty string is not substituted for missing variables.

### Inline Secret Detection

If any config value contains a well-known secret prefix (`sk-`, `ghp_`,
`sk-ant-`, `AKIA`, etc.) as a literal string, `LoadConfig` rejects the file.
Secrets must come through `${env:...}` interpolation, never inline. This is
enforced by `looksLikeInlineSecret` in `internal/mcp/config.go`.

### Server Name Validation

Server names must match `[a-z0-9_-]+`. Duplicate server names in the same
config file are rejected at load time.

## Server Lifecycle

Server lifecycle is managed by `mcp.Manager` (`internal/mcp/manager.go`).

### Startup

1. **Config loaded** — `LoadConfig` parses and validates `mcp.json`.
2. **Manager created** — `NewManager(cfg, auth, audit)` wires config,
   Authorizer, and AuditHook.
3. **Manager.Start(ctx)** — all enabled servers start in parallel:
   - Each server has a **per-server start timeout** (default 5s).
   - The aggregate start has a **manager start timeout** (default 10s).
   - Servers that exceed their per-server timeout are marked **unhealthy**
     but do not block other servers.
   - A misbehaving MCP server **never** takes down the agent loop.

### Handshake

Each server performs the MCP initialize handshake through the SDK's
`client.Connect`. For stdio servers, the subprocess is launched via
`exec.CommandContext`; for HTTP servers, the SDK connects via an SSE transport
with an optional header-injecting `http.Client`.

### Tool Registration

After startup, `Manager.ListAllTools(ctx)` collects tools from every running
server. Each tool is tagged with its server's name and namespaced (see below).
Tools with invalid names (characters outside `[A-Za-z0-9_]`) are dropped.

### Teardown

`Manager.Stop(ctx)` tears down every server in parallel under a stop timeout
(default 5s). Servers that error on stop are collected and joined; Stop still
attempts every other server. Calling Stop on a never-started Manager is a
no-op.

## Tool Namespacing

MCP tools are namespaced to prevent collisions between servers and with
built-in tools:

```
mcp__<server>__<tool>
```

For example, a tool named `write_file` from server `filesystem` becomes:

```
mcp__filesystem__write_file
```

The separator (`__`) matches Claude Code's convention so prompts written for
that host stay portable. Namespacing is handled by `namespaceName` in
`internal/mcp/manager.go` and validated in `internal/llm/mcp_tools.go`.

## Collision Rules

When building the agent's tool surface, `AssembleAgentTools` in
`internal/llm/mcp_tools.go` applies these rules:

1. **Local built-in tools always win** — local tools are appended first; any
   MCP tool with a colliding name is dropped and logged.
2. **Within MCP, first registration wins** — if two servers expose a tool
   with the same namespaced name, only the first one is registered.
3. **Missing `mcp__` prefix is dropped** — if a manager-level bug produces an
   unprefixed name, it is logged and skipped (defense in depth).

Since no local tool starts with `mcp__`, collision with local tools can only
happen if a future refactor adds such a name by mistake.

## Authorizer Interface

Every MCP tool call passes through an Authorizer before execution:

```go
type Authorizer interface {
    Authorize(ctx context.Context, server string, tool string, args json.RawMessage) error
}
```

- A non-nil error denies the call and is surfaced to the model verbatim.
- Returning nil authorizes the call.
- The `args` parameter is the resolved JSON after schema validation but
  before it reaches the wire — implementations may inspect args for shape
  gates.

**Default:** `AllowAllAuthorizer` permits every call. The trust gate is the
user's decision to enable MCP at all. A stricter per-server/per-tool policy
is planned for a follow-up.

See `internal/mcp/permission.go` for the interface definition.

## AuditHook Interface

AuditHook receives lifecycle events for every MCP tool call:

```go
type AuditHook interface {
    OnToolStart(ctx context.Context, ev ToolCallStart)
    OnToolEnd(ctx context.Context, ev ToolCallEnd)
}
```

- `ToolCallStart` carries the server name, tool name, args, and timestamp.
- `ToolCallEnd` carries the result (or error), duration in ms, and timestamp.

Implementations must be **non-blocking** — hand events off to a channel or
buffer if persistence is slow. The default `NoopAuditHook` discards all events.

See `internal/mcp/audit.go` for the interface and event types.

## Secret Redaction

Tool args and results are **not redacted by default** — by enabling MCP, the
user accepts that risk. However, resolved secrets in the server config are
kept out of transcripts and session JSONL:

- **Env maps** are entirely dropped from serialized config.
- **Header values** are replaced with `<redacted>`.
- **Command, Args, URL** are left visible — they contain the binary name and
  endpoint, not secrets.

`RedactServer` in `internal/mcp/redact.go` produces a copy of a `ServerConfig`
safe for display. `RedactString` replaces any substring matching a resolved
env/header value with `<redacted>` in free-form strings.

## Resource Reading

Resources can be read from running MCP servers. `Manager.ReadResource`
supports:

- Per-resource size caps via `MaxResourceBytes` (default 1 MiB).
- A total size check across all returned content segments (text + blob).

If the total content exceeds the cap, an error is returned. This prevents a
misbehaving MCP server from OOM'ing the agent.

Resources follow the same authorizer pattern as tools in the v1
implementation.

## Failure Modes

### Server Crash (Stdio)

If a stdio subprocess crashes after startup, the server's `CallTool` will
return an error. The server entry remains in the Manager but is not
automatically restarted. The error is surfaced to the model as a tool error.

### Slow Start

A server that exceeds `PerServerStartTimeout` (5s) is marked unhealthy. The
Manager continues without it. If the server eventually starts (e.g., slow npm
install), it is not retroactively registered — the agent must be restarted.

### Config Errors

- Missing file: MCP is disabled, no error surfaced.
- Parse error: MCP is disabled, error is surfaced once at startup.
- Missing env var: the offending server is rejected, config load fails.
- Duplicate server name: config load fails.
- Invalid transport: the offending server is rejected.

### Schema Errors

If a server's `ListTools` returns a tool with an invalid name (characters
outside `[A-Za-z0-9_]`), that tool is silently dropped. Other tools from the
same server are still registered.

### Timeout

- Per-server start: 5s
- Manager aggregate start: 10s
- Stop: 5s

All configurable via `Manager` fields. Tests reduce these aggressively.

## Testing Approach

MCP tests use `fakeServer` (a `mcp.Server` implementation) injected via
`Manager.SetServerFactory`. This avoids spinning up real subprocesses:

- `manager_test.go` tests lifecycle (start/stop), tool listing, namespacing,
  timeout behavior, unhealthy tracking, and resource reads.
- `config_test.go` tests config loading, env interpolation, secret detection,
  and all error paths.
- `permission_test.go` tests the Authorizer interface.
- `mcp_tools_test.go` in `internal/llm` tests `MCPTools`, `AssembleAgentTools`,
  schema splitting, and collision rules.

## Code Layout

| File | Contents |
|------|----------|
| `internal/mcp/config.go` | Config loading, env interpolation, secret detection |
| `internal/mcp/manager.go` | Server lifecycle, tool/resource namespacing, dispatch |
| `internal/mcp/client.go` | Server interface, stdio + HTTP transport, MCP SDK wrappers |
| `internal/mcp/permission.go` | Authorizer interface + default allow-all |
| `internal/mcp/audit.go` | AuditHook interface + no-op default |
| `internal/mcp/redact.go` | Secret redaction for transcripts and session JSONL |
| `internal/llm/mcp_tools.go` | Fantasy tool adapter, tool assembly, MCP→fantasy schema bridge |

## Design rationale

Originally documented in `archive/phase-6-mcp-contract.md` (archived).

**Per-workspace, never global.** The useful set of MCP servers is a property
of *this checkout*, not the user — a frontend repo wants different servers
than a sysadmin scripting dir. Global profiles and `~/.bitchtearc` are
intentionally not enable sites; the only enable site is the workspace
`mcp.json`. This also means a hostile workspace cannot turn on MCP behind
the user's back across all of their repos.

**Three opt-in layers, all required.** (1) file exists, (2) top-level
`enabled: true`, (3) per-server `enabled: true`. A user who has never heard
of MCP must never see an MCP-flavored error, tool name, or transcript line.
Failing any layer silences the corresponding scope.

**Both transports up front.** `stdio` covers everything `npm install`-able
locally; `http` covers the homelab-shared-server case real for solo-dev
setups. Adding HTTP later would force a config-format migration. Websocket
was rejected as having no compelling user.

**`${env:VARNAME}` is the only interpolation form.** No `$VAR`, no shell
expansion, no defaulting. A reference to an unset variable is a load-time
error and the server is skipped — we do *not* silently substitute the empty
string, because an empty bearer token is a footgun, not a no-op. The inline-
secret screen (`sk-`, `ghp_`, etc.) is a heuristic, not a vault, but it
catches every obvious paste.

**Configured secrets never reach transcript or session JSONL; tool
args/results do.** Connection config (env maps, headers) is structurally
invisible to logging. Tool args and results flow through the same path as
`bash` output — if the user passes a token *as a tool argument*, it shows
up. That is expected behavior, not a bug. The boundary is "did the user
hand it to a tool intentionally" vs "did the config system resolve a
secret to make the connection work."

**`mcp__<server>__<tool>` namespace.** Double-underscore separators match
the convention Claude Code's MCP plugins already use, so prompts written for
that host stay portable. It also resolves the collision case: a server
exposing `read` becomes `mcp__fs__read`, distinct from the built-in `read`,
which keeps its unprefixed name.

**Local built-in tools always win on collision.** Built-ins are appended
first; an MCP tool with a colliding name is dropped and logged. Built-ins
live in this repo and have been audited; a third-party MCP server is *its
author's* code running with *your* environment.

**Prompts skipped, sampling not supported.** Templated-prompt-injection
overlaps badly with bitchtea's slash commands and per-workspace
`AGENTS.md` / `CLAUDE.md` discovery. Sampling (server-asks-host-to-call-an-
LLM) opens a billing and authorization hole that is not worth the v1
complexity; servers requiring it see "method not found" and should degrade.

**A failing MCP server cannot crash the agent loop.** Connect timeouts,
mid-turn server death, schema errors, and HTTP non-2xx all surface as a
tool error returned to the model — never a Go panic and never a silent hang.
A misbehaving third-party process must not be able to take down the user's
session.

**Permission hook is consulted on every call, including reconnect retries.**
Built-in tools never go through it. v1 default is allow-all (the trust gate
is enabling MCP at all); a stricter `Authorizer` is a follow-up. The hook
runs *with resolved args* (after JSON parsing) but before they reach the
wire, leaving room for a future implementation to gate on argument shape.
