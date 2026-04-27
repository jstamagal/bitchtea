package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
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

	mu        sync.Mutex
	exitError error
	closed    bool
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
	cmd := exec.CommandContext(sessionCtx, "bash", "-lc", args.Command)
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

	go io.Copy(session.emu, pty) //nolint:errcheck
	go io.Copy(pty, session.emu) //nolint:errcheck
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

	s.cancel()
	if s.cmd.Process != nil && s.running() {
		_ = s.cmd.Process.Kill()
	}
	_ = s.pty.Close()
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

	var sb strings.Builder
	fmt.Fprintf(&sb, "terminal session %s (%dx%d) %s", s.id, s.emu.Width(), s.emu.Height(), state)
	if exitErr != nil {
		fmt.Fprintf(&sb, " (%v)", exitErr)
	}
	sb.WriteString("\n--- screen ---\n")
	if ansi {
		sb.WriteString(s.emu.Render())
	} else {
		sb.WriteString(s.emu.String())
	}
	return strings.TrimRight(sb.String(), "\n")
}

func sleepContext(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
