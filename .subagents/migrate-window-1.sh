#!/bin/bash
# migrate-window-1.sh — Window 1 of bitchtea fantasy migration.
#
# WORK_MAP.md says Window 1 fans out into 4 parallel workers AFTER types.go lands:
#   - Claude (this session): providers.go -> client.go (sequential, both Claude)
#   - codex-jstamagal:        debug.go
#   - codex-foreigner:        errors.go    (skip if quota-dead)
#   - gemini-3.1-pro:         cost.go
#
# This script fires the THREE non-Claude workers in the background.
# Claude does providers.go + client.go inline AFTER firing this.
#
# Prereqs (precheck.sh enforces):
#   - bitchtea-llm-impl worktree exists at ../bitchtea-llm-impl
#   - types.go already written + committed in bitchtea-llm-impl
#   - MIGRATION_PLAN.md v3 + WORK_MAP.md present

set -u
cd /home/admin/bitchtea || { echo "FAIL: not in bitchtea workspace"; exit 1; }

WORKTREE="/home/admin/bitchtea-llm-impl"
PLAN="/home/admin/bitchtea/MIGRATION_PLAN.md"

if [ ! -d "$WORKTREE" ]; then
  echo "FAIL: $WORKTREE missing. Run:"
  echo "  git worktree add $WORKTREE"
  exit 1
fi

if [ ! -f "$WORKTREE/internal/llm/types.go" ]; then
  echo "FAIL: $WORKTREE/internal/llm/types.go not written yet."
  echo "Claude must write types.go FIRST (other files import it)."
  exit 1
fi

STALE=$(ps aux | grep -E "codex|acpx" | grep -v grep | grep -v migrate-window-1 || true)
if [ -n "$STALE" ]; then
  echo "FAIL: stale codex/acpx processes:"
  echo "$STALE"
  echo "Kill them or wait before firing Window 1."
  exit 1
fi

# ---- shared context blob for all workers ----
read -r -d '' CONTEXT <<'EOF' || true
You are writing one file inside bitchtea's new internal/llm/ package, which is
backed by charm.land/fantasy v0.17.1. The migration plan
/home/admin/bitchtea/MIGRATION_PLAN.md (v3) is the source of truth — read it
before writing.

CRITICAL fantasy v0.17.1 surface (verified against
/home/admin/go/pkg/mod/charm.land/fantasy@v0.17.1):

  - fantasy.AgentTool.Run returns (fantasy.ToolResponse, error). Returning a Go
    error aborts the entire stream. For "tool failed but keep going" use
    fantasy.NewTextErrorResponse(content).
  - fantasy.Message{Role MessageRole, Content []MessagePart}. Roles:
    MessageRoleSystem/User/Assistant/Tool.
  - Parts: TextPart{Text}, ToolCallPart{ToolCallID, ToolName, Input,
    ProviderExecuted}, ToolResultPart{ToolCallID, Output ToolResultOutputContent}.
  - AgentStreamCall{Prompt string, Messages []Message, ...callbacks...}.
  - Callbacks: OnTextDelta(id, text string) error, OnToolCall(ToolCallContent)
    error, OnToolResult(ToolResultContent) error,
    OnStreamFinish(Usage, FinishReason, ProviderMetadata) error,
    OnError(error)  // no return.
  - Stop conditions via fantasy.WithStopConditions(fantasy.StepCountIs(n)).
    No WithMaxSteps.
  - ToolInfo{Name, Description, Parameters map[string]any, Required []string,
    Parallel bool}. NOT InputSchema.
  - Provider DefaultURLs: openai="https://api.openai.com/v1",
    openrouter="https://openrouter.ai/api/v1",
    vercel="https://ai-gateway.vercel.sh/v1",
    anthropic="https://api.anthropic.com" (NO /v1).

Hard rules:
  - Do NOT write code outside the file you were assigned.
  - Do NOT touch internal/agent/agent.go or internal/tools/tools.go.
  - Do NOT modify the session JSONL format.
  - Match the type/function signatures called out in MIGRATION_PLAN.md exactly.
  - Tests for your file are written separately (Window 3). Do not write tests.
  - All four checks must pass after merge: go build ./..., go test ./...,
    go test -race ./..., go vet ./....

