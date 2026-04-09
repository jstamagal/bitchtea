package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jstamagal/bitchtea/internal/llm"
	memorypkg "github.com/jstamagal/bitchtea/internal/memory"
)

// Registry holds all available tools and their definitions
type Registry struct {
	WorkDir    string
	SessionDir string
}

// NewRegistry creates a tool registry
func NewRegistry(workDir, sessionDir string) *Registry {
	return &Registry{WorkDir: workDir, SessionDir: sessionDir}
}

// Definitions returns OpenAI-compatible tool definitions
func (r *Registry) Definitions() []llm.ToolDef {
	return []llm.ToolDef{
		{
			Type: "function",
			Function: llm.ToolFuncDef{
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
			Function: llm.ToolFuncDef{
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
			Function: llm.ToolFuncDef{
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
			Function: llm.ToolFuncDef{
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
			Function: llm.ToolFuncDef{
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
	}
}

// Execute runs a tool and returns the result
func (r *Registry) Execute(ctx context.Context, name string, argsJSON string) (string, error) {
	switch name {
	case "read":
		return r.execRead(argsJSON)
	case "write":
		return r.execWrite(argsJSON)
	case "edit":
		return r.execEdit(argsJSON)
	case "search_memory":
		return r.execSearchMemory(argsJSON)
	case "bash":
		return r.execBash(ctx, argsJSON)
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
			return "", nil
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
		content = content[:maxSize] + "\n... (truncated)"
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

	return fmt.Sprintf("Wrote %d bytes to %s", len(args.Content), args.Path), nil
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

	return fmt.Sprintf("Applied %d edit(s) to %s", applied, args.Path), nil
}

func (r *Registry) execSearchMemory(argsJSON string) (string, error) {
	var args struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}

	results, err := memorypkg.Search(r.SessionDir, r.WorkDir, args.Query, args.Limit)
	if err != nil {
		return "", err
	}

	return memorypkg.RenderSearchResults(args.Query, results), nil
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
		output = output[:maxSize] + "\n... (truncated)"
	}

	if err != nil {
		if ctx.Err() != nil {
			return output, fmt.Errorf("command timed out after %ds", timeout)
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
