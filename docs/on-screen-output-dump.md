# bitchtea on-screen output dump

Verbatim catalog of every literal string the program can write to the user's
terminal. Sourced from the actual code paths under `internal/ui/`,
`main.go`, and `daemon_cli.go`. `{name}` placeholders stand in for runtime
`%s` / `%d` / `%v` interpolations — the surrounding text is exact.

Conventions:

- Quoted strings preserve the source exactly, including punctuation and
  trailing newlines (newlines shown as actual line breaks inside the quote).
- ANSI color escape codes (`\033[...m`) and the splash-art glyphs are
  reproduced as the raw literal in source — see `internal/ui/art.go`.
- "Same handler as X" means the slash-command branches into another command's
  handler so the output tree is identical and not duplicated here.
- Agent-streamed prose and tool stdout/stderr are not enumerated — they are
  variable. Mentioned once where they enter the viewport.

---

## Slash commands

### /set

Source: `internal/ui/commands.go` `handleSetCommand`.

- bare `/set` (no key) → multi-line system message, one line per registered
  key plus a `service` line. Format:
  ```
  Settings:
    {key} = {value}
    ...
    service = {serviceDisplay}
  ```
  Keys come from `config.SetKeys()`: `provider`, `model`, `apikey`,
  `baseurl`, `nick`, `profile`, `sound`, `auto-next`, `auto-idea`. Values
  come from `config.GetSetting`. `apikey` is masked to `xxxx...yyyy` if
  longer than 8 chars or rendered as `<unset>` when empty. `profile`
  renders `<none>` when unset. `sound`/`auto-next`/`auto-idea` render `on`
  or `off`. `serviceDisplay` returns `<unset>` when the field is empty.
- `/set <unknown-key>` → error message:
  `Unknown setting "{key}". Valid keys: {comma-joined list with service}`

#### /set model

- no arg → routes to `/models`. See `### /models` below.
- valid arg `<model-id>` → routes to `handleModelCommand`:
  `*** Model switched to: {model-id}` (system message). Also clears the
  loaded profile tag silently.
- bare `/model` (called via the routing path with no value) cannot happen
  through `/set` because `/set model` with no value already routed to
  `/models`. The legacy `handleModelCommand` no-arg branch emits:
  `Current model: {model}. Usage: /set model <name>`

#### /set provider

- no arg →
  `provider = {provider}`
  `  available: openai, anthropic`
  `  set: /set provider <name>`
- valid arg → routes to `handleProviderCommand`:
  `*** Provider set to: {prov}`
  `  requests -> {endpointPreview}`
  Followed (when applicable) by a separate system message from
  `providerTransportHint`. See "Provider transport hints" below.
- invalid arg (anything other than `openai` / `anthropic`) → falls into
  the generic unknown branch and emits:
  `Unknown setting "provider". Valid keys: {list}`
  (Because `applySetToConfig` silently rejects values it doesn't recognise
  for `provider`, the set fails and the default branch fires.)

#### /set service

- no arg →
  `service = {serviceDisplay}`
  `  available: {comma-joined config.ListServices()}`
  `  set: /set service <name>`
- empty value → error: `Usage: /set service <value>`
- any non-empty value → `Service set to: {value}` (verbatim, no validation).

#### /set baseurl

- no arg → routes to `handleBaseURLCommand`:
  `Base URL: {baseurl}`
  `Usage: /set baseurl <url>`
- valid arg →
  `*** Base URL set to: {url}`
  `  requests -> {endpointPreview}`
  Optional second system message from `providerTransportHint`. Possible
  hint strings (joined with `\n` if multiple):
  - `warning -> base URL already includes /chat/completions; omit /chat/completions because bitchtea appends endpoint paths automatically.`
  - `warning -> base URL already includes /messages; omit /messages because bitchtea appends endpoint paths automatically.`
  - `warning -> Anthropic transport with an OpenAI-style base URL looks suspicious. Requests will go to /messages. If this endpoint is OpenAI-compatible, switch with /set provider openai.`
  - `warning -> anthropic transport sends requests to /messages. If this server is OpenAI-compatible, switch with /set provider openai.`
  - `warning -> openai transport sends requests to /chat/completions. If this endpoint is Anthropic-compatible, switch with /set provider anthropic.`

#### /set apikey

- no arg → routes to `handleAPIKeyCommand`:
  `API Key: {masked-or-empty}`
  `Usage: /set apikey <key>`
  (Masking here uses the raw value if length <= 8, otherwise `xxxx...yyyy`.)
- valid arg → `*** API key set: {maskSecret(key)}`
  (`maskSecret` returns `<unset>` for empty/whitespace, raw if <= 8 chars,
  else `xxxx...yyyy`.)

