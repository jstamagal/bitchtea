# Flowchart: Session Persistence and Management

```mermaid
flowchart TD
    %% Entry Points
    EP1["**Entry: New()**<br>internal/session/session.go:82"]
    EP2["**Entry: Load()**<br>internal/session/session.go:98"]

    %% Session Creation Path
    subgraph CREATE["Session Creation + JSONL Append"]
        direction TB
        C1["os.MkdirAll(dir, 0755)<br>session.go:84"]
        C2["time.Now().Format timestamp<br>session.go:87"]
        C3["filepath.Join(dir, name + \".jsonl\")<br>session.go:88"]
        C4["**return &Session{Path, Entries: []}**<br>session.go:91-95<br>⚠️ File not yet written — first Append does it"]
    end

    %% Append Path
    subgraph APPEND["Append (Primary Write Path)"]
        direction TB
        A1["s.mu.Lock() — acquire in-process mutex<br>session.go:127"]
        A2["entry.Timestamp = time.Now()<br>session.go:129"]
        A3["entry.ID auto-generated if empty<br>session.go:131-132"]
        A4["entry.ParentID = last entry.ID if unset<br>session.go:134-136"]
        A5["s.Entries = append(s.Entries, entry)<br>session.go:138"]
        A6["json.Marshal(entry)<br>session.go:141"]
        A7["os.OpenFile(path, O_APPEND|O_CREATE|O_WRONLY, 0644)<br>session.go:144"]
        A8["syscall.Flock(LOCK_EX) — inter-process file lock<br>session.go:147-149"]
        A9["f.Write(data + '\n')<br>session.go:154"]
        A10["syscall.Flock(LOCK_UN) — release lock<br>session.go:150"]
        A11["**error? → caller**<br>session.go:155"]
    end

    %% Load / Resume Path
    subgraph LOAD["Load / Resume Path"]
        direction TB
        L1["os.ReadFile(path)<br>session.go:100"]
        L2["splitLines(data) — split on '\n'<br>session.go:122"]
        L3["json.Unmarshal(line) per entry<br>session.go:107"]
        L4["skip malformed lines silently<br>session.go:108"]
        L5["**return &Session{Path, Entries}**<br>session.go:110-114"]
    end

    %% Entry Format Path
    subgraph ENTRY["Fantasy-Native Entry vs Legacy v0 Fallback"]
        direction TB
        E1["**EntryFromFantasy(msg)**<br>session.go:339 → calls EntryFromFantasyWithBootstrap(msg, false)"]
        E2["projectFantasyToLegacy(msg) → (llm.Message, lossy)<br>session.go:361-366"]
        E3["**FantasyFromEntries(entries)**<br>session.go:373 — reconstruct fantasy.Message slice"]
        E4["**Reader precedence check:**<br>if e.V >= 1 && e.Msg != nil → use e.Msg directly<br>else → legacyEntryToFantasy(e)<br>session.go:376-384"]
        E5["legacyEntryToFantasy(e)<br>session.go:469-498 — synthesize fantasy.Message from v0 fields<br>role-based switch: user/assistant/tool/system"]
        E6["**V1 entry dual-write shape:**<br>V=1, Msg=fantasy.Message, LegacyLossy=bool<br>Legacy fields synthesized for downgrade compatibility<br>session.go:367-371"]
    end

    %% Fork + Tree Operations
    subgraph FORK["Fork + Tree Operations"]
        direction TB
        F1["**Fork(fromID)**<br>session.go:148"]
        F2["newPath = dir/base + \"_fork_\" + timestamp + \".jsonl\"<br>session.go:151"]
        F3["iterate s.Entries, copy until e.ID == fromID inclusive<br>session.go:162-166"]
        F4["os.OpenFile(newPath, O_CREATE|O_WRONLY|O_TRUNC, 0644)<br>session.go:169"]
        F5["write all entries atomically (single open)<br>session.go:174-177"]
        F6["f.Close() → return newSession<br>session.go:179-185"]
        F7["**Tree()** — text dump: prefix ├/└, timestamp, role, truncated content<br>session.go:207-243"]
    end

    %% Membership (separate file)
    subgraph MEMBER["Membership Persistence"]
        direction TB
        M1["**SaveMembership(dir, state)**<br>membership.go:15"]
        M2["os.MkdirAll(dir, 0755)<br>membership.go:17"]
        M3["json.MarshalIndent(state, \"\", \"  \")<br>membership.go:24"]
        M4["os.WriteFile(.bitchtea_membership.json, data, 0644)<br>membership.go:27-28"]
        M5["**LoadMembership(dir)**<br>membership.go:34"]
        M6["os.ReadFile(.bitchtea_membership.json)<br>membership.go:36"]
        M7["if os.IsNotExist → return zero-value MembershipState{}<br>membership.go:37-38"]
    end

    %% Focus + Checkpoint
    subgraph META["Focus + Checkpoint State"]
        direction TB
        FC1["**SaveFocus(dir, state)**<br>session.go:500"]
        FC2["**LoadFocus(dir)**<br>session.go:512"]
        FC3["**SaveCheckpoint(dir, checkpoint)**<br>session.go:299"]
        FC4["writes .bitchtea_focus.json<br>writes .bitchtea_checkpoint.json"]
    end

    %% Edges
    EP1 --> C1
    C1 --> C2
    C2 --> C3
    C3 --> C4

    EP1 --> A1
    A1 --> A2
    A2 --> A3
    A3 --> A4
    A4 --> A5
    A5 --> A6
    A6 --> A7
    A7 --> A8
    A8 --> A9
    A9 --> A10
    A10 --> A11

    EP2 --> L1
    L1 --> L2
    L2 --> L3
    L3 --> L4
    L4 --> L5

    E1 --> E2
    E2 --> E3
    E3 --> E4
    E4 --> E5

    F1 --> F2
    F2 --> F3
    F3 --> F4
    F4 --> F5
    F5 --> F6
    F6 --> F7

    M1 --> M2
    M2 --> M3
    M3 --> M4

    M5 --> M6
    M6 --> M7

    %% External Dependencies
    EXT1["charm.land/fantasy — fantasy.Message, TextPart, ReasoningPart, FilePart, ToolCallPart, ToolResultPart, ToolResultOutputContent*"]
    EXT2["github.com/jstamagal/bitchtea/internal/llm — llm.Message, llm.ToolCall, llm.FunctionCall"]
    EXT3["os, filepath, sync, syscall, time, encoding/json, strings — stdlib"]
```

## Summary

**Session creation** (`New()`): mkdir, build path, return Session struct. File NOT written at construction — first `Append()` creates it.

**Append / JSONL write**: mutex lock → auto-ID → auto-ParentID linking → append in-memory → JSON marshal → `os.OpenFile` with `O_APPEND|O_CREATE` → `syscall.Flock(LOCK_EX)` → write `data+'\n'` → unlock → close.

**Load / resume**: read file → split on `\n` → `json.Unmarshal` per line → skip malformed → return fully populated Session.

**Fantasy-native vs legacy v0 fallback**: V1 entries use `Msg` (fantasy.Message) directly; v0 falls back through `legacyEntryToFantasy()` which synthesizes from role-based fields. `LegacyLossy=true` when the fantasy message has reasoning, media, multi-part text, or error tool results that can't round-trip.

**Fork**: builds new `_fork_<timestamp>.jsonl`, copies entries up to `fromID`, atomic single-open write.

**Membership**: separate `.bitchtea_membership.json` file for channel→persona map.

**External dependencies:** fantasy, internal/llm, stdlib (os/filepath/sync/syscall/time/encoding/json/strings)
