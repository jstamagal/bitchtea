package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"regexp"
	"strings"
	"time"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"

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


// samplingParamsSupported reports whether the given service supports forwarding
// sampling parameters (temperature, top_p, top_k).
//
// "anthropic", "zai-anthropic" return HTTP 400 on non-default values — skip them
// and emit a debug-log warning. All other services (openai, openrouter, ollama,
// custom, etc.) get them forwarded.
// TODO(Agent A): if cfg.Service is the cliapiproxy identity, forward per
// Agent A's findings. For now treated as forwarding (like OpenAI).
func samplingParamsSupported(service string) bool {
	switch service {
	case "anthropic", "zai-anthropic":
		return false
	default:
		return true
	}
}

// applySamplingParams attaches sampling-param AgentOptions to opts when the
// service supports them. Skipped params are logged at debug level.
func applySamplingParams(service string, temp, topP, repPen *float64, topK *int, opts []fantasy.AgentOption) []fantasy.AgentOption {
	supported := samplingParamsSupported(service)
	if temp != nil {
		if supported {
			opts = append(opts, fantasy.WithTemperature(*temp))
		} else {
			log.Printf("[bitchtea debug] sampling: skipping temperature=%v for service=%q (returns 400)", *temp, service)
		}
	}
	if topP != nil {
		if supported {
			opts = append(opts, fantasy.WithTopP(*topP))
		} else {
			log.Printf("[bitchtea debug] sampling: skipping top_p=%v for service=%q (returns 400)", *topP, service)
		}
	}
	if topK != nil {
		if supported {
			opts = append(opts, fantasy.WithTopK(int64(*topK)))
		} else {
			log.Printf("[bitchtea debug] sampling: skipping top_k=%v for service=%q (returns 400)", *topK, service)
		}
	}
	// repetition_penalty: no fantasy.WithRepetitionPenalty exists yet.
	// TODO: wire WithRepetitionPenalty here once fantasy adds it.
	if repPen != nil {
		_ = fmt.Sprintf("rep_pen=%.4f stored, pending fantasy API support", *repPen)
	}
	return opts
}

// cliproxyEffortToReasoningEffort maps cfg.Effort strings to the
// openai.ReasoningEffort enum accepted by openaicompat.ProviderOptions.
//
// The CLIProxyAPI proxy translates the top-level JSON field reasoning_effort
// into Claude adaptive-thinking config upstream. Valid proxy values are
// low, medium, high, xhigh, max. The fantasy openaicompat PrepareCallFunc only
// knows the OpenAI enum (none/minimal/low/medium/high/xhigh); "max" has no
// enum constant and is promoted to "xhigh" here to avoid a PrepareCallFunc
// error. If the proxy ever exposes a semantically distinct "max" value,
// revisit this promotion.
//
// TODO(Agent D): verify after merge that validEfforts in rc.go includes
// "xhigh" and "max" so /set effort can reach both. Agent D may have already
// wired the SET key.
//
// Returns (effort, true) when a mapping exists, ("", false) for unknowns.
func cliproxyEffortToReasoningEffort(effort string) (openai.ReasoningEffort, bool) {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "low":
		return openai.ReasoningEffortLow, true
	case "medium":
		return openai.ReasoningEffortMedium, true
	case "high":
		return openai.ReasoningEffortHigh, true
	case "xhigh":
		return openai.ReasoningEffortXHigh, true
	case "max":
		// "max" has no OpenAI SDK enum; promote to xhigh (highest known value).
		return openai.ReasoningEffortXHigh, true
	default:
		return "", false
	}
}

// applyCliproxyReasoningEffort attaches a reasoning_effort AgentOption when
// service == "cliproxyapi". Defaults to "xhigh" when effort is empty (Opus 4.7
// agentic sweet spot). The option is passed as openaicompat.ProviderOptions so
// fantasy's PrepareCallFunc maps it onto params.ReasoningEffort in the outgoing
// JSON; the proxy reads that field and forwards it to Claude upstream as the
// adaptive-thinking effort level.
//
// X-Session-ID for upstream account affinity is NOT wired here.
// TODO(bead): add X-Session-ID header per Agent session for cliproxyapi
// account affinity. The proxy uses metadata.user_id or X-Session-ID to stick
// one bitchtea session to one upstream account. Scope: roughly half a day —
// needs a stable UUID threaded from Agent construction through Client to a
// per-request HTTP header. Defer to its own bead.
func applyCliproxyReasoningEffort(service, effort string, opts []fantasy.AgentOption) []fantasy.AgentOption {
	if service != "cliproxyapi" {
		return opts
	}
	if effort == "" {
		effort = "xhigh" // default for cliproxyapi when unset
	}
	re, ok := cliproxyEffortToReasoningEffort(effort)
	if !ok {
		log.Printf("[bitchtea debug] cliproxyapi: unrecognised effort %q, skipping reasoning_effort", effort)
		return opts
	}
	opts = append(opts, fantasy.WithProviderOptions(openaicompat.NewProviderOptions(&openaicompat.ProviderOptions{
		ReasoningEffort: &re,
	})))
	return opts
}

