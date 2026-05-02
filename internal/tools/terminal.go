package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/charmbracelet/x/xpty"
)

type terminalManager struct {
	workDir string
	mu      sync.Mutex
	next    int
	terms   map[string]*terminalSession
}

type terminalSession struct {
	id     string
	cmd    *exec.Cmd
	pty    xpty.Pty
	emu    *vt.SafeEmulator
	cancel context.CancelFunc
	done   chan struct{}
	copyWg sync.WaitGroup // tracks io.Copy goroutines

	mu        sync.Mutex
	exitError error
	closed    bool

	// emuMu serializes ALL access to the emulator (Write, Read, String,
	// Close). SafeEmulator's internal mutex only covers Write, but
	// String/Read/Close also touch the underlying buffer — so concurrent
	// snapshot + Write or Close + Read races without this outer lock.
	emuMu sync.Mutex

	// closing is set by close() before it begins teardown. The emu.Read
	// loop checks it after each Read returns and exits without re-entering
	// emu.Read — this guarantees no goroutine is inside emu.Read when
	// close() finally calls emu.Close(). Required because vt.Emulator's
	// `closed` field is a plain bool that Close writes and Read reads
	// without any synchronization (upstream race that vt.SafeEmulator
	// does not fix). See close() and the emu.Read goroutine in Start().
	closing atomic.Bool
}

func newTerminalManager(workDir string) *terminalManager {
	return &terminalManager{
		workDir: workDir,
		terms:   make(map[string]*terminalSession),
	}
}

func (m *terminalManager) Start(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Command string `json:"command"`
		Width   int    `json:"width"`
		Height  int    `json:"height"`
		DelayMS int    `json:"delay_ms"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	args.Command = strings.TrimSpace(args.Command)
	if args.Command == "" {
		return "", errors.New("command is required")
	}
	if args.Width <= 0 {
		args.Width = 100
	}
	if args.Height <= 0 {
		args.Height = 30
	}
	if args.DelayMS <= 0 {
		args.DelayMS = 200
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	pty, err := xpty.NewPty(args.Width, args.Height)
	if err != nil {
		return "", fmt.Errorf("create pty: %w", err)
	}

	sessionCtx, cancel := context.WithCancel(context.Background())
	// Use bash -c (not -lc) so the shell does NOT source the user's
	// login dotfiles (~/.bash_profile, ~/.bashrc). Sourcing them makes
	// tool behavior depend on the host environment — aliases, exported
	// vars, PROMPT_COMMAND, etc. — and breaks reproducibility.
	cmd := exec.CommandContext(sessionCtx, "bash", "-c", args.Command)
	cmd.Dir = m.workDir

	if err := pty.Start(cmd); err != nil {
		cancel()
		_ = pty.Close()
		return "", fmt.Errorf("start terminal command: %w", err)
	}

	m.mu.Lock()
	m.next++
	id := fmt.Sprintf("term-%d", m.next)
	session := &terminalSession{
		id:     id,
		cmd:    cmd,
		pty:    pty,
		emu:    vt.NewSafeEmulator(args.Width, args.Height),
		cancel: cancel,
		done:   make(chan struct{}),
	}
	m.terms[id] = session
	m.mu.Unlock()

	session.copyWg.Add(2)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := session.pty.Read(buf)
			if n > 0 {
				session.emuMu.Lock()
				session.emu.Write(buf[:n]) //nolint:errcheck
				session.emuMu.Unlock()
			}
			if readErr != nil {
				break
			}
		}
		session.copyWg.Done()
	}()
	go func() {
		// emu.Read() blocks on its internal pipe; emuMu must NOT be held
		// across it or snapshot/close will deadlock. The Write-vs-String
		// race is prevented by emuMu. The Close-vs-Read race on the
		// e.closed bool field (upstream vt bug — SafeEmulator does not
		// wrap Read/Close) is worked around by checking session.closing
		// here: close() sets closing, sends a wake byte via SendText to
		// unblock our pending emu.Read, then waits for us to exit before
		// calling emu.Close(). That establishes happens-before between
		// every emu.Read access of e.closed and Close's write of e.closed.
		buf := make([]byte, 4096)
		for {
			if session.closing.Load() {
				break
			}
			n, readErr := session.emu.Read(buf)
			if n > 0 && !session.closing.Load() {
				_, _ = session.pty.Write(buf[:n])
			}
			if readErr != nil {
				break
			}
		}
		session.copyWg.Done()
	}()
	go session.wait()

	sleepContext(ctx, time.Duration(args.DelayMS)*time.Millisecond)
	return session.snapshot(false), nil
}

func (m *terminalManager) Send(argsJSON string) (string, error) {
	var args struct {
		ID      string `json:"id"`
		Text    string `json:"text"`
		DelayMS int    `json:"delay_ms"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	if args.DelayMS <= 0 {
		args.DelayMS = 100
	}
	session, err := m.get(args.ID)
	if err != nil {
		return "", err
	}
	if !session.running() {
		return session.snapshot(false), nil
	}

	session.emu.SendText(args.Text)
	time.Sleep(time.Duration(args.DelayMS) * time.Millisecond)
	return session.snapshot(false), nil
}

