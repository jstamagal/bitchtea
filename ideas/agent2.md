# Agent 2 -- Retro-Futurism and Vintage Computing

*I crack my knuckles over a warm VT220. The 5.25" floppy drive grinds. Somewhere in the basement, a PDP-11 hums the song of its people. I have seen the future, and it smells like hot electrostatic plastic and burnt transformer wax.*

---

## Set 1: Five Necessary Fixes Using 1980s/90s Technology Paradigms (p < 0.5)

*The kind of maintenance a VMS sysadmin would knock out between compiling kernel patches and waiting for the line printer to finish the daily report.*

### 1. `refactor` -- The anonymous Theme struct is defined three times; extract to a named type

**Files:** `internal/ui/themes.go`

The 12-field anonymous struct for Theme data appears at line 10 (`var Theme`), line 44 (`var themes` map value), and line 93 (inside `SetTheme`). Every time you add a theme color, you must edit three identical struct literals. In 1983, Niklaus Wirth would have failed you on the spot for this. Extract a named `ThemeColors` struct. The `var Theme` becomes `ThemeColors`, the map becomes `map[string]ThemeColors`, and `SetTheme` is a single assignment instead of a field-by-field copy through an anonymous cast. Three definitions become one. This is not controversial. This is typing discipline.

### 2. `fix` -- Markdown renderer cache keyed on int width leaks when terminal resizes repeatedly

**Files:** `internal/ui/render.go`

The `markdownRenderers` map caches one `glamour.TermRenderer` per width value. Each renderer holds allocated buffers. If the user resizes the terminal repeatedly (dragging a window, tmux pane splits), the map grows unbounded -- one entry per distinct pixel width. This is a memory leak disguised as an optimization. The fix is a bounded cache: keep at most N renderers (3 is plenty -- current width, one above, one below), evicting the least-recently-used on insert. This is caching 101. DEC's VMS file cache had LRU eviction in 1977. We can manage it for a map of three renderers.

### 3. `fix` -- CLI flag parser is hand-rolled; fails on edge cases like `--model=` (equals syntax) and stops at first unknown flag

**Files:** `main.go:24-51`

The flag parser iterates `os.Args` with manual index tracking. It does not handle `--model=gpt-4o` (equals-separated values, standard GNU long option syntax). It silently ignores unknown flags instead of erroring. It has no `--` end-of-options sentinel. The `flag` package from the Go standard library would solve all of this in five lines, or `pflag` for GNU-style long options. The hand-rolled parser is 28 lines that do less than the 5-line standard library equivalent. In 1990, we called this "NIH syndrome" and we were right to mock it.

### 4. `chore` -- go.mod has no minimum Go version directive; build reproducibility is undefined

**Files:** `go.mod`

The `go.mod` file should declare a `go 1.24` directive (matching the AGENTS.md specification). Without it, different Go toolchains may produce different build outputs, use different default garbage collector parameters, or resolve dependencies differently. Build reproducibility is not optional. In the 1980s, we stamped the compiler version on every binary. The Go module system does this for you -- if you let it.

### 5. `test` -- No tests for session fork -- a core feature with tree-walking logic and file I/O

**Files:** `internal/session/session.go` -- needs `session_test.go` additions

Session fork copies entries to a new file and mutates the branch metadata. There are tests for `Load` and `New` but nothing for `Fork`. Does it handle an empty session? Does it handle a session where the last entry has no ID? Does the forked file contain exactly the right entries? Does the branch tag propagate correctly? Every filesystem operation is a potential failure point, and fork does three: create file, write entries, close. On NFS, any of those can fail silently. Write the tests. The pager will thank you at 3 AM.

---

## Set 2: Five Grand, Impractical Ideas Involving Specific, Delightful Antique Technology (p < 0.2)

*Too analog, you say? Difficult to wire? Of COURSE they're difficult to wire. The Apollo Guidance Computer was difficult to wire. Nobody complained about THAT.*

### 6. `feat` -- Illuminated signboard mode: Nixie tube token counter

Replace the flat `~4k tokens` text in the status bar with a simulated Nixie tube display. Each digit of the token count renders inside a glass-enclosed wire-frame digit, complete with a warm orange glow (ANSI color 214) and a subtle flicker on digit transitions. The tubes should show: input tokens (left pair), output tokens (center pair), and a glowing decimal cost (right pair). When a digit changes, the old digit fades through a brief "ghosting" effect while the new one warms up -- just like real Nixie tubes that needed 50ms to ionize. Add a `--nixie` flag to activate. The aesthetic payoff is enormous. The practical payoff is zero. This is the correct ratio.

### 7. `feat` -- VCR tape-indexed session browser with scan line artifacts

The `/sessions` command currently lists files as plain text. Replace it with a VHS tape index screen: blue background, blocky white text listing sessions in rows of four (like a video rental store shelf), each "tape" showing a date, model name, and a tiny 3-line preview of the first user message. Scroll with Up/Down. Press Enter to "insert tape" (resume session). The whole thing renders with faint horizontal scan lines (every 3rd line gets a dim ANSI overlay) and occasional tracking noise (a line of garbled characters that scrolls past once every 10 seconds). The `--resume` flag becomes a physical metaphor: you are literally pulling a tape off the shelf and putting it in the machine.

### 8. `feat` -- Pneumatic tube message delivery for inter-turn notifications

Between agent turns, display a small animated pneumatic tube canister that whooshes across the bottom status bar, carrying the next tool call result from "the lab" (off-screen) to "the operator" (the viewport). The canister is 8 characters wide, rendered as `[===>]`, slides right-to-left, pauses at center for 400ms (the " thunk"), then opens to reveal a one-line summary of the tool result. Multiple tool results queue up as separate canisters that arrive in sequence. This gives physical rhythm to the asynchronous agent loop. The operator can see the machine working, not just the output appearing.

