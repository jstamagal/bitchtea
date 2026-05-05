package jobs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jstamagal/bitchtea/internal/daemon"
	"github.com/jstamagal/bitchtea/internal/memory"
)

// memoryFixture builds a session_dir + work_dir pair (both inside a single
// temp dir) so DailyPathForScope and HotPath resolve under
// <tmp>/sessions/... and <tmp>/memory/... respectively.
type memoryFixture struct {
	root       string
	sessionDir string
	workDir    string
}

func newMemoryFixture(t *testing.T) memoryFixture {
	t.Helper()
	root := t.TempDir()
	sessionDir := filepath.Join(root, "sessions")
	workDir := filepath.Join(root, "work")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir sessionDir: %v", err)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workDir: %v", err)
	}
	return memoryFixture{root: root, sessionDir: sessionDir, workDir: workDir}
}

// writeDaily writes a daily file with the same shape AppendDailyForScope
// emits. day is YYYY-MM-DD; entries is a list of (rfc3339-timestamp, body)
// pairs to embed as `## TS compaction flush\n\nBODY` blocks (heading
// defaults to "compaction" to match the pre-fix format that parseDailyEntries
// already handles — it extracts only timestamp from the heading).
func (f memoryFixture) writeDaily(t *testing.T, scope memory.Scope, day string, entries [][2]string) {
	t.Helper()
	when, err := time.Parse("2006-01-02", day)
	if err != nil {
		t.Fatalf("parse day: %v", err)
	}
	path := memory.DailyPathForScope(f.sessionDir, f.workDir, scope, when)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir daily: %v", err)
	}
	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString("## " + e[0] + " compaction flush\n\n")
		sb.WriteString(e[1])
		sb.WriteString("\n\n")
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write daily: %v", err)
	}
}

func channelArgs(f memoryFixture, channel, since string) json.RawMessage {
	a := memoryConsolidateArgs{
		SessionDir: f.sessionDir,
		WorkDir:    f.workDir,
		ScopeKind:  string(memory.ScopeChannel),
		ScopeName:  channel,
		Since:      since,
	}
	data, _ := json.Marshal(a)
	return data
}

func TestMemoryConsolidateAppendsUniqueEntries(t *testing.T) {
	f := newMemoryFixture(t)
	scope := memory.ChannelScope("dev", nil)

	f.writeDaily(t, scope, "2026-04-29", [][2]string{
		{"2026-04-29T10:00:00Z", "alpha note"},
		{"2026-04-29T10:00:00Z", "alpha note"}, // intra-file dupe
		{"2026-04-29T11:00:00Z", "beta note"},
	})
	f.writeDaily(t, scope, "2026-04-30", [][2]string{
		{"2026-04-30T09:00:00Z", "gamma note"},
	})

	job := daemon.Job{
		Kind: KindMemoryConsolidate,
		Args: channelArgs(f, "dev", "2026-04-29"),
	}
	res := Handle(context.Background(), job)
	if !res.Success {
		t.Fatalf("consolidate: %+v", res)
	}

	hotPath := memory.HotPath(f.sessionDir, f.workDir, scope)
	hotData, err := os.ReadFile(hotPath)
	if err != nil {
		t.Fatalf("read hot: %v", err)
	}
	hot := string(hotData)

	// All three unique notes should be present, in chronological order
	// (by daily filename then by entry order within file).
	wantOrder := []string{"alpha note", "beta note", "gamma note"}
	last := -1
	for _, s := range wantOrder {
		idx := strings.Index(hot, s)
		if idx < 0 {
			t.Fatalf("hot missing %q\n%s", s, hot)
		}
		if idx <= last {
			t.Fatalf("hot has %q out of order at %d (prev %d)", s, idx, last)
		}
		last = idx
	}

	// The intra-file duplicate "alpha note" got the SAME timestamp+daily,
	// so its marker collides on the second iteration and it should appear
	// exactly once in the hot file.
	if c := strings.Count(hot, "alpha note"); c != 1 {
		t.Fatalf("alpha note appears %d times, want 1", c)
	}

	// Output stats should reflect what happened.
	var out memoryConsolidateOutput
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.DailiesSeen != 2 {
		t.Fatalf("DailiesSeen = %d, want 2", out.DailiesSeen)
	}
	if out.EntriesAdded != 3 {
		t.Fatalf("EntriesAdded = %d, want 3", out.EntriesAdded)
	}
	if out.EntriesSkip != 1 {
		t.Fatalf("EntriesSkip = %d, want 1 (the in-file alpha dupe)", out.EntriesSkip)
	}
}

