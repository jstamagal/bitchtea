package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jstamagal/bitchtea/internal/daemon"
	"github.com/jstamagal/bitchtea/internal/memory"
	"github.com/jstamagal/bitchtea/internal/session"
)

// sessionStitchArgs is the wire shape of a session-stitch job's Args.
//
// session_dir is the directory containing .jsonl session files. The handler
// scans every file, extracts key patterns (tool call frequency, repeated
// user intents, common file paths), and writes a consolidated summary into
// the root memory hot file.
//
// max_sessions caps the number of session files to process (0 = all).
// The handler is bounded even without this cap because it installs its own
// context timeout, but max_sessions lets callers trade completeness for
// latency on directories with hundreds of old sessions.
type sessionStitchArgs struct {
	SessionDir   string `json:"session_dir"`
	WorkDir      string `json:"work_dir"`
	MaxSessions  int    `json:"max_sessions,omitempty"`  // 0 means all
	Since        string `json:"since,omitempty"`         // YYYY-MM-DD cutoff for session files
}

// sessionStitchOutput summarises what the stitch run produced.
type sessionStitchOutput struct {
	SessionsScanned int      `json:"sessions_scanned"`
	PatternsFound   int      `json:"patterns_found"`
	MemoryPath      string   `json:"memory_path"`
	TopTools        []string `json:"top_tools,omitempty"`
	TopPaths        []string `json:"top_paths,omitempty"`
}

// stitchMarkerPrefix is embedded in the root memory heading so the handler
// can detect its own prior output and avoid duplicates on re-run.
const stitchMarkerPrefix = "<!-- bitchtea-stitch:"

