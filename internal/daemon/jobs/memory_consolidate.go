package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jstamagal/bitchtea/internal/daemon"
	"github.com/jstamagal/bitchtea/internal/memory"
)

// memoryConsolidateArgs is the wire shape of a memory-consolidate job's Args.
//
// session_dir + work_dir mirror the (sessionDir, workDir) pair every memory
// helper takes. scope_kind / scope_name reconstruct a memory.Scope without
// importing the agent package (where the live MemoryScope lives). since is
// a YYYY-MM-DD date; only daily files dated >= since are folded into hot.
//
// scope_parent_kind / scope_parent_name let callers express the parent scope
// hierarchy (e.g. a query scope under a channel). When both are non-empty,
// buildScope constructs the parent scope first and supplies it to the child.
type memoryConsolidateArgs struct {
	SessionDir       string `json:"session_dir"`
	WorkDir          string `json:"work_dir"`
	ScopeKind        string `json:"scope_kind,omitempty"`        // "root" | "channel" | "query"; defaults to root
	ScopeName        string `json:"scope_name,omitempty"`
	ScopeParentKind  string `json:"scope_parent_kind,omitempty"`  // parent scope kind (e.g. "channel")
	ScopeParentName  string `json:"scope_parent_name,omitempty"`  // parent scope name
	Since            string `json:"since,omitempty"`              // YYYY-MM-DD; empty means all
}

// memoryConsolidateOutput summarises what the run did. dailies_seen counts
// every daily file in scope since the cutoff; entries_added is how many
// new entries were appended to HOT.md (zero on a no-op idempotent re-run).
type memoryConsolidateOutput struct {
	HotPath      string `json:"hot_path"`
	DailiesSeen  int    `json:"dailies_seen"`
	EntriesAdded int    `json:"entries_added"`
	EntriesSkip  int    `json:"entries_skipped"`
}

// consolidatedMarkerPrefix is embedded as an HTML comment at the start of
// every consolidated block. It encodes the source daily file's basename
// (e.g. 2026-04-30.md) and the entry's RFC3339 timestamp, giving us a
// stable key the next run can grep for to decide "already done".
//
// We never delete or rewrite blocks the marker doesn't own — if a user
// hand-edits HOT.md, our markers just accumulate next to their notes.
const consolidatedMarkerPrefix = "<!-- bitchtea-consolidated:"

// dailyEntry is a parsed `## TIMESTAMP pre-compaction flush\n\nBODY` block
// from a daily memory file. The raw body is preserved verbatim so we don't
// silently rewrite the user's content during consolidation.
type dailyEntry struct {
	Timestamp string // raw RFC3339 string from the heading
	Body      string // trimmed body text under the heading
}