func TestMemoryConsolidateRespectsSinceCutoff(t *testing.T) {
	f := newMemoryFixture(t)
	scope := memory.ChannelScope("dev", nil)

	f.writeDaily(t, scope, "2026-03-01", [][2]string{
		{"2026-03-01T10:00:00Z", "old note"},
	})
	f.writeDaily(t, scope, "2026-04-30", [][2]string{
		{"2026-04-30T09:00:00Z", "fresh note"},
	})

	res := Handle(context.Background(), daemon.Job{
		Kind: KindMemoryConsolidate,
		Args: channelArgs(f, "dev", "2026-04-01"),
	})
	if !res.Success {
		t.Fatalf("consolidate: %+v", res)
	}
	hotData, _ := os.ReadFile(memory.HotPath(f.sessionDir, f.workDir, scope))
	hot := string(hotData)
	if strings.Contains(hot, "old note") {
		t.Fatalf("since cutoff missed: 'old note' should be excluded\n%s", hot)
	}
	if !strings.Contains(hot, "fresh note") {
		t.Fatalf("since cutoff over-eager: 'fresh note' missing\n%s", hot)
	}
}

func TestMemoryConsolidateIdempotent(t *testing.T) {
	f := newMemoryFixture(t)
	scope := memory.ChannelScope("dev", nil)
	f.writeDaily(t, scope, "2026-04-30", [][2]string{
		{"2026-04-30T09:00:00Z", "alpha"},
		{"2026-04-30T10:00:00Z", "beta"},
	})

	job := daemon.Job{Kind: KindMemoryConsolidate, Args: channelArgs(f, "dev", "")}

	res1 := Handle(context.Background(), job)
	if !res1.Success {
		t.Fatalf("first run: %+v", res1)
	}
	hotPath := memory.HotPath(f.sessionDir, f.workDir, scope)
	first, err := os.ReadFile(hotPath)
	if err != nil {
		t.Fatalf("read first: %v", err)
	}

	res2 := Handle(context.Background(), job)
	if !res2.Success {
		t.Fatalf("second run: %+v", res2)
	}
	second, err := os.ReadFile(hotPath)
	if err != nil {
		t.Fatalf("read second: %v", err)
	}

	if string(first) != string(second) {
		t.Fatalf("hot file changed on second run\nfirst:\n%s\nsecond:\n%s", first, second)
	}

	// Second run reports zero added, two skipped (idempotency proof).
	var out2 memoryConsolidateOutput
	if err := json.Unmarshal(res2.Output, &out2); err != nil {
		t.Fatalf("unmarshal out2: %v", err)
	}
	if out2.EntriesAdded != 0 {
		t.Fatalf("second run added %d entries; idempotency requires 0", out2.EntriesAdded)
	}
	if out2.EntriesSkip != 2 {
		t.Fatalf("second run skipped %d; want 2", out2.EntriesSkip)
	}
}

