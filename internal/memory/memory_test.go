package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestScopeName(t *testing.T) {
	got := scopeName(filepath.Join("tmp", "My Repo!"))
	if !strings.HasPrefix(got, "my-repo-") {
		t.Fatalf("scopeName() = %q, want sanitized base prefix", got)
	}
	if len(got) != len("my-repo-")+8 {
		t.Fatalf("scopeName() = %q, want 8-char hash suffix", got)
	}
}

func TestMemoryBaseDir(t *testing.T) {
	base := t.TempDir()
	sessionDir := filepath.Join(base, "sessions")
	workDir := filepath.Join(base, "Work Repo")

	got := memoryBaseDir(sessionDir, workDir)
	want := filepath.Join(base, "memory", scopeName(workDir))
	if got != want {
		t.Fatalf("memoryBaseDir() = %q, want %q", got, want)
	}
}

func TestHotPath_root(t *testing.T) {
	sessionDir, workDir := testDirs(t)

	got := HotPath(sessionDir, workDir, RootScope())
	want := filepath.Join(workDir, "MEMORY.md")
	if got != want {
		t.Fatalf("HotPath(root) = %q, want %q", got, want)
	}
}

func TestHotPath_channel(t *testing.T) {
	sessionDir, workDir := testDirs(t)
	root := RootScope()
	channel := ChannelScope("#Dev Ops", &root)

	got := HotPath(sessionDir, workDir, channel)
	want := filepath.Join(memoryBaseDir(sessionDir, workDir), "contexts", "channels", "dev-ops", "HOT.md")
	if got != want {
		t.Fatalf("HotPath(channel) = %q, want %q", got, want)
	}
}

func TestHotPath_nestedQueryUnderChannel(t *testing.T) {
	sessionDir, workDir := testDirs(t)
	root := RootScope()
	channel := ChannelScope("#Dev Ops", &root)
	query := QueryScope("Ada Lovelace", &channel)

	got := HotPath(sessionDir, workDir, query)
	want := filepath.Join(memoryBaseDir(sessionDir, workDir), "contexts", "channels", "dev-ops", "queries", "ada-lovelace", "HOT.md")
	if got != want {
		t.Fatalf("HotPath(query) = %q, want %q", got, want)
	}
}

func TestDailyPathForScope_root(t *testing.T) {
	sessionDir, workDir := testDirs(t)
	when := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)

	got := DailyPathForScope(sessionDir, workDir, RootScope(), when)
	want := filepath.Join(memoryBaseDir(sessionDir, workDir), "2026-05-04.md")
	if got != want {
		t.Fatalf("DailyPathForScope(root) = %q, want %q", got, want)
	}
}

func TestDailyPathForScope_channel(t *testing.T) {
	sessionDir, workDir := testDirs(t)
	root := RootScope()
	channel := ChannelScope("#Dev Ops", &root)
	when := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)

	got := DailyPathForScope(sessionDir, workDir, channel, when)
	want := filepath.Join(memoryBaseDir(sessionDir, workDir), "contexts", "channels", "dev-ops", "daily", "2026-05-04.md")
	if got != want {
		t.Fatalf("DailyPathForScope(channel) = %q, want %q", got, want)
	}
}

func TestAppendHot_createsParentDirs(t *testing.T) {
	sessionDir, workDir := testDirs(t)
	root := RootScope()
	channel := ChannelScope("#Ops", &root)
	when := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)

	if err := AppendHot(sessionDir, workDir, channel, when, "deploy", "use the blue pool"); err != nil {
		t.Fatalf("AppendHot() error = %v", err)
	}

	data := readFile(t, HotPath(sessionDir, workDir, channel))
	if !strings.Contains(data, "use the blue pool") {
		t.Fatalf("hot memory content = %q, want appended note", data)
	}
}

