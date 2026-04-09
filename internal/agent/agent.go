package agent

import (
	"context"
	"errors"
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
	streamer llm.ChatStreamer
	tools    *tools.Registry
	config   *config.Config
	messages []llm.Message

	bootstrapMsgCount int

	// Memory scope for the active IRC context.
	scope         MemoryScope
	injectedPaths map[string]bool // HOT paths already injected as context messages

	// Stats
	TurnCount   int
	ToolCalls   map[string]int // tool name -> call count
	StartTime   time.Time
	CostTracker *llm.CostTracker

	lastTurnState turnState
}

type turnState int

const (
	turnStateIdle turnState = iota
	turnStateCompleted
	turnStateErrored
	turnStateCanceled
)

// FollowUpRequest is an agent-authored autonomous continuation prompt.
type FollowUpRequest struct {
	Label  string
	Prompt string
}

// NewAgent creates a new agent
func NewAgent(cfg *config.Config) *Agent {
	return NewAgentWithStreamer(cfg, nil)
}

// NewAgentWithStreamer creates a new agent with an injectable chat streamer.
func NewAgentWithStreamer(cfg *config.Config, streamer llm.ChatStreamer) *Agent {
	client := llm.NewClient(cfg.APIKey, cfg.BaseURL, cfg.Model, cfg.Provider)
	if streamer == nil {
		streamer = client
	}

	// System prompt
	systemPrompt := buildSystemPrompt(cfg)

	a := &Agent{
		client:        client,
		streamer:      streamer,
		tools:         tools.NewRegistry(cfg.WorkDir, cfg.SessionDir),
		config:        cfg,
		messages:      []llm.Message{{Role: "system", Content: systemPrompt}},
		scope:         RootMemoryScope(),
		injectedPaths: make(map[string]bool),
		ToolCalls:     make(map[string]int),
		StartTime:     time.Now(),
		CostTracker:   llm.NewCostTracker(),
	}

	// Inject context files (AGENTS.md etc.)
	contextFiles := DiscoverContextFiles(cfg.WorkDir)
	if contextFiles != "" {
		a.messages = append(a.messages, llm.Message{
			Role:    "user",
			Content: "Here are the project context files:\n\n" + contextFiles,
		})
		a.messages = append(a.messages, llm.Message{
			Role:    "assistant",
			Content: "Got it. I've read the project context and will follow those conventions.",
		})
	}

	// Inject root MEMORY.md at bootstrap. Scoped HOT.md is injected lazily
	// via SetScope when the first turn begins in a non-root context.
	memory := LoadMemory(cfg.WorkDir)
	if memory != "" {
		a.messages = append(a.messages, llm.Message{
			Role:    "user",
			Content: "Here is the session memory from previous work:\n\n" + memory,
		})
		a.messages = append(a.messages, llm.Message{
			Role:    "assistant",
			Content: "Noted. I'll use this context going forward.",
		})
	}

	a.bootstrapMsgCount = len(a.messages)

	return a
}

