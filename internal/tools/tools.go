package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	memorypkg "github.com/jstamagal/bitchtea/internal/memory"
)

// ============================================================================
// Pattern 1 — Structured error result with inline reflection prompt
// ============================================================================

// reflectionPrompt is appended to every structured tool error so the model
// knows exactly what to do next instead of spinning its wheels.
const reflectionPrompt = "Reflect on the error above: (1) identify exactly what went wrong with the tool call, (2) explain why that mistake happened, (3) make the corrected tool call. Do NOT skip this reflection."

// wrapToolError wraps a tool execution error in the Forgecode-style structured
// XML envelope. The model receives cause + self-correction prompt instead of a
// bare error string, which measurably improves recovery rates.
func wrapToolError(err error) string {
	return "<tool_call_error>\n<cause>" + err.Error() + "</cause>\n<reflection>" + reflectionPrompt + "</reflection>\n</tool_call_error>"
}

// ============================================================================
// Registry
// ============================================================================

// Registry holds all available tools and their definitions.
type Registry struct {
	WorkDir    string
	SessionDir string
	Scope      memorypkg.Scope
	terminals  *terminalManager

	// Pattern 2 — read-before-edit guard
	// filesRead tracks absolute paths that were read in the current turn.
	// Populated by execRead; consulted at the top of execEdit and execWrite
	// (overwrite-existing path). Reset each turn via ResetTurnState.
	frMu      sync.Mutex
	filesRead map[string]struct{}

	// Pattern 4 — per-tool timeout
	// ToolTimeout is the default wall-clock limit applied to every tool call
	// that doesn't manage its own timeout (bash manages its own). Defaults to
	// 300 s; adjustable via /set tool_timeout <seconds>.
	ToolTimeout time.Duration
}

// NewRegistry creates a tool registry.
func NewRegistry(workDir, sessionDir string) *Registry {
	return &Registry{
		WorkDir:     workDir,
		SessionDir:  sessionDir,
		terminals:   newTerminalManager(workDir),
		filesRead:   make(map[string]struct{}),
		ToolTimeout: 300 * time.Second,
	}
}

// SetScope updates the memory scope used for search_memory queries.
func (r *Registry) SetScope(scope memorypkg.Scope) {
	r.Scope = scope
}

// ResetTurnState clears per-turn state (currently: the read-before-edit guard).
// The agent loop calls this at the start of each new user turn (sendMessage).
func (r *Registry) ResetTurnState() {
	r.frMu.Lock()
	r.filesRead = make(map[string]struct{})
	r.frMu.Unlock()
}

// SetToolTimeout updates the per-tool timeout. Values <= 0 are ignored.
// Safe to call concurrently (the timeout is read under no lock since it's set
// only during initialization before any concurrent tool calls begin).
func (r *Registry) SetToolTimeout(seconds int) {
	if seconds > 0 {
		r.ToolTimeout = time.Duration(seconds) * time.Second
	}
}

// markFileRead records that the given absolute path was read in this turn.
func (r *Registry) markFileRead(absPath string) {
	r.frMu.Lock()
	r.filesRead[absPath] = struct{}{}
	r.frMu.Unlock()
}

// wasFileRead reports whether the given absolute path was read in this turn.
func (r *Registry) wasFileRead(absPath string) bool {
	r.frMu.Lock()
	_, ok := r.filesRead[absPath]
	r.frMu.Unlock()
	return ok
}

