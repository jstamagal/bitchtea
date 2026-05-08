package llm

import (
	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
)

// applyAnthropicCacheMarkers stamps Anthropic prompt-cache markers
// (`cache_control: {type: "ephemeral"}`) onto the last bootstrap message in
// `prepared.Messages`. The bootstrap boundary is the longest stable prefix
// in a bitchtea session — the system prompt + injected AGENTS.md/CLAUDE.md
// pairs + the persona anchor — and stays byte-identical across every turn,
// so caching there maximizes prefix reuse.
//
// service is the upstream service identity (cfg.Service). Only "anthropic"
// triggers marker placement today. Per docs/phase-4-preparestep.md and the
// open question on `zai-anthropic`, the z.ai proxy is excluded until a
// captured-payload test confirms `cache_control` round-trips upstream.
//
// bootstrapInPrior is the index in `prepared.Messages` of the last surviving
// bootstrap message (system messages and the tail user prompt have already
// been peeled off in splitForFantasy). Negative or out-of-range values
// no-op so the call site doesn't have to special-case fresh sessions.
//
// The function is a no-op for any non-Anthropic service — that single gate
// at the top is the entire provider check. Callers should still pass the
// real service value so adding new gated services is a one-line change.
func applyAnthropicCacheMarkers(prepared *fantasy.PrepareStepResult, service string, bootstrapInPrior int) {
	// NOTE: service="cliproxyapi" intentionally does NOT receive cache_control
	// markers from bitchtea. The CLIProxyAPI proxy (router-for-me/CLIProxyAPI)
	// auto-injects ephemeral markers on tools[]/system[]/last-user-message
	// upstream and runs its own 4-breakpoint enforcement. If bitchtea sends ANY
	// cache_control marker, the proxy detects countCacheControls > 0 and skips
	// its smart placement entirely — which is strictly worse than letting the
	// proxy handle it. Leave the gate as-is: only "anthropic" gets markers here.
	//
	// Do NOT extend this function to cover "cliproxyapi". If you are reading
	// this while wondering whether cliproxyapi should get cache markers, the
	// answer is: no, never, let the proxy do it.
	if service != "anthropic" {
		return
	}
	if prepared == nil {
		return
	}
	if bootstrapInPrior < 0 || bootstrapInPrior >= len(prepared.Messages) {
		return
	}
	prepared.Messages[bootstrapInPrior].ProviderOptions = fantasy.ProviderOptions{
		anthropic.Name: &anthropic.ProviderCacheControlOptions{
			CacheControl: anthropic.CacheControl{Type: "ephemeral"},
		},
	}
}

// bootstrapPreparedIndex maps the agent's bootstrapMsgCount (an index into
// the full a.messages slice, including the system message at index 0) to
// the index of the last bootstrap message as it appears in PrepareStep's
// `opts.Messages` slice.
//
// fantasy assembles `opts.Messages` as:
//
//	[system?] + prior + [userPrompt?] + responseMessages
//
// where `prior` is what splitForFantasy returned (system messages stripped,
// tail user message peeled off into Prompt) and `system?` is reconstructed
// from systemPrompt. splitForFantasy concatenates ALL system messages from
// the agent's slice into one string for WithSystemPrompt; createPrompt then
// re-emits EXACTLY ONE system message back into opts.Messages regardless of
// how many existed in the original. So if the bootstrap window in
// a.messages contains N system messages (N ≥ 1), opts.Messages has only 1,
// and the downstream user/assistant messages start (N - 1) slots earlier
// than a naive `bootstrapMsgCount - 1` would predict.
//
// The fix: count system messages in msgs[:bootstrapMsgCount] and subtract
// (systemCount - 1) from the naive index. For the common single-system-
// message case this is a no-op (count is 1, subtract 0). The defensive
// math kicks in only when RestoreMessages carries a saved transcript with
// multiple adjacent system messages, which is rare but possible.
//
// Returns -1 when there is no usable bootstrap boundary so the caller can
// short-circuit fresh / restored sessions where bootstrapMsgCount is 0.
func bootstrapPreparedIndex(msgs []Message, bootstrapMsgCount int) int {
	if bootstrapMsgCount <= 0 || bootstrapMsgCount > len(msgs) {
		return -1
	}
	systemCount := 0
	for i := 0; i < bootstrapMsgCount; i++ {
		if msgs[i].Role == "system" {
			systemCount++
		}
	}
	idx := bootstrapMsgCount - 1
	if systemCount > 1 {
		idx -= (systemCount - 1)
	}
	if idx < 0 {
		return -1
	}
	return idx
}
