package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/llm"
)

// EntrySchemaVersion is the current entry schema version emitted by the writer.
// v0 (absent or 0) is the legacy llm.Message-only shape. v1 adds the Msg field
// with a fantasy.Message envelope alongside the legacy fields. See
// docs/phase-3-message-contract.md for the dual-write rules.
const EntrySchemaVersion = 1

// Entry is a single line in a JSONL session file.
//
// v0 entries carry only the legacy fields (Role, Content, ToolCalls, etc.).
// v1 entries set V = 1 and additionally populate Msg with the canonical
// fantasy.Message; legacy fields are still written so a downgraded binary
// can keep reading. When the legacy projection cannot losslessly represent
// the fantasy message (e.g. reasoning, media, multi-part text), the writer
// flags LegacyLossy = true so the downgrade reader can warn.
//
// Reader precedence: when V >= 1 and Msg is non-nil, Msg is the source of
// truth; otherwise the reader falls back to the legacy fields.
type Entry struct {
	Timestamp  time.Time      `json:"ts"`
	Role       string         `json:"role"` // user, assistant, system, tool
	Content    string         `json:"content"`
	Context    string         `json:"context,omitempty"` // IRC routing context label (e.g. "#main", "buddy")
	Bootstrap  bool           `json:"bootstrap,omitempty"`
	ToolName   string         `json:"tool_name,omitempty"`
	ToolArgs   string         `json:"tool_args,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []llm.ToolCall `json:"tool_calls,omitempty"`
	ParentID   string         `json:"parent_id,omitempty"` // for tree structure (branching)
	BranchTag  string         `json:"branch,omitempty"`    // branch label
	ID         string         `json:"id"`

	// V is the entry schema version. 0 (or absent) means legacy v0; 1 means
	// the Msg field is populated with the canonical fantasy.Message.
	V int `json:"v,omitempty"`
	// Msg is the canonical fantasy-native message. Populated for v1 entries.
	Msg *fantasy.Message `json:"msg,omitempty"`
	// LegacyLossy marks v1 entries whose fantasy parts cannot be losslessly
	// represented in the legacy fields (reasoning, file/media tool result,
	// multiple text parts on one message, etc.). A downgraded binary will
	// load the text projection but loses fidelity. Always false on v0
	// entries (which never had richer parts to lose).
	LegacyLossy bool `json:"legacy_lossy,omitempty"`
}

// Session manages reading/writing JSONL session files.
//
// mu serialises writes to the JSONL file. While UI updates happen on the
// Bubble Tea goroutine, autonomous-turn flushes and compaction can trigger
// concurrent calls from agent goroutines sharing the same Session.
type Session struct {
	Path    string
	Entries []Entry
	mu      sync.Mutex
}

// Checkpoint captures lightweight autonomous-turn state without mutating
// project files like MEMORY.md.
type Checkpoint struct {
	TurnCount int            `json:"turn_count"`
	ToolCalls map[string]int `json:"tool_calls,omitempty"`
	Model     string         `json:"model,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// ContextRecord is a serializable representation of a single IRC routing context.
type ContextRecord struct {
	Kind    string `json:"kind"`              // "channel", "subchannel", "direct"
	Channel string `json:"channel,omitempty"` // channel name (no '#')
	Sub     string `json:"sub,omitempty"`     // subchannel qualifier
	Target  string `json:"target,omitempty"`  // persona or nick for direct
}

// FocusState captures the ordered list of open contexts and which is active.
// This is persisted to disk so the workspace is restored on restart.
type FocusState struct {
	Contexts    []ContextRecord `json:"contexts"`
	ActiveIndex int             `json:"active"`
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

// Append adds an entry and writes it to the file. Uses internal mutex to
// serialise concurrent goroutine access from the same process, and flock to
// prevent interleaved writes from concurrent bitchtea processes.
func (s *Session) Append(entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

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

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("flock session: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck

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

	// Write all entries to the new file atomically (single open)
	f, err := os.OpenFile(newPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("write fork: %w", err)
	}
	for _, e := range newSession.Entries {
		data, err := json.Marshal(e)
		if err != nil {
			continue
		}
		f.Write(append(data, '\n'))
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("close fork file: %w", err)
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

// SaveCheckpoint writes the current autonomous-turn checkpoint into the session
// directory using a fixed hidden filename.
func SaveCheckpoint(dir string, checkpoint Checkpoint) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}
	checkpoint.Timestamp = time.Now()
	if checkpoint.ToolCalls == nil {
		checkpoint.ToolCalls = map[string]int{}
	}

	data, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}

	path := filepath.Join(dir, ".bitchtea_checkpoint.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write checkpoint: %w", err)
	}
	return nil
}

// EntryFromMessage converts an in-memory LLM message into a session entry.
func EntryFromMessage(msg llm.Message) Entry {
	return EntryFromMessageWithBootstrap(msg, false)
}

// EntryFromMessageWithBootstrap converts an in-memory LLM message into a
// session entry and marks whether it came from startup/bootstrap injection.
func EntryFromMessageWithBootstrap(msg llm.Message, bootstrap bool) Entry {
	return Entry{
		Bootstrap:  bootstrap,
		Role:       msg.Role,
		Content:    msg.Content,
		ToolCallID: msg.ToolCallID,
		ToolCalls:  append([]llm.ToolCall(nil), msg.ToolCalls...),
	}
}

// DisplayEntries filters out internal bootstrap-only messages from the
// user-visible transcript while leaving replay history untouched.
func DisplayEntries(entries []Entry) []Entry {
	display := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.Bootstrap {
			continue
		}
		display = append(display, e)
	}
	return display
}

