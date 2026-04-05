package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Entry is a single line in a JSONL session file
type Entry struct {
	Timestamp time.Time `json:"ts"`
	Role      string    `json:"role"`      // user, assistant, system, tool
	Content   string    `json:"content"`
	ToolName  string    `json:"tool_name,omitempty"`
	ToolArgs  string    `json:"tool_args,omitempty"`
	ParentID  string    `json:"parent_id,omitempty"` // for tree structure (branching)
	ID        string    `json:"id"`
}

// Session manages reading/writing JSONL session files
type Session struct {
	Path    string
	Entries []Entry
}

// New creates a new session
func New(dir string) (*Session, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create session dir: %w", err)
	}

	name := time.Now().Format("2006-01-02_150405") + ".jsonl"
	path := filepath.Join(dir, name)

	return &Session{
		Path:    path,
		Entries: []Entry{},
	}, nil
}

// Load reads an existing session file
func Load(path string) (*Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read session: %w", err)
	}

	s := &Session{Path: path}

	for _, line := range splitLines(data) {
		if len(line) == 0 {
			continue
		}
		var entry Entry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // skip malformed lines
		}
		s.Entries = append(s.Entries, entry)
	}

	return s, nil
}

// Append adds an entry and writes it to the file
func (s *Session) Append(entry Entry) error {
	entry.Timestamp = time.Now()
	entry.ID = fmt.Sprintf("%d", time.Now().UnixNano())

	s.Entries = append(s.Entries, entry)

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}

	f, err := os.OpenFile(s.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open session file: %w", err)
	}
	defer f.Close()

	_, err = f.Write(append(data, '\n'))
	return err
}

// List returns all session files in a directory
func List(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".jsonl" {
			sessions = append(sessions, filepath.Join(dir, e.Name()))
		}
	}
	return sessions, nil
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