#### /set nick

- valid arg → `Nick set to: {value}` (uses `settingLabel("nick") = "Nick"`).
- empty value (covered by the bare `/set <key>` branch) →
  `nick = {UserNick}`

#### /set profile

- no arg → routes to `/profile` with no args. See `### /profile`.
- valid name → calls `config.ApplySet`. On success:
  `Profile set to: {profile}`
  On unknown profile name → `applySetToConfig` succeeds because lookup
  failure is swallowed in config; if `m.config.Profile` ends up empty it
  emits `profileLookupMessage(value)`:
  - For `openai` / `anthropic`:
    `{name} is a provider, not a profile. Use /set provider {name} or /profile to list profiles.`
  - Otherwise:
    `Unknown profile "{name}". Use /profile load <name> or /profile to list profiles.`

#### /set sound

- valid arg (`on`/`off`/`true`/`false`/`yes`/`1`/anything else) →
  `Sound set to: {on|off}`
- bare value at `/set sound` → `sound = {on|off}`

#### /set auto-next

- valid arg → `Auto Next set to: {on|off}`
- bare → `auto-next = {on|off}`

#### /set auto-idea

- valid arg → `Auto Idea set to: {on|off}`
- bare → `auto-idea = {on|off}`

### /quit, /q, /exit

Source: `handleQuitCommand`. Emits no on-screen text directly. Triggers
`tea.Quit`, after which `main.go` may print to stderr (after Bubble Tea
exits altscreen):
- `\nLater, coward.` (when the program loop returns `tea.ErrInterrupted`).

### /help, /h

Source: `handleHelpCommand`. Emits a single multi-line system message
(`helpCommandText`):

```
Commands:
  /join <#channel>    Switch focus to channel (creates if new)
  /part [#channel]    Leave context (default: current)
  /query <nick>       Route Enter persistently to nick
  /msg <nick> <text>  One-shot send to nick, no focus change
  /channels           List open contexts
  /set [key [value]]  Show or change a setting
                        keys: provider, model, baseurl, apikey, service,
                              nick, profile, sound, auto-next, auto-idea
                        e.g. /set apikey sk-..., /set provider anthropic
  /profile [cmd]      save/load/show/delete profiles (built-ins: ollama, openrouter, etc.)
                        bare /profile <name> loads the named profile
  /models             Open a fuzzy picker of models for the active service
                        (uses the catwalk catalog cache; offline-safe)
  /compact            Compact conversation context
  /clear              Clear chat display
  /restart            Reset agent and start a fresh conversation
  /copy [n]           Copy last or nth assistant response
  /tokens             Token usage estimate
  /status             Show endpoint, model, context window usage, cost
  /save               Snapshot current settings to bitchtearc (auto-backup)
  /memory             Show MEMORY.md contents
  /sessions           List saved sessions
  /resume <number>    Resume a session by number
  /tree               Show session tree
  /fork               Fork session
  /debug on|off       Toggle verbose API logging
  /activity [clear]   Show or clear queued background activity
  /mp3 [cmd]          Toggle MP3 panel and player
  /quit               Exit

  Use @filename to include file contents.
  Type while agent works to queue (steering).
  Ctrl+C to interrupt, again to quit.
```

### /clear

Source: `handleClearCommand`. Empties `m.messages` and refreshes. No
system text emitted.

### /restart

Source: `handleRestartCommand`. May emit (in this order):
- (only if streaming) the cancel message — see `cancelActiveTurn`:
  `Restart` (literal, system message).
- Always: `*** Conversation restarted. Fresh context.`

### /compact

Source: `handleCompactCommand`.
- If streaming: `Can't compact while agent is working. Be patient.`
- On compaction failure: `Compaction failed: {err}` (error message).
- On success: `Compacted: ~{before-tokens} -> ~{after-tokens} tokens`
  (token counts via `formatTokens`: digits if <1000, else `{X.X}k`).

### /copy

Source: `handleCopyCommand` and `internal/ui/clipboard.go`.
- `/copy` with no arg → copies last assistant message; on success:
  `Copied last assistant response via {method}.` where `method` is
  one of `OSC 52` / `pbcopy` / `xclip`.
- `/copy <n>` valid → `Copied assistant response {n} via {method}.`
- `/copy <bad-n>` (non-int or <1) → error:
  `Usage: /copy [n] where n is a positive assistant message number`
- `/copy <n>` past end → error:
  `Assistant message {n} does not exist. {available} available.`
- No assistant messages yet → error:
  `No assistant responses available to copy.`
