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
	"time"
	"unicode/utf8"

	memorypkg "github.com/jstamagal/bitchtea/internal/memory"
)

// Registry holds all available tools and their definitions
type Registry struct {
	WorkDir    string
	SessionDir string
	Scope      memorypkg.Scope
	terminals  *terminalManager
}

// NewRegistry creates a tool registry
func NewRegistry(workDir, sessionDir string) *Registry {
	return &Registry{
		WorkDir:    workDir,
		SessionDir: sessionDir,
		terminals:  newTerminalManager(workDir),
	}
}

// SetScope updates the memory scope used for search_memory queries.
func (r *Registry) SetScope(scope memorypkg.Scope) {
	r.Scope = scope
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

// Execute runs a tool and returns the result
func (r *Registry) Execute(ctx context.Context, name string, argsJSON string) (string, error) {
	// Early cancellation check so tools that don't use context internally
	// (read, write, edit, terminal_send, etc.) still respond to CancelTool
	// instead of running to completion.
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("tool cancelled: %w", err)
	}

	switch name {
	case "read":
		return r.execRead(argsJSON)
	case "write":
		return r.execWrite(argsJSON)
	case "edit":
		return r.execEdit(argsJSON)
	case "search_memory":
		return r.execSearchMemory(argsJSON)
	case "write_memory":
		return r.execWriteMemory(argsJSON)
	case "bash":
		return r.execBash(ctx, argsJSON)
	case "terminal_start":
		return r.terminals.Start(ctx, argsJSON)
	case "terminal_send":
		return r.terminals.Send(argsJSON)
	case "terminal_keys":
		return r.terminals.Keys(argsJSON)
	case "terminal_snapshot":
		return r.terminals.Snapshot(argsJSON)
	case "terminal_wait":
		return r.terminals.Wait(argsJSON)
	case "terminal_resize":
		return r.terminals.Resize(argsJSON)
	case "terminal_close":
		return r.terminals.Close(argsJSON)
	case "preview_image":
		return r.execPreviewImage(argsJSON)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
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

	// Truncate if too large
	const maxSize = 50 * 1024
	if len(content) > maxSize {
		content = truncateUTF8(content, maxSize) + "\n... (truncated)"
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

	// Truncate
	const maxSize = 50 * 1024
	if len(output) > maxSize {
		output = truncateUTF8(output, maxSize) + "\n... (truncated)"
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