// Definitions returns OpenAI-compatible tool definitions
func (r *Registry) Definitions() []ToolDef {
	return []ToolDef{
		{
			Type: "function",
			Function: ToolFuncDef{
				Name:        "read",
				Description: "Read the contents of a file. For text files, returns content. Supports offset/limit for large files.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Path to the file to read (relative to working directory or absolute)",
						},
						"offset": map[string]interface{}{
							"type":        "integer",
							"description": "Line number to start reading from (1-indexed)",
						},
						"limit": map[string]interface{}{
							"type":        "integer",
							"description": "Maximum number of lines to read",
						},
					},
					"required": []string{"path"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFuncDef{
				Name:        "write",
				Description: "Write content to a file. Creates the file if it doesn't exist, overwrites if it does. Automatically creates parent directories.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Path to the file to write",
						},
						"content": map[string]interface{}{
							"type":        "string",
							"description": "Content to write to the file",
						},
					},
					"required": []string{"path", "content"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFuncDef{
				Name:        "edit",
				Description: "Edit a file by replacing exact text matches. Each edit replaces oldText with newText.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Path to the file to edit",
						},
						"edits": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"oldText": map[string]interface{}{
										"type":        "string",
										"description": "Exact text to find and replace",
									},
									"newText": map[string]interface{}{
										"type":        "string",
										"description": "Replacement text",
									},
								},
								"required": []string{"oldText", "newText"},
							},
						},
					},
					"required": []string{"path", "edits"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFuncDef{
				Name:        "search_memory",
				Description: "Search the hot MEMORY.md file and durable daily markdown memory for past decisions, notes, and context relevant to the current worktree.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "Keywords or a short natural-language query describing what to recall",
						},
						"limit": map[string]interface{}{
							"type":        "integer",
							"description": "Maximum number of memory matches to return (default: 5)",
						},
					},
					"required": []string{"query"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFuncDef{
				Name:        "write_memory",
				Description: "Persist a memory entry (decision, preference, work-state note) into hot memory for the current scope, or override with scope='root' or a specific channel/query. Use 'daily' to append to the durable daily archive instead. Appended as a dated markdown section so search_memory can recall it later.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"content": map[string]interface{}{
							"type":        "string",
							"description": "Markdown content to remember. Plain prose or a bulleted list works.",
						},
						"title": map[string]interface{}{
							"type":        "string",
							"description": "Optional heading for the entry (e.g. 'decision: drop daemon').",
						},
						"scope": map[string]interface{}{
							"type":        "string",
							"description": "Optional scope override: 'current' (default), 'root', 'channel', or 'query'.",
						},
						"name": map[string]interface{}{
							"type":        "string",
							"description": "Required when scope is 'channel' or 'query'. The channel (#name) or query (nick) to write to.",
						},
						"daily": map[string]interface{}{
							"type":        "boolean",
							"description": "If true, append to the durable daily archive instead of the hot file (default false).",
						},
					},
					"required": []string{"content"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFuncDef{
				Name:        "bash",
				Description: "Execute a bash command. Returns stdout and stderr.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"command": map[string]interface{}{
							"type":        "string",
							"description": "Bash command to execute",
						},
						"timeout": map[string]interface{}{
							"type":        "integer",
							"description": "Timeout in seconds (default: 30)",
						},
					},
					"required": []string{"command"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFuncDef{
				Name:        "terminal_start",
				Description: "Start a persistent interactive terminal session attached to a real PTY. Use this for terminal apps, REPLs, editors, TUIs, and long-running commands that need follow-up input.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"command": map[string]interface{}{
							"type":        "string",
							"description": "Command to run inside bash -c",
						},
						"width": map[string]interface{}{
							"type":        "integer",
							"description": "Terminal width in cells (default 100)",
						},
						"height": map[string]interface{}{
							"type":        "integer",
							"description": "Terminal height in cells (default 30)",
						},
						"delay_ms": map[string]interface{}{
							"type":        "integer",
							"description": "Milliseconds to wait before taking the initial screen snapshot (default 200)",
						},
					},
					"required": []string{"command"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFuncDef{
				Name:        "terminal_send",
				Description: "Send raw text/input to a persistent terminal session, then return a fresh screen snapshot. Prefer terminal_keys for control keys like Escape, Ctrl+C, arrows, and Enter.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Terminal session id returned by terminal_start",
						},
						"text": map[string]interface{}{
							"type":        "string",
							"description": "Raw text to send to the terminal session",
						},
						"delay_ms": map[string]interface{}{
							"type":        "integer",
							"description": "Milliseconds to wait before taking the screen snapshot (default 100)",
						},
					},
					"required": []string{"id", "text"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFuncDef{
				Name:        "terminal_keys",
				Description: "Send named keys or literal text chunks to a persistent terminal session, then return a fresh screen snapshot. Use this for editors and TUIs. Named keys include esc, enter, tab, backspace, delete, up, down, left, right, home, end, pageup, pagedown, ctrl-a through ctrl-z. Unknown entries are sent as literal text, so [\"esc\",\":q!\",\"enter\"] quits vim.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Terminal session id returned by terminal_start",
						},
						"keys": map[string]interface{}{
							"type":        "array",
							"description": "Named keys or literal text chunks to send in order",
							"items": map[string]interface{}{
								"type": "string",
							},
						},
						"delay_ms": map[string]interface{}{
							"type":        "integer",
							"description": "Milliseconds to wait before taking the screen snapshot (default 100)",
						},
					},
					"required": []string{"id", "keys"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFuncDef{
				Name:        "terminal_snapshot",
				Description: "Return the current screen contents and status for a persistent terminal session.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Terminal session id returned by terminal_start",
						},
						"ansi": map[string]interface{}{
							"type":        "boolean",
							"description": "Return ANSI styled screen output instead of plain text (default false)",
						},
					},
					"required": []string{"id"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFuncDef{
				Name:        "terminal_wait",
				Description: "Wait until a persistent terminal session screen contains text, exits, or times out. Use this instead of guessing sleeps after starting REPLs, editors, servers, and TUIs.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Terminal session id returned by terminal_start",
						},
						"text": map[string]interface{}{
							"type":        "string",
							"description": "Text to wait for in the terminal screen",
						},
						"timeout_ms": map[string]interface{}{
							"type":        "integer",
							"description": "Maximum milliseconds to wait (default 5000)",
						},
						"interval_ms": map[string]interface{}{
							"type":        "integer",
							"description": "Polling interval in milliseconds (default 100)",
						},
						"case_sensitive": map[string]interface{}{
							"type":        "boolean",
							"description": "Use case-sensitive matching (default false)",
						},
					},
					"required": []string{"id", "text"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFuncDef{
				Name:        "terminal_resize",
				Description: "Resize a persistent terminal session PTY and virtual screen, then return a fresh screen snapshot.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Terminal session id returned by terminal_start",
						},
						"width": map[string]interface{}{
							"type":        "integer",
							"description": "New terminal width in cells",
						},
						"height": map[string]interface{}{
							"type":        "integer",
							"description": "New terminal height in cells",
						},
						"delay_ms": map[string]interface{}{
							"type":        "integer",
							"description": "Milliseconds to wait before taking the screen snapshot (default 100)",
						},
					},
					"required": []string{"id", "width", "height"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFuncDef{
				Name:        "terminal_close",
				Description: "Close a persistent terminal session and kill its process if still running.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Terminal session id returned by terminal_start",
						},
					},
					"required": []string{"id"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFuncDef{
				Name:        "preview_image",
				Description: "Render an image file into terminal-friendly ANSI block art for screenshot and visual debugging. Supports PNG, JPEG, and GIF.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Path to the image file to preview",
						},
						"width": map[string]interface{}{
							"type":        "integer",
							"description": "Output width in terminal cells (default 80)",
						},
						"height": map[string]interface{}{
							"type":        "integer",
							"description": "Output height in terminal cells (default preserves aspect ratio)",
						},
					},
					"required": []string{"path"},
				},
			},
		},
	}
}

