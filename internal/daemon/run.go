package daemon

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Dispatcher is the function the run loop calls for every dequeued job.
// internal/daemon/jobs.Handle satisfies this signature; the indirection
// keeps the daemon scaffold from importing the jobs package directly,
// avoiding a forward dependency in the (small) chance another consumer
// wants to wire in a different registry.
type Dispatcher func(ctx context.Context, job Job) Result

// RunOptions controls a single daemon process. All fields are optional; the
// zero value picks sane defaults but does not redirect logs anywhere helpful.
type RunOptions struct {
	BaseDir     string        // typically config.BaseDir(); required
	PollEvery   time.Duration // mailbox poll cadence; defaults to 5s
	DrainBudget time.Duration // grace period for in-flight work on shutdown; defaults to 30s
	Logger      *log.Logger   // log destination; defaults to stderr
	// Dispatch is invoked for every dequeued job. When nil the loop falls
	// back to the scaffold "no handler registered" failure — preserved so
	// older tests and callers keep their semantics. Real callers (cmd/daemon)
	// pass internal/daemon/jobs.Handle here.
	Dispatch Dispatcher
}

// Run executes the daemon main loop synchronously: acquires the lock, sets
// up signal handling, polls the mailbox, and returns when SIGTERM/SIGINT
// arrives or ctx is canceled. It returns ErrLocked if another daemon is
// already running.
//
// When Dispatch is nil (legacy callers, or scaffold tests), every job is
// failed with "no handler registered". Real callers pass jobs.Handle.
func Run(ctx context.Context, opts RunOptions) error {
	if opts.BaseDir == "" {
		return fmt.Errorf("daemon: RunOptions.BaseDir is required")
	}
	if opts.PollEvery <= 0 {
		opts.PollEvery = 5 * time.Second
	}
	if opts.DrainBudget <= 0 {
		opts.DrainBudget = 30 * time.Second
	}
	logger := opts.Logger
	if logger == nil {
		logger = log.New(os.Stderr, "daemon: ", log.LstdFlags)
	}

	paths := Layout(opts.BaseDir)

	lock, err := Acquire(paths.LockPath)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	mailbox := New(opts.BaseDir)
	if err := mailbox.Init(); err != nil {
		return fmt.Errorf("daemon: init mailbox: %w", err)
	}

	if err := WritePid(paths.PidPath, os.Getpid()); err != nil {
		return fmt.Errorf("daemon: write pidfile: %w", err)
	}
	defer func() { _ = RemovePid(paths.PidPath) }()

	// Crash recovery scan: per the design doc, mail entries older than the
	// daemon's start time are presumed to have crashed the previous instance
	// and get moved to failed/ with a diagnostic. Newer ones are processed
	// normally. We use file mtime, not envelope SubmittedAt, because mtime
	// survives clock skew and is harder to spoof.
	startTime := time.Now()
	if err := recoverCrashedJobs(mailbox, startTime, logger); err != nil {
		logger.Printf("crash recovery scan failed: %v", err)
		// non-fatal: keep going
	}

	// Signal handling: SIGTERM and SIGINT are identical (graceful drain).
	// We translate them into ctx cancellation so the loop terminates cleanly.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	loopCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		select {
		case sig := <-sigCh:
			logger.Printf("received %s, beginning %s graceful drain", sig, opts.DrainBudget)
			cancel()
		case <-ctx.Done():
		}
	}()

	logger.Printf("started (pid %d, poll %s, base %s)", os.Getpid(), opts.PollEvery, opts.BaseDir)

	ticker := time.NewTicker(opts.PollEvery)
	defer ticker.Stop()

	// Process anything sitting in mail/ at startup (post-recovery), then enter
	// the poll loop. The combined fsnotify+5s-poll setup from the design is
	// future work — the current variant uses poll-only. Real-time notification
	// is a wakeup-cost optimization, not a correctness one.
	processOnce(loopCtx, mailbox, logger, opts.Dispatch)

	for {
		select {
		case <-loopCtx.Done():
			drainCtx, drainCancel := context.WithTimeout(context.Background(), opts.DrainBudget)
			defer drainCancel()
			drainShutdown(drainCtx, mailbox, logger)
			logger.Printf("stopped")
			return nil
		case <-ticker.C:
			processOnce(loopCtx, mailbox, logger, opts.Dispatch)
		}
	}
}