- Clipboard backend missing → error:
  `Clipboard copy failed. Need a terminal that accepts OSC 52 or a working pbcopy/xclip.`

### /tokens

Source: `handleTokensCommand`.
- `~{tokens} tokens | ${cost} | {n} messages | {n} turns`

### /status

Source: `handleStatusCommand`. Single multi-line system message:
```
Status:
  profile:   {profile-or-<none>}
  service:   {serviceDisplay}
  provider:  {provider}
  model:     {model}
  baseurl:   {baseurl}
  endpoint:  {endpointPreview}
  apikey:    {maskSecret}
  context:   {contextLine}
  cost:      ${cost}
  messages:  {n}
  turns:     {n}
```
Where `contextLine` is either:
- `~{tokens} / {window} tokens ({pct}%)` when catalog reports a window, or
- `~{tokens} tokens (window unknown)` otherwise.

### /save

Source: `handleSaveCommand`.
- mkdir failure: `save: cannot create {dir}: {err}` (error)
- backup rename failure: `save: cannot back up existing rc: {err}` (error)
- write failure: `save: cannot write {path}: {err}` (error)
- success without prior file: `Saved current config to {path}.`
- success with existing file backed up:
  `Saved current config to {path} (previous backed up to {backup-base-name}).`

### /debug

Source: `handleDebugCommand`.
- no arg → `Debug mode: {ON|OFF}. Usage: /debug on|off`
- `on` → `Debug mode: ON` and registers a debug hook that, per debug
  request, emits a system message of the form:
  `[DEBUG] {Method} {URL}\nRequest Headers: {headers}\nRequest Body: {body}\nResponse Status: {status}`
- `off` → `Debug mode: OFF`
- anything else → error: `Usage: /debug on|off`

### /activity

Source: `handleActivityCommand` and `Model.backgroundActivityReport`.
- `/activity` (no args), no entries → `No background activity queued.`
- `/activity` (no args), with entries → multi-line system message:
  ```
  Background activity:
    {HH:MM} {[ctx]} {<sender>} {summary}
    ...
  ```
  Each line uses `BackgroundActivity.displayLine()`:
  - `[{context}]` if no sender, else `[{context}] <{sender}> {summary}`.
  - `context` defaults to `main` when blank; `summary` defaults to
    `activity waiting` when blank.
- `/activity clear` → `Cleared {n} background activity notice(s).`
- anything else → error: `Usage: /activity [clear]`