func TestAppendHot_flockExclusive(t *testing.T) {
	sessionDir, workDir := testDirs(t)
	root := RootScope()
	when := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)

	const writes = 25
	var wg sync.WaitGroup
	errs := make(chan error, writes)
	for i := 0; i < writes; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- AppendHot(sessionDir, workDir, root, when, fmt.Sprintf("entry-%02d", i), fmt.Sprintf("payload-%02d", i))
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("AppendHot() concurrent error = %v", err)
		}
	}

	data := readFile(t, HotPath(sessionDir, workDir, root))
	if got := strings.Count(data, "\n\npayload-"); got != writes {
		t.Fatalf("concurrent append wrote %d entries, want %d\n%s", got, writes, data)
	}
}

func TestAppendHot_emptyContentNoOp(t *testing.T) {
	sessionDir, workDir := testDirs(t)

	if err := AppendHot(sessionDir, workDir, RootScope(), time.Now(), "ignored", " \n\t "); err != nil {
		t.Fatalf("AppendHot(empty) error = %v", err)
	}
	if _, err := os.Stat(HotPath(sessionDir, workDir, RootScope())); !os.IsNotExist(err) {
		t.Fatalf("empty AppendHot created file or unexpected stat error: %v", err)
	}
}

func TestAppendHot_headingFormat(t *testing.T) {
	sessionDir, workDir := testDirs(t)
	when := time.Date(2026, 5, 4, 12, 30, 45, 0, time.UTC)

	if err := AppendHot(sessionDir, workDir, RootScope(), when, "remember", "trimmed"); err != nil {
		t.Fatalf("AppendHot() error = %v", err)
	}

	got := readFile(t, HotPath(sessionDir, workDir, RootScope()))
	want := "## remember (2026-05-04T12:30:45Z)\n\ntrimmed\n\n"
	if got != want {
		t.Fatalf("hot entry = %q, want %q", got, want)
	}
}

func TestAppendDailyForScope_headingReflectsWriterSource(t *testing.T) {
	sessionDir, workDir := testDirs(t)
	when := time.Date(2026, 5, 4, 12, 30, 45, 0, time.UTC)

	// Compaction source
	path := DailyPathForScope(sessionDir, workDir, RootScope(), when)
	os.Remove(path) // fresh start
	if err := AppendDailyForScope(sessionDir, workDir, RootScope(), when, SourceCompaction, "daily note"); err != nil {
		t.Fatalf("AppendDailyForScope(compaction) error = %v", err)
	}
	got := readFile(t, path)
	want := "## 2026-05-04T12:30:45Z compaction flush\n\ndaily note\n\n"
	if got != want {
		t.Fatalf("compaction daily entry = %q, want %q", got, want)
	}

	// Tool-write source — re-create path for clean start
	os.Remove(path)
	if err := AppendDailyForScope(sessionDir, workDir, RootScope(), when, SourceToolWrite, "tool write note"); err != nil {
		t.Fatalf("AppendDailyForScope(tool-write) error = %v", err)
	}
	got = readFile(t, path)
	wantTool := "## 2026-05-04T12:30:45Z tool-write flush\n\ntool write note\n\n"
	if got != wantTool {
		t.Fatalf("tool-write daily entry = %q, want %q", got, wantTool)
	}

	// Daemon-consolidation source
	os.Remove(path)
	if err := AppendDailyForScope(sessionDir, workDir, RootScope(), when, SourceDaemonConsolidation, "daemon note"); err != nil {
		t.Fatalf("AppendDailyForScope(daemon) error = %v", err)
	}
	got = readFile(t, path)
	wantDaemon := "## 2026-05-04T12:30:45Z daemon-consolidation flush\n\ndaemon note\n\n"
	if got != wantDaemon {
		t.Fatalf("daemon daily entry = %q, want %q", got, wantDaemon)
	}
}