// processOnce reads mail/ and dispatches each job through the handler
// registry. On success the result is written to done/<id>.json; on
// handler error or unknown kind, the job moves to failed/<id>.json with
// the diagnostic in Result.Error.
//
// When opts.Dispatch is nil (legacy callers, or the scaffold tests),
// every job is failed with "no handler registered" — preserved so older
// callers keep their semantics. When Dispatch is non-nil but returns
// an error Result with a "no handler" message, the job is also moved to
// failed/ so the on-disk distinction between "ran and failed" and "never
// had a handler" survives.
func processOnce(ctx context.Context, mailbox *Mailbox, logger *log.Logger, dispatch Dispatcher) {
	jobs, parseErrs, err := mailbox.List()
	if err != nil {
		logger.Printf("list mailbox: %v", err)
		return
	}
	for _, perr := range parseErrs {
		logger.Printf("malformed envelope: %v", perr)
		// We cannot extract the ULID from a malformed envelope reliably, but
		// the filename is in the wrapped error. Leave it in mail/ — the
		// operator should remove it; auto-deletion would silently lose data.
	}
	for _, j := range jobs {
		if dispatch == nil {
			reason := fmt.Sprintf("no handler registered for kind=%q", j.Kind)
			logger.Printf("rejecting job %s: %s", j.ID, reason)
			if err := mailbox.Fail(j.ID, reason); err != nil {
				logger.Printf("move job %s to failed/: %v", j.ID, err)
			}
			continue
		}
		result := dispatch(ctx, j)
		if result.Success {
			logger.Printf("job %s (kind=%s) succeeded", j.ID, j.Kind)
			if err := mailbox.Complete(j.ID, result); err != nil {
				logger.Printf("move job %s to done/: %v", j.ID, err)
			}
			continue
		}
		// Failure path: the dispatcher already filled in Result.Error.
		// Forward-compat: if the handler reports "no handler registered"
		// we keep the same diagnostic in failed/ so callers grepping the
		// scaffold sentinel keep working.
		reason := result.Error
		if reason == "" {
			reason = "handler returned success=false with no error message"
		}
		if strings.Contains(reason, "no handler registered") {
			logger.Printf("rejecting job %s: %s", j.ID, reason)
		} else {
			logger.Printf("job %s (kind=%s) failed: %s", j.ID, j.Kind, reason)
		}
		if err := mailbox.Fail(j.ID, reason); err != nil {
			logger.Printf("move job %s to failed/: %v", j.ID, err)
		}
	}
}

// drainShutdown waits for in-flight handlers to finish within the DrainBudget.
// With no long-running handlers currently registered, drain is a no-op. When
// in-flight handler tracking is added, this is where it waits up to DrainBudget
// for them to finish, then moves any stragglers to failed/ with reason
// "shutdown deadline".
func drainShutdown(_ context.Context, _ *Mailbox, logger *log.Logger) {
	logger.Printf("drain complete (no in-flight handlers in scaffold build)")
}

func recoverCrashedJobs(mailbox *Mailbox, startTime time.Time, logger *log.Logger) error {
	entries, err := os.ReadDir(mailbox.Paths().MailDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(startTime) {
			// Newer than start: leave for the run loop.
			continue
		}
		id := trimJSON(e.Name())
		logger.Printf("recovering pre-start mail entry %s", id)
		if err := mailbox.Fail(id, "previous daemon crashed mid-job"); err != nil {
			logger.Printf("recover %s: %v", id, err)
		}
	}
	return nil
}

func trimJSON(name string) string {
	if len(name) > 5 && name[len(name)-5:] == ".json" {
		return name[:len(name)-5]
	}
	return name
}

// OpenLog opens the daemon log file for append. Callers (cmd/daemon) wire
// stdout and stderr to this writer so all process output ends up in
// daemon.log per the design.
func OpenLog(path string) (io.WriteCloser, error) {
	if err := os.MkdirAll(parentDir(path), 0o700); err != nil {
		return nil, fmt.Errorf("daemon: log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("daemon: open log: %w", err)
	}
	return f, nil
}
