# Agent 4 Ideas - Industrial IoT Systems Integrator & Electrical Engineer

*Spent 22 years splicing fiber in grain elevators, hardening PLC firmware against EMI from 480V motor drives, and convincing plant managers that "it works on my laptop" is not a qualification. I look at this TUI the way I look at a SCADA panel someone left on a loading dock in a rainstorm.*

---

## Set 1: Five Obvious Code Fixes Framed as Physical Resilience (p < 0.5)

*Like walking into a control room and seeing the UPS indicator blinking amber. You don't need a schematic -- you fix it before the next power flicker takes out the historian.*

### 1. fix: `http.Client{}` in NewClient has zero transport hardening -- one bad network takes down the whole session

**File:** `internal/llm/client.go:281-289`

`NewClient` returns a bare `&http.Client{}` with no custom transport. No `DialContext` timeout. No `TLSHandshakeTimeout`. No `IdleConnTimeout`. No `MaxIdleConnsPerHost`. On a cellular backhaul, a VPN over a satellite link, or factory floor WiFi competing with VFDs spraying 2.4GHz hash, the client hangs for OS TCP timeouts (120+ seconds on Linux) before failing silently. This is the equivalent of running a PLC with no watchdog timer. Set a `&http.Transport{}` with `DialContext` (10s connect timeout), `TLSHandshakeTimeout` (10s), `IdleConnTimeout` (90s), and `MaxIdleConnsPerHost` (2). Two lines of code. Prevents the entire session from locking up on a flaky network.

### 2. fix: Session Append opens/closes the file per write -- corrupted JSONL on power loss, no recovery

**File:** `internal/session/session.go:74-100`

Every call to `Append` does `os.OpenFile` -> `Write` -> `Close`. On an eMMC boot device, SD card, or NFS mount over a congested link, this is one fsync per message. If the process dies mid-write (power blip, OOM kill, `kill -9`, someone trips over the power strip), you get a partial last line that `Load` silently skips via `continue` on line 64. Data loss with no warning. Write to a `.wal` temp file first, `fsync`, then rename over the target. On `Load`, check for orphaned WAL entries and replay them. This is filesystem journaling 101 -- AIX was doing it in 1990.

### 3. fix: Bash tool `execBash` has zero resource confinement -- one runaway command starves the host

**File:** `internal/tools/tools.go:264-305`

The bash tool sets a `context.WithTimeout` (good), but there are no limits on RSS memory, file descriptors, child process spawning, or CPU. A `fork bomb`, `yes > /dev/null &` in a loop, or `find / -type f` saturates resources. On a shared industrial controller running bitchtea alongside an HMI, one bad LLM-generated command starves the operator panel. Use `cmd.SysProcAttr` with `Rlimit` fields (`RLIMIT_AS` for memory, `RLIMIT_NOFILE` for FDs, `RLIMIT_NPROC` for child count), or use a cgroup. The timeout only kills the parent process -- zombie children inherit PID 1 and keep running.

### 4. perf: Streaming token accumulation uses `strings.Builder` per turn -- GC pressure on long sessions

**File:** `internal/agent/agent.go:133-140`

Each `SendMessage` turn allocates a fresh `strings.Builder` for `textAccum`. Over a long session (100+ turns), this is 100+ heap allocations that the GC has to sweep. The SSE scanner in `client.go:197` allocates a 256KB buffer per request too. On a memory-constrained edge device (Raspberry Pi running bitchtea over SSH from a factory floor terminal), this creates visible pauses when the GC kicks in. Use `sync.Pool` for both the builders and the scanner buffers. Pool, use, return. Same pattern we use for MODBUS frame buffers on PLCs.

### 5. test: Zero test coverage for tool execution failure modes -- ticking time bombs

**File:** `internal/tools/tools.go` -- needs `tools_test.go`

Tests exist for the agent loop, themes, rendering. Nothing for what happens when tools fail in the field: `execRead` on a binary file (returns raw bytes, no encoding check), `execEdit` with `oldText` that exists but spans encoding boundaries, `execBash` when stdout exceeds the 50KB truncation limit mid-write (partial write, garbled output), `execWrite` to a full disk. These are "works on the bench, blows up in production" scenarios. Add `tools_test.go` covering read-binary, edit-missing, edit-ambiguous, bash-timeout, bash-truncation, write-permission-denied, write-disk-full.

