# Agent 5 Ideas — Retro-Futurism and Vintage Computing

*Adjusting phosphor-green tint on the CRT. The smell of warm plastic and ozone fills the room. A dot-matrix printer chatters in the corner.*

---

## Set 1: Necessary Fixes via 1980s/90s Technology Paradigms

These are the bread-and-butter improvements -- the kind of thing you'd fix on a VAX/VMS terminal between sips of cold coffee and a cigarette. Pull from the mean of the distribution. Every sysadmin worth their pager knows these.

### 1. `fix` — Session JSONL writes need a write-ahead log like a filesystem journal

The session persistence layer (`session.go`) appends entries directly to JSONL. If the process dies mid-write -- Ctrl+C, kernel panic, power spike from a faulty UPS -- you get a corrupted last line. This is the exact problem journaling filesystems solved in the late 80s. Add a small WAL file: write the entry to `.wal` first, fsync, then append to the main JSONL and delete the WAL. On load, replay any orphaned WAL entry. This is not exotic. This is what IBM was doing in 1990 with AIX.

**Scope:** `internal/session/session.go` -- modify `Append()`, add `replayWAL()` in `Load()`.

### 2. `refactor` — Bubbletea Model struct is a monolithic mainframe; decompose into subsystems

The `Model` struct in `model.go` holds config, agent state, viewport, input, theming, session, sound, tool panel, message history, slash commands, and debug state all in one flat struct. This is a single mainframe doing every job. Decompose into logical subsystems: `UIState`, `AgentState`, `InputState`. Each subsystem handles its own `Update`/`View` fragment. The top-level Model becomes a bus coordinator. This is modular architecture 101 -- the same reason DEC moved from single PDP machines to modular VAXen.

**Scope:** `internal/ui/model.go` -- extract structs, split `Update()` into delegated calls.

### 3. `perf` — Stream token buffering should use a ring buffer, not repeated string concatenation

The streaming SSE parser in `client.go` and `anthropic.go` builds response content by repeatedly concatenating strings as tokens arrive. At 100+ tokens per response, this is O(n^2) allocation. A ring buffer (or even a pre-allocated `bytes.Buffer`) would amortize this to near-zero. This is the kind of optimization Bjarne Stroustrup would have made you do on a whiteboard in 1986 before letting you touch the production compiler.

**Scope:** `internal/llm/client.go`, `internal/llm/anthropic.go` -- replace `+=` string concat with `bytes.Buffer` in the SSE stream accumulation path.

### 4. `fix` — Cost tracker pricing table is stale; needs a refresh mechanism or external data source

`cost.go` has hardcoded pricing from 2024. Model pricing changes frequently. The prefix-matching fallback is clever but fragile. Either ship a small embedded JSON pricing table that gets updated on build, or add an optional `/prices fetch` command that pulls from a known URL (or even just a static GitHub raw file). In the 90s we called this "configuration management" and we solved it with INI files and a scheduled FTP job. Same principle.

**Scope:** `internal/llm/cost.go` -- extract pricing to a loadable data structure, add refresh mechanism.

### 5. `test` — Tool registry has zero test coverage; add integration tests for the bash tool sandbox

`tools.go` defines the registry and tool definitions but there are no tests verifying that tool execution works end-to-end. The bash tool especially needs integration tests: does it enforce working directory? Does it capture stdout/stderr correctly? Does timeout work? This is the kind of regression that bites you at 3am when the pager goes off. Write them once, sleep forever.

**Scope:** `internal/tools/tools.go` -- add `tools_test.go` with tests for `Execute()` covering read, write, edit, bash with working directory enforcement, timeout, and error capture.

---

## Set 2: Grand, Impractical Ideas Involving Delightful Antique Technology

*Sets down the soldering iron. Pushes the oscilloscope aside. These are the ideas that would make Steve Jobs cry and Nikola Tesla smile. Too analog? Absolutely. Difficult to wire? Incredibly. But consider the aesthetics.*

### 6. `feat` — CRT phosphor persistence rendering mode for streaming tokens

When tokens stream in, they should render with simulated CRT phosphor persistence -- the new text glows bright green/amber (depending on theme), then slowly fades to the normal color over ~300ms, mimicking the slow decay of P1 or P3 phosphor on a real monitor. This is a post-render animation hook on the viewport. The BitchX heritage deserves nothing less than authentic green-screen ambience. Requires per-line timestamp tracking in the viewport to compute fade state.

**Scope:** `internal/ui/render.go`, `internal/ui/styles.go` -- add phosphor decay animation hook, integrate with Bubbletea's tick mechanism.

### 7. `feat` — VCR-style session navigation with tracking adjustment

Sessions are stored as JSONL with tree structure. Add a VCR metaphor: `/rewind`, `/fastforward`, `/eject` (quit), and a visual "tape counter" in the bottom bar showing position within the session. `/tracking` auto-adjusts to find the nearest branch point. Session tree visualization already exists as `/tree` but it should look like a VHS tape index screen -- blue background, blocky white text, scan lines. The `fork` command becomes `record` (pressing REC on the VCR to start a new branch).

**Scope:** `internal/ui/model.go` -- add VCR-mode slash commands, `internal/session/session.go` -- add navigation helpers, `internal/ui/render.go` -- tape counter UI.

### 8. `feat` — Pneumatic tube notification system for background agent completion

When the agent finishes a long-running task (auto-next-steps, multi-tool execution), instead of just a terminal bell, send a notification through a pneumatic tube metaphor: a small ASCII art canister slides across the bottom bar with a satisfying thunk animation, containing the summary of what completed. The canister stays visible for 5 seconds then gets "sucked back" off screen. This gives tangible physicality to async events.

