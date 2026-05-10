# Flowchart: Tool Registry and Execution

```mermaid
flowchart TD
    %% Entry points
    A1["NewRegistry(workDir, sessionDir)<br/>tools.go:61"]
    A2["Definitions()<br/>tools.go:109"]
    A3["Execute(ctx, name, argsJSON)<br/>tools.go:487"]

    A1 --> A1a["Registry fields:<br/>WorkDir, SessionDir, Scope<br/>terminals: newTerminalManager()<br/>filesRead: make(map)<br/>ToolTimeout: 300s"]

    A2 --> A2a["Returns []ToolDef — 13 tools<br/>read, write, edit<br/>search_memory, write_memory<br/>bash<br/>terminal_start, terminal_send, terminal_keys<br/>terminal_snapshot, terminal_wait, terminal_resize, terminal_close<br/>preview_image"]

    A3 --> A3a["ctx cancelled check<br/>tools.go:491"]
    A3a --> A3b{"tool == \"bash\"?<br/>tools.go:500"}
    A3b -->|yes| A3c["bash manages own timeout<br/>skip WithTimeout"]
    A3b -->|no| A3d["toolCtx, toolCancel :=<br/>context.WithTimeout(r.ToolTimeout)<br/>tools.go:504"]
    A3c --> A3e["switch name dispatch<br/>tools.go:511"]

    %% Dispatch branches
    A3e --> B_read["case \"read\": execRead()"]
    A3e --> B_write["case \"write\": execWrite()"]
    A3e --> B_edit["case \"edit\": execEdit()"]
    A3e --> B_search_mem["case \"search_memory\": execSearchMemory()"]
    A3e --> B_write_mem["case \"write_memory\": execWriteMemory()"]
    A3e --> B_bash["case \"bash\": execBash()"]
    A3e --> B_term_start["case \"terminal_start\": terminals.Start()"]
    A3e --> B_term_send["case \"terminal_send\": terminals.Send()"]
    A3e --> B_term_keys["case \"terminal_keys\": terminals.Keys()"]
    A3e --> B_term_snap["case \"terminal_snapshot\": terminals.Snapshot()"]
    A3e --> B_term_wait["case \"terminal_wait\": terminals.Wait()"]
    A3e --> B_term_res["case \"terminal_resize\": terminals.Resize()"]
    A3e --> B_term_close["case \"terminal_close\": terminals.Close()"]
    A3e --> B_preview["case \"preview_image\": execPreviewImage()"]
    A3e --> B_unknown["default: unknown tool error"]

    %% === READ ===
    B_read --> R1["json.Unmarshal args<br/>tools.go:580"]
    R1 --> R2["resolvePath(args.Path)<br/>tools.go:584"]
    R2 --> R3["os.ReadFile(path)<br/>tools.go:585"]
    R3 --> R4["markFileRead(absPath)<br/>PATTERN 2: guard state<br/>tools.go:591"]
    R4 --> R5{offset or limit<br/>specified?}
    R5 -->|yes| R6["slice lines, apply 1-indexed offset<br/>tools.go:596-609"]
    R5 -->|no| R7["content = string(data)"]
    R6 --> R7
    R7 --> R8{content ><br/>50KB?}
    R8 -->|yes| R9["truncateWithOverflow()<br/>head + sep + tail + temp file<br/>tools.go:615"]
    R8 -->|no| R10["return content<br/>tools.go:626"]
    R9 --> R10

    %% === WRITE ===
    B_write --> W1["json.Unmarshal args<br/>tools.go:634"]
    W1 --> W2["resolvePath(args.Path)<br/>tools.go:638"]
    W2 --> W3{"os.Stat(path)<br/>file exists?"]
    W3 -->|yes| W4{"wasFileRead(path)<br/>tools.go:644"}
    W3 -->|no| W6
    W4 -->|no| W5["ERROR: must read before<br/>overwriting<br/>tools.go:645"]
    W4 -->|yes| W6
    W5 --> W_err["wrapToolError(err)"]
    W6["os.MkdirAll(parent)<br/>tools.go:650"]
    W6 --> W7["os.WriteFile(path, content)<br/>tools.go:654"]
    W7 --> W8["return confirmation"]

    %% === EDIT ===
    B_edit --> ED1["json.Unmarshal args<br/>tools.go:669"]
    ED1 --> ED2["resolvePath(args.Path)<br/>tools.go:673"]
    ED2 --> ED3{"wasFileRead(path)<br/>tools.go:676 — PATTERN 2 guard"]
    ED3 -->|no| ED4["ERROR: must read before edit<br/>tools.go:677"]
    ED3 -->|yes| ED5["os.ReadFile(path)<br/>tools.go:680"]
    ED5 --> ED6["for each edit:<br/>- validate oldText not empty<br/>- validate oldText found & unique<br/>- strings.Replace(content, oldText, newText, 1)<br/>tools.go:688-700"]
    ED6 --> ED7["os.WriteFile(path, content)<br/>tools.go:703"]
    ED7 --> ED8["return confirmation"]
    ED4 --> W_err

    %% === SEARCH_MEMORY ===
    B_search_mem --> SM1["json.Unmarshal args<br/>tools.go:715"]
    SM1 --> SM2["memorypkg.SearchInScope()<br/>tools.go:719"]
    SM2 --> SM3["memorypkg.RenderSearchResults()<br/>tools.go:724"]

    %% === WRITE_MEMORY ===
    B_write_mem --> WM1["json.Unmarshal args<br/>tools.go:735"]
    WM1 --> WM2{"args.Scope<br/>current/root/channel/query?"]
    WM2 --> WM3{"args.Daily?"]
    WM3 -->|yes| WM4["memorypkg.AppendDailyForScope()<br/>tools.go:770"]
    WM3 -->|no| WM5["memorypkg.AppendHot()<br/>tools.go:776"]

    %% === BASH ===
    B_bash --> BA1["json.Unmarshal args<br/>tools.go:787"]
    BA1 --> BA2{"args.Timeout > 0?"]
    BA2 -->|yes| BA3["timeout = args.Timeout"]
    BA2 -->|no| BA4["timeout = 30"]
    BA3 --> BA5["ctx, cancel = context.WithTimeout<br/>(ctx, timeout*time.Second)<br/>tools.go:796 — bash manages own deadline"]
    BA4 --> BA5
    BA5 --> BA6["exec.CommandContext(ctx, \"bash\", \"-c\", cmd)<br/>cmd.Dir = r.WorkDir"]
    BA6 --> BA7["capture stdout+stderr"]
    BA7 --> BA8{output > 50KB?}
    BA8 -->|yes| BA9["truncateWithOverflow()<br/>tools.go:812"]
    BA8 -->|no| BA10{"cmd.Run() error?"]
    BA9 --> BA10
    BA10 -->|yes| BA11["handle: DeadlineExceeded / Canceled / failed to start / exit code"]
    BA10 -->|no| BA12["return output"]

    %% === TERMINAL_START ===
    B_term_start --> TS1["json.Unmarshal args<br/>terminal.go:94"]
    TS1 --> TS2{"command is empty?"]
    TS2 -->|yes| TS_err["error: command required"]
    TS2 -->|no| TS3{"MaxSessions reached?"]
    TS3 -->|yes| TS_err2["error: max PTY sessions<br/>terminal.go:122"]
    TS3 -->|no| TS4["xpty.NewPty(width, height)<br/>terminal.go:126"]
    TS4 --> TS5["exec.CommandContext(sessionCtx, \"bash\", shellFlag, command)<br/>shellFlag = \"-c\" (default)<br/>shellFlag = \"-lc\" if source_dotfiles=true"]
    TS5 --> TS6["pty.Start(cmd)<br/>terminal.go:149"]
    TS6 --> TS7["create terminalSession<br/>id = \"term-{next}\"<br/>emu = vt.NewSafeEmulator()<br/>terminal.go:158-165"]
    TS7 --> TS8["store in m.terms map<br/>terminal.go:167"]
    TS8 --> TS9["start goroutines:<br/>- pty.Read → emu.Write (io.Copy)<br/>- emu.Read → pty.Write (ioCopy)<br/>- session.wait()"]
    TS9 --> TS10["sleepContext(ctx, delay_ms)<br/>terminal.go:212"]
    TS10 --> TS11["return session.snapshot(false)<br/>terminal.go:213"]

    %% === TERMINAL_SEND ===
    B_term_send --> TSN1["json.Unmarshal args<br/>terminal.go:227"]
    TSN1 --> TSN2["m.get(id) → session lookup<br/>terminal.go:233"]
    TSN2 --> TSN3{"session.running()?"]
    TSN3 -->|no| TSN4["return snapshot (session dead)<br/>terminal.go:238"]
    TSN3 -->|yes| TSN5["session.emu.SendText(text)<br/>terminal.go:241"]
    TSN5 --> TSN6["sleepContext(ctx, delay_ms)<br/>terminal.go:242"]
    TSN6 --> TSN7["return session.snapshot(false)<br/>terminal.go:243"]

    %% === TERMINAL_KEYS ===
    B_term_keys --> TK1["json.Unmarshal args<br/>terminal.go:252"]
    TK1 --> TK2{"keys empty?"]
    TK2 -->|yes| TK_err["error: keys required"]
    TK2 -->|no| TK3["for each key in args.Keys:<br/>terminalKeyInput(key)<br/>validate ALL before sending<br/>terminal.go:276-281"]
    TK3 --> TK4{"any key unknown?"]
    TK4 -->|yes| TK_err2["error: unknown key<br/>terminal.go:279"]
    TK4 -->|no| TK5["build input string from sequences<br/>session.emu.SendText(input)"]
    TK5 --> TK6["sleepContext(ctx, delay_ms)<br/>terminal.go:284"]
    TK6 --> TK7["return session.snapshot(false)<br/>terminal.go:285"]

    %% === TERMINAL_SNAPSHOT ===
    B_term_snap --> TSSP1["json.Unmarshal args<br/>terminal.go:293"]
    TSSP1 --> TSSP2["ctx.Err() check<br/>terminal.go:296"]
    TSSP2 --> TSSP3["m.get(id) → session"]
    TSSP3 --> TSSP4["return session.snapshot(ansi)<br/>terminal.go:303"]

    %% === TERMINAL_WAIT ===
    B_term_wait --> TW1["json.Unmarshal args<br/>terminal.go:314"]
    TW1 --> TW2{"text empty?"]
    TW2 -->|yes| TW_err["error: text required"]
    TW2 -->|no| TW3["m.get(id) → session"]
    TW3 --> TW4["deadline = now + timeout_ms<br/>interval = interval_ms"]
    TW4 --> TW5{"LOOP: check ctx.Err() first<br/>terminal.go:339"]
    TW5 --> TW6["snapshot = session.snapshot()<br/>containsTerminalText()?"]
    TW6 -->|yes| TW7["return matched + snapshot"]
    TW6 -->|no| TW8{"session.running() == false?"]
    TW8 -->|yes| TW9["return exited + snapshot"]
    TW8 -->|no| TW10{"deadline passed?"]
    TW10 -->|yes| TW11["return timeout + snapshot"]
    TW10 -->|no| TW12["select:<br/>- ctx.Done() → cancelled + snapshot<br/>- time.After(interval) → next iteration"]
    TW12 --> TW5

    %% === TERMINAL_RESIZE ===
    B_term_res --> TR1["json.Unmarshal args<br/>terminal.go:370"]
    TR1 --> TR2{"width/height <= 0?"]
    TR2 -->|yes| TR_err["error: dimensions must be positive"]
    TR2 -->|no| TR3["m.get(id) → session"]
    TR3 --> TR4["session.pty.Resize(width, height)<br/>terminal.go:387"]
    TR4 --> TR5["session.emu.Resize(width, height)<br/>terminal.go:390"]
    TR5 --> TR6["sleepContext(ctx, delay_ms)<br/>terminal.go:391"]
    TR6 --> TR7["return session.snapshot(false)<br/>terminal.go:392"]

    %% === TERMINAL_CLOSE ===
    B_term_close --> TC1["json.Unmarshal args<br/>terminal.go:399"]
    TC1 --> TC2["m.get(id) + delete from m.terms<br/>terminal.go:403-408"]
    TC2 --> TC3["session.close(graceTimeout)<br/>SIGTERM → grace period → SIGKILL<br/>terminal.go:461-518"]
    TC3 --> TC4["return confirmation string"]

    %% === PREVIEW_IMAGE ===
    B_preview --> PI1["json.Unmarshal args<br/>image.go:22"]
    PI1 --> PI2["resolvePath(args.Path)<br/>image.go:39"]
    PI2 --> PI3["os.Open(path)<br/>image.go:40"]
    PI3 --> PI4["image.Decode(f)<br/>image.go:46"]
    PI4 --> PI5["mosaic.New().Width(args.Width)<br/>image.go:52-54"]
    PI5 --> PI6["renderer.Render(img)<br/>image.go:59"]
    PI6 --> PI7["return ANSI art preview"]

    %% === ERROR WRAPPING ===
    W_err --> EW1["wrapToolError(err)<br/>tools.go:31"]
    TS_err --> EW1
    TS_err2 --> EW1
    TK_err --> EW1
    TK_err2 --> EW1
    TW_err --> EW1
    TR_err --> EW1
    ED4 --> EW1

    EW1 --> EW2["<tool_call_error><br/><cause>err.Error()</cause><br/><reflection>reflectionPrompt</reflection><br/></tool_call_error>"]
    EW2 --> FIN["return result, nil<br/>tools.go:562"]

    %% Successful paths
    R10 --> FIN
    W8 --> FIN
    ED8 --> FIN
    SM3 --> FIN
    WM4 --> FIN
    WM5 --> FIN
    BA12 --> FIN
    TS11 --> FIN
    TSN7 --> FIN
    TK7 --> FIN
    TSSP4 --> FIN
    TW7 --> FIN
    TW9 --> FIN
    TW11 --> FIN
    TR7 --> FIN
    TC4 --> FIN
    PI7 --> FIN

    %% ToolTimeout check
    A3d --> TT1["check: toolCtx.Err() after tool returns<br/>tools.go:545-548"]
    TT1 --> FIN2["if timeout: err = \"tool exceeded Ns timeout\""]
    FIN2 --> FIN

    %% Legend
    subgraph legend["PATTERNS"]
        L1["Pattern 1: wrapToolError + reflectionPrompt"]
        L2["Pattern 2: read-before-edit guard (filesRead map)"]
        L3["Pattern 3: head+tail truncation with overflow temp file"]
        L4["Pattern 4: per-tool timeout via context.WithTimeout"]
    end

    %% External deps
    Z1["External Dependencies:<br/>• github.com/charmbracelet/x/xpty — PTY (terminal.go)<br/>• github.com/charmbracelet/x/vt — VT emulator (terminal.go)<br/>• github.com/charmbracelet/x/mosaic — image renderer (image.go)<br/>• github.com/jstamagal/bitchtea/internal/memory — memory search/write"]
```