Working directory for your edits: /home/admin/bitchtea-llm-impl
EOF

LOG_DIR="/tmp/bitchtea-migration"
mkdir -p "$LOG_DIR"
TS=$(date +%Y%m%d-%H%M%S)

# ---- 1. codex-jstamagal: debug.go ----
DEBUG_PROMPT="$CONTEXT

YOUR FILE: /home/admin/bitchtea-llm-impl/internal/llm/debug.go

Spec: MIGRATION_PLAN.md decision D8 (debug RoundTripper) + the DebugInfo
struct contract referenced from internal/ui/commands.go SetDebugHook callsite
(grep the original deleted file in git history at HEAD~ for reference:
  git -C /home/admin/bitchtea show HEAD:internal/llm/debug.go
).

Build a debug RoundTripper that captures request/response bodies and emits
DebugInfo via a hook. Match the old DebugInfo field set exactly so
ui/commands.go SetDebugHook still compiles unchanged.

Write the file. Then exit. Do not modify other files."

echo "[1/3] firing codex-jstamagal -> debug.go (background)"
CODEX_HOME=/home/admin/.codex-jstamagal nohup codex exec --skip-git-repo-check "$DEBUG_PROMPT" \
  >"$LOG_DIR/codex-jstamagal-debug-$TS.log" 2>&1 &
echo "      PID $! log $LOG_DIR/codex-jstamagal-debug-$TS.log"

# ---- 2. codex-foreigner: errors.go (skip if quota-dead) ----
ERRORS_PROMPT="$CONTEXT

YOUR FILE: /home/admin/bitchtea-llm-impl/internal/llm/errors.go

Spec: MIGRATION_PLAN.md decision on errors. Mechanical port from the old
ErrorHint helpers, plus a wrapper that does errors.As(*fantasy.ProviderError)
to surface upstream rate-limit / auth / context-length errors with friendly
hints. Reference the old file in git history:
  git -C /home/admin/bitchtea show HEAD:internal/llm/errors.go

Write the file. Then exit."

echo "[2/3] firing codex-foreigner -> errors.go (background, may quota-fail)"
CODEX_HOME=/home/admin/.codex-foreigner nohup codex exec --skip-git-repo-check "$ERRORS_PROMPT" \
  >"$LOG_DIR/codex-foreigner-errors-$TS.log" 2>&1 &
echo "      PID $! log $LOG_DIR/codex-foreigner-errors-$TS.log"

# ---- 3. gemini-3.1-pro: cost.go ----
COST_PROMPT="$CONTEXT

YOUR FILE: /home/admin/bitchtea-llm-impl/internal/llm/cost.go

Spec: MIGRATION_PLAN.md decision on cost tracking. Build a CostTracker that
takes fantasy.Usage and computes \$ per provider/model using
charm.land/catwalk v0.35.1's embedded.GetAll() pricing snapshot. Field set
must match the old CostTracker so callers in internal/agent/agent.go compile
unchanged.

Reference the old file in git history:
  git -C /home/admin/bitchtea show HEAD:internal/llm/cost.go

Write the file. Then exit."

echo "[3/3] firing gemini-3.1-pro -> cost.go (background)"
nohup acpx --approve-all --model gemini-3.1-pro gemini --no-wait "$COST_PROMPT" \
  >"$LOG_DIR/gemini-pro-cost-$TS.log" 2>&1 &
echo "      PID $! log $LOG_DIR/gemini-pro-cost-$TS.log"

echo
echo "=== Window 1 fanout fired. ==="
echo "Logs in $LOG_DIR/*-$TS.log"
echo
echo "While they run, YOU (Claude) write:"
echo "  1. providers.go  (per plan §D4)"
echo "  2. client.go     (per plan §D5/D7)"
echo
echo "Check progress:"
echo "  ps aux | grep -E 'codex|acpx' | grep -v grep"
echo "  tail -f $LOG_DIR/*-$TS.log"
echo
echo "When all 5 files (types/providers/client/debug/errors/cost) exist,"
echo "verify in $WORKTREE:"
echo "  go build ./internal/llm/...   # may still fail until stream.go (Window 2)"
echo "  go vet  ./internal/llm/..."
echo "Then move on to Window 2."