func TestMemoryConsolidateHonorsCancellation(t *testing.T) {
	f := newMemoryFixture(t)
	scope := memory.ChannelScope("dev", nil)
	f.writeDaily(t, scope, "2026-04-30", [][2]string{
		{"2026-04-30T09:00:00Z", "alpha"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res := Handle(ctx, daemon.Job{
		Kind: KindMemoryConsolidate,
		Args: channelArgs(f, "dev", ""),
	})
	if res.Success {
		t.Fatalf("pre-cancelled ctx: want failure, got success")
	}
	if !strings.Contains(res.Error, "canceled") {
		t.Fatalf("error = %q", res.Error)
	}
	// HOT.md should not have been created.
	if _, err := os.Stat(memory.HotPath(f.sessionDir, f.workDir, scope)); !os.IsNotExist(err) {
		t.Fatalf("hot file should not exist after pre-cancel: %v", err)
	}
}

func TestMemoryConsolidateFallsBackToEnvelopeScope(t *testing.T) {
	// Caller provides scope via envelope (job.Scope) instead of args.
	f := newMemoryFixture(t)
	scope := memory.ChannelScope("backchat", nil)
	f.writeDaily(t, scope, "2026-04-30", [][2]string{
		{"2026-04-30T09:00:00Z", "envelope-routed"},
	})

	args, _ := json.Marshal(memoryConsolidateArgs{
		SessionDir: f.sessionDir,
	})
	job := daemon.Job{
		Kind:    KindMemoryConsolidate,
		Args:    args,
		WorkDir: f.workDir,
		Scope:   daemon.Scope{Kind: "channel", Name: "backchat"},
	}
	res := Handle(context.Background(), job)
	if !res.Success {
		t.Fatalf("envelope-routed: %+v", res)
	}
	hot, _ := os.ReadFile(memory.HotPath(f.sessionDir, f.workDir, scope))
	if !strings.Contains(string(hot), "envelope-routed") {
		t.Fatalf("envelope scope not honored:\n%s", hot)
	}
}

func TestMemoryConsolidateRequiresWorkDir(t *testing.T) {
	args, _ := json.Marshal(memoryConsolidateArgs{SessionDir: t.TempDir()})
	res := Handle(context.Background(), daemon.Job{Kind: KindMemoryConsolidate, Args: args})
	if res.Success {
		t.Fatalf("missing work_dir: want failure")
	}
	if !strings.Contains(res.Error, "work_dir") {
		t.Fatalf("error = %q", res.Error)
	}
}

func TestMemoryConsolidateRequiresSessionDir(t *testing.T) {
	args, _ := json.Marshal(memoryConsolidateArgs{WorkDir: t.TempDir()})
	res := Handle(context.Background(), daemon.Job{Kind: KindMemoryConsolidate, Args: args})
	if res.Success {
		t.Fatalf("missing session_dir: want failure")
	}
	if !strings.Contains(res.Error, "session_dir") {
		t.Fatalf("error = %q", res.Error)
	}
}

func TestMemoryConsolidateQueryUnderChannelPreservesParent(t *testing.T) {
	f := newMemoryFixture(t)
	channelScope := memory.ChannelScope("dev", nil)
	// Query scope under channel: runtime writes use this hierarchy where
	// the daily and hot files land under channels/<chan>/queries/<nick>/
	// NOT flat under queries/<nick>/.
	scope := memory.QueryScope("tj", &channelScope)

	f.writeDaily(t, scope, "2026-04-30", [][2]string{
		{"2026-04-30T09:00:00Z", "query under channel"},
	})

	args, _ := json.Marshal(memoryConsolidateArgs{
		SessionDir:      f.sessionDir,
		WorkDir:         f.workDir,
		ScopeKind:       string(memory.ScopeQuery),
		ScopeName:       "tj",
		ScopeParentKind: string(memory.ScopeChannel),
		ScopeParentName: "dev",
	})
	res := Handle(context.Background(), daemon.Job{
		Kind: KindMemoryConsolidate,
		Args: args,
	})
	if !res.Success {
		t.Fatalf("query under channel: %+v", res)
	}

	// The hot path should be nested, not flat.
	hotPath := memory.HotPath(f.sessionDir, f.workDir, scope)
	if !strings.Contains(hotPath, filepath.Join("channels", "dev", "queries", "tj")) {
		t.Fatalf("hot path %q should contain channels/dev/queries/tj", hotPath)
	}
	// It must NOT be the flat queries/<nick>/HOT.md.
	flatHot := memory.HotPath(f.sessionDir, f.workDir, memory.QueryScope("tj", nil))
	if hotPath == flatHot {
		t.Fatalf("hot path %q should differ from flat query path %q", hotPath, flatHot)
	}

	hot, _ := os.ReadFile(hotPath)
	if !strings.Contains(string(hot), "query under channel") {
		t.Fatalf("hot missing content:\n%s", hot)
	}
}