func TestSearchInScope_allTermsRequired(t *testing.T) {
	sessionDir, workDir := testDirs(t)
	when := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	if err := AppendHot(sessionDir, workDir, RootScope(), when, "first", "alpha beta"); err != nil {
		t.Fatal(err)
	}
	if err := AppendDailyForScope(sessionDir, workDir, RootScope(), when, SourceCompaction, "alpha only"); err != nil {
		t.Fatal(err)
	}

	results, err := SearchInScope(sessionDir, workDir, RootScope(), "alpha beta", 10)
	if err != nil {
		t.Fatalf("SearchInScope() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("SearchInScope() returned %d results, want 1: %#v", len(results), results)
	}
	if !strings.Contains(results[0].Snippet, "alpha beta") {
		t.Fatalf("snippet = %q, want full-term hit", results[0].Snippet)
	}
}

func TestSearchInScope_preferFullQueryMatch(t *testing.T) {
	sessionDir, workDir := testDirs(t)
	content := strings.Repeat("prefix ", 80) + "alpha far away " + strings.Repeat("middle ", 80) + "alpha beta exact phrase " + strings.Repeat("suffix ", 80)
	if err := SaveScoped(sessionDir, workDir, RootScope(), content); err != nil {
		t.Fatal(err)
	}

	results, err := SearchInScope(sessionDir, workDir, RootScope(), "alpha beta", 1)
	if err != nil {
		t.Fatalf("SearchInScope() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("SearchInScope() returned %d results, want 1", len(results))
	}
	if !strings.Contains(results[0].Snippet, "alpha beta exact phrase") {
		t.Fatalf("snippet = %q, want anchored near full query", results[0].Snippet)
	}
}

func TestSearchInScope_lineageOrder(t *testing.T) {
	sessionDir, workDir := testDirs(t)
	root := RootScope()
	channel := ChannelScope("#Ops", &root)
	query := QueryScope("deploy", &channel)
	when := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	mustAppendHot(t, sessionDir, workDir, root, when, "root", "lineage needle from root")
	mustAppendHot(t, sessionDir, workDir, channel, when, "channel", "lineage needle from channel")
	mustAppendHot(t, sessionDir, workDir, query, when, "query", "lineage needle from query")

	results, err := SearchInScope(sessionDir, workDir, query, "lineage needle", 10)
	if err != nil {
		t.Fatalf("SearchInScope() error = %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("SearchInScope() returned %d results, want 3: %#v", len(results), results)
	}
	for i, want := range []string{"query", "channel", "root"} {
		if !strings.HasPrefix(results[i].Heading, want+" (") {
			t.Fatalf("result %d heading = %q, want timestamped %q prefix; all results: %#v", i, results[i].Heading, want, results)
		}
	}
}

func TestSearchInScope_childDoesNotLeakToRoot(t *testing.T) {
	sessionDir, workDir := testDirs(t)
	root := RootScope()
	channel := ChannelScope("#Ops", &root)
	mustAppendHot(t, sessionDir, workDir, channel, time.Now(), "channel", "child-only secret")

	results, err := SearchInScope(sessionDir, workDir, root, "child-only secret", 10)
	if err != nil {
		t.Fatalf("SearchInScope() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("root search leaked child results: %#v", results)
	}
}

func TestSearchInScope_oneResultPerFile(t *testing.T) {
	sessionDir, workDir := testDirs(t)
	when := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	mustAppendHot(t, sessionDir, workDir, RootScope(), when, "one", "duplicate needle")
	mustAppendHot(t, sessionDir, workDir, RootScope(), when, "two", "duplicate needle")

	results, err := SearchInScope(sessionDir, workDir, RootScope(), "duplicate needle", 10)
	if err != nil {
		t.Fatalf("SearchInScope() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("SearchInScope() returned %d results, want one per file: %#v", len(results), results)
	}
}

func TestSearchInScope_readErrorReturnsError(t *testing.T) {
	sessionDir, workDir := testDirs(t)
	dailyDir := filepath.Dir(DailyPathForScope(sessionDir, workDir, RootScope(), time.Now()))
	if err := os.MkdirAll(filepath.Dir(dailyDir), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dailyDir, []byte("not a directory"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := SearchInScope(sessionDir, workDir, RootScope(), "anything", 10)
	if err == nil {
		t.Fatal("SearchInScope() error = nil, want read daily memory dir error")
	}
	if !strings.Contains(err.Error(), "read daily memory dir") {
		t.Fatalf("SearchInScope() error = %v, want daily dir read context", err)
	}
}

func TestSearchResult_snippetEllipsis(t *testing.T) {
	content := strings.Repeat("a", 200) + " needle " + strings.Repeat("b", 200)

	got := extractSnippet(content, strings.Index(content, "needle"), 80)
	if !strings.HasPrefix(got, "... ") || !strings.HasSuffix(got, " ...") {
		t.Fatalf("extractSnippet() = %q, want both ellipses", got)
	}
	if !strings.Contains(got, "needle") {
		t.Fatalf("extractSnippet() = %q, want match retained", got)
	}
}

func TestLoad_permissionErrorReturnsEmpty(t *testing.T) {
	workDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(workDir, "MEMORY.md"), 0755); err != nil {
		t.Fatal(err)
	}

	if got := Load(workDir); got != "" {
		t.Fatalf("Load() = %q, want empty string on read error", got)
	}
}

func TestSaveScoped_createsParentDirs(t *testing.T) {
	sessionDir, workDir := testDirs(t)
	root := RootScope()
	channel := ChannelScope("#Ops", &root)

	if err := SaveScoped(sessionDir, workDir, channel, "saved content"); err != nil {
		t.Fatalf("SaveScoped() error = %v", err)
	}
	if got := readFile(t, HotPath(sessionDir, workDir, channel)); got != "saved content" {
		t.Fatalf("scoped hot memory = %q, want saved content", got)
	}
}

func TestSanitizeSegment(t *testing.T) {
	tests := map[string]string{
		"#Dev Ops":        "dev-ops",
		"  Alice/Bob  ":   "alice-bob",
		"!!!":             "unnamed",
		"Already--Clean?": "already-clean",
	}
	for input, want := range tests {
		if got := sanitizeSegment(input); got != want {
			t.Fatalf("sanitizeSegment(%q) = %q, want %q", input, got, want)
		}
	}
}

func testDirs(t *testing.T) (string, string) {
	t.Helper()
	base := t.TempDir()
	sessionDir := filepath.Join(base, "sessions")
	workDir := filepath.Join(base, "work")
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatal(err)
	}
	return sessionDir, workDir
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func mustAppendHot(t *testing.T, sessionDir, workDir string, scope Scope, when time.Time, title, content string) {
	t.Helper()
	if err := AppendHot(sessionDir, workDir, scope, when, title, content); err != nil {
		t.Fatalf("AppendHot(%s): %v", title, err)
	}
}

func TestSearchInScopeWhitespaceOnlyQuery(t *testing.T) {
	sessionDir, workDir := testDirs(t)

	_, err := SearchInScope(sessionDir, workDir, RootScope(), "   \t  ", 10)
	if err == nil {
		t.Fatal("expected error for whitespace-only query, got nil")
	}
	if !strings.Contains(err.Error(), "query is required") {
		t.Fatalf("error = %q, want 'query is required'", err.Error())
	}
}

func TestSearchInScopeLimitOne(t *testing.T) {
	sessionDir, workDir := testDirs(t)
	when := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)

	// Write two separate files that both match.
	mustAppendHot(t, sessionDir, workDir, RootScope(), when, "first", "unique needle alpha")
	root := RootScope()
	channel := ChannelScope("#Test", &root)
	mustAppendHot(t, sessionDir, workDir, channel, when, "second", "unique needle beta")

	results, err := SearchInScope(sessionDir, workDir, channel, "unique needle", 1)
	if err != nil {
		t.Fatalf("SearchInScope limit=1: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected exactly 1 result with limit=1, got %d", len(results))
	}
}
