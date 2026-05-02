package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/jstamagal/bitchtea/internal/daemon"
	"github.com/jstamagal/bitchtea/internal/session"
)

// checkpointArgs is the wire shape of a session-checkpoint job's Args.
//
// session_path is the JSONL session file to checkpoint. The handler reads
// it via session.Load (read-only) and writes a sibling .bitchtea_checkpoint.json
// in the same directory via session.SaveCheckpoint.
//
// Per docs/phase-7-process-model.md the daemon must NEVER write into the
// active session JSONL. This handler honors that: SaveCheckpoint targets a
// fixed sibling filename, never the JSONL itself.
type checkpointArgs struct {
	SessionPath string `json:"session_path"`
	Model       string `json:"model,omitempty"`
}

// checkpointOutput is what we write into the Result.Output for a successful
// session-checkpoint run. Kept small and structured so a TUI rendering it
// later can pull out specific fields.
type checkpointOutput struct {
	CheckpointPath string `json:"checkpoint_path"`
	TurnCount      int    `json:"turn_count"`
	ToolCallCount  int    `json:"tool_call_count"`
}

// handleSessionCheckpoint loads a session JSONL, summarizes it into a
// session.Checkpoint, and writes the sibling checkpoint file.
//
// Idempotency: SaveCheckpoint truncates and rewrites the same fixed path
// (.bitchtea_checkpoint.json) on every call. Two runs against the same
// session produce a checkpoint with identical TurnCount and ToolCalls
// counts; only the embedded Timestamp differs. The on-disk *file* identity
// and the meaningful state are stable.
//
// Bound: 30s context. Loading a JSONL is cheap (O(file size)); 30s is
// generous for any sane session and short enough that a stuck filesystem
// surfaces as an error instead of pinning a daemon worker.
func handleSessionCheckpoint(ctx context.Context, job daemon.Job) daemon.Result {
	started := time.Now().UTC()
	kind := KindSessionCheckpoint

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := ctx.Err(); err != nil {
		return errorResult(kind, started, fmt.Errorf("session-checkpoint: %w", err))
	}

	var args checkpointArgs
	if len(job.Args) > 0 {
		if err := json.Unmarshal(job.Args, &args); err != nil {
			return errorResult(kind, started, fmt.Errorf("session-checkpoint: parse args: %w", err))
		}
	}
	// session_path is the only required field; fall back to the envelope's
	// SessionPath so a caller can supply it at the top level instead of
	// duplicating in args.
	if args.SessionPath == "" {
		args.SessionPath = job.SessionPath
	}
	if args.SessionPath == "" {
		return errorResult(kind, started, fmt.Errorf("session-checkpoint: session_path is required"))
	}

	if err := ctx.Err(); err != nil {
		return errorResult(kind, started, fmt.Errorf("session-checkpoint: %w", err))
	}

	sess, err := session.Load(args.SessionPath)
	if err != nil {
		return errorResult(kind, started, fmt.Errorf("session-checkpoint: load %s: %w", args.SessionPath, err))
	}

	// Build the checkpoint summary. We treat any non-bootstrap user entry
	// as a "turn" boundary — same convention the agent's own checkpoint
	// path uses for the autonomous-turn counter. For tool calls, count
	// every assistant entry that emitted at least one tool_call.
	turns := 0
	toolCalls := map[string]int{}
	for _, e := range sess.Entries {
		if e.Bootstrap {
			continue
		}
		if e.Role == "user" {
			turns++
		}
		for _, tc := range e.ToolCalls {
			if tc.Function.Name != "" {
				toolCalls[tc.Function.Name]++
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return errorResult(kind, started, fmt.Errorf("session-checkpoint: %w", err))
	}

	cp := session.Checkpoint{
		TurnCount: turns,
		ToolCalls: toolCalls,
		Model:     args.Model,
	}
	dir := filepath.Dir(args.SessionPath)
	if err := session.SaveCheckpoint(dir, cp); err != nil {
		return errorResult(kind, started, fmt.Errorf("session-checkpoint: save: %w", err))
	}

	totalTools := 0
	for _, n := range toolCalls {
		totalTools += n
	}
	out := checkpointOutput{
		CheckpointPath: filepath.Join(dir, ".bitchtea_checkpoint.json"),
		TurnCount:      turns,
		ToolCallCount:  totalTools,
	}
	return successResult(kind, started, out)
}
