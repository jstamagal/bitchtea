#!/bin/bash
# migrate-window-2.sh — Window 2 of bitchtea fantasy migration.
#
# WORK_MAP/HANDOFF say Window 2 (after Window 1 lands and event-split merges):
#   - codex-jstamagal: convert.go  (splitForFantasy, fantasyToLLM, toLLMUsage)
#   - codex-foreigner: tools.go    (bitchteaTool wrapper, translateTools, splitSchema)
#       — quota may be dead; if so, Claude writes it inline.
#   - Claude (after both land): stream.go  (the integrator)
#
# Prereqs:
#   - bitchtea-llm-impl worktree exists
#   - Window 1 files committed: types.go, providers.go, client.go, debug.go,
#     errors.go, cost.go (commit 1a9782b)
#   - event-split merged to master, llm-impl pulled it (commit b8b8d2c-ish — merge of master)
#   - MIGRATION_PLAN.md v3.1 present at /home/admin/bitchtea/MIGRATION_PLAN.md

set -u
cd /home/admin/bitchtea || { echo "FAIL: not in bitchtea workspace"; exit 1; }

WORKTREE="/home/admin/bitchtea-llm-impl"
PLAN="/home/admin/bitchtea/MIGRATION_PLAN.md"

if [ ! -d "$WORKTREE" ]; then
  echo "FAIL: $WORKTREE missing."
  exit 1
fi

for f in types.go providers.go client.go debug.go errors.go cost.go; do
  if [ ! -f "$WORKTREE/internal/llm/$f" ]; then
    echo "FAIL: $WORKTREE/internal/llm/$f missing — run Window 1 first."
    exit 1
  fi
done

if [ ! -d "$WORKTREE/internal/agent/event" ]; then
  echo "FAIL: $WORKTREE/internal/agent/event missing — merge event-split into llm-impl first."
  exit 1
fi

STALE=$(ps aux | grep -E "codex|acpx" | grep -v grep | grep -v migrate-window || true)
if [ -n "$STALE" ]; then
  echo "FAIL: stale codex/acpx processes:"
  echo "$STALE"
  echo "Wait for them or kill them before firing Window 2."
  exit 1
fi

# ---- shared context blob ----
read -r -d '' CONTEXT <<'EOF' || true
You are writing one file inside bitchtea's new internal/llm/ package, which is
backed by charm.land/fantasy v0.17.1. The migration plan
/home/admin/bitchtea/MIGRATION_PLAN.md (v3.1) is the source of truth.

Window 1 landed: types.go, providers.go, client.go, debug.go, errors.go,
cost.go all exist in /home/admin/bitchtea-llm-impl/internal/llm/. Read them
before writing — your file must dovetail with their public surface.

CRITICAL fantasy v0.17.1 surface (verified against
/home/admin/go/pkg/mod/charm.land/fantasy@v0.17.1):

  - fantasy.AgentTool interface: Info() ToolInfo, Run(ctx, ToolCall)
    (ToolResponse, error), ProviderOptions() ProviderOptions,
    SetProviderOptions(ProviderOptions).
  - fantasy.ToolCall has a ToolName + Input string (raw JSON args). Returning
    a Go error from Run aborts the entire stream. Use
    fantasy.NewTextErrorResponse(...) to surface a "tool failed but keep
    going" message. Use fantasy.NewTextResponse(...) for success.
  - fantasy.Message{Role MessageRole, Content []MessagePart}. Roles:
    MessageRoleSystem/User/Assistant/Tool.
  - Parts: TextPart{Text}, ToolCallPart{ToolCallID, ToolName, Input,
    ProviderExecuted}, ToolResultPart{ToolCallID, Output ToolResultOutputContent}.
  - ToolResultOutputContent: a sealed interface; common impl is
    ToolResultOutputContentText{Text string}.
  - fantasy.Usage fields: InputTokens, OutputTokens, CacheCreationTokens,
    CacheReadTokens (verify exact case in fantasy source — some fields are
    int64).
  - fantasy.ToolInfo{Name string, Description string, Parameters
    map[string]any, Required []string, Parallel bool}. NOT InputSchema.

