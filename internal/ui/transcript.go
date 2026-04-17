package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// TranscriptLogger writes a human-readable daily conversation log.
type TranscriptLogger struct {
	dir          string
	now          func() time.Time
	mu           sync.Mutex
	currentDate  string
	file         *os.File
	streamActive bool
}

func NewTranscriptLogger(dir string) (*TranscriptLogger, error) {
	if dir == "" {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create transcript dir: %w", err)
	}
	return &TranscriptLogger{
		dir: dir,
		now: time.Now,
	}, nil
}

func (t *TranscriptLogger) Close() error {
	if t == nil {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	return t.closeLocked()
}

func (t *TranscriptLogger) LogMessage(msg ChatMessage) error {
	if t == nil {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Thinking blocks are transient UI state. They mutate in place and should
	// not leak placeholder text or partial reasoning into the persisted log.
	if msg.Type == MsgThink {
		return nil
	}

	if msg.Type == MsgAgent && strings.TrimSpace(msg.Content) == "" {
		return nil
	}

	if t.streamActive && msg.Type != MsgAgent {
		if err := t.writeStringLocked("\n"); err != nil {
			return err
		}
		t.streamActive = false
	}

	formatted := formatTranscriptMessage(msg)
	if formatted == "" {
		return nil
	}
	return t.writeStringLocked(formatted)
}

func (t *TranscriptLogger) AppendAgentChunk(at time.Time, nick, chunk string) error {
	if t == nil {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	clean := sanitizeTranscriptText(chunk)
	if clean == "" {
		return nil
	}

	if !t.streamActive {
		if err := t.writeStringLocked(transcriptPrefix(at, nick)); err != nil {
			return err
		}
		t.streamActive = true
	}

	return t.writeStringLocked(clean)
}

func (t *TranscriptLogger) FinishAgentMessage() error {
	if t == nil {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.streamActive {
		return nil
	}
	if err := t.writeStringLocked("\n"); err != nil {
		return err
	}
	t.streamActive = false
	return nil
}

func (t *TranscriptLogger) writeStringLocked(s string) error {
	if s == "" {
		return nil
	}
	if err := t.ensureFileLocked(); err != nil {
		return err
	}
	_, err := t.file.WriteString(s)
	return err
}

func (t *TranscriptLogger) ensureFileLocked() error {
	today := t.now().Format("2006-01-02")
	if t.file != nil && t.currentDate == today {
		return nil
	}

	if err := t.closeLocked(); err != nil {
		return err
	}

	path := filepath.Join(t.dir, today+".log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open transcript log: %w", err)
	}
	t.file = f
	t.currentDate = today
	return nil
}

func (t *TranscriptLogger) closeLocked() error {
	if t.file == nil {
		t.currentDate = ""
		t.streamActive = false
		return nil
	}
	err := t.file.Close()
	t.file = nil
	t.currentDate = ""
	t.streamActive = false
	return err
}

func formatTranscriptMessage(msg ChatMessage) string {
	switch msg.Type {
	case MsgUser:
		return transcriptLine(msg.Time, fmt.Sprintf("<%s>", msg.Nick), msg.Content)
	case MsgAgent:
		content := sanitizeTranscriptText(msg.Content)
		if content == "" {
			return ""
		}
		return transcriptPrefix(msg.Time, msg.Nick) + content + "\n"
	case MsgSystem:
		return transcriptLine(msg.Time, "***", msg.Content)
	case MsgError:
		return transcriptLine(msg.Time, "!!!", msg.Content)
	case MsgTool:
		content := indentTranscriptContent(msg.Content)
		if content == "" {
			return transcriptLine(msg.Time, fmt.Sprintf("-> %s:", msg.Nick), "")
		}
		return fmt.Sprintf("[%s] -> %s:\n%s\n", msg.Time.Format("15:04"), msg.Nick, content)
	case MsgRaw:
		content := strings.TrimRight(sanitizeTranscriptText(msg.Content), "\n")
		if content == "" {
			return ""
		}
		return content + "\n"
	default:
		return transcriptLine(msg.Time, "[message]", msg.Content)
	}
}

func transcriptLine(ts time.Time, prefix, content string) string {
	clean := sanitizeTranscriptText(content)
	if clean == "" {
		return fmt.Sprintf("[%s] %s\n", ts.Format("15:04"), prefix)
	}

	lines := strings.Split(clean, "\n")
	if len(lines) == 1 {
		return fmt.Sprintf("[%s] %s %s\n", ts.Format("15:04"), prefix, lines[0])
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%s] %s %s\n", ts.Format("15:04"), prefix, lines[0]))
	for _, line := range lines[1:] {
		sb.WriteString("    ")
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return sb.String()
}

func transcriptPrefix(ts time.Time, nick string) string {
	return fmt.Sprintf("[%s] <%s> ", ts.Format("15:04"), nick)
}

func indentTranscriptContent(content string) string {
	clean := sanitizeTranscriptText(content)
	if clean == "" {
		return ""
	}
	lines := strings.Split(clean, "\n")
	for i, line := range lines {
		lines[i] = "    " + line
	}
	return strings.Join(lines, "\n")
}

func sanitizeTranscriptText(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = ansiEscapePattern.ReplaceAllString(s, "")
	return strings.TrimRight(s, "\n")
}
