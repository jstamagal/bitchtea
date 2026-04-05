package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/llm"
	"github.com/jstamagal/bitchtea/internal/tools"
)

// State represents the agent's current state
type State int

const (
	StateIdle     State = iota
	StateThinking       // waiting for LLM response
	StateToolCall       // executing a tool
)

// Event is emitted by the agent to update the UI
type Event struct {
	Type string // "text", "tool_start", "tool_result", "thinking", "done", "error", "state"

	Text string // for text events (streamed tokens)

	ToolName   string // for tool events
	ToolArgs   string
	ToolResult string
	ToolError  error

	State State // for state events
	Error error // for error events
}

// Agent manages the conversation loop
type Agent struct {
	client   *llm.Client
	tools    *tools.Registry
	config   *config.Config
	messages []llm.Message

	// Stats
	TotalTokens int
	ToolCalls   map[string]int // tool name -> call count
	StartTime   time.Time
}

// NewAgent creates a new agent
func NewAgent(cfg *config.Config) *Agent {
	client := llm.NewClient(cfg.APIKey, cfg.BaseURL, cfg.Model)

	// System prompt
	systemPrompt := buildSystemPrompt(cfg)

	return &Agent{
		client: client,
		tools:  tools.NewRegistry(cfg.WorkDir),
		config: cfg,
		messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
		},
		ToolCalls: make(map[string]int),
		StartTime: time.Now(),
	}
}

// SendMessage processes a user message through the agent loop
// Events are sent to the channel as they happen
func (a *Agent) SendMessage(ctx context.Context, userMsg string, events chan<- Event) {
	defer close(events)

	// Add user message
	a.messages = append(a.messages, llm.Message{Role: "user", Content: userMsg})

	// Agent loop - keeps going while there are tool calls
	for {
		events <- Event{Type: "state", State: StateThinking}

		// Stream LLM response
		streamEvents := make(chan llm.StreamEvent, 100)
		go a.client.StreamChat(ctx, a.messages, a.tools.Definitions(), streamEvents)

		var textAccum strings.Builder
		var toolCalls []llm.ToolCall

		for ev := range streamEvents {
			switch ev.Type {
			case "text":
				textAccum.WriteString(ev.Text)
				events <- Event{Type: "text", Text: ev.Text}

			case "tool_call":
				toolCalls = append(toolCalls, llm.ToolCall{
					ID:   ev.ToolCallID,
					Type: "function",
					Function: llm.FunctionCall{
						Name:      ev.ToolName,
						Arguments: ev.ToolArgs,
					},
				})

			case "error":
				events <- Event{Type: "error", Error: ev.Error}
				return

			case "done":
				// handled below
			}
		}

		// Add assistant message to history
		assistantMsg := llm.Message{
			Role:    "assistant",
			Content: textAccum.String(),
		}
		if len(toolCalls) > 0 {
			assistantMsg.ToolCalls = toolCalls
		}
		a.messages = append(a.messages, assistantMsg)

		// If no tool calls, we're done
		if len(toolCalls) == 0 {
			events <- Event{Type: "state", State: StateIdle}
			events <- Event{Type: "done"}
			return
		}

		// Execute tool calls
		events <- Event{Type: "state", State: StateToolCall}

		for _, tc := range toolCalls {
			a.ToolCalls[tc.Function.Name]++

			events <- Event{
				Type:     "tool_start",
				ToolName: tc.Function.Name,
				ToolArgs: tc.Function.Arguments,
			}

			result, err := a.tools.Execute(ctx, tc.Function.Name, tc.Function.Arguments)

			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
				events <- Event{
					Type:      "tool_result",
					ToolName:  tc.Function.Name,
					ToolResult: result,
					ToolError: err,
				}
			} else {
				events <- Event{
					Type:       "tool_result",
					ToolName:   tc.Function.Name,
					ToolResult: result,
				}
			}

			// Add tool result to messages
			a.messages = append(a.messages, llm.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}

		// Loop back to get the next LLM response (agent loop continues)
	}
}

func buildSystemPrompt(cfg *config.Config) string {
	var sb strings.Builder
	sb.WriteString("You are bitchtea, an agentic coding assistant running in a terminal.\n")
	sb.WriteString("You have access to tools: read, write, edit, bash.\n")
	sb.WriteString("Working directory: " + cfg.WorkDir + "\n")
	sb.WriteString("\nRules:\n")
	sb.WriteString("- Use read to examine files before editing them\n")
	sb.WriteString("- Use edit for precise changes with exact text matching\n")
	sb.WriteString("- Use write for new files or complete rewrites\n")
	sb.WriteString("- Use bash for running commands, file operations like ls/find/grep\n")
	sb.WriteString("- Be direct. No fluff. Get shit done.\n")
	sb.WriteString("- When you're done with a task, say so clearly.\n")

	// Load AGENTS.md from cwd if it exists
	// This is handled at a higher level and injected into context

	return sb.String()
}

// MessageCount returns the number of messages in the conversation
func (a *Agent) MessageCount() int {
	return len(a.messages)
}

// Model returns the current model name
func (a *Agent) Model() string {
	return a.config.Model
}

// SetModel changes the model
func (a *Agent) SetModel(model string) {
	a.config.Model = model
	a.client.Model = model
}