// SendMessage processes a user message through the agent loop
// Events are sent to the channel as they happen
func (a *Agent) SendMessage(ctx context.Context, userMsg string, events chan<- Event) {
	defer close(events)

	// Expand @file references
	expanded := ExpandFileRefs(userMsg, a.config.WorkDir)

	// Add user message
	a.messages = append(a.messages, llm.Message{Role: "user", Content: expanded})
	a.TurnCount++

	// Agent loop - keeps going while there are tool calls
	for {
		events <- Event{Type: "state", State: StateThinking}
		estimatedInputTokens := a.EstimateTokens()

		// Stream LLM response
		streamEvents := make(chan llm.StreamEvent, 100)
		go a.streamer.StreamChat(ctx, a.messages, a.tools.Definitions(), streamEvents)

		var textAccum strings.Builder
		var toolCalls []llm.ToolCall
		var usage llm.TokenUsage
		var gotUsage bool

		for ev := range streamEvents {
			switch ev.Type {
			case "text":
				textAccum.WriteString(ev.Text)
				events <- Event{Type: "text", Text: ev.Text}

			case "usage":
				if ev.Usage != nil {
					usage = *ev.Usage
					gotUsage = true
				}

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
				if errors.Is(ev.Error, context.Canceled) {
					a.lastTurnState = turnStateCanceled
				} else {
					a.lastTurnState = turnStateErrored
				}
				events <- Event{Type: "state", State: StateIdle}
				events <- Event{Type: "error", Error: ev.Error}
				events <- Event{Type: "done"}
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

		if gotUsage {
			a.CostTracker.AddTokenUsage(usage)
		} else {
			a.CostTracker.AddUsage(estimatedInputTokens, textAccum.Len()/4)
		}

		// If no tool calls, we're done (maybe auto-next)
		if len(toolCalls) == 0 {
			a.lastTurnState = turnStateCompleted
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
					Type:       "tool_result",
					ToolName:   tc.Function.Name,
					ToolResult: result,
					ToolError:  err,
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
	sb.WriteString("You have access to tools: read, write, edit, search_memory, bash.\n")
	sb.WriteString("Working directory: " + cfg.WorkDir + "\n")
	sb.WriteString("\nRules:\n")
	sb.WriteString("- Use read to examine files before editing them\n")
	sb.WriteString("- Use edit for precise changes with exact text matching\n")
	sb.WriteString("- Use write for new files or complete rewrites\n")
	sb.WriteString("- Use search_memory when the user asks about prior decisions, history, or anything you cannot justify from current context alone\n")
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

// SetBaseURL changes the API base URL
func (a *Agent) SetBaseURL(url string) {
	a.config.BaseURL = url
	a.client.BaseURL = url
}

// SetAPIKey changes the API key
func (a *Agent) SetAPIKey(key string) {
	a.config.APIKey = key
	a.client.APIKey = key
}

// SetProvider changes the LLM provider
func (a *Agent) SetProvider(provider string) {
	a.config.Provider = provider
	a.client.Provider = provider
}

// SetDebugHook sets a debug callback on the underlying LLM client
func (a *Agent) SetDebugHook(hook func(llm.DebugInfo)) {
	a.client.DebugHook = hook
}

// Config returns the current config (for profile save)
func (a *Agent) Config() *config.Config {
	return a.config
}

// EstimateTokens returns a rough token count (chars / 4)
func (a *Agent) EstimateTokens() int {
	total := 0
	for _, m := range a.messages {
		total += len(m.Content)
	}
	return total / 4
}

// Compact summarizes the conversation to reduce context size.
// Keeps the system prompt and last N messages, replaces the middle with a summary.
func (a *Agent) Compact(ctx context.Context) error {
	if len(a.messages) < 6 {
		return nil // nothing to compact
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	end := len(a.messages) - 4
	if err := a.flushCompactedMessagesToDailyMemory(ctx, a.messages[1:end]); err != nil {
		return err
	}

	// Build a summary request
	var sb strings.Builder
	sb.WriteString("Summarize the following conversation concisely, preserving all important ")
	sb.WriteString("technical details, decisions made, files modified, and current state:\n\n")

	// Everything except system prompt and last 4 messages
	for _, m := range a.messages[1:end] {
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", m.Role, truncateStr(m.Content, 500)))
	}

	summaryMsgs := []llm.Message{
		{Role: "user", Content: sb.String()},
	}

	events := make(chan llm.StreamEvent, 100)
	go a.streamer.StreamChat(ctx, summaryMsgs, nil, events)

	var summary strings.Builder
	for ev := range events {
		if err := ctx.Err(); err != nil {
			return err
		}
		if ev.Type == "text" {
			summary.WriteString(ev.Text)
		}
	}

	// Rebuild messages: system + summary + last 4
	keep := a.messages[end:]
	a.messages = []llm.Message{
		a.messages[0], // system prompt
		{Role: "user", Content: "[Previous conversation summary]:\n" + summary.String()},
		{Role: "assistant", Content: "Got it, I have the context from the summary."},
	}
	a.messages = append(a.messages, keep...)

	return nil
}

func (a *Agent) flushCompactedMessagesToDailyMemory(ctx context.Context, messages []llm.Message) error {
	if len(messages) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	var sb strings.Builder
	sb.WriteString("Extract durable memory from this conversation slice before it is compacted.\n")
	sb.WriteString("Return concise markdown bullets covering only lasting facts: user preferences, decisions, completed work, relevant files, and open follow-ups.\n")
	sb.WriteString("Skip transient chatter and tool noise. If nothing deserves durable memory, reply with exactly NONE.\n\n")
	for _, m := range messages {
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", m.Role, truncateStr(m.Content, 700)))
	}

	streamEvents := make(chan llm.StreamEvent, 100)
	go a.streamer.StreamChat(ctx, []llm.Message{{Role: "user", Content: sb.String()}}, nil, streamEvents)

	var summary strings.Builder
	for ev := range streamEvents {
		if err := ctx.Err(); err != nil {
			return err
		}
		if ev.Type == "text" {
			summary.WriteString(ev.Text)
		}
	}

	text := strings.TrimSpace(summary.String())
	if text == "" || strings.EqualFold(text, "none") {
		return nil
	}

	return AppendScopedDailyMemory(a.config.SessionDir, a.config.WorkDir, a.scope, time.Now(), text)
}

// AutoNextPrompt returns the auto-next-steps prompt
func AutoNextPrompt() string {
	return "What are the next steps? If there are remaining tasks, continue working on them. " +
		"If everything is done, say so clearly."
}

// AutoIdeaPrompt returns the auto-next-idea prompt
func AutoIdeaPrompt() string {
	return "Based on what you've done so far, what improvements or optimizations would you suggest? " +
		"Pick the most impactful one and implement it."
}

// MaybeQueueFollowUp returns an autonomous continuation prompt derived from the
// last completed assistant turn. Failed or canceled turns never queue follow-up
// work because they need an explicit user decision first.
func (a *Agent) MaybeQueueFollowUp() *FollowUpRequest {
	if a.lastTurnState != turnStateCompleted {
		return nil
	}
	if !a.config.AutoNextSteps && !a.config.AutoNextIdea {
		return nil
	}

	summary := truncateStr(compactWhitespace(a.lastAssistantContent()), 1200)
	if summary == "" {
		summary = "No assistant summary was captured for the previous turn."
	}

	req := &FollowUpRequest{}
	switch {
	case a.config.AutoNextSteps && a.config.AutoNextIdea:
		req.Label = "auto-next"
		req.Prompt = "Continue from your most recent completed turn.\n\n" +
			"Last assistant summary:\n" + summary + "\n\n" +
			"Identify the highest-impact remaining task or improvement, implement it now, " +
			"and keep going until the work is actually complete. If everything is already done, say so clearly."
	case a.config.AutoNextSteps:
		req.Label = "auto-next-steps"
		req.Prompt = "Continue from your most recent completed turn.\n\n" +
			"Last assistant summary:\n" + summary + "\n\n" +
			"Resume the highest-priority remaining work. If everything is already done, say so clearly."
	case a.config.AutoNextIdea:
		req.Label = "auto-next-idea"
		req.Prompt = "Continue from your most recent completed turn.\n\n" +
			"Last assistant summary:\n" + summary + "\n\n" +
			"Pick the most impactful improvement suggested by this summary and implement it. " +
			"If there is nothing worthwhile left, say so clearly."
	default:
		return nil
	}

	return req
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func compactWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func (a *Agent) lastAssistantContent() string {
	for i := len(a.messages) - 1; i >= 0; i-- {
		if a.messages[i].Role != "assistant" {
			continue
		}
		content := strings.TrimSpace(a.messages[i].Content)
		if content != "" {
			return content
		}
	}
	return ""
}

// SetScope updates the active memory scope. If the scoped HOT.md is non-empty
// and has not been injected yet, it is added to the conversation as context.
func (a *Agent) SetScope(scope MemoryScope) {
	a.scope = scope
	a.tools.SetScope(scope)

	hot := LoadScopedMemory(a.config.SessionDir, a.config.WorkDir, scope)
	if hot == "" {
		return
	}
	path := ScopedHotMemoryPath(a.config.SessionDir, a.config.WorkDir, scope)
	if a.injectedPaths[path] {
		return
	}
	a.injectedPaths[path] = true
	a.messages = append(a.messages,
		llm.Message{Role: "user", Content: "Context memory for " + scopeLabel(scope) + ":\n\n" + hot},
		llm.Message{Role: "assistant", Content: "Got it."},
	)
}

func scopeLabel(scope MemoryScope) string {
	switch scope.Kind {
	case MemoryScopeChannel:
		return "#" + scope.Name
	case MemoryScopeQuery:
		return scope.Name
	default:
		return "root"
	}
}

// Messages returns the current message history (for session saving)
func (a *Agent) Messages() []llm.Message {
	return a.messages
}

// BootstrapMessageCount returns how many startup-injected messages should be
// hidden from the normal transcript when persisted.
func (a *Agent) BootstrapMessageCount() int {
	return a.bootstrapMsgCount
}

// InjectNote adds a synthetic context note to the conversation history without
// running the agent loop. Used for catch-up on channel invite so the agent
// is aware of the invited persona and the prior conversation.
func (a *Agent) InjectNote(note string) {
	a.messages = append(a.messages,
		llm.Message{Role: "user", Content: note},
		llm.Message{Role: "assistant", Content: "Understood."},
	)
}

// RestoreMessages replaces the current message history with a prior session.
// It resets session-local stats so that counters and timing start fresh.
func (a *Agent) RestoreMessages(messages []llm.Message) {
	a.messages = append([]llm.Message(nil), messages...)
	if len(a.messages) == 0 || a.messages[0].Role != "system" {
		a.messages = append([]llm.Message{{
			Role:    "system",
			Content: buildSystemPrompt(a.config),
		}}, a.messages...)
	}

	// Reset session-local stats so resume starts with clean counters
	a.bootstrapMsgCount = 0
	a.TurnCount = 0
	a.ToolCalls = make(map[string]int)
	a.CostTracker = llm.NewCostTracker()
	a.StartTime = time.Now()
	a.lastTurnState = turnStateIdle
}

// Elapsed returns time since agent creation
func (a *Agent) Elapsed() time.Duration {
	return time.Since(a.StartTime)
}

// Cost returns the estimated cost in USD using the cost tracker
func (a *Agent) Cost() float64 {
	return a.CostTracker.EstimateCost(a.config.Model)
}