// MessagesFromEntries reconstructs LLM message history from session entries.
// Legacy tool entries without tool_call_id are skipped because the provider APIs
// cannot replay them safely. v1 entries are projected back through their
// legacy fields (the dual-write writer keeps these populated as a text
// projection even for lossy messages), so this function is forward-compatible
// with v1 sessions but loses fidelity on parts the legacy shape cannot
// express. Callers that need full fidelity should use FantasyFromEntries.
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

// EntryFromFantasy converts a fantasy.Message into a v1 session Entry.
// The returned Entry has both Msg populated (the canonical shape) and the
// legacy fields synthesized from the fantasy parts (the dual-write mirror
// for downgrade compatibility). LegacyLossy is set when the fantasy message
// has parts that the legacy projection cannot represent — multiple text
// parts, ReasoningPart, FilePart, or non-text tool-result outputs.
//
// Provider options on the message are persisted as part of Msg. The design
// note in docs/phase-3-message-contract.md leaves "strip provider_options
// on write" as an open question; persisting them here is the simplest
// defensible choice (any caller that wants them stripped can do so before
// calling). Defer the strip-or-keep decision until the agent boundary lands.
func EntryFromFantasy(msg fantasy.Message) Entry {
	return EntryFromFantasyWithBootstrap(msg, false)
}

// EntryFromFantasyWithBootstrap is the bootstrap-aware variant of
// EntryFromFantasy.
func EntryFromFantasyWithBootstrap(msg fantasy.Message, bootstrap bool) Entry {
	cloned := msg
	if len(msg.Content) > 0 {
		cloned.Content = append([]fantasy.MessagePart(nil), msg.Content...)
	}
	legacy, lossy := projectFantasyToLegacy(msg)
	return Entry{
		V:           EntrySchemaVersion,
		Msg:         &cloned,
		LegacyLossy: lossy,
		Bootstrap:   bootstrap,
		Role:        legacy.Role,
		Content:     legacy.Content,
		ToolCallID:  legacy.ToolCallID,
		ToolCalls:   legacy.ToolCalls,
	}
}

// FantasyFromEntries reconstructs a fantasy.Message slice from session
// entries. For v1 entries (V >= 1 and Msg non-nil) the canonical Msg is
// returned verbatim. For v0 entries the legacy fields are synthesized into
// fantasy parts the same way the in-flight llm→fantasy conversion does
// (see internal/llm/convert.go). Tool entries without a tool_call_id are
// skipped — provider APIs reject them and the existing
// MessagesFromEntries already drops them, so we keep the policies aligned.
func FantasyFromEntries(entries []Entry) []fantasy.Message {
	msgs := make([]fantasy.Message, 0, len(entries))
	for _, e := range entries {
		if e.V >= 1 && e.Msg != nil {
			cloned := *e.Msg
			if len(e.Msg.Content) > 0 {
				cloned.Content = append([]fantasy.MessagePart(nil), e.Msg.Content...)
			}
			msgs = append(msgs, cloned)
			continue
		}
		if e.Role == "tool" && e.ToolCallID == "" {
			continue
		}
		msgs = append(msgs, legacyEntryToFantasy(e))
	}
	return msgs
}

// projectFantasyToLegacy returns the best-effort legacy llm.Message
// projection of a fantasy.Message and reports whether the projection was
// lossy. This is used by the dual-write writer to populate the legacy
// fields alongside Msg, so a downgraded binary can still read the entry.
func projectFantasyToLegacy(msg fantasy.Message) (llm.Message, bool) {
	out := llm.Message{Role: string(msg.Role)}
	var (
		text       strings.Builder
		textParts  int
		lossy      bool
		toolCallID string
	)

	for _, part := range msg.Content {
		switch p := part.(type) {
		case fantasy.TextPart:
			if textParts > 0 {
				text.WriteString("\n\n")
			}
			text.WriteString(p.Text)
			textParts++
		case *fantasy.TextPart:
			if p == nil {
				continue
			}
			if textParts > 0 {
				text.WriteString("\n\n")
			}
			text.WriteString(p.Text)
			textParts++
		case fantasy.ReasoningPart, *fantasy.ReasoningPart:
			// Reasoning is not representable in the legacy shape — flag and
			// drop. The v1 Msg field still has it for forward readers.
			lossy = true
		case fantasy.FilePart, *fantasy.FilePart:
			lossy = true
		case fantasy.ToolCallPart:
			out.ToolCalls = append(out.ToolCalls, llm.ToolCall{
				ID:   p.ToolCallID,
				Type: "function",
				Function: llm.FunctionCall{
					Name:      p.ToolName,
					Arguments: p.Input,
				},
			})
		case *fantasy.ToolCallPart:
			if p == nil {
				continue
			}
			out.ToolCalls = append(out.ToolCalls, llm.ToolCall{
				ID:   p.ToolCallID,
				Type: "function",
				Function: llm.FunctionCall{
					Name:      p.ToolName,
					Arguments: p.Input,
				},
			})
		case fantasy.ToolResultPart:
			if toolCallID == "" {
				toolCallID = p.ToolCallID
			}
			lossy = lossy || appendToolResultText(&text, &textParts, p.Output)
		case *fantasy.ToolResultPart:
			if p == nil {
				continue
			}
			if toolCallID == "" {
				toolCallID = p.ToolCallID
			}
			lossy = lossy || appendToolResultText(&text, &textParts, p.Output)
		default:
			// Unknown part type → flag lossy; legacy gets nothing for it.
			lossy = true
		}
	}

	if textParts > 1 {
		// Concatenated multi-part text is a lossy projection — the v1 Msg
		// keeps the part boundaries, the legacy field collapses them.
		lossy = true
	}

	out.Content = text.String()
	out.ToolCallID = toolCallID
	return out, lossy
}