### 9. `feat` -- Dot-matrix print spooler for session export

Add `/print` command that "prints" the session to a text file using simulated dot-matrix output: each character is composed of a 5x7 grid of dots (Unicode block characters), every line has a faint perforation line beneath it, and every 66 lines (standard US Letter page) a form-feed separator appears with page number and timestamp in the footer. The output file looks like it was printed on a Panasonic KX-P2124 in draft mode. Optionally pipe directly to `lpr` for actual physical printing. The 80-column fixed-width output is also genuinely useful as a session archive format that renders identically in every terminal and every decade.

### 10. `feat` -- Reel-to-reel audio visualization for thinking/processing states

When the agent is in `StateThinking`, render a pair of spinning reel-to-reel tape spools in the tool panel area. The left reel starts full (representing the unprocessed prompt) and the right reel starts empty (representing the streamed response). As tokens stream in, the tape visually transfers from left to right. The reel speed is proportional to the streaming rate -- fast during token bursts, slow during pauses, and a visible "take-up" coast when streaming stops. Draw with box-drawing characters, updating at 4fps via Bubbletea tick. When the agent completes a turn, the tape "rewinds" with both reels spinning backward for 300ms. This is not decorative. This is INFORMATION. The operator can gauge response progress from the reel state alone, without reading a single token.

---

## Set 3: Five Wildly Whimsical Electromechanical Gadgets (Steam Power or Vacuum Tubes Required) (p < 0.07)

*I wave my hand dismissively. The oscilloscope catches the gesture and plots it as a perfect square wave. From beneath a tarpaulin in the corner, five contraptions emerge that would make Nikola Tesla file a patent and a restraining order simultaneously.*

### 11. `feat` -- Babbage-style difference engine for context window arithmetic

The agent's context compaction (`/compact`) currently just truncates old messages. Replace this with a visual difference engine: a room-sized brass contraption (rendered in the viewport as ASCII art gears, cams, and levers) that mechanically computes the optimal set of messages to retain. The engine has seven columns representing seven message attributes (recency, length, tool density, user-message ratio, code-content ratio, error-reference count, and "emotional weight" derived from exclamation marks and capitalization). Each column is a stack of numbered brass wheels. When compaction runs, the wheels spin, click into position, and the retained message set is "calculated" mechanically. The user watches gears turn for 2 seconds of animation, then sees the result. The algorithm underneath is real -- it is a weighted scoring function that selects messages to retain -- but it is presented as a 19th-century mechanical computation.

### 12. `feat` -- Vacuum tube signal amplifier for API response quality visualization

Each API request is visualized as a signal passing through a vacuum tube amplifier. The tube bank (rendered as ASCII art along the top of the viewport, 8 tubes wide) shows signal quality in real-time: tube filament brightness corresponds to first-token latency (bright = fast, dim = slow), and the plate glow intensity corresponds to streaming throughput (bright = many tokens/sec, dim = trickle). Tubes that experience errors "blow out" with a brief flash and go dark. After N consecutive blowouts, the tube is marked "BURNED OUT" and the system suggests switching to the backup tube bank (other provider). The tube status persists across the session as a rolling health display. This is a real observability feature dressed in 1940s radio engineering cosplay.

### 13. `feat` -- Steam-powered auto-next-step governor with flywheel animation

The `autoNextSteps` feature currently runs without any throttling or visual feedback. Replace the boolean flag with a steam governor: two brass balls on spinning arms that rise as the agent works harder (more consecutive auto-next turns) and drop as it idles. When the balls reach the top (too many consecutive auto-next cycles), the governor engages and forces a cooldown pause -- the steam hisses (a brief ASCII particle effect), the flywheel slows, and the user gets a "GOVERNOR ENGAGED -- cooling down" message. The user can override with `/override-governor` which renders as physically prying the governor arms apart with a crowbar (ASCII art). This prevents runaway agent loops while providing tangible visual feedback about system strain.

### 14. `feat` -- Electromechanical patch panel for provider routing

Replace the current static provider selection (`/provider openai` or `/provider anthropic`) with a visual telephone switchboard patch panel. The panel shows 6 jacks on the left (input sources: OpenAI, Anthropic, local, custom1, custom2, custom3) and 6 jacks on the right (output: current-session, auto-next, forked-session, pipe-mode, replay-mode, background). The user routes connections by typing `/patch openai current-session` which renders as an animated patch cable being plugged in -- a colored line snakes from the left jack to the right jack with a satisfying "click" frame. Multiple simultaneous connections are supported. The panel renders in a dedicated viewport section and updates in real-time. This makes multi-provider, multi-session topology visible and tactile.

### 15. `feat` -- CRT electron beam debugger for real-time rendering introspection

Add `/crt-debug` mode that renders the terminal output as if viewed through the glass of a CRT monitor being serviced on a bench. The electron beam is visible as a bright scan line that sweeps top-to-bottom at 60Hz (simulated by highlighting the "current render line" with a brighter-than-normal ANSI background). The phosphor persistence trail shows recently-rendered content fading slowly. The operator can see exactly which lines of the viewport are being redrawn each frame, which reveals unnecessary re-renders and rendering bottlenecks. The beam also highlights tool calls with a momentary brightness spike and shows a "retrace" animation when the viewport scrolls. This is a genuine rendering performance profiler that happens to look like you are servicing a Sony Trinitron in 1987.