Status-bar segment (always-visible when there's any background activity)
from `Model.backgroundStatus()`:
`bg:{unread} /activity {truncated-latest-line}`  (latest line truncated
to 22 runes, ellipsis if cut).

### /mp3

Source: `handleMP3Command`, `internal/ui/mp3.go`.
- Controller missing (defensive): error `MP3 controller unavailable.`
- `/mp3` (no args) — calls `mp3Controller.toggle()`. Possible system
  messages it returns:
  - hide path: `MP3 panel hidden.`
  - show path with no tracks: passes through `rescan()`:
    - `MP3 scan failed: {err}`, or
    - `No MP3s found in {libraryDir}`, or
    - `Loaded {n} track(s) from {libraryDir}`
  - show path with player already running:
    `MP3 panel ready. {track-name}`
  - show path that auto-starts playback:
    `Now playing: {track-name}`, or `MP3 panel ready.` if play returned
    an empty status.
- `/mp3 rescan` → see `rescan()` strings above.
- `/mp3 play` → either `Now playing: {track}` or `MP3 playback failed: {err}`
  or `No MP3s found in {libraryDir}`.
- `/mp3 pause` / `/mp3 toggle` →
  - `No MP3 track is playing.`
  - `Pause failed: {err}`
  - `Resume failed: {err}`
  - `Paused: {track}` / `Resumed: {track}`
- `/mp3 next` →
  - `No MP3s found in {libraryDir}` if empty,
  - else `Now playing: {track}` (or `MP3 playback failed: {err}`).
- `/mp3 prev` / `/mp3 previous` → same as next.
- unknown subcommand → error: `Usage: /mp3 [rescan|play|pause|next|prev]`
- After playback ends naturally (`mp3DoneMsg`):
  - `MP3 playback ended: {err}` on error, or
  - `Finished: {track}` if only one track,
  - else auto-advances and emits `Now playing: {next-track}` (or
    `Finished: {track}` if `playMsg` was empty).

The MP3 player's panel itself (when visible) is a side-bar render
containing literals: `MP3 Player`, `Dir: {libraryDir-truncated}`,
`Controls: space pause, ←/j prev, →/k next`,
`Drop .mp3 files into the library dir.`, `Now Playing`, `Playlist`,
plus per-track lines `  {n}. {name}` (cursor row prefixed `▶ `).

The status-bar slot (when player has tracks) is:
`♫ {track} {▶|▌▌|■} [{progress}] {elapsed}/{total} {visualizer}`.

### /theme

Source: `handleThemeCommand`. Always emits:
`Theme switching is disabled. Built-in theme: {CurrentThemeName}.`
(`CurrentThemeName()` returns `BitchX`.)

### /memory

Source: `handleMemoryCommand`.
- If MEMORY.md exists: raw message (so ANSI escapes survive):
  `\033[1;36m--- MEMORY.md ---\033[0m\n{contents}`
  (truncated to 1000 chars + `\n... (truncated)`).
- If in a non-root context, scoped HOT.md exists:
  `\033[1;36m--- HOT.md ({label}) ---\033[0m\n{contents}` (also truncated).
- If in non-root context with no HOT.md: `No HOT.md for {label} yet.`
- If both root MEMORY.md is empty and root scope: `No MEMORY.md found in working directory.`

### /sessions, /ls

Source: `handleSessionsCommand`.
- No sessions or list error: `No saved sessions.`
- Otherwise multi-line system message:
  ```
  Sessions:
    {n}. {session.Info(path)}
    ...
    Resume: /resume <number>
  ```
  When pages > 1 the header becomes `Sessions (page {p}/{total}):` and a
  pre-trailer line `  ... use /sessions {p+1} for next page` is added.

### /resume

Source: `handleResumeCommand`.
- no arg → `Usage: /resume <number>  (use /sessions to list)`
- non-numeric arg → `Invalid session number: {arg}`
- empty list → `No saved sessions.`
- out of range → `Session {n} not found. {avail} sessions available.`
- (if streaming) cancel message: `Session resume`
- load failure → `Error loading session: {err}`
- success → `Resumed session {n}: {basename}`

### /tree

Source: `handleTreeCommand`.
- No session → `No active session.`
- Else raw message: `\033[1;36m{m.session.Tree()}\033[0m` (tree text from
  the session module, ANSI cyan-wrapped).

### /fork

Source: `handleForkCommand`.
- No session or no entries → `No session to fork from.`
- Fork error → `Fork failed: {err}` (error)
- Success → `Forked to new session: {path}`

### /profile

Source: `handleProfileCommand`.
- no sub → system message:
  `Profiles: {comma-joined config.ListProfiles()}\nUsage: /profile save <name> | /profile load <name> | /profile show <name> | /profile delete <name>`

#### /profile show

- no name → error: `Usage: /profile show <name>`
- unknown → error: `profileLookupMessage` (see /set profile)
- success → multi-line system message:
  ```
  Profile: {name}
    provider={p.Provider} service={serviceDisplay} model={p.Model}
    baseurl={p.BaseURL}
    endpoint={endpointPreview}
    apikey={maskSecret}
  ```

#### /profile save

- no name → error: `Usage: /profile save <name>`
- save failure → error: `Save failed: {err}`
- success →
  `*** Profile saved: {name} (provider={p} service={s} model={m})`

#### /profile load

- no name → error: `Usage: /profile load <name>`
- unknown → `profileLookupMessage(name)`
- success → calls `applyProfileToModel(verbose=true)` which emits:
  ```
  *** Profile loaded: {name}
    provider={p} service={s} model={m}
    baseurl={url}
    endpoint={endpointPreview}
    apikey={maskSecret}
  ```
  Plus, if `p.APIKey == ""`:
  `This profile did not provide an API key. Set one with /set apikey <key> or the matching env var before connecting.`
  Plus optional `providerTransportHint` line(s).

#### /profile delete

- no name → error: `Usage: /profile delete <name>`
- delete failure → error: `Delete failed: {err}`
- success → `*** Profile deleted: {name}`

#### /profile <name>

(default branch — bare profile name, equivalent to load but non-verbose)
- unknown → `profileLookupMessage(name)`
- success → `applyProfileToModel(verbose=false)`:
  `*** Profile loaded: {name} (provider={p} service={s} model={m})`
  (Plus optional missing-API-key and transport-hint messages as above.)

### /models

Source: `handleModelsCommand`. Errors only emit text inline; success opens
the model picker.
- No active service → error:
  `models: no active service — set one with /set service <name> or load a profile (e.g. /profile openrouter).`
- Empty catalog → error:
  `models: catalog is empty — try BITCHTEA_CATWALK_AUTOUPDATE=true with BITCHTEA_CATWALK_URL set, or wait for the embedded snapshot.`
- No matches for the service → error:
  `models: no catalog data for service "{service}" — try BITCHTEA_CATWALK_AUTOUPDATE=true or check /profile.`
  (When other services are available, appended:
  `\n  available services: {comma-joined-lowercased}`)
- Success → opens model picker with title:
  `models for {service} ({n} total) — type to filter`
  See "Model picker" below.

On selection (`applyModelSelection`):
`*** Model switched to: {choice}`

### /join

Source: `handleJoinCommand`.
- no arg → error: `Usage: /join <#channel>`
- focus save failure → error: `focus save: {err}`
- success → `Joined {label}` (label is `#channel-name`).

### /part

Source: `handlePartCommand`.
- no arg → parts the active context.
- if removing fails because only one context remains → error:
  `Can't part the last context.`
- if removing fails because the named context isn't open → error:
  `Not in context {label}.`
- focus save failure → error: `focus save: {err}`
- success → `Parted {old-label} — now in {new-label}`

### /query

Source: `handleQueryCommand`.
- no arg → error: `Usage: /query <persona>`
- focus save failure → error: `focus save: {err}`
- success → `Query open: {label}`

### /channels, /ch

Source: `handleChannelsCommand`. Single system message:
```
Open contexts:
  {label} [member1, member2, ...]
* {active-label} [members...]
  ...
```
(`*` prefix on the active context; member list only appears when the
context is a channel with known members.)

### /msg

Source: `handleMsgCommand`.
- arg count <3 or empty text → error: `Usage: /msg <nick> <text>`
- streaming → system message: `Queued /msg to {nick} (agent busy).` and
  enqueues `[to:{nick}] {text}`.
- not streaming → echoes a user message in the viewport:
  `→{nick}: {text}` (formatted as a `MsgUser` line).
  Then sends `[to:{nick}] {text}` to the agent (no system message).

### /invite

Source: `internal/ui/invite.go` `handleInviteCommand`.
- no arg → error: `Usage: /invite <persona> [#channel]`
- in a DM context with no explicit channel → error:
  `Cannot /invite in a DM context. Switch to a channel first.`
- already a member → `{persona} is already in #{channel}`
- success → two messages:
  `*** {persona} joined #{channel}` (system),
  followed by a catch-up summary system message from
  `buildChannelCatchup(...)`:
  - `Catch-up: no session history available.` (no session), or
  - `Catch-up for {channel}: no prior conversation found.`, or
  - multi-line:
    ```
    Catch-up for {channel} ({n} messages):
      [{role}] {content}
      ...
    ```

### /kick

Source: `handleKickCommand`.
- no arg → error: `Usage: /kick <persona>`
- not a member → error: `{persona} is not in #{channel}`
- success → `*** {persona} has been kicked from #{channel}`

### Unknown command

Source: `Model.handleCommand`.
`Unknown command: {token}. Try /help, genius.` (error).

---

## Model picker

Source: `internal/ui/model_picker.go` and `model_picker_keys.go`.

The picker is rendered as a single `MsgRaw` chat block, rewritten in place
on every keystroke. Layout:

```
{title}
filter> {query}
  {row}
  {row}
> {selected-row}
  {row}
  ...
  ...
  enter=pick  esc=cancel  type=filter
```

Special states:
- Empty filter result: `  (no matches — backspace to widen)`
- Truncated tail (more rows below window): `  ...`

Picker dismissal:
- Esc / Ctrl-C → `Picker cancelled.` (system).
- Enter on empty filter → `Picker: no selection (filter excludes everything).`
- Enter on a selection → callback fires; for `/models` that's
  `*** Model switched to: {choice}`.

---

## Splash / startup banner

Source: `internal/ui/art.go`, emitted from `model.go` `splashMsg` handler.

In order of emission:

1. `SplashArt()` — one of six ANSI art blocks selected at random
   (`splashArt1` … `splashArt6`). All are pre-colored ANSI literals:
   - `splashArt1`: cyan ASCII-shade "BITCHTEA" block
   - `splashArt2`: red double-line box-glyph "BITCHTEA" block
   - `splashArt3`: magenta ASCII-stack "bitchtea" block with grey
     subtitle: `        your code's worst nightmare       `
   - `splashArt4`: yellow framed box with white inner text:
     `bitchtea v0.1.0`, `(c) 2026 jstamagal`, ASCII pattern lines
   - `splashArt5`: green framed inner box with text:
     `an agentic harness for the unapologetic`
   - `splashArt6`: red box-glyph "bitchtea" block

2. `SplashTagline` (always):
   ```
     « bitchtea » — putting the BITCH back in your terminal since 2026
     an agentic coding harness for people who don't need hand-holding
   ```

3. `ConnectMsg` (formatted with provider, model, workdir):
   ```
     *** Connecting to {provider}...
     *** Using model: {model}
     *** Working directory: {workdir}
   ```

4. (conditional) `Loaded {n} context file(s) from project tree` —
   system message when `agent.DiscoverContextFiles` returned non-empty.

5. (conditional) `Loaded MEMORY.md from working directory` — system
   message when MEMORY.md is present.

6. (conditional) `Session: {sess.Path}` — system message when session
   was created.

7. `MOTD` (always):
   ```
     ─────────────────────────────────────────────────────────────
     Type a message to start coding. Use /help for commands.
     /models to browse, /set model to switch. /quit to exit. Don't be a wimp.
     Use @filename to include file contents. /set auto-next on for autopilot.
     ─────────────────────────────────────────────────────────────
   ```

8. (conditional) The agent's full system prompt as a system message
   (`m.agent.SystemPrompt()` — variable content composed by the agent
   package; not enumerated here).

Pre-ready fallback view (before the first `WindowSizeMsg`):
`initializing bitchtea...`

Input area placeholder (textarea, when input is empty):
`type something, coward...`
Input prompt: `>> `

---

## Top bar

Source: `Model.View`. Format:
` bitchtea — {provider-or-profile}/{model} [{active-context-label}]{flags}{queued} `
right-padded; right side: ` {3:04pm} ` (clock).

Flags appended in this order if enabled:
- ` [auto]` when `cfg.AutoNextSteps`
- ` [idea]` when `cfg.AutoNextIdea`
- ` [queued:{n}]` when there are queued messages.

---

## Status bar

Source: `Model.View`. Left segment:
` [{AgentNick}] {state-string} `
where `state-string` is one of:
- `idle`
- `{spinner} thinking...`
- `{spinner} running tools...`

Right segment (joined with ` | `):
- per-tool stat tokens: `{name}({count})` (one per known tool)
- mp3 status (if mp3 has tracks): see "/mp3" status text above
- background activity slot (if any): `bg:{n} /activity {truncated-latest}`
- `~{tokens} tok | {elapsed}` (always last)

---

## Signal and key feedback

Source: `internal/ui/model.go` `handleCtrlCKey`, `handleEscKey`.

Ctrl+C ladder (system messages, refreshes viewport each time):
- 1st press, streaming, no queue:
  cancelActiveTurn message =
  `Interrupted. Press Ctrl+C again to clear queued messages; press it a third time to quit.`
- 1st press, streaming, with queue:
  `Interrupted. {n} queued message(s) remain. Press Ctrl+C again to clear them; press it a third time to quit.`
- 1st press, idle, with queue:
  `{n} queued message(s) remain. Press Ctrl+C again to clear them; press it a third time to quit.`
- 1st press, idle, no queue: `Press Ctrl+C twice more to quit.`
- 2nd press, queue present: `Cleared {n} queued message(s). Press Ctrl+C again to quit.`
- 2nd press, no queue: `Press Ctrl+C again to quit.`
- 3rd press: triggers `tea.Quit` (no message).

Esc ladder:
- Tool panel open → closed: `Tool panel closed.`
- MP3 panel open → closed: `MP3 panel closed.`
- Queue clear armed and queue present: `Cleared {n} queued message(s).`
- 1st meaningful Esc, active tool present:
  - on cancel success: `Cancelled {tool}.`
  - on cancel failure: `Could not cancel {tool}: {err}`
- 1st meaningful Esc, no active tool: `Press Esc again to cancel the turn.`
- 2nd Esc: triggers `cancelActiveTurnWithQueueArm` →
  `cancelActiveTurn` system message: `Interrupted by Esc.`

OS signal (SIGINT/SIGTERM forwarded as `SignalMsg` while streaming):
`cancelActiveTurn` message: `Interrupted by signal.`

Up-arrow on empty input with queue (unqueueing):
`Unqueued message: {input}` (system).

Enter pressed while streaming (queueing):
`Queued message (agent is busy): {input}` (system).

Stale-queue discard after a turn finishes:
`Discarded {n} queued message(s) older than {duration} — context changed. Re-send if still relevant.`

Auto-follow-up start banner before each follow-up turn:
`*** {label}: continuing...` (label is `auto-next-steps` or `auto-next-idea`).

Daemon-checkpoint background notice (after each turn, when daemon is up):
- failure: `daemon submit failed: {err}` (error).
- success → enqueues a BackgroundActivity entry with summary:
  `session-checkpoint submitted to daemon ({jobID})`.

Other background-activity errors (from synchronous `submitDaemonCheckpoint`
helper used in tests but also live):
- `daemon submit failed: {err}` (error).

Per-turn save errors:
- `focus save failed: {err}` (error)
- `checkpoint save failed: {err}` (error)

---

## Agent event surface

Source: `Model.handleAgentEvent` in `model.go`. The agent emits events
that translate to chat lines:

- `text` events → streamed assistant prose, rendered via Glamour markdown
  inside an `MsgAgent` line. Content is variable.
- `thinking` events → `MsgThink` line, content is the thinking placeholder
  literal `thinking...` initially, then replaced with the model's
  reasoning text as it streams.
- `tool_start` → `MsgTool` line: `calling {tool}...`
- `tool_result` → `MsgTool` line containing the raw tool output, truncated
  if more than 20 lines with trailing `\n... ({n} more lines)`. If the
  tool errored, it's an `MsgError` line instead.
- `state` event with `StateThinking` → seeds an `MsgThink` placeholder
  with content `thinking...`.
- `error` event → `MsgError` line:
  `Error: {err}` (one line per error). When `llm.ErrorHint` returns a
  non-empty string, a second line is appended: `\n  hint: {hint}`.
  Possible hints (from `internal/llm/errors.go`):
  - `context too large — try /compact`
  - `auth failed — check /set apikey`
  - `access denied — your API key may lack permissions for this model`
  - `model not found — check the model name with your provider`
  - `request timeout — try again`
  - `rate limited — too many requests; slow down or upgrade tier`
  - `provider error — try again in a moment`
  - `cannot reach the server — is it running? (for local models, try: ollama serve)`
  - `DNS lookup failed — check /set baseurl`
  - `connection refused — is the local server running?`
  - `TLS error — check /set baseurl or your network proxy`
  - `request timed out — model may be slow or network unstable`

(Agent text output itself is rendered through Glamour and contains
arbitrary model output; not enumerated. Tool stdout is also passed
through verbatim and not enumerated.)

---

## Tool side panel (`Ctrl+T` toggles visibility)

Source: `internal/ui/toolpanel.go`. Box-rendered when visible and the
agent is streaming. Static labels:
- `Tools` (header)
- `  {tool-name}({count})` per known tool
- `  Tokens: ~{n}` or `~{n.X}k` when token count > 0
- `  Time: {elapsed}` when elapsed > 0
- `Recent` (header)
- Per recent call: `  {icon} {tool}{ duration}` where icon is `◉` running,
  `✓` done, `✗` error.

---

## CLI / pre-TUI emissions (stderr, before Bubble Tea takes the screen)

Source: `main.go`.

- `bitchtea: data migration warning: {err}` (when migration fails)
- `bitchtea: {err}` (generic CLI parse / startup error)
- No-API-key block (4 stderr lines):
  ```
  bitchtea: no API key found
    Set OPENAI_API_KEY, ANTHROPIC_API_KEY, OPENROUTER_API_KEY, or ZAI_API_KEY
    Or load the local ollama profile if you really mean no auth
    Or are you too cool for authentication?
  ```
- Resume failure paths:
  - `bitchtea: no sessions to resume`
  - `bitchtea: failed to load session: {err}`
  - `Resuming session: {path} ({n} entries)` (info, on success)
- Headless prompt errors: `bitchtea: {err}` (e.g.
  `headless mode requires --prompt or piped stdin`, `read stdin: ...`,
  etc., as wrapped from `collectHeadlessPrompt`/`runHeadless`).
- After bubbletea exits with `tea.ErrInterrupted`:
  `\nLater, coward.`
- Other crash exit: `bitchtea crashed: {err}`

CLI flag parser errors (from `parseCLIArgs`, surfaced as
`bitchtea: {err}`):
- `missing value for {flag}`
- `unknown flag: {token}`

`--help` / `-h` (from `printUsage()`, written to stdout via `fmt.Println`):
```
bitchtea — putting the BITCH back in your terminal

Usage: bitchtea [flags]
       bitchtea daemon <start|status|stop>

Flags:
  -m, --model <name>     Model to use (default: gpt-4o)
  -p, --profile <name>   Load a saved or built-in profile (ollama, openrouter, zai-openai, zai-anthropic)
  -r, --resume [path]    Resume session (latest if no path given)
  -H, --headless         Run once without the TUI
  --prompt <text>        Prompt to send in headless mode
  --auto-next-steps      Auto-inject next-step prompts
  --auto-next-idea       Auto-generate improvement ideas
  -h, --help             Show this help

Environment:
  OPENAI_API_KEY         OpenAI API key
  OPENAI_BASE_URL        OpenAI-compatible base URL
  ANTHROPIC_API_KEY      Anthropic API key
  OPENROUTER_API_KEY     OpenRouter API key for the openrouter profile
  ZAI_API_KEY            Z.ai API key for the zai-* profiles
  BITCHTEA_MODEL         Default model
  BITCHTEA_PROVIDER      Provider name (openai, anthropic)
  BITCHTEA_CATWALK_URL   Catwalk catalog base URL (no default; off when unset)
  BITCHTEA_CATWALK_AUTOUPDATE
                         Enable background catalog refresh (default: false)

Commands (inside the TUI):
  Core:
    /set <key> [value]     Show/change settings
    /profile [cmd] <name>  save/load/show/delete profiles
    /models                Open a fuzzy model picker
    /compact               Compact conversation context
    /clear                 Clear chat display
    /restart               Reset agent, start fresh
    /copy [n]              Copy last or nth assistant response
    /tokens                Token usage and cost estimate
    /sessions              List saved sessions
    /resume <number>       Resume a session by number
    /tree                  Show session tree
    /fork                  Fork session
    /theme                 Show current theme
    /help                  Show help
    /quit                  Exit

  IRC:
    /join <#channel>       Switch focus to a channel
    /part [#channel]       Leave a context
    /query <nick>          Route Enter persistently to nick
    /msg <nick> <text>     One-shot message to nick
    /channels              List open contexts and members
    /invite <nick>         Invite a persona
    /kick <nick>           Kick a persona

  Diagnostic:
    /debug on|off          Toggle verbose API logging
    /activity [clear]      Show or clear background activity

  Memory:
    /memory                Show MEMORY.md contents

  Media:
    /mp3 [cmd]             Toggle MP3 panel and player

Don't be a wimp. Just run it.
```

Headless mode (`-H`) emissions (not in the TUI; stdout/stderr stream of
`runHeadlessLoop`):
- `[auto] {label}\n` (stderr) before each follow-up turn
- `[tool] {name} args={args}\n` (stderr) on tool start
- `[tool] {name} result={result}\n` (stderr) on tool result success
- `[tool] {name} error={err} result={result}\n` (stderr) on tool error
- `[status] {label}\n` (stderr) on state change, where label is
  `thinking`, `tool_call`, or `idle`.
- assistant prose flowed verbatim to stdout.

---

## Daemon CLI emissions (`bitchtea daemon ...`)

Source: `daemon_cli.go`.

- unknown subcommand → stderr:
  `bitchtea: unknown daemon subcommand: {arg}` then usage.
- `daemon start`:
  - `bitchtea: cannot open daemon log: {err}` (stderr)
  - `bitchtea: daemon already running (pid {pid})` (stderr)
  - `bitchtea: another daemon is already running` (stderr; pid lookup
    failed)
  - `bitchtea: cannot create daemon directories under {base}: {err}`
    (stderr)
  - `bitchtea: daemon failed: {err}` (stderr)
- `daemon status`:
  - `unknown (probe error: {err})`
  - `not running`
  - `running (pid {pid})`
  - `running (pid unknown)`
- `daemon stop`:
  - `not running`
  - `not running (pidfile error: {err})`
  - `not running (pid {pid} not found)`
  - `stop failed: {err}`
  - `stop signal sent (pid {pid})`
- `daemon -h` / `--help` / `help` → `printDaemonUsage` (stdout):
  ```
  bitchtea daemon — manage the background daemon

  Usage: bitchtea daemon <subcommand>

  Subcommands:
    start    Run the daemon in the foreground (Ctrl-C to stop). Manual launch
             only — for backgrounding, use nohup/systemd/launchd.
    status   Print "running (pid N)" or "not running". Exit 0 either way.
    stop     Send SIGTERM to the running daemon. "not running" if absent.

  The daemon is opt-in. The TUI works fine without it. See
  docs/phase-7-process-model.md for the full design.
  ```

---

## Notes on rendering

- All `MsgSystem` lines are prefixed in the viewport with `***` (yellow);
  all `MsgError` lines with `!!!` (red); all `MsgTool` lines with
  `  → {tool}:` in cyan; `MsgUser` is `<{nick}>` in green; `MsgAgent` is
  `<{nick}>` in magenta with content rendered through Glamour markdown;
  `MsgThink` is prefixed with `💭` in italic magenta; `MsgRaw` writes
  ANSI passthrough verbatim. Each line is timestamped `[HH:MM]` in grey.
- Truncation rule for restored session messages: anything over 500 chars
  is suffixed `... (truncated from session)` when replayed by
  `ResumeSession`.