// appendToolResultText writes the text projection of a tool result output
// into buf and returns true if the projection was lossy (media or error
// payloads cannot round-trip through a plain string).
func appendToolResultText(buf *strings.Builder, textParts *int, output fantasy.ToolResultOutputContent) bool {
	switch o := output.(type) {
	case fantasy.ToolResultOutputContentText:
		if *textParts > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString(o.Text)
		*textParts++
		return false
	case *fantasy.ToolResultOutputContentText:
		if o == nil {
			return false
		}
		if *textParts > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString(o.Text)
		*textParts++
		return false
	case fantasy.ToolResultOutputContentMedia:
		if *textParts > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString(o.Text) // accompanying text only; media data is dropped
		if o.Text != "" {
			*textParts++
		}
		return true
	case *fantasy.ToolResultOutputContentMedia:
		if o == nil {
			return false
		}
		if *textParts > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString(o.Text)
		if o.Text != "" {
			*textParts++
		}
		return true
	case fantasy.ToolResultOutputContentError:
		if o.Error != nil {
			if *textParts > 0 {
				buf.WriteString("\n\n")
			}
			buf.WriteString(o.Error.Error())
			*textParts++
		}
		return true
	case *fantasy.ToolResultOutputContentError:
		if o == nil {
			return false
		}
		if o.Error != nil {
			if *textParts > 0 {
				buf.WriteString("\n\n")
			}
			buf.WriteString(o.Error.Error())
			*textParts++
		}
		return true
	default:
		return true
	}
}

// legacyEntryToFantasy synthesizes a fantasy.Message from a v0 entry's
// legacy fields. Mirrors the shape produced by splitForFantasy in
// internal/llm/convert.go so the agent loop sees the same parts whether a
// turn was just emitted or replayed from disk.
func legacyEntryToFantasy(e Entry) fantasy.Message {
	role := fantasy.MessageRole(e.Role)
	switch e.Role {
	case "user":
		return fantasy.Message{
			Role:    fantasy.MessageRoleUser,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: e.Content}},
		}
	case "assistant":
		parts := make([]fantasy.MessagePart, 0, 1+len(e.ToolCalls))
		if e.Content != "" {
			parts = append(parts, fantasy.TextPart{Text: e.Content})
		}
		for _, tc := range e.ToolCalls {
			parts = append(parts, fantasy.ToolCallPart{
				ToolCallID: tc.ID,
				ToolName:   tc.Function.Name,
				Input:      tc.Function.Arguments,
			})
		}
		return fantasy.Message{Role: fantasy.MessageRoleAssistant, Content: parts}
	case "tool":
		return fantasy.Message{
			Role: fantasy.MessageRoleTool,
			Content: []fantasy.MessagePart{fantasy.ToolResultPart{
				ToolCallID: e.ToolCallID,
				Output:     fantasy.ToolResultOutputContentText{Text: e.Content},
			}},
		}
	case "system":
		return fantasy.Message{
			Role:    fantasy.MessageRoleSystem,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: e.Content}},
		}
	default:
		return fantasy.Message{
			Role:    role,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: e.Content}},
		}
	}
}

// SaveFocus writes the focus state to .bitchtea_focus.json in dir.
func SaveFocus(dir string, state FocusState) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal focus: %w", err)
	}
	path := filepath.Join(dir, ".bitchtea_focus.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write focus: %w", err)
	}
	return nil
}

// LoadFocus reads focus state from .bitchtea_focus.json in dir.
// Returns a zero-value FocusState (no error) when the file does not exist yet.
func LoadFocus(dir string) (FocusState, error) {
	path := filepath.Join(dir, ".bitchtea_focus.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return FocusState{}, nil
	}
	if err != nil {
		return FocusState{}, fmt.Errorf("read focus: %w", err)
	}
	var state FocusState
	if err := json.Unmarshal(data, &state); err != nil {
		return FocusState{}, fmt.Errorf("unmarshal focus: %w", err)
	}
	return state, nil
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