// Execute runs a tool and returns the result.
//
// Pattern 1 (structured errors): when a tool returns an error, Execute converts
// it to a <tool_call_error> XML result string rather than propagating it as a
// Go error. The model sees both the cause and a self-correction reflection
// prompt. The only Go errors that still propagate are pre-dispatch problems
// (context cancellation, unknown tool name) that the caller must handle.
//
// Pattern 4 (per-tool timeout): every tool that doesn't manage its own timeout
// (bash manages its own) is wrapped in context.WithTimeout(r.ToolTimeout).
// Future agent-delegation tools should be added to the bypass list here.
func (r *Registry) Execute(ctx context.Context, name string, argsJSON string) (string, error) {
	// Early cancellation check so tools that don't use context internally
	// (read, write, edit, terminal_send, etc.) still respond to CancelTool
	// instead of running to completion.
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("tool cancelled: %w", err)
	}

	// Pattern 4: apply per-tool timeout to all tools except bash (which
	// manages its own timeout via the `timeout` arg). Future agent-delegation
	// tools that spawn long sub-agents should also bypass this wrapper.
	toolCtx := ctx
	var toolCancel context.CancelFunc
	switch name {
	case "bash":
		// bash manages its own deadline internally; wrapping would race.
	default:
		toolCtx, toolCancel = context.WithTimeout(ctx, r.ToolTimeout)
		defer toolCancel()
	}

	var result string
	var err error

	switch name {
	case "read":
		result, err = r.execRead(argsJSON)
	case "write":
		result, err = r.execWrite(argsJSON)
	case "edit":
		result, err = r.execEdit(argsJSON)
	case "search_memory":
		result, err = r.execSearchMemory(argsJSON)
	case "write_memory":
		result, err = r.execWriteMemory(argsJSON)
	case "bash":
		result, err = r.execBash(ctx, argsJSON) // bash uses its own ctx
	case "terminal_start":
		result, err = r.terminals.Start(toolCtx, argsJSON)
	case "terminal_send":
		result, err = r.terminals.Send(toolCtx, argsJSON)
	case "terminal_keys":
		result, err = r.terminals.Keys(toolCtx, argsJSON)
	case "terminal_snapshot":
		result, err = r.terminals.Snapshot(toolCtx, argsJSON)
	case "terminal_wait":
		result, err = r.terminals.Wait(toolCtx, argsJSON)
	case "terminal_resize":
		result, err = r.terminals.Resize(toolCtx, argsJSON)
	case "terminal_close":
		result, err = r.terminals.Close(toolCtx, argsJSON)
	case "preview_image":
		result, err = r.execPreviewImage(argsJSON)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}

	// Check timeout expiry after the tool returns.
	if err == nil && toolCancel != nil {
		if toolCtx.Err() != nil {
			err = fmt.Errorf("tool %s exceeded %s timeout — consider breaking into smaller operations", name, r.ToolTimeout)
		}
	}
	if err == nil && result == "" && toolCancel != nil && toolCtx.Err() != nil {
		err = fmt.Errorf("tool %s exceeded %s timeout — consider breaking into smaller operations", name, r.ToolTimeout)
	}

	// Pattern 4: convert timeout error if tool returned an error AND context
	// deadline was exceeded.
	if err != nil && toolCancel != nil && errors.Is(toolCtx.Err(), context.DeadlineExceeded) {
		err = fmt.Errorf("tool %s exceeded %s timeout — consider breaking into smaller operations", name, r.ToolTimeout)
	}

	// Pattern 1: convert all tool-level errors to structured XML results.
	if err != nil {
		return wrapToolError(err), nil
	}
	return result, nil
}