## Summary

**Entry points:** `NewRegistry` (tools.go:61), `Definitions` (tools.go:109), `Execute` (tools.go:487)

**Definitions()** returns 13 OpenAI-compatible tool definitions: `read`, `write`, `edit`, `search_memory`, `write_memory`, `bash`, `terminal_start`, `terminal_send`, `terminal_keys`, `terminal_snapshot`, `terminal_wait`, `terminal_resize`, `terminal_close`, `preview_image`.

**Execute()** runs a per-tool timeout pattern (default 300s, bash bypasses it), validates context cancellation before dispatch, then routes to the appropriate executor via a switch statement. All tool errors are wrapped via `wrapToolError()` into a `<tool_call_error>` XML envelope with reflection prompt.

**Read-before-edit guard (Pattern 2):** `execRead` calls `markFileRead(absPath)`. Both `execWrite` (for existing files) and `execEdit` consult `wasFileRead(path)` before proceeding.

**Terminal PTY tools:** Managed by `terminalManager`. `terminal_start` creates xpty.Pty, runs `bash -c`, starts io.Copy goroutines (pty↔emu), stores in `m.terms` map. `terminal_close` applies SIGTERM → grace → SIGKILL.

**Side effects:** `filesRead` map mutations, PTY goroutines, temp file writes via `truncateWithOverflow`, session map mutations.

**External dependencies:** `xpty` (Charm), `vt` (Charm), `mosaic` (Charm), `internal/memory`