Hard rules:
  - Do NOT write code outside the file you were assigned.
  - Do NOT touch internal/agent/agent.go or internal/tools/tools.go.
  - Do NOT modify the session JSONL format.
  - Match the type/function signatures called out in MIGRATION_PLAN.md exactly.
  - Tests for your file are written separately (Window 3). Do not write tests.

Working directory: /home/admin/bitchtea-llm-impl
EOF

LOG_DIR="/tmp/bitchtea-migration"
mkdir -p "$LOG_DIR"
TS=$(date +%Y%m%d-%H%M%S)

# ---- 1. codex-jstamagal: convert.go ----
CONVERT_PROMPT="$CONTEXT

YOUR FILE: /home/admin/bitchtea-llm-impl/internal/llm/convert.go

Spec: MIGRATION_PLAN.md decisions D3, D4, D8.

Three exported-package-private functions live here:

  // splitForFantasy splits []llm.Message into the (Prompt, Messages) pair that
  // fantasy.AgentStreamCall expects. Walks msgs; the LAST 'user' message
  // becomes Prompt (string); everything else (system dropped, prior user/
  // assistant/tool turned into fantasy.Message via parts) becomes prior.
  //
  // 'system' role messages are DROPPED (caller passes system prompt via
  // fantasy.WithSystemPrompt, not as a Message).
  //
  // Convention used by stream.go and CompleteText: the new user turn was
  // already appended to msgs by the agent layer before StreamChat was called.
  // splitForFantasy pops it off the tail and returns it as Prompt.
  //
  // If msgs has zero user messages (compaction edge case), Prompt is \"\" and
  // all non-system messages flow into prior.
  func splitForFantasy(msgs []Message) (prompt string, prior []fantasy.Message)

  // fantasyToLLM converts a fantasy.Message back to llm.Message form for
  // appending into a.messages. Used in stream.go after fa.Stream returns,
  // when walking result.Steps[].Messages.
  //
  //   - MessageRoleAssistant + Content: [TextPart, ToolCallPart...] →
  //     llm.Message{Role: \"assistant\", Content: textConcat,
  //                 ToolCalls: [{ID, Type:\"function\", Function:{Name, Arguments: input}}]}
  //   - MessageRoleTool + Content: [ToolResultPart{ToolCallID, Output: ToolResultOutputContentText{Text}}] →
  //     llm.Message{Role: \"tool\", Content: text, ToolCallID: id}
  //   - MessageRoleUser → llm.Message{Role: \"user\", Content: textConcat}
  //   - MessageRoleSystem → llm.Message{Role: \"system\", Content: textConcat}
  func fantasyToLLM(fm fantasy.Message) Message

  // toLLMUsage converts fantasy.Usage to llm.TokenUsage. Pure mechanical cast.
  func toLLMUsage(u fantasy.Usage) TokenUsage

Field names on llm.Message, llm.ToolCall, llm.FunctionCall, llm.TokenUsage are
already defined in /home/admin/bitchtea-llm-impl/internal/llm/types.go — read
that file first.

When converting ToolCallPart → llm.ToolCall: ToolCallID becomes ID, ToolName
becomes Function.Name, Input (which is a string of JSON) becomes
Function.Arguments. Type field is always \"function\".

When converting MessageRoleAssistant: concatenate all TextPart.Text into
Content (in order), and gather all ToolCallPart into ToolCalls.

The package is 'package llm'. Imports needed: \"charm.land/fantasy\". Possibly
\"strings\" for concat.

Write only this one file. Then exit."

echo "[1/2] firing codex-jstamagal -> convert.go (background)"
CODEX_HOME=/home/admin/.codex-jstamagal nohup codex exec --skip-git-repo-check "$CONVERT_PROMPT" \
  >"$LOG_DIR/codex-jstamagal-convert-$TS.log" 2>&1 &
echo "      PID $! log $LOG_DIR/codex-jstamagal-convert-$TS.log"

# ---- 2. codex-foreigner: tools.go (skip if quota-dead) ----
TOOLS_PROMPT="$CONTEXT