func (r *Registry) resolvePath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(r.WorkDir, p)
}

func (r *Registry) execRead(argsJSON string) (string, error) {
	var args struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}

	path := r.resolvePath(args.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", args.Path, err)
	}

	// Pattern 2: mark file read so execEdit/execWrite can guard on it.
	r.markFileRead(path)

	content := string(data)

	// Apply offset/limit if specified
	if args.Offset > 0 || args.Limit > 0 {
		lines := strings.Split(content, "\n")
		start := 0
		if args.Offset > 0 {
			start = args.Offset - 1 // 1-indexed
		}
		if start >= len(lines) {
			return "", fmt.Errorf("read: offset %d is past end of file (file has %d lines)", args.Offset, len(lines))
		}
		end := len(lines)
		if args.Limit > 0 && start+args.Limit < end {
			end = start + args.Limit
		}
		content = strings.Join(lines[start:end], "\n")
	}

	// Pattern 3: head+tail truncation with overflow temp file pointer.
	const maxSize = 50 * 1024
	if len(content) > maxSize {
		truncated, overflowPath, oErr := r.truncateWithOverflow(content, maxSize)
		if oErr != nil {
			// Fall back to simple truncation on temp-file error.
			content = truncateUTF8(content, maxSize) + "\n... (truncated)"
		} else if overflowPath != "" {
			content = truncated + "\n[TRUNCATED — full output at " + overflowPath + "; use read tool to view specific line ranges]"
		} else {
			content = truncated
		}
	}

	return content, nil
}

