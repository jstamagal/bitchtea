package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jstamagal/bitchtea/internal/llm"
)

// Entry is a single line in a JSONL session file
type Entry struct {
	Timestamp  time.Time      `json:"ts"`
	Role       string         `json:"role"` // user, assistant, system, tool
	Content    string         `json:"content"`
	ToolName   string         `json:"tool_name,omitempty"`
	ToolArgs   string         `json:"tool_args,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []llm.ToolCall `json:"tool_calls,omitempty"`
	ParentID   string         `json:"parent_id,omitempty"` // for tree structure (branching)
	BranchTag  string         `json:"branch,omitempty"`    // branch label
	ID         string         `json:"id"`
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
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}

	// Set parent ID to previous entry if not already set
	if entry.ParentID == "" && len(s.Entries) > 0 {
		entry.ParentID = s.Entries[len(s.Entries)-1].ID
	}

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

// Fork creates a branch from the given entry ID. Returns a new session
// that includes all entries up to (and including) the fork point.
func (s *Session) Fork(fromID string) (*Session, error) {
	dir := filepath.Dir(s.Path)
	base := strings.TrimSuffix(filepath.Base(s.Path), ".jsonl")
	branch := time.Now().Format("150405")
	newPath := filepath.Join(dir, base+"_fork_"+branch+".jsonl")

	newSession := &Session{
		Path:    newPath,
		Entries: []Entry{},
	}

	// Copy entries up to and including the fork point
	for _, e := range s.Entries {
		newSession.Entries = append(newSession.Entries, e)
		if e.ID == fromID {
			break
		}
	}

	// Write all entries to the new file
	for _, e := range newSession.Entries {
		data, err := json.Marshal(e)
		if err != nil {
			continue
		}
		f, err := os.OpenFile(newPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("write fork: %w", err)
		}
		f.Write(append(data, '\n'))
		f.Close()
	}

	return newSession, nil
}

// Tree returns a text representation of the session tree structure
func (s *Session) Tree() string {
	if len(s.Entries) == 0 {
		return "(empty session)"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Session: %s\n", filepath.Base(s.Path)))
	sb.WriteString(fmt.Sprintf("Entries: %d\n\n", len(s.Entries)))

	for i, e := range s.Entries {
		prefix := "├── "
		if i == len(s.Entries)-1 {
			prefix = "└── "
		}

		content := e.Content
		if len(content) > 60 {
			content = content[:60] + "..."
		}
		// Replace newlines for display
		content = strings.ReplaceAll(content, "\n", " ")

		ts := e.Timestamp.Format("15:04:05")
		role := e.Role
		if e.ToolName != "" {
			role = "tool:" + e.ToolName
		}

		sb.WriteString(fmt.Sprintf("%s[%s] %s: %s\n", prefix, ts, role, content))
	}

	return sb.String()
}

// LastUserEntry returns the last user entry, or empty string if none
func (s *Session) LastUserEntry() string {
	for i := len(s.Entries) - 1; i >= 0; i-- {
		if s.Entries[i].Role == "user" {
			return s.Entries[i].Content
		}
	}
	return ""
}

// List returns all session files in a directory, sorted newest first
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

	// Sort newest first
	sort.Sort(sort.Reverse(sort.StringSlice(sessions)))

	return sessions, nil
}

// Info returns a summary string for a session file
func Info(path string) string {
	s, err := Load(path)
	if err != nil {
		return filepath.Base(path) + " (error loading)"
	}

	userMsgs := 0
	for _, e := range s.Entries {
		if e.Role == "user" {
			userMsgs++
		}
	}

	lastContent := ""
	if last := s.LastUserEntry(); last != "" {
		lastContent = last
		if len(lastContent) > 50 {
			lastContent = lastContent[:50] + "..."
		}
	}

	name := filepath.Base(path)
	return fmt.Sprintf("%s (%d entries, %d user msgs) %s", name, len(s.Entries), userMsgs, lastContent)
}

// Latest returns the most recent session file in a directory, or empty string
func Latest(dir string) string {
	sessions, err := List(dir)
	if err != nil || len(sessions) == 0 {
		return ""
	}
	return sessions[0]
}

// EntryFromMessage converts an in-memory LLM message into a session entry.
func EntryFromMessage(msg llm.Message) Entry {
	return Entry{
		Role:       msg.Role,
		Content:    msg.Content,
		ToolCallID: msg.ToolCallID,
		ToolCalls:  append([]llm.ToolCall(nil), msg.ToolCalls...),
	}
}

// MessagesFromEntries reconstructs LLM message history from session entries.
// Legacy tool entries without tool_call_id are skipped because the provider APIs
// cannot replay them safely.
func MessagesFromEntries(entries []Entry) []llm.Message {
	msgs := make([]llm.Message, 0, len(entries))
	for _, e := range entries {
		if e.Role == "tool" && e.ToolCallID == "" {
			continue
		}

		msg := llm.Message{
			Role:       e.Role,
			Content:    e.Content,
			ToolCallID: e.ToolCallID,
		}
		if len(e.ToolCalls) > 0 {
			msg.ToolCalls = append([]llm.ToolCall(nil), e.ToolCalls...)
		}
		msgs = append(msgs, msg)
	}
	return msgs
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