**Scope:** `internal/ui/model.go` -- add notification queue, `internal/ui/render.go` -- canister animation, `internal/sound/sound.go` -- optional thunk sound.

### 9. `feat` — Teletype output mode for hardcopy session export

Add `/hardcopy` command that exports the current session as formatted text suitable for a physical teletype or line printer: fixed 80-column width, monospaced, with form feeds between long exchanges, and a header that looks like a DEC WRITER header (date, session ID, page number). Output goes to a file that can be `lpr`'d directly. For extra authenticity, add optional paper-feed perforation marks (`--- tear here ---`).

**Scope:** `internal/session/session.go` -- add `ExportHardcopy()` method, `internal/ui/model.go` -- wire `/hardcopy` command.

### 10. `feat` — Reel-to-reel audio cue system for thinking/processing states

When the model is "thinking" (streaming hasn't started yet but the request is in flight), play an ASCII-art reel-to-reel tape animation: two spinning reels with tape moving between them, visible in the tool panel area. The tape speed corresponds to how long the model has been thinking. This is not functional -- it is pure atmospheric delight. The reels should be drawn with box-drawing characters and spin at a rate of ~4 frames per second using Bubbletea's tick.

**Scope:** `internal/ui/toolpanel.go` -- add reel-to-reel spinner, `internal/ui/model.go` -- integrate during pre-stream waiting state.

---

## Set 3: Wildly Whimsical Electromechanical Gadgets (Steam Power or Vacuum Tubes Required)

*Waves hand dismissively. The oscilloscope traces a perfect sine wave and then flatlines. From behind a curtain of dust, emerge five contraptions that would make Charles Babbage weep with joy and modern health inspectors flee.*

### 11. `feat` — Steam-powered context compaction engine with gauge visualization

The `/compact` command should be visualized as an actual steam engine: a pressure gauge in the UI that rises as context approaches token limits. When `/compact` fires, the gauge needle drops with an animated hiss (ASCII steam particles). The compaction algorithm itself should be weighted by a "temperature" metaphor: high-pressure (aggressive compression) vs low-pressure (gentle summarization). The user can set the PSI threshold. Below 50 PSI, context is comfortable. Above 90 PSI, auto-compact triggers with a flashing red warning. The gauge renders as ASCII art with a needle that physically moves.

**Scope:** `internal/ui/model.go` -- add pressure gauge state, `internal/agent/agent.go` -- add temperature-based compaction, `internal/ui/render.go` -- gauge and steam particle rendering.

### 12. `feat` — Vacuum tube reliability scoring for API provider health

Each API provider (OpenAI, Anthropic, custom) gets represented as a bank of vacuum tubes. Each tube represents a recent request: lit = successful, flickering = slow (>5s), dark = failed. The tube bank lives in the status bar area. Over time, tubes that fail too often "burn out" and the system automatically suggests switching providers via a 1950s-radio-announcer-style ASCII message: "ATTENTION OPERATOR: TUBE BANK ANTHROPIC SHOWS 40% FAILURE RATE. RECOMMEND SWITCHING TO OPENAI TUBE BANK FOR RELIABILITY." This is a real health monitor dressed in vacuum tube cosplay.

**Scope:** `internal/llm/client.go` -- add request outcome tracking per provider, `internal/ui/model.go` -- tube bank rendering, `internal/ui/render.go` -- tube ASCII art states.

### 13. `feat` — Electromechanical relay-based routing for multi-provider failover

Build a visual relay system for provider switching. When the primary provider fails, an ASCII relay "clicks" animatedly in the UI, rerouting the connection to the backup provider. The relay board shows 4 relays (one per provider config), with physical throw positions: NORMALLY CLOSED = active, NORMALLY OPEN = standby. During failover, the relay arm physically swings (3-frame animation) with a spark effect. If all relays fail, the system enters "brownout mode" with dimmed UI and a warning klaxon (terminal bell pattern).

**Scope:** `internal/llm/client.go` -- add failover logic with provider priority queue, `internal/ui/model.go` -- relay board state and animation, `internal/config/config.go` -- provider priority config.

### 14. `feat` — Punch card deck visualization for session branching history

The `/tree` command should render session branches as a physical punch card deck. Each card represents a session entry, with visual "holes" (dark spaces in a grid) representing the hash of the content. Branch points show cards fanning out into multiple decks. Users can `/pull <card-number>` to resume from any card in the deck. The visual metaphor: you are literally pulling a card from the tray and inserting it into the reader to resume that branch. Cards are numbered in IBM 80-column format (right-justified, 2-digit).

**Scope:** `internal/session/session.go` -- add card-deck metadata structure, `internal/ui/render.go` -- punch card ASCII rendering with fan-out branch visualization, `internal/ui/model.go` -- wire `/pull` command.

### 15. `feat` — Telegraph key input mode for hands-free command triggering

Add an optional "telegraph mode" where specific rhythmic tap patterns on a single key (like Space) trigger commands: single tap = send message, double tap = toggle tool panel, triple tap = compact, long hold = interrupt. The input area shows a telegraph key ASCII art that physically depresses on tap. Modeled after actual Morse code timing (dit = <200ms, dah = >200ms). This is an accessibility feature disguised as a steampunk fantasy -- it allows one-key operation for users with limited mobility. The key visualization sits below the input area and provides visual confirmation of recognized patterns.

**Scope:** `internal/ui/model.go` -- add telegraph input handler with timing recognition, `internal/ui/render.go` -- telegraph key ASCII art with depression animation, `internal/config/config.go` -- telegraph mode toggle.