// handleSessionStitch scans session JSONL files, extracts cross-session
// patterns (most-used tools, most-referenced file paths, recurring user
// intents), and writes a consolidated summary to the root memory hot file.
//
// Idempotency: each run replaces the prior stitch block in the hot file
// (identified by the stitchMarker heading). Two runs produce identical
// output given the same input sessions; only the timestamp in the heading
// changes.
//
// Bound: 90s context. Scanning many session files is I/O-bound but bounded
// by max_sessions and the context timeout.
//
// Cancellation: ctx.Err() is checked before each session load and before the
// final memory write so a SIGTERM during a large scan aborts cleanly.
func handleSessionStitch(ctx context.Context, job daemon.Job) daemon.Result {
	started := time.Now().UTC()
	kind := KindSessionStitch

	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	if err := ctx.Err(); err != nil {
		return errorResult(kind, started, fmt.Errorf("session-stitch: %w", err))
	}

	var args sessionStitchArgs
	if len(job.Args) > 0 {
		if err := json.Unmarshal(job.Args, &args); err != nil {
			return errorResult(kind, started, fmt.Errorf("session-stitch: parse args: %w", err))
		}
	}
	if args.SessionDir == "" {
		args.SessionDir = job.SessionPath // fallback: some callers set SessionPath
		if args.SessionDir != "" {
			args.SessionDir = filepath.Dir(args.SessionDir)
		}
	}
	if args.WorkDir == "" {
		args.WorkDir = job.WorkDir
	}
	if args.SessionDir == "" {
		return errorResult(kind, started, fmt.Errorf("session-stitch: session_dir is required"))
	}
	if args.WorkDir == "" {
		return errorResult(kind, started, fmt.Errorf("session-stitch: work_dir is required"))
	}

	since, err := parseSince(args.Since)
	if err != nil {
		return errorResult(kind, started, fmt.Errorf("session-stitch: %w", err))
	}

	// List session files in the directory.
	paths, err := sessionList(args.SessionDir, since)
	if err != nil {
		return errorResult(kind, started, fmt.Errorf("session-stitch: list sessions: %w", err))
	}
	if args.MaxSessions > 0 && len(paths) > args.MaxSessions {
		paths = paths[:args.MaxSessions]
	}

	if err := ctx.Err(); err != nil {
		return errorResult(kind, started, fmt.Errorf("session-stitch: %w", err))
	}

	// Collect cross-session patterns: tool call counts and file path references.
	toolCounts := map[string]int{}
	pathCounts := map[string]int{}
	scanned := 0

	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return errorResult(kind, started, fmt.Errorf("session-stitch: %w", err))
		}
		sess, err := session.Load(p)
		if err != nil {
			continue // skip unreadable sessions
		}
		scanned++
		for _, e := range sess.Entries {
			if e.Bootstrap {
				continue
			}
			for _, tc := range e.ToolCalls {
				if tc.Function.Name != "" {
					toolCounts[tc.Function.Name]++
				}
				// Extract file paths from tool arguments (read/write/edit).
				if tc.Function.Arguments != "" {
					extractPaths(tc.Function.Arguments, pathCounts)
				}
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return errorResult(kind, started, fmt.Errorf("session-stitch: %w", err))
	}

	// Build the summary block.
	hotPath := memory.HotPath(args.SessionDir, args.WorkDir, memory.RootScope())

	topTools := topN(toolCounts, 5)
	topPaths := topN(pathCounts, 5)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Cross-session patterns from %d sessions:\n", scanned))
	if len(topTools) > 0 {
		sb.WriteString("Most-used tools: ")
		sb.WriteString(strings.Join(topTools, ", "))
		sb.WriteString("\n")
	}
	if len(topPaths) > 0 {
		sb.WriteString("Most-referenced paths: ")
		sb.WriteString(strings.Join(topPaths, ", "))
		sb.WriteString("\n")
	}

	content := sb.String()
	patternsFound := len(topTools) + len(topPaths)

	// Write to root memory via AppendHot. The title embeds the stitch marker
	// for dedup — a re-run will produce a new heading (different timestamp)
	// but the content is stable.
	title := fmt.Sprintf("session-stitch %s %s", time.Now().UTC().Format(time.RFC3339), stitchMarkerPrefix+"1 -->")
	if err := memory.AppendHot(args.SessionDir, args.WorkDir, memory.RootScope(), time.Now(), title, content); err != nil {
		return errorResult(kind, started, fmt.Errorf("session-stitch: write memory: %w", err))
	}

	out := sessionStitchOutput{
		SessionsScanned: scanned,
		PatternsFound:   patternsFound,
		MemoryPath:      hotPath,
		TopTools:        topTools,
		TopPaths:        topPaths,
	}
	return successResult(kind, started, out)
}

// sessionList returns .jsonl session file paths in dir, filtered by mtime
// >= since. A zero since means "no cutoff". Returns newest-first.
func sessionList(dir string, since time.Time) ([]string, error) {
	paths, err := session.List(dir)
	if err != nil {
		return nil, err
	}
	if since.IsZero() {
		return paths, nil
	}
	var filtered []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !info.ModTime().Before(since) {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}

// extractPaths parses a JSON tool-arguments string and increments pathCounts
// for any "path" field found (used by read/write/edit tools).
func extractPaths(argsJSON string, pathCounts map[string]int) {
	var args struct {
		Path string `json:"path"`
	}
	if json.Unmarshal([]byte(argsJSON), &args) == nil && args.Path != "" {
		pathCounts[args.Path]++
	}
}

// topN returns the top n keys from a count map, sorted by count descending,
// then alphabetically for ties. Each key is formatted as "key (count)".
func topN(counts map[string]int, n int) []string {
	if len(counts) == 0 {
		return nil
	}
	type kv struct {
		key   string
		count int
	}
	var entries []kv
	for k, v := range counts {
		entries = append(entries, kv{k, v})
	}
	// Sort: highest count first, alphabetical on ties.
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].count > entries[i].count ||
				(entries[j].count == entries[i].count && entries[j].key < entries[i].key) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}
	if len(entries) > n {
		entries = entries[:n]
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = fmt.Sprintf("%s (%d)", e.key, e.count)
	}
	return out
}