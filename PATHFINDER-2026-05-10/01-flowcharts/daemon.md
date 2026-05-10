# Flowchart: Background Daemon and Jobs

```mermaid
flowchart TD
    %% Entry point: cmd/daemon/main.go
    A["cmd/daemon/main.go:42<br/>daemon.Run(ctx, RunOptions{<br/>Dispatch: jobs.Handle})"] --> B

    %% internal/daemon/run.go:43 - Run()
    B["internal/daemon/run.go:43<br/>Run(ctx, opts RunOptions)"]

    B --> C["run.go:44-56<br/>Validate options & set defaults<br/>BaseDir required, PollEvery 5s,<br/>DrainBudget 30s, Logger stderr"]
    C --> D["run.go:58<br/>paths := Layout(baseDir)<br/>Resolve mail/done/failed/lock/pid paths"]
    D --> E["run.go:60-63<br/>lock, err := Acquire(paths.LockPath)<br/>flock EX | LOCK_NB<br/>Returns ErrLocked if already held<br/><b>Side effect:</b> creates lock file dir & file 0600"]
    E --> F{"err == ErrLocked?"}
    F -->|yes| G["return ErrLocked<br/>CLI exits 0 with message"]
    F -->|no| H["defer lock.Release()"]
    H --> I["run.go:66-69<br/>mailbox := New(baseDir)<br/>mailbox.Init()<br/><b>Side effect:</b> mkdir mail/, done/, failed/ 0700"]
    I --> J["run.go:71-73<br/>WritePid(paths.PidPath, pid)<br/><b>Side effect:</b> creates pidfile"]
    J --> K["run.go:81-85<br/>recoverCrashedJobs(mailbox, startTime)<br/><b>Side effect:</b> moves pre-start<br/>mail/ entries to failed/<br/>(\"previous daemon crashed mid-job\")"]
    K --> L["run.go:89-91<br/>signal.Notify(sigCh, SIGTERM, SIGINT)"]
    L --> M["run.go:93-94<br/>loopCtx, cancel := context.WithCancel(ctx)"]
    M --> N["run.go:107-108<br/>ticker := time.NewTicker(PollEvery)<br/>PollEvery defaults to 5s"]
    N --> O["run.go:114<br/>processOnce(loopCtx, mailbox,<br/>logger, opts.Dispatch)<br/><i>Initial pass before entering loop</i>"]
    O --> P

    %% Main run loop
    P["run.go:116-127<br/>Main Loop<br/>select {<br/>  case <-loopCtx.Done:<br/>    drainShutdown()<br/>    return nil<br/>  case <-ticker.C:<br/>    processOnce()<br/>}"]

    P -.-&gt;|"SIGTERM/SIGINT"| Q["run.go:97-102<br/>goroutine: sig := <-sigCh<br/>cancel() signals loopCtx.Done"]
    Q -.-&gt; P

    %% processOnce
    subgraph processOnce["internal/daemon/run.go:141 - processOnce()"]
        R["run.go:142<br/>jobs, parseErrs, err := mailbox.List()"]
        R --> S{"parseErrs any?"}
        S -->|yes| T["run.go:147-152<br/>Log malformed envelopes<br/>Leave in mail/ for operator"]
        S --> U
        T --> U["run.go:153<br/>for _, j := range jobs"]
        U --> V{"dispatch == nil?"}
        V -->|yes| W["run.go:154-159<br/>mailbox.Fail(j.ID,<br/>\"no handler registered\")<br/>move to failed/"]
        V -->|no| X["run.go:162<br/>result := dispatch(ctx, j)"]
        W --> Y
        X --> Z{"result.Success?"}
        Z -->|yes| AA["run.go:165<br/>mailbox.Complete(j.ID, result)<br/>write done/<id>.json<br/>remove mail/<id>.json"]
        Z -->|no| AB["run.go:183<br/>mailbox.Fail(j.ID, result.Error)<br/>write failed/<id>.json<br/>remove mail/<id>.json"]
        AA --> Y
        AB --> Y["U: next job in list"]
        U --> Y
    end

    %% Mailbox operations
    subgraph mailbox["internal/daemon/mailbox.go - Mailbox"]
        AC["mailbox.go:76<br/>List() <-- reads mail/ directory<br/>Sorted by ULID (time order)<br/>Returns []Job, []error, error"]
        AD["mailbox.go:121<br/>Complete(id, Result)<br/>mkdir done/<br/>atomicWrite(done/<id>.json, data)<br/>remove mail/<id>.json"]
        AE["mailbox.go:138<br/>Fail(id, reason)<br/>mkdir failed/<br/>atomicWrite(failed/<id>.json,<br/>Result{Success:false})<br/>remove mail/<id>.json"]
        AF["mailbox.go:167<br/>atomicWrite(target, data)<br/>write target.tmp<br/>fsync<br/>rename tmp --> target<br/><b>Side effect:</b> crash-safe write"]
    end

    %% Lock/pidfile
    subgraph lock["internal/daemon/lock.go - Lock/Pid"]
        AG["lock.go:31<br/>Acquire(path) --> *Lock<br/>os.MkdirAll(parent, 0700)<br/>os.OpenFile(path, O_RDWR|O_CREATE, 0600)<br/>unix.Flock(fd, LOCK_EX|LOCK_NB)<br/>Returns ErrLocked on EWOULDBLOCK"]
        AH["lock.go:50<br/>Lock.Release()<br/>unix.Flock(fd, LOCK_UN)<br/>f.Close()"]
        AI["lock.go:61<br/>IsLocked(path) --> bool<br/>probe flock for status command"]
    end

    %% Jobs dispatch
    subgraph dispatch["internal/daemon/jobs/jobs.go - Handle()"]
        AJ["jobs.go:52<br/>Handle(ctx, job daemon.Job)<br/>daemon.Result"]
        AK["jobs.go:54<br/>h, ok := registry[job.Kind]"]
        AK --> AL{"ok?"}
        AL -->|no| AM["jobs.go:55-62<br/>return Result{Success:false,<br/>Error: \"no handler registered\"}"]
        AL -->|yes| AN["jobs.go:64<br/>res := h(ctx, job)<br/>Call handler function"]
    end

    %% Registry
    AO["jobs.go:79-84<br/>registry map[string]Handler<br/>session-checkpoint:<br/>  handleSessionCheckpoint<br/>memory-consolidate:<br/>  handleMemoryConsolidate<br/>stale-cleanup: handleStaleCleanup<br/>session-stitch: handleSessionStitch"]

    %% handleSessionCheckpoint
    subgraph checkpoint["internal/daemon/jobs/checkpoint.go:49 - handleSessionCheckpoint()"]
        AP["checkpoint.go:53<br/>ctx, cancel := context.WithTimeout(ctx, 30s)<br/>defer cancel()"]
        AP --> AQ
        AQ["checkpoint.go:60-65<br/>Parse checkpointArgs from job.Args<br/>Fall back to job.SessionPath"]
        AQ --> AR{"args.SessionPath == \"\"?"}
        AR -->|yes| AS["checkpoint.go:73<br/>return errorResult:<br/>\"session_path is required\""]
        AR -->|no| AT["checkpoint.go:80<br/>sess, err := session.Load(args.SessionPath)<br/><b>Side effect:</b> reads session JSONL"]
        AT --> AU
        AU["checkpoint.go:89-103<br/>Count turns (non-bootstrap user entries)<br/>Count tool calls per function name"]
        AU --> AV["checkpoint.go:109-116<br/>cp := session.Checkpoint{...}<br/>session.SaveCheckpoint(dir, cp)<br/><b>Side effect:</b> writes<br/><dir>/.bitchtea_checkpoint.json"]
        AV --> AW["checkpoint.go:123-128<br/>return successResult with<br/>checkpointOutput{Path, TurnCount,<br/>ToolCallCount}"]
    end

    %% handleMemoryConsolidate
    subgraph memory["internal/daemon/jobs/memory_consolidate.go:75 - handleMemoryConsolidate()"]
        AX["memory_consolidate.go:79<br/>ctx, cancel := context.WithTimeout(ctx, 60s)<br/>defer cancel()"]
        AX --> AY
        AY["memory_consolidate.go:86-109<br/>Parse args, fall back to job.WorkDir,<br/>job.Scope.Kind/Name"]
        AY --> AZ{"work_dir == \"\"?"}
        AZ -->|yes| BA["memory_consolidate.go:105<br/>return errorResult:<br/>\"work_dir is required\""]
        AZ -->|no| BB["memory_consolidate.go:111<br/>buildScope(kind, name,<br/>parentKind, parentName)<br/>Returns memory.Scope"]
        BB --> BC["memory_consolidate.go:116-118<br/>parseSince(args.Since)"]
        BC --> BD["memory_consolidate.go:124<br/>dailyDir := filepath.Dir(<br/>memory.DailyPathForScope(...))"]
        BD --> BE["memory_consolidate.go:127<br/>listDailyFiles(dailyDir, since)<br/>Scan dir, filter by date, sort ascending"]
        BE --> BF
        BF["memory_consolidate.go:136<br/>loadConsolidatedMarkers(hotPath)<br/>Scan existing HTML comment markers"]
        BF --> BG["memory_consolidate.go:143-163<br/>For each daily file:<br/>  parseDailyEntries()<br/>  For each entry:<br/>    If marker not in existing --><br/>      appendConsolidatedBlock()<br/>    Else --> skipped++"]
        BG --> BH["memory_consolidate.go:166-172<br/>return successResult with<br/>HotPath, DailiesSeen,<br/>EntriesAdded, EntriesSkipped"]
    end

    %% Connections
    X --> dispatch
    dispatch --> AO
    AD -.-&gt;|uses| AF
    AE -.-&gt;|uses| AF
    AC -.-&gt;|uses| AF

    style G fill:#ffcccc
    style AS fill:#ffcccc
    style BA fill:#ffcccc
    style AM fill:#ffcccc
```

## Summary

**Daemon startup**: `daemon.Run()` → validate → `Layout()` → `Acquire()` (flock) → mailbox init (mail/done/failed dirs) → pidfile → crash recovery → signal handlers → initial `processOnce()`.

**Poll loop**: 5s ticker or `loopCtx.Done()` → `processOnce()` → `mailbox.List()` → for each job, `dispatch(ctx, j)` → `Complete` or `Fail`.

**Mailbox IPC**: file-based via mail/done/failed/ triad. Atomic write (tmp+fsync+rename). Jobs sorted by ULID.

**Lock/pidfile**: non-blocking exclusive flock (kernel-released on close/SIGKILL). Separate informational pidfile.

**Jobs**: `session-checkpoint` (30s timeout, reads session JSONL, writes checkpoint), `memory-consolidate` (60s timeout, deduplicates daily files to HOT.md), `stale-cleanup`, `session-stitch`.

**External dependencies:** golang.org/x/sys/unix (Flock), internal/session, internal/memory, filesystem (os MkdirAll/Rename)