func (r *Registry) execWrite(argsJSON string) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}

	path := r.resolvePath(args.Path)

	// Pattern 2: read-before-edit guard for EXISTING files.
	// New-file writes are always allowed; the guard only fires when the file
	// already exists and was not read in the current turn.
	if _, statErr := os.Stat(path); statErr == nil {
		if !r.wasFileRead(path) {
			return "", fmt.Errorf("must read %s in this turn before overwriting it", args.Path)
		}
	}

	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}

	if err := os.WriteFile(path, []byte(args.Content), 0644); err != nil {
		return "", fmt.Errorf("write %s: %w", args.Path, err)
	}

	return fmt.Sprintf("Wrote %d bytes to %s", len(args.Content), path), nil
}

func (r *Registry) execEdit(argsJSON string) (string, error) {
	var args struct {
		Path  string `json:"path"`
		Edits []struct {
			OldText string `json:"oldText"`
			NewText string `json:"newText"`
		} `json:"edits"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}

	path := r.resolvePath(args.Path)

	// Pattern 2: read-before-edit guard.
	if !r.wasFileRead(path) {
		return "", fmt.Errorf("must read %s in this turn before editing it", args.Path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", args.Path, err)
	}

	content := string(data)
	applied := 0

	for _, edit := range args.Edits {
		if edit.OldText == "" {
			return "", fmt.Errorf("edit: oldText must not be empty (use the write tool to create a new file or replace its contents)")
		}
		if !strings.Contains(content, edit.OldText) {
			return "", fmt.Errorf("oldText not found in %s: %q", args.Path, truncate(edit.OldText, 80))
		}
		count := strings.Count(content, edit.OldText)
		if count > 1 {
			return "", fmt.Errorf("oldText matches %d times in %s (must be unique): %q", count, args.Path, truncate(edit.OldText, 80))
		}
		content = strings.Replace(content, edit.OldText, edit.NewText, 1)
		applied++
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write %s: %w", args.Path, err)
	}

	return fmt.Sprintf("Applied %d edit(s) to %s", applied, path), nil
}

func (r *Registry) execSearchMemory(argsJSON string) (string, error) {
	var args struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}

	results, err := memorypkg.SearchInScope(r.SessionDir, r.WorkDir, r.Scope, args.Query, args.Limit)
	if err != nil {
		return "", err
	}

	return memorypkg.RenderSearchResults(args.Query, results), nil
}

func (r *Registry) execWriteMemory(argsJSON string) (string, error) {
	var args struct {
		Content string `json:"content"`
		Title   string `json:"title"`
		Scope   string `json:"scope"`
		Name    string `json:"name"`
		Daily   bool   `json:"daily"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	if strings.TrimSpace(args.Content) == "" {
		return "", fmt.Errorf("content is required")
	}

	scope := r.Scope
	switch strings.ToLower(strings.TrimSpace(args.Scope)) {
	case "", "current":
		// keep r.Scope
	case "root":
		scope = memorypkg.RootScope()
	case "channel":
		if strings.TrimSpace(args.Name) == "" {
			return "", fmt.Errorf("name is required when scope='channel'")
		}
		root := memorypkg.RootScope()
		scope = memorypkg.ChannelScope(args.Name, &root)
	case "query":
		if strings.TrimSpace(args.Name) == "" {
			return "", fmt.Errorf("name is required when scope='query'")
		}
		root := memorypkg.RootScope()
		scope = memorypkg.QueryScope(args.Name, &root)
	default:
		return "", fmt.Errorf("unknown scope %q (want 'current', 'root', 'channel', or 'query')", args.Scope)
	}

	now := time.Now()
	if args.Daily {
		body := args.Content
		if t := strings.TrimSpace(args.Title); t != "" {
			body = "### " + t + "\n\n" + body
		}
		if err := memorypkg.AppendDailyForScope(r.SessionDir, r.WorkDir, scope, now, memorypkg.SourceToolWrite, body); err != nil {
			return "", err
		}
		return fmt.Sprintf("Appended %d bytes to daily memory (%s)", len(args.Content), memorypkg.DailyPathForScope(r.SessionDir, r.WorkDir, scope, now)), nil
	}

	if err := memorypkg.AppendHot(r.SessionDir, r.WorkDir, scope, now, args.Title, args.Content); err != nil {
		return "", err
	}
	return fmt.Sprintf("Wrote %d bytes to %s", len(args.Content), memorypkg.HotPath(r.SessionDir, r.WorkDir, scope)), nil
}

func (r *Registry) execBash(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}

	timeout := 30
	if args.Timeout > 0 {
		timeout = args.Timeout
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", args.Command)
	cmd.Dir = r.WorkDir

	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	output := out.String()

	// Pattern 3: head+tail truncation with overflow temp file pointer.
	const maxSize = 50 * 1024
	if len(output) > maxSize {
		truncated, overflowPath, oErr := r.truncateWithOverflow(output, maxSize)
		if oErr != nil {
			output = truncateUTF8(output, maxSize) + "\n... (truncated)"
		} else if overflowPath != "" {
			output = truncated + "\n[TRUNCATED — full output at " + overflowPath + "; use read tool to view specific line ranges]"
		} else {
			output = truncated
		}
	}

	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return output, fmt.Errorf("command timed out after %ds", timeout)
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return output, fmt.Errorf("command cancelled")
		}
		if cmd.ProcessState == nil {
			return output, fmt.Errorf("failed to start command: %w", err)
		}
		return output + "\nExit code: " + strconv.Itoa(cmd.ProcessState.ExitCode()), nil
	}

	return output, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// truncateUTF8 returns s truncated to at most maxBytes bytes, walking back
