package llm

import (
	"context"
	"strings"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/tools"
)

// maxAgentSteps is the StopCondition cap on a single Stream call. The old
// hand-rolled loop ran until the model emitted a final text response with no
// tool calls; fantasy stops earliest of (StepCountIs, no-more-tool-calls).
// 64 is enough for long tool chains without runaway loops.
const maxAgentSteps = 64

// StreamChat runs one full agent turn against fantasy and emits StreamEvents on
// events. Closes events when the turn ends (success, error, or cancellation).
//
// Event order:
//   - zero-or-more "text" / "thinking" / "tool_call" events as the model streams
//   - one "usage" event when the stream finishes (per stream — fantasy emits
//     OnStreamFinish at the end of each step's stream segment)
//   - one terminal "done" event with Messages populated (rebuilt transcript
//     from result.Steps[].Messages) OR one "error" event
//
// The agent layer translates these to agent.Event for the UI and appends
// ev.Messages to its own a.messages on "done".
func (c *Client) StreamChat(ctx context.Context, msgs []Message, reg *tools.Registry, events chan<- StreamEvent) {
	defer close(events)

	model, err := c.ensureModel(ctx)
	if err != nil {
		safeSend(ctx, events, StreamEvent{Type: "error", Error: err})
		return
	}

	prompt, prior, systemPrompt := splitForFantasy(msgs)

	opts := []fantasy.AgentOption{
		fantasy.WithStopConditions(fantasy.StepCountIs(maxAgentSteps)),
	}
	if systemPrompt != "" {
		opts = append(opts, fantasy.WithSystemPrompt(systemPrompt))
	}
	if reg != nil {
		opts = append(opts, fantasy.WithTools(translateTools(reg)...))
	}

	fa := fantasy.NewAgent(model, opts...)

	result, err := fa.Stream(ctx, fantasy.AgentStreamCall{
		Prompt:   prompt,
		Messages: prior,

		PrepareStep: func(stepCtx context.Context, _ fantasy.PrepareStepFunctionOptions) (context.Context, fantasy.PrepareStepResult, error) {
			if sendErr := safeSend(stepCtx, events, StreamEvent{Type: "thinking"}); sendErr != nil {
				return stepCtx, fantasy.PrepareStepResult{}, sendErr
			}
			return stepCtx, fantasy.PrepareStepResult{}, nil
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
			// Tool results land in result.Steps[].Messages; the agent rebuilds
			// transcript from there. UI sees a tool_result event for live feedback.
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

		OnError: func(streamErr error) {
			_ = safeSend(ctx, events, StreamEvent{Type: "error", Error: streamErr})
		},
	})
	if err != nil {
		safeSend(ctx, events, StreamEvent{Type: "error", Error: err})
		return
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
}

// CompleteText runs a non-streaming completion for compaction. It builds a
// tool-less agent, streams tokens into a buffer, and returns the full text.
//
// The agent's hand-rolled compaction loop used to walk events and concatenate
// "text" deltas itself; centralising that here means callers don't have to
// know about the StreamEvent surface for one-shot completions.
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

// safeSend is a context-aware channel send. A canceled context returns
// ctx.Err() instead of blocking forever; callbacks bubble that back to fantasy
// to abort cleanly.
func safeSend(ctx context.Context, ch chan<- StreamEvent, ev StreamEvent) error {
	select {
	case ch <- ev:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// toolResultText extracts the text payload from a fantasy tool result for the
// "tool_result" StreamEvent. Non-text outputs collapse to an empty string —
// the canonical record lives in result.Steps[].Messages anyway.
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