// handleMemoryConsolidate scans daily memory files for a scope and folds
// new entries into the hot file. Re-running produces the same final hot
// file (no duplicates) thanks to the consolidatedMarker dedupe.
//
// Bound: 60s context. Memory dirs are small (one file per day per scope),
// but the dedupe scan reads the whole hot file and we want to play safe
// on a slow filesystem.
//
// Cancellation: ctx.Err() is checked between every per-daily-file pass and
// before the final hot-file write so a SIGTERM during a large consolidation
// aborts cleanly.
func handleMemoryConsolidate(ctx context.Context, job daemon.Job) daemon.Result {
	started := time.Now().UTC()
	kind := KindMemoryConsolidate

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if err := ctx.Err(); err != nil {
		return errorResult(kind, started, fmt.Errorf("memory-consolidate: %w", err))
	}

	var args memoryConsolidateArgs
	if len(job.Args) > 0 {
		if err := json.Unmarshal(job.Args, &args); err != nil {
			return errorResult(kind, started, fmt.Errorf("memory-consolidate: parse args: %w", err))
		}
	}
	// Fall back to envelope-level fields where the args omit them. The
	// daemon envelope already carries WorkDir + Scope, so a caller can
	// drop Args entirely for the common case.
	if args.WorkDir == "" {
		args.WorkDir = job.WorkDir
	}
	if args.ScopeKind == "" {
		args.ScopeKind = job.Scope.Kind
	}
	if args.ScopeName == "" {
		args.ScopeName = job.Scope.Name
	}
	if args.WorkDir == "" {
		return errorResult(kind, started, fmt.Errorf("memory-consolidate: work_dir is required"))
	}
	if args.SessionDir == "" {
		return errorResult(kind, started, fmt.Errorf("memory-consolidate: session_dir is required"))
	}

	scope, err := buildScope(args.ScopeKind, args.ScopeName, args.ScopeParentKind, args.ScopeParentName)
	if err != nil {
		return errorResult(kind, started, fmt.Errorf("memory-consolidate: %w", err))
	}

	since, err := parseSince(args.Since)
	if err != nil {
		return errorResult(kind, started, fmt.Errorf("memory-consolidate: %w", err))
	}

	// Resolve the daily directory by asking memory for any same-scope
	// daily path and stripping the filename. This keeps the path layout
	// owned entirely by internal/memory.
	dailyDir := filepath.Dir(memory.DailyPathForScope(args.SessionDir, args.WorkDir, scope, time.Now()))
	hotPath := memory.HotPath(args.SessionDir, args.WorkDir, scope)

	dailyFiles, err := listDailyFiles(dailyDir, since)
	if err != nil {
		return errorResult(kind, started, fmt.Errorf("memory-consolidate: list daily: %w", err))
	}

	if err := ctx.Err(); err != nil {
		return errorResult(kind, started, fmt.Errorf("memory-consolidate: %w", err))
	}

	existingMarkers, err := loadConsolidatedMarkers(hotPath)
	if err != nil {
		return errorResult(kind, started, fmt.Errorf("memory-consolidate: scan hot: %w", err))
	}

	added := 0
	skipped := 0
	for _, dailyPath := range dailyFiles {
		if err := ctx.Err(); err != nil {
			return errorResult(kind, started, fmt.Errorf("memory-consolidate: %w", err))
		}
		entries, err := parseDailyEntries(dailyPath)
		if err != nil {
			return errorResult(kind, started, fmt.Errorf("memory-consolidate: parse %s: %w", dailyPath, err))
		}
		dailyBase := filepath.Base(dailyPath)
		for _, entry := range entries {
			marker := makeMarker(dailyBase, entry.Timestamp)
			if _, seen := existingMarkers[marker]; seen {
				skipped++
				continue
			}
			if err := appendConsolidatedBlock(hotPath, marker, entry); err != nil {
				return errorResult(kind, started, fmt.Errorf("memory-consolidate: write hot: %w", err))
			}
			existingMarkers[marker] = struct{}{}
			added++
		}
	}

	out := memoryConsolidateOutput{
		HotPath:      hotPath,
		DailiesSeen:  len(dailyFiles),
		EntriesAdded: added,
		EntriesSkip:  skipped,
	}
	return successResult(kind, started, out)
}

// buildScope reconstructs a memory.Scope from the wire kind/name/parent fields.
// When parentKind and parentName are both non-empty, the parent scope is
// constructed first and supplied to the child scope so the resulting scope
// hierarchy mirrors what runtime memory writes produce (e.g. a query scope
// under a channel scope, yielding path channels/<chan>/queries/<nick>/HOT.md
// instead of the flat queries/<nick>/HOT.md).
func buildScope(kind, name, parentKind, parentName string) (memory.Scope, error) {
	var parent *memory.Scope
	if parentKind != "" && parentName != "" {
		p, err := buildScope(parentKind, parentName, "", "")
		if err != nil {
			return memory.Scope{}, fmt.Errorf("parent scope: %w", err)
		}
		parent = &p
	}

	switch memory.ScopeKind(kind) {
	case "", memory.ScopeRoot:
		return memory.RootScope(), nil
	case memory.ScopeChannel:
		if name == "" {
			return memory.Scope{}, fmt.Errorf("scope_name is required for channel scope")
		}
		return memory.ChannelScope(name, parent), nil
	case memory.ScopeQuery:
		if name == "" {
			return memory.Scope{}, fmt.Errorf("scope_name is required for query scope")
		}
		return memory.QueryScope(name, parent), nil
	default:
		return memory.Scope{}, fmt.Errorf("unknown scope_kind %q", kind)
	}
}

func parseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("since must be YYYY-MM-DD: %w", err)
	}
	return t, nil
}

// listDailyFiles returns daily files in dir whose basename date is >= since.
// A zero since means "no cutoff". Output is sorted ascending by filename
// (which matches chronological order for our YYYY-MM-DD naming).
func listDailyFiles(dir string, since time.Time) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var paths []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		if !since.IsZero() {
			datePart := strings.TrimSuffix(e.Name(), ".md")
			t, err := time.Parse("2006-01-02", datePart)
			if err != nil {
				// Non-date filenames in the daily dir get skipped; we are
				// not the police of what else lands there.
				continue
			}
			if t.Before(since) {
				continue
			}
		}
		paths = append(paths, filepath.Join(dir, e.Name()))
	}
	sort.Strings(paths)
	return paths, nil
}

// loadConsolidatedMarkers reads hotPath (if it exists) and returns the set
// of consolidatedMarker strings already present in it. Used to skip entries
// that a previous run already folded in. We scan the file content (not just
// lines starting with the prefix) because markers are embedded inside
// markdown heading lines like `## consolidated TS <!-- ... -->`.
func loadConsolidatedMarkers(hotPath string) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	data, err := os.ReadFile(hotPath)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	content := string(data)
	for {
		idx := strings.Index(content, consolidatedMarkerPrefix)
		if idx < 0 {
			break
		}
		end := strings.Index(content[idx:], " -->")
		if end < 0 {
			// Truncated marker; bail to avoid an infinite loop.
			break
		}
		marker := content[idx : idx+end+len(" -->")]
		out[marker] = struct{}{}
		content = content[idx+end+len(" -->"):]
	}
	return out, nil
}

// makeMarker formats the dedupe sentinel for a (daily-file, entry-ts) pair.
// We pipe-separate fields so a daily filename containing a colon can't
// collide with the prefix punctuation.
func makeMarker(dailyBase, ts string) string {
	return fmt.Sprintf("%s%s|%s -->", consolidatedMarkerPrefix, dailyBase, ts)
}

// appendConsolidatedBlock writes a marker + entry body to the hot file via
// memory.AppendHot. We embed the marker inside the title argument so it
// becomes part of the heading line and survives any future reformatting
// AppendHot may do.
func appendConsolidatedBlock(hotPath, marker string, entry dailyEntry) error {
	// AppendHot needs sessionDir/workDir/scope, but we already know the
	// final hot file path. Rather than reverse-engineer those inputs we
	// open the file directly: the contract here is the same (append a
	// "## title\n\nbody\n\n" block, mkdir parents, no flock contention
	// because the daemon is single-instance).
	if err := os.MkdirAll(filepath.Dir(hotPath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(hotPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	heading := fmt.Sprintf("consolidated %s %s", entry.Timestamp, marker)
	block := fmt.Sprintf("## %s\n\n%s\n\n", heading, strings.TrimSpace(entry.Body))
	_, err = f.WriteString(block)
	return err
}

// parseDailyEntries reads a daily memory file and returns its entries in
// file order. Daily files have the format produced by AppendDailyForScope:
//
//   ## RFC3339 pre-compaction flush
//
//   <body lines...>
//
//   ## RFC3339 pre-compaction flush
//
//   <body lines...>
//
// We split on `## ` headings and pull the timestamp out of the first
// whitespace-delimited token. Malformed blocks (no timestamp) are skipped
// silently — the daemon should not crash on garbage in the memory dir.
func parseDailyEntries(path string) ([]dailyEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")

	var entries []dailyEntry
	var cur *dailyEntry
	flush := func() {
		if cur == nil {
			return
		}
		cur.Body = strings.TrimSpace(cur.Body)
		if cur.Timestamp != "" {
			entries = append(entries, *cur)
		}
		cur = nil
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			flush()
			heading := strings.TrimPrefix(line, "## ")
			fields := strings.Fields(heading)
			if len(fields) == 0 {
				continue
			}
			cur = &dailyEntry{Timestamp: fields[0]}
			continue
		}
		if cur == nil {
			continue
		}
		cur.Body += line + "\n"
	}
	flush()
	return entries, nil
}