---

## Set 2: Five Grand Ideas Requiring External Physical Sensors and Mechanisms (p < 0.2)

*The purely digital fixes are fine. Now let me dismiss them entirely and talk about what this thing ACTUALLY needs -- hardware. Real, physical, wires-and-solder hardware. The kind of ideas that make the project manager reach for the budget spreadsheet and the electrician reach for the conduit bender.*

### 6. feat: Offline local LLM fallback -- the UPS for your coding assistant

**Files:** `internal/llm/client.go`, `internal/config/config.go`, new: `internal/llm/fallback.go`

Add a config option for a local LLM endpoint (Ollama, llama.cpp server, any OpenAI-compatible local server). When the primary API client fails N consecutive times, automatically fall back to the local model. The local model is dumber -- think "junior technician with a multimeter" vs "senior engineer with an oscilloscope" -- but it can still run read/write/edit/bash tools. The user sees `[OFFLINE - local]` in the status bar. Config via `BITCHTEA_FALLBACK_URL` env var or `--fallback` flag. When the primary comes back, auto-promote back with a visual indicator. This is how every critical system works: primary fails, backup takes over, primary recovers, you switch back.

### 7. feat: Bidirectional pipe mode for embedding bitchtea in automation pipelines

**Files:** New: `internal/pipe/pipe.go`, modify `main.go`

Add `--pipe` mode: read JSONL commands from stdin, write JSONL responses to stdout. No TUI, no Bubbletea, no alt-screen buffer. This lets you embed bitchtea in shell pipelines, cron jobs, CI/CD systems, or IIoT data processing pipelines. Example: `cat sensor_anomaly.json | bitchtea --pipe -m claude-sonnet-4-20250514 | jq '.analysis'`. Reuses the existing agent loop and tool system but replaces the Bubbletea UI with a JSONL serializer. This is the integration play. Any system that can pipe text can now use bitchtea as a processing stage. Think of it as MODBUS gateway mode -- the protocol stays the same, but now it talks to everything.

### 8. feat: Bandwidth-adaptive streaming with local tool result caching

**Files:** `internal/llm/client.go`, `internal/agent/context.go`, new: `internal/agent/bandwidth.go`

Add a latency monitor that tracks API response time and payload size. When first-token latency exceeds a threshold (say, >3s), enter "low-bandwidth mode": aggressive context compaction, shorter context windows, and local caching of tool results so the agent does not re-read files it already saw. Cache entries get a content hash for invalidation and a TTL for expiry. Show current mode in the status bar: `[NORMAL]`, `[LOW-BW]`, `[OFFLINE]`. This is directly useful for anyone on a tethered phone, a flaky VPN, or a satellite link. The cache is the same pattern we use for SCADA polling -- you do not re-poll a thermocouple if the reading is 200ms old.

### 9. feat: Session replay and deterministic reproduction mode

**Files:** `internal/session/session.go`, `internal/agent/agent.go`

Add `--replay <session-file>` that replays a session entry-by-entry with configurable pause between turns, showing exactly what the agent did and when. Pair with `--dry-run` mode where tool calls are logged but not executed (the agent receives a mock "ok" response). This is the software equivalent of an event recorder in a substation. When something goes wrong -- the agent deleted the wrong file, the LLM generated a destructive bash command, the session corrupted mid-compaction -- you need the black box. Regulators and auditors want to see the chain of events. This is not optional in any environment where the tool touches production systems.

### 10. build: Cross-compilation targets and minimal container image for edge deployment

**Files:** New: `Makefile` or `Taskfile.yml`, `Dockerfile`

Add build automation: cross-compile for `linux/arm64` (Raspberry Pi, NVIDIA Jetson, industrial edge gateways), `linux/amd64`, `darwin/arm64`. Add a minimal Dockerfile (multi-stage build, distroless or scratch base, <20MB final image). Add `.goreleaser.yml` for automated versioned releases. The project currently has `go.mod` but no build system beyond `go build`. On a factory floor, bitchtea runs on whatever hardware is available -- usually an old Thin Client running ARM Linux with 2GB RAM. Packaging it for that environment is not optional, it is deployment. A NEMA-rated enclosure does not change the circuit inside, but it makes the circuit deployable.

