package llm

import (
	"context"
	"strings"
	"time"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/tools"
)

// maxAgentSteps is the StopCondition cap on a single Stream call.
const maxAgentSteps = 64

// retryBackoff is the exponential backoff sequence used when the provider
// returns a retryable error (429, 5xx, connection refused, timeout).
// 1s → 2s → 4s → 8s → 16s → 32s → 64s. After exhausting all delays the
// last error is returned.
var retryBackoff = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	16 * time.Second,
	32 * time.Second,
	64 * time.Second,
}

// StreamChat runs one full agent turn against fantasy with exponential backoff
// retry. Emits StreamEvents on events; closes events when the turn ends.
func (c *Client) StreamChat(ctx context.Context, msgs []Message, reg *tools.Registry, events chan<- StreamEvent) {
	defer close(events)

	var lastErr error
	for attempt := 0; attempt <= len(retryBackoff); attempt++ {
		if attempt > 0 {
			delay := retryBackoff[attempt-1]
			// Tell UI we're retrying so user sees what's happening.
			safeSend(ctx, events, StreamEvent{
				Type:  "text",
				Text:  "\n\n🦍 *retrying after server error… waiting " + delay.String() + "* 🦍",
			})
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				safeSend(ctx, events, StreamEvent{Type: "error", Error: ctx.Err()})
				return
			}
		}

		err := c.streamOnce(ctx, msgs, reg, events)
		if err == nil {
			return // success — "done" event already sent by streamOnce
		}
		lastErr = err

		if !isRetryable(err) {
			safeSend(ctx, events, StreamEvent{Type: "error", Error: err})
			return
		}
	}
	safeSend(ctx, events, StreamEvent{Type: "error", Error: lastErr})
}

// streamOnce performs a single StreamChat attempt. Returns nil on success
// (which means a "done" event was already sent to events), or an error.
func (c *Client) streamOnce(ctx context.Context, msgs []Message, reg *tools.Registry, events chan<- StreamEvent) error {
	model, err := c.ensureModel(ctx)
	if err != nil {
		return err
	}

	prompt, prior, systemPrompt := splitForFantasy(msgs)

	// Snapshot Service / BootstrapMsgCount under the mutex so PrepareStep
	// closes over a stable view even if a concurrent SetService runs while
	// this turn is in flight. PrepareStep itself is invoked from inside the
	// fantasy goroutine and must not take c.mu.
	c.mu.Lock()
	cacheService := c.Service
	cacheBootstrap := c.BootstrapMsgCount
	c.mu.Unlock()
	cacheBoundaryIdx := bootstrapPreparedIndex(msgs, cacheBootstrap)

	// Create a per-turn ToolContextManager so each tool call gets its own
	// cancellable context. The manager is stored on the client so the agent
	// can expose CancelTool to the UI.
	toolCtxMgr := NewToolContextManager(ctx)
	c.mu.Lock()
	c.toolCtx = toolCtxMgr
	c.mu.Unlock()
	defer func() {
		toolCtxMgr.CancelAll()
		c.mu.Lock()
		c.toolCtx = nil
		c.mu.Unlock()
	}()

	opts := []fantasy.AgentOption{
		fantasy.WithStopConditions(fantasy.StepCountIs(maxAgentSteps)),
	}
	if systemPrompt != "" {
		opts = append(opts, fantasy.WithSystemPrompt(systemPrompt))
	}
	if reg != nil {
		// Assemble the per-turn tool list. When no MCP manager is wired
		// in, MCPTools returns nil and AssembleAgentTools degrades to the
		// pre-Phase-6 behavior (translateTools(reg) only). The MCP listing
		// error is non-fatal: schema errors at the manager level drop
		// individual tools, and a fully-failed listing should still let
		// the local tool surface stay usable for this turn.
		mcpTools, _ := MCPTools(ctx, c.MCPManager())
		assembled := AssembleAgentTools(reg, mcpTools)
		opts = append(opts, fantasy.WithTools(wrapToolsWithContext(assembled, toolCtxMgr)...))
	}

	fa := fantasy.NewAgent(model, opts...)

	var streamErr error
	result, err := fa.Stream(ctx, fantasy.AgentStreamCall{
		Prompt:   prompt,
		Messages: prior,

		PrepareStep: func(stepCtx context.Context, stepOpts fantasy.PrepareStepFunctionOptions) (context.Context, fantasy.PrepareStepResult, error) {
			if sendErr := safeSend(stepCtx, events, StreamEvent{Type: "thinking"}); sendErr != nil {
				return stepCtx, fantasy.PrepareStepResult{}, sendErr
			}
			// Per docs/phase-4-preparestep.md, cache markers run after queue
			// drain and tool refresh on the final prepared.Messages shape.
			// Until the full PrepareStep abstraction (bt-p4) lands, we
			// operate on stepOpts.Messages directly — fantasy treats a nil
			// prepared.Messages as "use stepOpts.Messages unchanged", so we
			// have to substitute the slice we mutate.
			prepared := fantasy.PrepareStepResult{Messages: stepOpts.Messages}
			applyAnthropicCacheMarkers(&prepared, cacheService, cacheBoundaryIdx)
			return stepCtx, prepared, nil
		},

		OnTextDelta: func(_, text string) error {
			return safeSend(ctx, events, StreamEvent{Type: "text", Text: text})
		},

		OnReasoningDelta: func(_, text string) error {
			return safeSend(ctx, events, StreamEvent{Type: "thinking", Text: text})
		},

		OnToolCall: func(call fantasy.ToolCallContent) error {
			return safeSend(ctx, events, StreamEvent{
				Type:       "tool_call",
				ToolName:   call.ToolName,
				ToolArgs:   call.Input,
				ToolCallID: call.ToolCallID,
			})
		},

		OnToolResult: func(res fantasy.ToolResultContent) error {
			return safeSend(ctx, events, StreamEvent{
				Type:       "tool_result",
				ToolCallID: res.ToolCallID,
				ToolName:   res.ToolName,
				Text:       toolResultText(res.Result),
			})
		},

		OnStreamFinish: func(u fantasy.Usage, _ fantasy.FinishReason, _ fantasy.ProviderMetadata) error {
			usage := toLLMUsage(u)
			return safeSend(ctx, events, StreamEvent{Type: "usage", Usage: &usage})
		},

		OnError: func(err error) {
			streamErr = err
		},
	})
	if streamErr != nil {
		return streamErr
	}
	if err != nil {
		return err
	}

	rebuilt := make([]Message, 0)
	if result != nil {
		for _, step := range result.Steps {
			for _, fm := range step.Messages {
				rebuilt = append(rebuilt, fantasyToLLM(fm))
			}
		}
	}
	safeSend(ctx, events, StreamEvent{Type: "done", Messages: rebuilt})
	return nil
}

