package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Mailbox is the file-mailbox front-end: a thin wrapper around the
// mail/done/failed directory triad. All writes are tmp+rename; all moves are
// rename across same-filesystem directories. See docs/phase-7-process-model.md
// §"IPC contract — file mailbox v1".
type Mailbox struct {
	paths Paths
}

// New returns a Mailbox rooted at baseDir (typically config.BaseDir()).
// The directories are NOT created here — call Init when the daemon owns them.
// The TUI submitter side may call EnsureMailDir to create just mail/.
func New(baseDir string) *Mailbox {
	return &Mailbox{paths: Layout(baseDir)}
}

// Paths exposes the resolved layout for callers that need direct paths.
func (m *Mailbox) Paths() Paths { return m.paths }

// Init creates mail/, done/, failed/ with mode 0700. Daemon-only.
func (m *Mailbox) Init() error {
	for _, d := range []string{m.paths.MailDir, m.paths.DoneDir, m.paths.FailedDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("daemon: mkdir %s: %w", d, err)
		}
	}
	return nil
}

// EnsureMailDir creates only mail/. Used by Submit so a TUI can drop a job
// even if no daemon has ever run on this machine.
func (m *Mailbox) EnsureMailDir() error {
	if err := os.MkdirAll(m.paths.MailDir, 0o700); err != nil {
		return fmt.Errorf("daemon: mkdir mail: %w", err)
	}
	return nil
}

// Submit writes job to mail/<ulid>.json atomically. The returned ID is the
// ULID embedded in the filename — callers (TUI) record it so they can later
// match a Result in done/.
func (m *Mailbox) Submit(job Job) (string, error) {
	if err := m.EnsureMailDir(); err != nil {
		return "", err
	}
	id := job.ID
	if id == "" {
		id = NewULID()
		job.ID = id
	}
	data, err := MarshalJob(job)
	if err != nil {
		return "", err
	}
	target := filepath.Join(m.paths.MailDir, id+".json")
	if err := atomicWrite(target, data); err != nil {
		return "", err
	}
	return id, nil
}

// List returns all pending jobs in mail/, sorted by ULID (which sorts by
// time). Malformed envelopes are skipped with their parse error included
// in the returned error slice — they should be moved to failed/ by the
// caller (the daemon main loop is the natural owner).
func (m *Mailbox) List() ([]Job, []error, error) {
	entries, err := os.ReadDir(m.paths.MailDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("daemon: list mail: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !strings.HasSuffix(n, ".json") || strings.HasSuffix(n, ".tmp") {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)

	var jobs []Job
	var parseErrs []error
	for _, n := range names {
		path := filepath.Join(m.paths.MailDir, n)
		data, err := os.ReadFile(path)
		if err != nil {
			parseErrs = append(parseErrs, fmt.Errorf("read %s: %w", n, err))
			continue
		}
		j, err := UnmarshalJob(data)
		if err != nil {
			parseErrs = append(parseErrs, fmt.Errorf("parse %s: %w", n, err))
			continue
		}
		j.ID = strings.TrimSuffix(n, ".json")
		jobs = append(jobs, j)
	}
	return jobs, parseErrs, nil
}

// Complete writes a Result to done/<id>.json and removes mail/<id>.json.
// Same-filesystem rename gives atomicity for the result write; the mail
// removal is a separate syscall (nothing else is safe to do here, since
// rename across the dirs would clobber any existing same-name file).
func (m *Mailbox) Complete(id string, r Result) error {
	if err := os.MkdirAll(m.paths.DoneDir, 0o700); err != nil {
		return fmt.Errorf("daemon: mkdir done: %w", err)
	}
	data, err := MarshalResult(r)
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(m.paths.DoneDir, id+".json"), data); err != nil {
		return err
	}
	return removeMailFile(m.paths.MailDir, id)
}

// Fail moves a job to failed/<id>.json with the supplied reason. It records
// the failure as a Result with success=false so the TUI side can render it
// using the same code path as a completed-with-error job.
func (m *Mailbox) Fail(id string, reason string) error {
	if err := os.MkdirAll(m.paths.FailedDir, 0o700); err != nil {
		return fmt.Errorf("daemon: mkdir failed: %w", err)
	}
	r := Result{
		Success: false,
		Error:   reason,
	}
	data, err := MarshalResult(r)
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(m.paths.FailedDir, id+".json"), data); err != nil {
		return err
	}
	return removeMailFile(m.paths.MailDir, id)
}

func removeMailFile(mailDir, id string) error {
	err := os.Remove(filepath.Join(mailDir, id+".json"))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("daemon: remove mail %s: %w", id, err)
	}
	return nil
}

// atomicWrite writes data to target via target.tmp + rename. The fsync is
// best-effort: we want crash-safety, not guaranteed durability across power
// loss (the queue is recoverable from session state if it ever vanished).
func atomicWrite(target string, data []byte) error {
	tmp := target + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("daemon: create tmp %s: %w", tmp, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("daemon: write tmp %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		// Sync failures are not fatal — log via return and continue.
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("daemon: fsync tmp %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("daemon: close tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("daemon: rename %s -> %s: %w", tmp, target, err)
	}
	return nil
}