// to a rune boundary so multi-byte characters are not split mid-encoding.
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	// utf8.RuneStart returns true for ASCII bytes and the leading byte of a
	// multi-byte sequence; walk back until we find one.
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// truncateWithOverflow implements Pattern 3 (head+tail truncation with overflow
// temp file pointer). When content fits under maxBytes the original is returned
// unchanged. When it overflows:
//   - keeps first maxBytes/2 bytes (UTF-8 safe) as head
//   - keeps last maxBytes/2 bytes (UTF-8 safe) as tail
//   - writes the FULL original to a temp file under SessionDir/cache
//   - returns head + separator + tail, the temp path, and nil error
//
// Overflow files live under SessionDir/cache and are cleaned up
// when the session is torn down.
func (r *Registry) truncateWithOverflow(content string, maxBytes int) (truncated string, overflowPath string, err error) {
	if len(content) <= maxBytes {
		return content, "", nil
	}

	half := maxBytes / 2

	// UTF-8-safe head: walk back to a rune boundary.
	headEnd := half
	for headEnd > 0 && !utf8.RuneStart(content[headEnd]) {
		headEnd--
	}
	head := content[:headEnd]

	// UTF-8-safe tail: walk forward from (len-half) to a rune start.
	tailStart := len(content) - half
	if tailStart < 0 {
		tailStart = 0
	}
	for tailStart < len(content) && !utf8.RuneStart(content[tailStart]) {
		tailStart++
	}
	tail := content[tailStart:]

	// Write the full original to a temp file in ~/.bitchtea/cache.
	// os.CreateTemp with dir="" uses os.TempDir(); we own the path instead so
	// overflow files are scoped to the session and cleaned on session end.
	cacheDir := filepath.Join(r.SessionDir, "cache")
	if mkerr := os.MkdirAll(cacheDir, 0755); mkerr != nil {
		return "", "", mkerr
	}
	f, ferr := os.CreateTemp(cacheDir, "overflow-*.txt")
	if ferr != nil {
		return "", "", ferr
	}
	if _, werr := f.WriteString(content); werr != nil {
		f.Close()
		os.Remove(f.Name())
		return "", "", werr
	}
	if cerr := f.Close(); cerr != nil {
		os.Remove(f.Name())
		return "", "", cerr
	}

	sep := "\n... [" + strconv.Itoa(len(content)) + " bytes total; middle omitted] ...\n"
	return head + sep + tail, f.Name(), nil
}