// streamOnce performs a single StreamChat attempt. Returns nil on success
// (which means a "done" event was already sent to events), or an error.
func (c *Client) streamOnce(ctx context.Context, msgs []Message, reg *tools.Registry, events chan<- StreamEvent) error {
	model, err := c.ensureModel(ctx)
	if err != nil {
		return err
	}

	prompt, prior, systemPrompt := splitForFantasy(msgs)

	// Snapshot Service / BootstrapMsgCount / sampling params / effort /
	// promptDrain under the mutex so PrepareStep closes over a stable view
	// even if a concurrent SetService / SetSamplingParams / SetEffort /
	// SetPromptDrain call runs while this turn is in flight. PrepareStep
	// itself is invoked from inside the fantasy goroutine and must NOT take
	// c.mu — that's the whole point of snapshotting here.
	//
	// The promptDrain snapshot in particular fixes HIGH #4: PrepareStep used
	// to dereference c.promptDrain directly, which raced against
	// SetPromptDrain(nil) from the agent's deferred cleanup. go test -race
	// caught this. Closing over the snapshot below keeps PrepareStep
	// touching a stable function value for the lifetime of the turn.
	c.mu.Lock()
	cacheService := c.Service
	cacheBootstrap := c.BootstrapMsgCount
	cacheTemp := c.Temperature
	cacheTopP := c.TopP
	cacheTopK := c.TopK
	cacheRepPen := c.RepetitionPenalty
	cacheEffort := c.effort
	cachePromptDrain := c.promptDrain
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
	// Forward sampling params when the service supports them.
	opts = applySamplingParams(cacheService, cacheTemp, cacheTopP, cacheRepPen, cacheTopK, opts)
	// Forward Anthropic effort hint when applicable. Setting Effort auto-
	// attaches adaptive thinking inside the fantasy anthropic provider
	// (anthropic.go ~line 324: params.Thinking.OfAdaptive when Effort != nil),
	// so this single option turns on both effort and adaptive reasoning.
	//
	// CAVEAT: LO routes Opus 4.7 through CLIAPIPROXY (an OpenAI-compatible
	// proxy) so cfg.Service is typically "openai" and this branch will not
	// fire for him — the openaicompat path needs its own ReasoningEffort
	// wiring (handled elsewhere). This Anthropic-native wiring still belongs
	// here for Anthropic-direct callers.
	if cacheService == "anthropic" && cacheEffort != "" {
		opts = append(opts, fantasy.WithProviderOptions(
			anthropic.NewProviderOptions(&anthropic.ProviderOptions{
				Effort: anthropicEffortPtr(cacheEffort),
			}),
		))
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
			// Drain queued prompts (bt-p4-queue) before cache markers so
			// mid-turn input is part of the prepared message slice.
			msgs := stepOpts.Messages
			if cachePromptDrain != nil {
				if drained := cachePromptDrain(); len(drained) > 0 {
					for _, text := range drained {
						msgs = append(msgs, fantasy.NewUserMessage(text))
					}
				}
			}
			prepared := fantasy.PrepareStepResult{Messages: msgs}
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

// retryableHTTPCodeRe matches transient HTTP status codes only as whole
// numbers — substring matching produced false positives like "5029" → 502
// in random identifier-bearing error strings.
var retryableHTTPCodeRe = regexp.MustCompile(`\b(429|502|503|504)\b`)

// retryableSingleWordRe matches single-word retry hints with word boundaries.
// Substring matching for "timeout" / "eof" caught things like
// "timeoutHandler" or identifiers ending in "...beof..." — neither of which
// indicate a retryable transport-layer failure.
var retryableSingleWordRe = regexp.MustCompile(`\b(timeout|timed out|eof)\b`)

// retryablePhrases are multi-word indicators where substring matching is
// safe — these phrases are distinctive enough not to false-match.
var retryablePhrases = []string{
	"rate limit", "rate_limit", "too many requests",
	"server overloaded", "server error", "internal server error",
	"service unavailable",
	"connection refused", "connection reset", "broken pipe",
	"tls handshake timeout", "tls: handshake",
	"no such host", "dial tcp", "i/o timeout",
	"temporary failure", "try again",
}

// isRetryable returns true when the error is likely transient and a retry
// after backoff has a reasonable chance of succeeding.
//
// Resolution order:
//  1. Explicit non-retryable sentinel: context.Canceled (user/upstream
//     cancellation — retrying defeats the cancel intent).
//  2. Explicit retryable sentinels: context.DeadlineExceeded, io.EOF,
//     io.ErrUnexpectedEOF.
//  3. Typed network errors (*net.OpError, *net.DNSError) — generally
//     transient transport failures.
//  4. Word-boundary regex for HTTP status codes (429/502/503/504) and for
//     "timeout"/"eof" — replaces the previous substring matching that
//     produced false positives.
//  5. Substring match for multi-word retryable phrases — distinctive
//     enough to be safe.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		// IsNotFound = NXDOMAIN, won't help by retrying.
		// IsTemporary = transient resolver issue, retry.
		// IsTimeout = obvious retry candidate.
		return dnsErr.IsTemporary || dnsErr.IsTimeout
	}

	msg := strings.ToLower(err.Error())
	if retryableHTTPCodeRe.MatchString(msg) {
		return true
	}
	if retryableSingleWordRe.MatchString(msg) {
		return true
	}
	for _, phrase := range retryablePhrases {
		if strings.Contains(msg, phrase) {
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

// anthropicEffortPtr maps a config string ("low" / "medium" / "high" / "max")
// to a pointer-to-anthropic.Effort suitable for ProviderOptions.Effort. Unknown
// values fall back to anthropic.EffortHigh because the cfg layer should have
// already validated against validEfforts; this helper just keeps the map in one
// place. fantasy does not expose "xhigh" yet — when it does, add the case here.
func anthropicEffortPtr(s string) *anthropic.Effort {
	var e anthropic.Effort
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "low":
		e = anthropic.EffortLow
	case "medium":
		e = anthropic.EffortMedium
	case "max":
		e = anthropic.EffortMax
	default:
		e = anthropic.EffortHigh
	}
	return &e
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