YOUR FILE: /home/admin/bitchtea-llm-impl/internal/llm/tools.go

Spec: MIGRATION_PLAN.md decision D2.

Three things live in this file:

  // bitchteaTool wraps an entry from internal/tools.Registry as a fantasy.AgentTool.
  // ProviderOptions/SetProviderOptions are no-op stubs (we don't pipe provider-
  // specific options through tools yet).
  type bitchteaTool struct {
      info fantasy.ToolInfo
      reg  *tools.Registry
  }

  // Info returns t.info verbatim.
  func (t *bitchteaTool) Info() fantasy.ToolInfo

  // Run executes the underlying tool via Registry.Execute. CRITICAL: a Go
  // error returned from Run aborts the entire fantasy stream. For \"this tool
  // failed but keep the conversation alive\", return
  //   fantasy.NewTextErrorResponse(fmt.Sprintf(\"Error: %v\", err)), nil
  // For success, return fantasy.NewTextResponse(out), nil.
  //
  // call.ToolName is the tool name; call.Input is the raw JSON arguments
  // string. Pass them straight through to reg.Execute(ctx, name, input).
  func (t *bitchteaTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error)

  // No-op provider option plumbing.
  func (t *bitchteaTool) ProviderOptions() fantasy.ProviderOptions
  func (t *bitchteaTool) SetProviderOptions(opts fantasy.ProviderOptions)

  // translateTools builds one *bitchteaTool per Registry definition. Walk
  // reg.Definitions() (returns []llm.ToolDef from types.go), use splitSchema to
  // separate the JSON-schema 'required' field from the rest of the parameters.
  func translateTools(reg *tools.Registry) []fantasy.AgentTool

  // splitSchema takes a JSON-schema 'parameters' map (map[string]any with the
  // usual {type:\"object\", properties:{...}, required:[...]} shape) and
  // returns (params, required) where required is the []string slice from the
  // top-level 'required' key (or nil if missing) and params is the same map
  // with that key removed. Be defensive: required may be []string OR
  // []interface{} (json.Unmarshal default). Returning a fresh copy of the map
  // (without 'required') is fine.
  func splitSchema(params map[string]any) (map[string]any, []string)

Verify the actual signatures by reading:
  /home/admin/go/pkg/mod/charm.land/fantasy@v0.17.1/agent_tool.go
  /home/admin/go/pkg/mod/charm.land/fantasy@v0.17.1/tool.go (or wherever
    NewTextResponse / NewTextErrorResponse / ToolCall live)
  /home/admin/bitchtea/internal/tools/tools.go (Registry, Definitions, Execute
    signatures — Execute takes (ctx, name, args string) and returns (string, error))

Imports needed: \"context\", \"fmt\", \"charm.land/fantasy\",
\"github.com/jstamagal/bitchtea/internal/tools\".

Do NOT import internal/agent or internal/agent/event. Run does not emit on
the events channel; the agent layer translates fantasy callbacks (in stream.go
and the agent select loop) into agent.Event.

Package is 'package llm'.

Write only this one file. Then exit."

echo "[2/2] firing codex-foreigner -> tools.go (background, may quota-fail)"
CODEX_HOME=/home/admin/.codex-foreigner nohup codex exec --skip-git-repo-check "$TOOLS_PROMPT" \
  >"$LOG_DIR/codex-foreigner-tools-$TS.log" 2>&1 &
echo "      PID $! log $LOG_DIR/codex-foreigner-tools-$TS.log"

echo
echo "=== Window 2 fanout fired. ==="
echo "Logs in $LOG_DIR/*-$TS.log"
echo
echo "Foreigner profile may quota-fail. If so, Claude writes tools.go inline."
echo
echo "Check progress:"
echo "  ps aux | grep -E 'codex|acpx' | grep -v grep"
echo "  tail -f $LOG_DIR/*-$TS.log"
echo
echo "When convert.go AND tools.go exist, Claude writes stream.go (the"
echo "integrator). Verify in $WORKTREE:"
echo "  go build ./internal/llm/..."
echo "  go vet  ./internal/llm/..."
echo "Then move on to Window 3."