---

## Set 3: Five Last-Minute, Profoundly Unpolished Jury-Rigging Ideas (p < 0.07)

*This is the part of the blueprint where I've been awake for 36 hours, the delivery truck is late, and I'm soldering a 5V logic probe to a 24V industrial bus through a voltage divider made from resistors I pulled out of a dead VFD. These ideas work. I have seen worse working. Do not ask me to guarantee the MTBF.*

### 11. feat: Real-time system telemetry overlay -- the operator needs to see if the machine is healthy WHILE it is running

**Files:** New: `internal/telemetry/telemetry.go`, modify `internal/ui/model.go`

Add `/telemetry` slash command that overlays a live dashboard: CPU%, RSS memory, goroutine count, open file descriptors, network I/O (bytes sent/received to API endpoint), and disk writes (session file). Use `gopsutil` or raw `/proc/self/` reads on Linux. Display as a floating panel that updates every second. Log telemetry snapshots to session metadata so you can correlate "agent was slow at 14:32" with "system was swapping at 14:32." In any SCADA HMI, the operator sees process variables updating in real time. A coding assistant that runs for hours autonomously should show the same. The alternative is finding out the agent was OOM-killed two hours ago and you lost all the work.

### 12. feat: Predictive pre-fetch of likely tool calls while the LLM is still streaming

**Files:** `internal/agent/agent.go`, new: `internal/agent/prefetch.go`

While the LLM streams a response, analyze the partial text for likely tool call patterns. "Let me read the file..." -> pre-read likely file paths from recent context. "I'll run the tests..." -> pre-stage the bash command. Use simple heuristic matching (regex, keyword detection) to predict what tool call is coming and pre-execute reads speculatively. If the prediction matches the actual tool call, inject the cached result instantly. If not, discard and execute normally. On a high-latency API link (cellular, satellite), this shaves 2-5 seconds off each turn. This is the same pattern as predictive prefetch in CNC controllers -- the G-code says "load tool 3 next" and the tool changer starts moving before the current cut finishes. The tool changer is faster than the spindle.

### 13. feat: Session diffing and merge for reconciling forked multi-agent conversations

**Files:** `internal/session/session.go`, new: `internal/session/diff.go`

Add the ability to diff two session files: show which messages differ, which tool calls diverged, what the outcomes were. Then add merge capability that combines two forked sessions back together (like `git merge` for conversation trees). The JSONL tree structure already has `ParentID` and `BranchTag` fields -- it supports this conceptually but lacks the logic. In a multi-agent scenario where two bitchtea instances work on different parts of the same codebase, you need to reconcile their work. Think of it as OTDR trace comparison: two fibers spliced at the same point, diverging, and you need to find where they reconnect.

### 14. feat: Hardware watchdog timer integration for unattended autonomous sessions

**Files:** New: `internal/watchdog/watchdog.go`, modify `main.go`

For `--auto-next-steps` mode running unattended on a remote machine, integrate with the Linux hardware watchdog (`/dev/watchdog`). Periodically stroke the watchdog to prevent a hardware reset. If bitchtea hangs -- agent loop deadlock, API infinite loop, goroutine leak -- the watchdog fires and the system reboots. Pair with `--max-runtime` flag for graceful stop after N minutes. On startup, check for a previous session interrupted without a clean shutdown marker in the JSONL and auto-resume it. This is how you run autonomous equipment in a substation you cannot physically reach. The hardware keeps the software alive, and the software keeps the hardware fed.

### 15. feat: Multi-provider concurrent streaming with automatic quality-based routing

**Files:** `internal/llm/client.go`, `internal/llm/anthropic.go`, new: `internal/llm/router.go`

When multiple providers are configured (OpenAI + Anthropic + local fallback), send the same prompt to two providers concurrently. Use the first response that arrives and cancels the other. Track per-provider metrics: latency (p50, p99), error rate, cost per token. Over time, the router learns which provider is faster for which types of requests (short edits vs long refactors) and preferentially routes. Display the router state in the status bar: `[anthropic:12ms | openai:45ms]`. This is active-active failover in networking terms -- both links are up, traffic flows on the faster one, the slower one is a hot standby. It costs 2x the API calls, but on a deadline, the latency savings are worth it.