// isRetryable returns true when the error is likely transient and a retry
// after backoff has a reasonable chance of succeeding.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())

	// Provider-level transient errors.
	retryable := []string{
		"rate limit", "rate_limit", "too many requests",
		"429", "503", "502", "504",
		"server overloaded", "server error",
		"internal server error",
		"service unavailable",
		"connection refused",
		"connection reset",
		"broken pipe",
		"tls handshake timeout", "tls: handshake",
		"deadline exceeded",
		"timed out", "timeout",
		"eof",
		"unexpected eof",
		"no such host",
		"dial tcp",
		"i/o timeout",
		"temporary failure",
		"try again",
	}
	for _, keyword := range retryable {
		if strings.Contains(msg, keyword) {
			return true
		}
	}
	return false
}

// CompleteText runs a non-streaming completion for compaction.
func (c *Client) CompleteText(ctx context.Context, msgs []Message) (string, error) {
	model, err := c.ensureModel(ctx)
	if err != nil {
		return "", err
	}
	prompt, prior, systemPrompt := splitForFantasy(msgs)

	opts := []fantasy.AgentOption{
		fantasy.WithStopConditions(fantasy.StepCountIs(1)),
	}
	if systemPrompt != "" {
		opts = append(opts, fantasy.WithSystemPrompt(systemPrompt))
	}
	fa := fantasy.NewAgent(model, opts...)

	var out strings.Builder
	_, err = fa.Stream(ctx, fantasy.AgentStreamCall{
		Prompt:   prompt,
		Messages: prior,
		OnTextDelta: func(_, text string) error {
			out.WriteString(text)
			return nil
		},
	})
	if err != nil {
		return "", err
	}
	return out.String(), nil
}

func safeSend(ctx context.Context, ch chan<- StreamEvent, ev StreamEvent) error {
	select {
	case ch <- ev:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func toolResultText(out fantasy.ToolResultOutputContent) string {
	switch v := out.(type) {
	case fantasy.ToolResultOutputContentText:
		return v.Text
	case *fantasy.ToolResultOutputContentText:
		if v != nil {
			return v.Text
		}
	}
	return ""
}