func (m *terminalManager) Keys(argsJSON string) (string, error) {
	var args struct {
		ID      string   `json:"id"`
		Keys    []string `json:"keys"`
		DelayMS int      `json:"delay_ms"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	if len(args.Keys) == 0 {
		return "", errors.New("keys is required")
	}
	if args.DelayMS <= 0 {
		args.DelayMS = 100
	}
	session, err := m.get(args.ID)
	if err != nil {
		return "", err
	}
	if !session.running() {
		return session.snapshot(false), nil
	}

	var input strings.Builder
	for _, key := range args.Keys {
		input.WriteString(terminalKeyInput(key))
	}
	session.emu.SendText(input.String())
	time.Sleep(time.Duration(args.DelayMS) * time.Millisecond)
	return session.snapshot(false), nil
}

func (m *terminalManager) Snapshot(argsJSON string) (string, error) {
	var args struct {
		ID   string `json:"id"`
		ANSI bool   `json:"ansi"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	session, err := m.get(args.ID)
	if err != nil {
		return "", err
	}
	return session.snapshot(args.ANSI), nil
}

func (m *terminalManager) Wait(argsJSON string) (string, error) {
	var args struct {
		ID            string `json:"id"`
		Text          string `json:"text"`
		TimeoutMS     int    `json:"timeout_ms"`
		IntervalMS    int    `json:"interval_ms"`
		CaseSensitive bool   `json:"case_sensitive"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	if strings.TrimSpace(args.Text) == "" {
		return "", errors.New("text is required")
	}
	if args.TimeoutMS <= 0 {
		args.TimeoutMS = 5000
	}
	if args.IntervalMS <= 0 {
		args.IntervalMS = 100
	}

	session, err := m.get(args.ID)
	if err != nil {
		return "", err
	}

	deadline := time.Now().Add(time.Duration(args.TimeoutMS) * time.Millisecond)
	for {
		snapshot := session.snapshot(false)
		if containsTerminalText(snapshot, args.Text, args.CaseSensitive) {
			return "matched terminal text " + fmt.Sprintf("%q", args.Text) + "\n" + snapshot, nil
		}
		if !session.running() {
			return "terminal exited before matching text " + fmt.Sprintf("%q", args.Text) + "\n" + snapshot, nil
		}
		if time.Now().After(deadline) {
			return "timeout waiting for terminal text " + fmt.Sprintf("%q", args.Text) + "\n" + snapshot, nil
		}
		time.Sleep(time.Duration(args.IntervalMS) * time.Millisecond)
	}
}

func (m *terminalManager) Resize(argsJSON string) (string, error) {
	var args struct {
		ID      string `json:"id"`
		Width   int    `json:"width"`
		Height  int    `json:"height"`
		DelayMS int    `json:"delay_ms"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	if args.Width <= 0 {
		return "", errors.New("width must be positive")
	}
	if args.Height <= 0 {
		return "", errors.New("height must be positive")
	}
	if args.DelayMS <= 0 {
		args.DelayMS = 100
	}

	session, err := m.get(args.ID)
	if err != nil {
		return "", err
	}
	if err := session.pty.Resize(args.Width, args.Height); err != nil {
		return "", fmt.Errorf("resize pty: %w", err)
	}
	session.emu.Resize(args.Width, args.Height)
	time.Sleep(time.Duration(args.DelayMS) * time.Millisecond)
	return session.snapshot(false), nil
}

func (m *terminalManager) Close(argsJSON string) (string, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}

	m.mu.Lock()
	session, ok := m.terms[args.ID]
	if ok {
		delete(m.terms, args.ID)
	}
	m.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("unknown terminal session: %s", args.ID)
	}

	session.close()
	return fmt.Sprintf("closed terminal session %s", args.ID), nil
}

