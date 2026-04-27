# Crush Reference — for bitchtea

Audit of `/home/admin/bitchtea/butter/crush/` (Charm's coding-agent TUI), focused on what
to steal for `bitchtea`. Ground truth: source as of the local `butter/` checkout, not the
README.

`go.mod` highlights:

- `charm.land/fantasy v0.21.0` — agent loop / provider abstraction
- `charm.land/catwalk v0.37.14` — provider+model catalog (data only)
- `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, `charm.land/lipgloss/v2`,
  `charm.land/glamour/v2` — TUI stack
- `charm.land/fang/v2` — cobra wrapper for nicer CLI help
- `github.com/charmbracelet/openai-go` — direct, **only one import** (in
  `internal/agent/coordinator.go` for `openaisdk.WithJSONSet`)
- `github.com/charmbracelet/anthropic-sdk-go` — **indirect only** (transitively via
  fantasy's anthropic provider)
- `github.com/modelcontextprotocol/go-sdk` — MCP client
- `github.com/ncruces/go-sqlite3` + `github.com/pressly/goose/v3` — embedded SQLite +
  migrations (sessions, messages, history)
- `mvdan.cc/sh/v3` — bash interpreter for the bash tool (no real `/bin/sh`)
- `github.com/sourcegraph/jsonrpc2` — for LSP clients

---

## 1. Crush feature list

### 1.1 CLI entry points (`internal/cmd/`)

Cobra root in `internal/cmd/root.go` with these subcommands:

- `crush` (interactive TUI; default)
- `crush run [prompt...]` — non-interactive single shot, with `--quiet`,
  `--verbose`, `--model`, `--small-model`, `--session`, `--continue`. Reads stdin.
  (`internal/cmd/run.go`)
- `crush dirs` — print data/config/cache dirs (`dirs.go`)
- `crush projects` — list and inspect known projects (`projects.go`)
- `crush update-providers` — force refresh of the catwalk catalog
  (`update_providers.go`)
- `crush logs [-f]` — tail crush log file (`logs.go`)
- `crush schema` — print JSON Schema for `crush.json` (`schema.go`)
- `crush login` — OAuth flow for Hyper/Copilot/etc. (`login.go`)
- `crush stats` — render an HTML/SVG stats report (`stats.go`, `cmd/stats/`)
- `crush session ...` — list/inspect/export/delete sessions (`session.go`, ~720 LoC)
- `crush server` — runs the daemon server itself (`server.go`)

Root flags: `--cwd`, `--data-dir`, `--debug`, `--host`, `--yolo`, `--session`,
`--continue`. `--yolo` auto-approves all permission prompts.

`main.go` is 35 lines: pprof if `CRUSH_PROFILE`, then `cmd.Execute()`. Also pulls
`_ "github.com/joho/godotenv/autoload"` — picks up `.env` automatically.

### 1.2 Slash commands / dialog actions (`internal/ui/dialog/commands.go`)

Crush does NOT use IRC-style `/cmd` parsing. Instead, "commands" are entries in a
fuzzy-filtered command palette dialog (opened via a keybind). The full system list
comes from `defaultCommands()` (lines ~420-531):

| ID                          | Label                          | Keybind | Action                            |
| --------------------------- | ------------------------------ | ------- | --------------------------------- |
| `new_session`               | New Session                    | ctrl+n  | `ActionNewSession`                |
| `switch_session`            | Sessions                       | ctrl+s  | open Sessions dialog              |
| `switch_model`              | Switch Model                   | ctrl+l  | open Models dialog                |
| `summarize`                 | Summarize Session              | —       | `ActionSummarize{SessionID}`      |
| `toggle_thinking`           | Enable/Disable Thinking Mode   | —       | Anthropic models with `CanReason` |
| `select_reasoning_effort`   | Select Reasoning Effort        | —       | OpenAI models with effort levels  |
| `toggle_sidebar`            | Toggle Sidebar                 | —       | wide windows only                 |
| `file_picker`               | Open File Picker               | ctrl+f  | image-supporting models           |
| `open_external_editor`      | Open External Editor           | ctrl+o  | requires `$EDITOR`                |
| `enable_docker_mcp`         | Enable Docker MCP Catalog      | —       | if Docker MCP detected            |
| `disable_docker_mcp`        | Disable Docker MCP Catalog     | —       | when active                       |
| `toggle_pills`              | Toggle To-Dos/Queue            | ctrl+t  | when present                      |
| `toggle_notifications`      | Enable/Disable Notifications   | —       |                                   |
| `toggle_yolo`               | Toggle Yolo Mode               | —       | bypasses permission prompts       |
| `toggle_help`               | Toggle Help                    | ctrl+g  |                                   |
| `init`                      | Initialize Project             | —       | runs the `initialize.md.tpl`      |
| `toggle_transparent`        | Enable/Disable Background      | —       |                                   |
| `quit`                      | Quit                           | ctrl+c  |                                   |

Three command sources, switchable via Tab inside the dialog
(`internal/commands/commands.go`):

- **System** — the table above
- **User** — markdown files under `$XDG_CONFIG_HOME/crush/commands/`,
  `~/.crush/commands/`, and `<datadir>/commands/`. Args declared as `$UPPER_NAME`
  in body, prompted via `arguments.go`.
- **MCP** — prompt templates exposed by connected MCP servers
  (`mcp.GetPromptMessages`).

Custom command IDs are namespaced `user:foo:bar` or `project:foo:bar`.

### 1.3 Tools the agent can call (`internal/agent/tools/`)

Every tool returns `fantasy.ToolResponse` and is registered with
`fantasy.NewAgentTool[TInput]` (typed wrapper that auto-generates JSON schema from
the input struct). Wired in `coordinator.buildTools` at `coordinator.go:443-551`.

Built-in tools:

- `bash` (`bash.go`) — shell via `mvdan.cc/sh/v3`. Hard banlist for net commands
  (curl/wget/ssh/etc.) and package managers; goes through permission prompt unless
  `safeCommands` prefix matches. Supports background jobs (`run_in_background`,
  `auto_background_after`).
- `crush_info` (`crush_info.go`) — diagnostic dump of config, LSPs, skills.
- `crush_logs` (`crush_logs.go`) — tail/grep crush's own log.
- `download` (`download.go`) — URL → file with permission gate.
- `edit` (`edit.go`) — old/new string replace, integrates with `lsp` + `history` +
  `filetracker` (mtime guard, diff history).
- `multiedit` (`multiedit.go`) — atomic batch of edits to one file.
- `fetch` (`fetch.go`) — HTTP GET, HTML→Markdown via `JohannesKaufmann/html-to-markdown`.
- `glob` (`glob.go`) — `bmatcuk/doublestar` patterns.
- `grep` (`grep.go`) — wraps `rg` (`rg.go`) with fallback to native search (`search.go`).
- `job_kill` / `job_output` — for background bash jobs.
- `list_mcp_resources`, `read_mcp_resource` — MCP resource browsing.
- `ls` (`ls.go`) — directory listing.
- `lsp_restart` (`lsp_restart.go`) — restart a configured LSP.
- `diagnostics` (`diagnostics.go`) — pull LSP diagnostics for paths.
- `references` (`references.go`) — LSP textDocument/references.
- `sourcegraph` (`sourcegraph.go`) — public Sourcegraph search API.
- `todos` (`todos.go`) — per-session todo list, persisted via `sessions` service.
- `view` (`view.go`) — read file with offset/limit; emits `<system-reminder>`-style
  hints for skills (`skills` integration).
- `web_fetch` / `web_search` (`web_fetch.go`, `web_search.go`) — provider-side
  Anthropic tools (registered as fantasy tools with `ProviderExecuted`).
- `write` (`write.go`) — full-file write with mtime guard.
- MCP tools (`mcp-tools.go`) — auto-registered from each connected MCP server,
  filtered by `agent.AllowedMCP` config.

Two pseudo-tools that re-enter the agent loop:

- `agent` (`agent_tool.go`) — sub-agent (Claude-Code-style "Task" tool). Builds
  a fresh `SessionAgent` with the `task` config (different prompt + tool set).
- `agentic_fetch` (`agentic_fetch_tool.go`) — sub-agent specialized for web fetch.

Tool descriptions are markdown files (`*.md`) `//go:embed`'d next to each `.go`,
optionally truncated to first line via `CRUSH_SHORT_TOOL_DESCRIPTIONS=0`
(`tools.go:75`).

### 1.4 Provider/model support

Providers are fantasy provider drivers, selected in
`coordinator.buildProvider` (`coordinator.go:828`):

- `openai.Name` → `buildOpenaiProvider` (Responses API by default)
- `anthropic.Name` → `buildAnthropicProvider` (handles MiniMax weirdness, OAuth
  Bearer prefix, interleaved-thinking beta header)
- `openrouter.Name` → `buildOpenrouterProvider`
- `vercel.Name` → `buildVercelProvider`
- `azure.Name` → `buildAzureProvider`
- `bedrock.Name` → `buildBedrockProvider`
- `google.Name` → `buildGoogleProvider`
- `"google-vertex"` → `buildGoogleVertexProvider`
- `openaicompat.Name` / `hyper.Name` → `buildOpenaiCompatProvider` (LM Studio,
  Ollama, ZAI/GLM, Copilot, MiniMax-OAI, Hyper)

Catalog of providers + models is supplied by `charm.land/catwalk`:

- `internal/config/catwalk.go` — fetches `catwalk.GetProviders` over HTTP, falls
  back to `charm.land/catwalk/pkg/embedded` (compiled-in snapshot) on error or
  when `--no-autoupdate` (default for non-interactive).
- Model selection is split into "large" (chat) and "small" (titles, summaries) via
  `cfg.Models[config.SelectedModelTypeLarge|Small]`.
- `internal/agent/hyper/provider.json` + `provider.go` — Charm's hosted
  multi-provider proxy. Activated by env vars (`HYPER`, `HYPERCRUSH`).

Provider options can be set globally (`crush.json` `providers.<id>.extraBody`),
per call (`SessionAgentCall.ProviderOptions`), or hard-coded per provider in
`getProviderOptions` (`coordinator.go:236`).

### 1.5 Session / persistence (`internal/session`, `internal/message`, `internal/history`, `internal/db`)

- SQLite via `ncruces/go-sqlite3`, schema migrated by `pressly/goose/v3`. Both
  pure-Go drivers — no CGO.
- `session.Service`: create, get, list, save, delete; supports
  `ParentSessionID` for sub-agent sessions and `CreateAgentToolSessionID` for
  deterministic IDs derived from the parent message + tool-call ID.
- `message.Service`: typed message persistence with content parts (text, tool
  call, tool result, reasoning, attachment). `ToAIMessage()` converts to
  `[]fantasy.Message`.
- `history.Service`: per-file edit history (used by `edit`/`write` tools so the
  agent can see what it changed earlier in the session).
- `--continue` resumes the most recent top-level session; `--session <id>`
  resumes a specific one (`app.resolveSession`, `app.go:179`).
- Auto-summarize when remaining context < threshold (large window: 20K buffer;
  small: 20% of window). Triggered as a fantasy `StopCondition`
  (`agent.go:445-466`). The summary becomes the start of a fresh session
  conversation.
- Session "fork"/branching is approximated by sub-agent sessions; there's no
  user-facing branch UI.
- `crush session export` produces a markdown transcript of a session.

### 1.6 UI features (`internal/ui/`)

- Single chat buffer + sidebar, both `bubbletea/v2`. Top-level model
  `internal/ui/model/ui.go`.
- Compact/wide layout (`>=120` cols → sidebar), toggleable.
- Dialog system: `internal/ui/dialog/dialog.go` defines a `Dialog` interface;
  individual dialogs:
  - `commands.go` — command palette
  - `models.go` / `models_item.go` / `models_list.go` — model picker, two-tier
  - `sessions.go` / `sessions_item.go` — session picker
  - `permissions.go` — permission prompt
  - `filepicker.go` — image attachment picker
  - `arguments.go` — collect `$ARG` values for custom commands
  - `api_key_input.go` — first-run API key entry
  - `oauth.go` / `oauth_copilot.go` / `oauth_hyper.go` — device-code flows
  - `quit.go` — confirm quit
  - `reasoning.go` — pick reasoning effort (low/med/high)
- Mouse support (default for bubbletea/v2). Keybinds in
  `internal/ui/model/keys.go`.
- Themes via `charm.land/x/exp/charmtone` + `internal/ui/styles/styles.go`;
  background-color autodetect; "transparent background" toggle.
- Notifications: desktop via `gen2brain/beeep` (system-tray-aware); in-TUI
  status messages via `internal/ui/util.InfoMsg`.
- Markdown rendering with `glamour/v2`; chroma syntax highlight in
  `internal/ui/xchroma`.
- Diff view: `internal/ui/diffview/` with unified diff renderer.
- Image preview (sixel/iTerm/kitty graphics) in `internal/ui/image/`.
- Animations / spinners under `internal/ui/anim/`.
- TUI logo `internal/ui/logo/`.

### 1.7 Context / memory features

- `AGENTS.md` and `CLAUDE.md` discovery: walks parent dirs and reads them into
  the system prompt as project context (handled inside the `coder.md.tpl` build
  via `prompt.Prompt.Build`, which reads `AGENTS.md` etc.). See
  `internal/agent/prompt/prompt.go` and `templates/coder.md.tpl`.
- Skills: `internal/skills/skills.go` discovers "skills" (named markdown
  fragments under `<datadir>/skills/` and `internal/skills/builtin/embed.go`)
  and surfaces them via the `view` tool's system-reminder injection
  (`tracker.go` records what's been shown).
- File references: typing `@<path>` in the prompt input attaches the file
  (handled by completions in `internal/ui/completions/`).
- Image attachments: pasted, file-picked, or `@image.png`; only when
  `model.SupportsImages`.
- MCP tools, prompts, AND server `Instructions` are merged into the system
  prompt at request time (`agent.go:186-198`).
- Auto-summarize on context window pressure (see Sessions).
- LSP context: workspace LSPs report diagnostics that are auto-injected into
  the prompt at the start of each turn via `lsp` package + the `diagnostics`
  tool. `internal/app/lsp_events.go` keeps state in sync.

### 1.8 Approval / safety (`internal/permission/`, `internal/agent/hooked_tool.go`)

- Per-tool, per-session permission prompts (`permission.Service.Request`).
- Persistent approvals: "always allow" stored per `(session, tool, action)`.
- `--yolo` / Yolo mode toggle skips all prompts.
- Pre-approved allowlist: `cfg.Permissions.AllowedTools` (also
  `--allowed-tools` style).
- Auto-approve for non-interactive sessions (`AutoApproveSession` on
  `crush run`).
- Bash safelist: read-only commands (`ls`, `cat`, `grep`, etc.) bypass
  permissions. Hard banlist of dangerous binaries
  (`bash.go:71-189` — curl, wget, sudo, rm -rf protections, package managers).
- Hooks: `internal/hooks/`. Currently only `PreToolUse`, but generalizable.
  Hooks are user shell commands declared in `crush.json` with matchers. They
  receive JSON on stdin and emit JSON or exit code; can `allow`, `deny`,
  rewrite tool input, halt the turn (exit 49), or inject context into the
  result. Wired via `hookedTool` wrapper (`hooked_tool.go`).

### 1.9 Headless / scripting / CI

- `crush run` reads stdin if no prompt args.
- `--quiet` hides spinner; `--verbose` shows logs.
- Output is plain text on the agent's final assistant message.
- `crush schema` emits JSON Schema so config can be validated in CI.
- `crush session export` for transcripts.
- HTTP/Unix-socket API (Swagger-documented in `internal/swagger/`): see "client/
  server split" below.

### 1.10 Weird / unique stuff

- **Client/server split** (`internal/server`, `internal/client`,
  `internal/proto`). Crush actually runs as a daemon listening on a Unix socket
  (or named pipe on Windows). The TUI is a client to that daemon. This lets
  multiple TUI instances share state and lets the agent keep running if the
  TUI dies. Routes documented in `internal/swagger/swagger.json`. The default
  is to spawn the server in-process on first run.
- **Hyper provider** (`internal/agent/hyper/`): Charm's own multi-model proxy.
  Auth is OAuth2 device flow.
- **Loop detection** (`internal/agent/loop_detection.go`): detects repeated
  identical tool calls in the last N steps and stops the agent.
- **Workspace abstraction** (`internal/workspace/`): each project gets its own
  data dir, sessions are scoped to it.
- **Skills system**: bundled skill markdown packs (`skills/builtin/`) auto-shown
  to the model when relevant tools fire.
- **VCR-replay testing** (`charm.land/x/vcr`): full HTTP record/replay of
  provider responses for deterministic tests.
- **Filetracker** (`internal/filetracker/`): mtime journal so agent can detect
  external file modifications between turns.
- **Model JSON cache + ETag** for catwalk (`config/catwalk.go`).
- **Project init** action runs `initialize.md.tpl` to bootstrap an `AGENTS.md`
  for the user's repo.

---

## 2. How crush wires `fantasy` + `openai-go`

### 2.1 The actual import sites

Crush imports `fantasy` and its provider sub-packages directly:

```go
// internal/agent/agent.go:26-33
"charm.land/catwalk/pkg/catwalk"
"charm.land/fantasy"
"charm.land/fantasy/providers/anthropic"
"charm.land/fantasy/providers/bedrock"
"charm.land/fantasy/providers/google"
"charm.land/fantasy/providers/openai"
"charm.land/fantasy/providers/openrouter"
"charm.land/fantasy/providers/vercel"
```

`coordinator.go` adds `azure` and `openaicompat`. The only direct import of
`openai-go` in the entire crush codebase:

```go
// internal/agent/coordinator.go:49
openaisdk "github.com/charmbracelet/openai-go/option"
```

…and it's used in exactly one place — passing extra JSON body fields through to
the OpenAI-compatible provider:

```go
// internal/agent/coordinator.go:737-738
for extraKey, extraValue := range extraBody {
    opts = append(opts, openaicompat.WithSDKOptions(openaisdk.WithJSONSet(extraKey, extraValue)))
}
```

That's it. **Crush does not construct `openai.Client` directly.** Fantasy does
that internally inside `providers/openai/openai.go:180`:

```go
client := openai.NewClient(openaiClientOptions...)
```

The way Crush passes credentials in is via fantasy's `openai.Option` functions:

```go
// internal/agent/coordinator.go:669-685
func (c *coordinator) buildOpenaiProvider(baseURL, apiKey string, headers map[string]string) (fantasy.Provider, error) {
    opts := []openai.Option{
        openai.WithAPIKey(apiKey),
        openai.WithUseResponsesAPI(),
    }
    if c.cfg.Config().Options.Debug {
        opts = append(opts, openai.WithHTTPClient(log.NewHTTPClient()))
    }
    if len(headers) > 0 {
        opts = append(opts, openai.WithHeaders(headers))
    }
    if baseURL != "" {
        opts = append(opts, openai.WithBaseURL(baseURL))
    }
    return openai.New(opts...)  // returns fantasy.Provider
}
```

### 2.2 Agent construction

Fantasy's `Agent` is built per request, not per session. `agent.go:205-210`:

```go
agent := fantasy.NewAgent(
    largeModel.Model,                // fantasy.LanguageModel
    fantasy.WithSystemPrompt(systemPrompt),
    fantasy.WithTools(agentTools...), // []fantasy.AgentTool
    fantasy.WithUserAgent(userAgent),
)
```

Where `largeModel.Model` came from earlier:

```go
// coordinator.go:617
largeModel, err := largeProvider.LanguageModel(ctx, largeModelID)
```

So the chain is: `crush.Coordinator` → builds `fantasy.Provider` (via
`fantasy/providers/openai.New(...)`) → `provider.LanguageModel(ctx, "gpt-5")`
returns a `fantasy.LanguageModel` → `fantasy.NewAgent(model, ...)`.

The `openai-go` client is created lazily inside the fantasy provider when
`LanguageModel()` is first called.

### 2.3 How tools are registered

Each tool implements `fantasy.AgentTool`. Crush uses the typed helper
`fantasy.NewAgentTool[TInput]` which auto-generates a JSON schema from the
struct's tags. Example:

```go
// internal/agent/tools/bash.go:191-237
func NewBashTool(perms permission.Service, cwd string, attr *config.Attribution, modelName string) fantasy.AgentTool {
    return fantasy.NewAgentTool(
        BashToolName,                              // name
        string(bashDescription(attr, modelName)), // description (embedded md)
        func(ctx context.Context, params BashParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
            // ... permission check ...
            // ... execute ...
            return fantasy.NewTextResponse(out), nil
        },
    )
}
```

`BashParams` is a Go struct with `json:"..." description:"..."` tags. Fantasy's
`schema.Generate` (in `schema/`) walks the type with `reflect` and produces a
JSON Schema for the model.

`coordinator.buildTools` (`coordinator.go:443-551`) collects every tool, filters
by `agent.AllowedTools` from config, sorts, then wraps each in `hookedTool` so
PreToolUse hooks fire before delegation (`hooked_tool.go`).

### 2.4 The streaming loop

The work happens in `agent.Stream()`. Crush passes a giant `fantasy.AgentStreamCall`
with **callbacks** (`agent.go:260-471`):

```go
result, err := agent.Stream(genCtx, fantasy.AgentStreamCall{
    Prompt:           ...,
    Files:            files,
    Messages:         history,
    ProviderOptions:  call.ProviderOptions,
    MaxOutputTokens:  maxOutputTokens,
    PrepareStep:      func(ctx, options) (ctx, prepared, err) { ... },
    OnReasoningStart: func(id, reasoning) error { ... },
    OnReasoningDelta: func(id, text) error { ... },
    OnReasoningEnd:   func(id, reasoning) error { ... },
    OnTextDelta:      func(id, text) error { ... },
    OnToolInputStart: func(id, toolName) error { ... },
    OnToolCall:       func(tc fantasy.ToolCallContent) error { ... },
    OnToolResult:     func(r fantasy.ToolResultContent) error { ... },
    OnStepFinish:     func(s fantasy.StepResult) error { ... },
    OnRetry:          func(err *fantasy.ProviderError, d time.Duration) { ... },
    StopWhen:         []fantasy.StopCondition{ autoSummarize, loopDetection },
})
```

**Fantasy is the orchestrator**: it calls the model, parses streaming events,
calls tools, feeds results back, decides when to stop. Crush's callbacks just
mirror the events into its own `message.Service` (so the SQLite persistence and
the TUI subscriber see them) and update the assistant message in place.

Tool execution is fully owned by fantasy — Crush does NOT mediate. When the
model emits a tool call, fantasy looks up the registered tool, calls
`tool.Run(ctx, call)`, and threads the result back into the next model call,
firing `OnToolResult` for the consumer to observe.

### 2.5 PrepareStep — Crush's main intervention

`PrepareStep` runs once per model call (each step of the agent loop). Crush uses
it to:

1. Strip stale provider options off historical messages.
2. Refresh the tool list (so MCP tools added mid-session take effect).
3. Drain any prompts that the user queued while busy and append them as new user
   messages.
4. Apply Anthropic prompt-cache markers to last system message + last 2 messages.
5. Create the empty `Assistant` message in SQLite so the TUI can render the
   "typing" indicator immediately, and stash its ID + capabilities into context.

(`agent.go:271-327`)

### 2.6 Canonical "send a message" path

```
TUI input (internal/ui/model/chat.go)
  → app.SendEvent / direct call →
app.AgentCoordinator.Run(ctx, sessionID, prompt, attachments...)         // app.go (interactive) or RunNonInteractive (run cmd)
  → coordinator.Run (internal/agent/coordinator.go:153)
  → coordinator wraps as SessionAgentCall, calls a.currentAgent.Run
  → sessionAgent.Run (internal/agent/agent.go:160)
       1. queue if busy
       2. build agentTools snapshot
       3. instructions += MCP server.Instructions
       4. fantasy.NewAgent(largeModel.Model, WithSystemPrompt, WithTools, ...)
       5. createUserMessage (persist user msg)
       6. agent.Stream(ctx, AgentStreamCall{..., callbacks, StopWhen})
            → fantasy calls model
            → fantasy parses stream → invokes OnText/OnToolCall/...
            → fantasy invokes tool.Run(ctx, call)  // direct, no Crush intermediary
            → fantasy continues loop until StopWhen or no tool calls
       7. on err: classify (cancel, hyper auth, fantasy.ProviderError, etc.)
       8. notify, summarize-if-needed, drain queue
```

Subscribers see streaming via `app.Messages.Subscribe(ctx)` (a pubsub broker
fed by every `messages.Update` and `messages.Create` Crush does inside the
callbacks). The TUI's chat model subscribes and re-renders on each event.

---

## 3. How crush wires `anthropic-sdk-go` and `catwalk`

### 3.1 Anthropic

`anthropic-sdk-go` is **only** an indirect dep:

```
github.com/charmbracelet/anthropic-sdk-go v0.0.0-... // indirect
```

Crush never imports it. It only uses `charm.land/fantasy/providers/anthropic`:

```go
// coordinator.go imports
"charm.land/fantasy/providers/anthropic"
```

…which gives Crush:

- `anthropic.New(opts ...anthropic.Option) (fantasy.Provider, error)` — constructor
- `anthropic.WithAPIKey`, `WithBaseURL`, `WithHeaders`, `WithHTTPClient` —
  options
- `anthropic.ParseOptions(map) (*ProviderOptions, error)` — turns the user's
  `crush.json` string-keyed map into typed Anthropic provider options
  (e.g. `Thinking`)
- `anthropic.Name` — the string `"anthropic"`
- `anthropic.ReasoningOptionMetadata` — used in `OnReasoningEnd` to grab the
  thinking signature (`agent.go:338`)

The Anthropic SDK `Client` itself is constructed inside fantasy's
`providers/anthropic/anthropic.go`. Crush never sees it.

Special handling Crush adds: `interleaved-thinking-2025-05-14` beta header,
`Bearer ` API key prefix detection (for OAuth tokens), and MiniMax-as-Anthropic
quirk (`coordinator.go:637-666`).

### 3.2 Catwalk

`charm.land/catwalk` is **data only** — it's a catalog of providers and models,
not a runtime. Crush uses it for:

- **Model picker**: lists every provider × model with capabilities
  (`SupportsImages`, `CanReason`, `ContextWindow`, `DefaultMaxTokens`,
  `ReasoningLevels`).
- **Pricing**: `catwalk.Model.CostPer1MIn/Out/Cache...` — feeds the cost
  display in the sidebar and the `crush stats` SVG report.
- **Capability gating** in the agent loop (e.g. only Anthropic-compatible
  providers get prompt-cache markers; only image-capable models accept
  attachments).
- **Embedded fallback**: `catwalk/pkg/embedded.GetAll()` returns a
  compiled-in snapshot. Live updates fetched from
  `https://catwalk.charm.sh/...` with ETag caching to disk
  (`internal/config/catwalk.go`).

Catwalk's tree:

```
butter/catwalk/
├── pkg/catwalk/    # types + HTTP client
├── pkg/embedded/   # `embed.FS` snapshot of the catalog
├── cmd/            # `catwalk` CLI to maintain the catalog
└── internal/       # the public catalog itself, JSON files per provider
```

You get **a typed `[]catwalk.Provider`** with everything needed to drive a model
picker and configure providers without baking provider knowledge into your
agent. Worth absolutely stealing.

---

## 4. Architecture map

Layers, from bottom up:

```
infra:        db, csync, pubsub, log, env, home, fsext, filepathext,
              shell, version, hooks (pure leaf utils)

data/state:   session, message, history, filetracker, permission, projects,
              workspace, oauth   (services, mostly SQLite-backed)

provider:     config (parses crush.json + catwalk),
              agent/hyper (Hyper provider catalog),
              fantasy (external)

agent core:   agent/tools/* (each tool isolated),
              agent/tools/mcp (MCP client + tool projection),
              agent/prompt (template loader),
              agent  (sessionAgent.Run + coordinator + hookedTool +
                      loop_detection + agent_tool + agentic_fetch_tool)

orchestration: app  (wires everything; owns AgentCoordinator, services,
                     LSP manager, agent notification broker, events channel)

transport:    proto, server, client  (HTTP+Unix-socket daemon API,
                                      JSON over the wire)

UI:           ui/styles, ui/anim, ui/list, ui/completions, ui/util,
              ui/diffview, ui/image, ui/xchroma, ui/notification,
              ui/dialog/*, ui/chat/*, ui/model/* (top-level Bubble Tea model)

CLI:          cmd/* (cobra commands; `crush`, `crush run`, `crush session`,
                      `crush login`, `crush server`, ...)
```

`internal/app/app.go` is the spine: takes a `*sql.DB` and a
`*config.ConfigStore`, returns an `*App` with all services constructed and an
event channel the TUI consumes. `cmd/root.go` calls into `app.New` via
`workspace.Setup`. The TUI in `internal/ui/model/ui.go` is just a
`bubbletea.Model` over that `*App`.

The daemon split (`server` ↔ `client`) means the CLI in `cmd/` first tries to
connect to a running server; if none, spawns one in-process. The `app` layer is
shared by both sides.

---

## 5. Should bitchtea have this?

### 5.1 Definitely steal

- **fantasy as the agent loop** — don't write your own retry/streaming/tool
  dispatcher. `fantasy.NewAgent(model, WithTools(...))` + `Stream(ctx, AgentStreamCall{...})`
  is the right abstraction. Your code just provides callbacks that update your
  data model.
- **`fantasy.NewAgentTool[TInput]` for tools** — auto schema from struct tags
  is a huge win.
- **catwalk for model selection + capabilities + pricing**. Even if you start
  with one model, the cost data alone is worth it.
- **fantasy provider drivers**: never construct `openai-go` / `anthropic-sdk-go`
  yourself. `fantasy/providers/openai.New(...)` etc. handles streaming,
  Responses-vs-Chat, prompt cache markers, etc.
- **Per-tool permission prompts** with a "yolo" bypass. Even for personal use,
  the bash safelist + banlist (`tools/bash.go:71-189`) is a sane starting
  point — copy it verbatim.
- **`PrepareStep` callback** for last-mile message massaging (cache markers,
  tool refresh, queued-prompt drain). This is where Crush does most of its
  cleverness.
- **Auto-summarize as a `StopCondition`** (`agent.go:445-466`). Falls out
  cleanly because fantasy already supports it.
- **Markdown tool descriptions with `//go:embed`**. Keeps prompts versioned in
  files instead of stringly inline.
- **`AGENTS.md` + `CLAUDE.md` discovery** via the prompt-build step. Trivial
  win for context.
- **filetracker mtime guard** on `edit`/`write`. Stops you from clobbering
  external changes.
- **Sub-session for `agent` tool** (Claude-Code-style Task). The pattern of
  `coordinator.runSubAgent → CreateAgentToolSessionID(parentMsg, toolCallID)`
  → run a fresh agent → return its final text gives you nested agents
  cheaply.

### 5.2 Probably steal

- **MCP integration**: small surface (`agent/tools/mcp/`), gives you a huge
  ecosystem of tools and prompts for free. If you skip MCP, you'll regret it
  the first time you want a Slack/GitHub tool.
- **Loop detection** (`internal/agent/loop_detection.go`) — short, drop-in.
- **The dialog framework** (`ui/dialog/dialog.go` + `Dialog` interface). Even
  if your TUI is IRC-style, modal dialogs for model/session pickers are
  cleaner than inline.
- **Hooks** (`internal/hooks/`) as a generic shell-out for PreToolUse. Good
  way to give the user knobs without recompiling.
- **`charm.land/fang/v2`** for nicer cobra rendering.
- **Skills system** if you want the agent to discover capability docs at
  runtime; modest cost, big payoff for a personal assistant.
- **`CRUSH_SHORT_TOOL_DESCRIPTIONS`** trick: full description in markdown,
  first-line at runtime to save tokens.
- **Provider OAuth flows** (`internal/oauth/`) if you'll ever use Hyper or
  Copilot.

### 5.3 Skip

- **Daemon / Unix-socket server split** (`internal/server`, `internal/client`,
  `internal/proto`, `internal/swagger`). Useful when you have multiple TUIs
  attached or want a long-running shared agent — you're solo, your TUI lives
  on a single machine, this is overkill. You already have `bitchtea/daemon`
  for janitor work; that's enough.
- **Workspace abstraction** if your bitchtea is one-project-per-install.
- **Catwalk auto-update over HTTP** — just embed a snapshot. The
  `pkg/embedded` package gives you that for free; turn off `autoupdate` in
  `catwalkSync.Init`.
- **`crush stats`** SVG/HTML report. Cute, not load-bearing.
- **Hyper provider** unless you want to use it.
- **Docker MCP autodetection** — niche.
- **`charm.land/x/vcr` HTTP record/replay** — useful for crush's own test suite,
  not for shipping.
- **Glamour markdown renderer in the chat buffer** if you're going IRC-style;
  glamour expects multi-line block layout. Plain ANSI + chroma is probably
  enough.
- **goose migrations**: bitchtea is markdown-first per the project notes;
  don't introduce SQLite for sessions if you don't have to. If you DO want
  SQLite later, ncruces+goose is the right combo (no CGO).
- **The `agentic_fetch` sub-agent**: if you have plain `fetch` + a smart
  model, this is largely redundant.
- **Hard-coded provider-specific handling** (MiniMax-as-Anthropic, Copilot
  quirks) until you actually use those providers.

---

### TL;DR for tonight's build

The minimum viable bitchtea coding agent looks like:

1. `fantasy.NewAgent(model, WithTools(...))` per turn.
2. Tools defined with `fantasy.NewAgentTool[Params]`, descriptions in embedded
   markdown, banlist + permission prompt for `bash`.
3. `agent.Stream(ctx, AgentStreamCall{...})` with callbacks that mutate your
   message/UI state.
4. `catwalk.GetProviders` (or `embedded.GetAll`) for the model picker.
5. `fantasy/providers/openai.New(WithAPIKey, WithBaseURL, ...)` to build
   `fantasy.Provider` for each provider type, then
   `provider.LanguageModel(ctx, modelID)` for the actual `LanguageModel`.
6. Optional: MCP client (modelcontextprotocol/go-sdk) registered as
   additional `AgentTool`s + a `system_prompt += server.Instructions` step.

Don't touch `openai-go` directly. Don't touch `anthropic-sdk-go` at all.
Fantasy handles both.