func (m *terminalManager) get(id string) (*terminalSession, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.terms[id]
	if !ok {
		return nil, fmt.Errorf("unknown terminal session: %s", id)
	}
	return session, nil
}

func (s *terminalSession) wait() {
	err := xpty.WaitProcess(context.Background(), s.cmd)
	s.mu.Lock()
	s.exitError = err
	s.mu.Unlock()
	_ = s.pty.Close()
	close(s.done)
}

func (s *terminalSession) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()

	// Tell the emu.Read goroutine to stop iterating BEFORE we touch the
	// emulator's racy `closed` field via emu.Close(). See the goroutine
	// in Start() and the closing field comment for why this ordering
	// matters.
	s.closing.Store(true)

	s.cancel()
	if s.cmd.Process != nil && s.running() {
		_ = s.cmd.Process.Kill()
	}

	// Wake the emu.Read goroutine if it is blocked on the emulator's
	// internal pipe. SendText writes to the same pipe emu.Read reads
	// from, so any pending Read returns with data; the goroutine then
	// sees s.closing and exits without re-entering emu.Read. SafeEmulator
	// serializes SendText under its own mutex so this is race-free.
	s.emu.SendText("\x00")

	// Wait for process to exit and close pty from wait() goroutine.
	<-s.done

	// Wait for both io.Copy goroutines to drain. This guarantees no
	// goroutine is inside emu.Read when we call emu.Close() below — so
	// emu.Close()'s write to e.closed can no longer race with a Read.
	s.copyWg.Wait()

	// Now safe to close the emulator. emu.Write callers (the pty.Read
	// goroutine) have already exited because pty.Close in wait() made
	// pty.Read return EOF.
	_ = s.emu.Close()
}

func (s *terminalSession) running() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

func (s *terminalSession) snapshot(ansi bool) string {
	state := "running"
	if !s.running() {
		state = "exited"
	}

	s.mu.Lock()
	exitErr := s.exitError
	s.mu.Unlock()

	s.emuMu.Lock()
	w, h := s.emu.Width(), s.emu.Height()
	var screen string
	if ansi {
		screen = s.emu.Render()
	} else {
		screen = s.emu.String()
	}
	s.emuMu.Unlock()

	var sb strings.Builder
	fmt.Fprintf(&sb, "terminal session %s (%dx%d) %s", s.id, w, h, state)
	if exitErr != nil {
		fmt.Fprintf(&sb, " (%v)", exitErr)
	}
	sb.WriteString("\n--- screen ---\n")
	sb.WriteString(screen)
	return strings.TrimRight(sb.String(), "\n")
}

func terminalKeyInput(key string) string {
	normalized := strings.ToLower(strings.TrimSpace(key))
	switch normalized {
	case "esc", "escape":
		return "\x1b"
	case "enter", "return":
		return "\r"
	case "newline", "linefeed", "lf":
		return "\n"
	case "tab":
		return "\t"
	case "backspace", "bs":
		return "\x7f"
	case "delete", "del":
		return "\x1b[3~"
	case "up":
		return "\x1b[A"
	case "down":
		return "\x1b[B"
	case "right":
		return "\x1b[C"
	case "left":
		return "\x1b[D"
	case "home":
		return "\x1b[H"
	case "end":
		return "\x1b[F"
	case "pageup", "pgup":
		return "\x1b[5~"
	case "pagedown", "pgdown":
		return "\x1b[6~"
	case "space":
		return " "
	}

	if strings.HasPrefix(normalized, "ctrl-") && len(normalized) == len("ctrl-a") {
		ch := normalized[len("ctrl-")]
		if ch >= 'a' && ch <= 'z' {
			return string([]byte{ch - 'a' + 1})
		}
	}

	return key
}

func containsTerminalText(snapshot string, text string, caseSensitive bool) bool {
	if caseSensitive {
		return strings.Contains(snapshot, text)
	}
	return strings.Contains(strings.ToLower(snapshot), strings.ToLower(text))
}

func sleepContext(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
