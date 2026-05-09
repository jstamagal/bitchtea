package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	memorypkg "github.com/jstamagal/bitchtea/internal/memory"
)

func TestDiscoverContextFiles(t *testing.T) {
	// CWD-only discovery: a context file in workDir is loaded, but ancestor
	// directories must NOT be consulted. Running bitchtea inside a subdirectory
	// should never absorb unrelated parent AGENTS.md files.
	root := t.TempDir()
	child := filepath.Join(root, "sub", "project")
	os.MkdirAll(child, 0755)

	os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("root agent rules"), 0644)
	os.WriteFile(filepath.Join(child, "BITCHTEA.md"), []byte("project bitchtea rules"), 0644)

	result := DiscoverContextFiles(child)

	if !strings.Contains(result, "project bitchtea rules") {
		t.Error("missing child BITCHTEA.md content")
	}
	if strings.Contains(result, "root agent rules") {
		t.Error("ancestor AGENTS.md must NOT be loaded — discovery is CWD-only")
	}
}

func TestDiscoverContextFilesNoWalkUp(t *testing.T) {
	// Explicit guard: if workDir has no context file but an ancestor does,
	// the result must be empty. This is the bt-yuh regression fix.
	root := t.TempDir()
	child := filepath.Join(root, "nested")
	os.MkdirAll(child, 0755)

	os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("ancestor context that must not leak"), 0644)

	result := DiscoverContextFiles(child)
	if result != "" {
		t.Errorf("expected empty result (no walk-up), got: %q", result)
	}
}

func TestDiscoverContextFilesPreferenceOrder(t *testing.T) {
	// When both BITCHTEA.md and AGENTS.md exist in the same dir,
	// BITCHTEA.md should win.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "BITCHTEA.md"), []byte("bitchtea wins"), 0644)
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("agents loses"), 0644)

	result := DiscoverContextFiles(dir)
	if !strings.Contains(result, "bitchtea wins") {
		t.Error("BITCHTEA.md should beat AGENTS.md")
	}
	if strings.Contains(result, "agents loses") {
		t.Error("AGENTS.md should not be loaded when BITCHTEA.md exists in same dir")
	}
}

func TestDiscoverContextFilesNoCLAUDE(t *testing.T) {
	// CLAUDE.md is no longer loaded — only put it in the dir and confirm it's ignored.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("claude should be ignored"), 0644)

	result := DiscoverContextFiles(dir)
	if strings.Contains(result, "claude should be ignored") {
		t.Error("CLAUDE.md should not be loaded (not in preference list)")
	}
}

func TestDiscoverContextFilesAgentMDFallback(t *testing.T) {
	// AGENT.md (singular) is the second preference.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("single agent context"), 0644)

	result := DiscoverContextFiles(dir)
	if !strings.Contains(result, "single agent context") {
		t.Error("AGENT.md should be loaded as second preference")
	}
}

func TestDiscoverContextFilesNone(t *testing.T) {
	dir := t.TempDir()
	result := DiscoverContextFiles(dir)
	// Should be empty (will find nothing before hitting root)
	// The function walks up, so it might find things in /tmp etc.
	// But in practice there won't be AGENTS.md there
	_ = result
}

func TestLoadSaveMemory(t *testing.T) {
	dir := t.TempDir()

	// Initially empty
	mem := LoadMemory(dir)
	if mem != "" {
		t.Fatalf("expected empty memory, got %q", mem)
	}

	// Save and reload
	err := SaveMemory(dir, "# Memory\n- discovered pattern X\n")
	if err != nil {
		t.Fatalf("save memory: %v", err)
	}

	mem = LoadMemory(dir)
	if !strings.Contains(mem, "discovered pattern X") {
		t.Fatalf("memory content: %q", mem)
	}
}

func TestAppendDailyMemory(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "repo")
	sessionDir := filepath.Join(t.TempDir(), "sessions")
	when := time.Date(2026, 4, 8, 13, 14, 15, 0, time.UTC)

	if err := AppendDailyMemory(sessionDir, workDir, when, memorypkg.SourceCompaction, "- Keep the IRC metaphor\n- Restore channel focus on restart"); err != nil {
		t.Fatalf("append daily memory: %v", err)
	}

	path := DailyMemoryPath(sessionDir, workDir, when)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read daily memory: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "## 2026-04-08T13:14:15Z compaction flush") {
		t.Fatalf("missing flush heading: %q", content)
	}
	if !strings.Contains(content, "Keep the IRC metaphor") {
		t.Fatalf("missing durable memory content: %q", content)
	}
}

func TestSearchMemoryFindsHotAndDurableMarkdown(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "repo")
	sessionDir := filepath.Join(t.TempDir(), "sessions")
	when := time.Date(2026, 4, 8, 13, 14, 15, 0, time.UTC)

	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("mkdir workDir: %v", err)
	}
	if err := SaveMemory(workDir, "# Working memory\n- Keep the IRC metaphor intact\n"); err != nil {
		t.Fatalf("save memory: %v", err)
	}
	if err := AppendDailyMemory(sessionDir, workDir, when, memorypkg.SourceCompaction, "- Restore channel focus after restart\n- Query memory should inherit parent notes"); err != nil {
		t.Fatalf("append daily memory: %v", err)
	}

	results, err := SearchMemory(sessionDir, workDir, "restore channel focus", 5)
	if err != nil {
		t.Fatalf("search memory: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 durable memory result, got %d", len(results))
	}
	if !strings.Contains(results[0].Source, "memory/") {
		t.Fatalf("expected durable memory source, got %q", results[0].Source)
	}
	if !strings.Contains(results[0].Heading, "compaction flush") {
		t.Fatalf("expected flush heading, got %q", results[0].Heading)
	}
	if !strings.Contains(results[0].Snippet, "Restore channel focus") {
		t.Fatalf("expected snippet to include durable memory, got %q", results[0].Snippet)
	}

	results, err = SearchMemory(sessionDir, workDir, "IRC metaphor", 5)
	if err != nil {
		t.Fatalf("search hot memory: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 hot memory result, got %d", len(results))
	}
	if results[0].Source != "MEMORY.md" {
		t.Fatalf("expected MEMORY.md source, got %q", results[0].Source)
	}
}

func TestScopedMemoryPathsUseChannelAndQueryLayout(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "repo")
	sessionDir := filepath.Join(t.TempDir(), "sessions")
	when := time.Date(2026, 4, 8, 13, 14, 15, 0, time.UTC)

	root := RootMemoryScope()
	channel := ChannelMemoryScope("#CornHub", &root)
	query := QueryMemoryScope("coding buddy", &channel)

	hotPath := ScopedHotMemoryPath(sessionDir, workDir, query)
	if !strings.Contains(hotPath, filepath.Join("contexts", "channels", "cornhub", "queries", "coding-buddy", "HOT.md")) {
		t.Fatalf("unexpected scoped hot path: %q", hotPath)
	}

	dailyPath := ScopedDailyMemoryPath(sessionDir, workDir, query, when)
	if !strings.Contains(dailyPath, filepath.Join("contexts", "channels", "cornhub", "queries", "coding-buddy", "daily", "2026-04-08.md")) {
		t.Fatalf("unexpected scoped daily path: %q", dailyPath)
	}
}

func TestScopedMemorySearchInheritsParentsWithoutLeakingChildWrites(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "repo")
	sessionDir := filepath.Join(t.TempDir(), "sessions")
	when := time.Date(2026, 4, 8, 13, 14, 15, 0, time.UTC)

	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("mkdir workDir: %v", err)
	}

	root := RootMemoryScope()
	channel := ChannelMemoryScope("lounge", &root)
	query := QueryMemoryScope("coding-buddy", &channel)

	if err := SaveMemory(workDir, "# Root memory\n- Root IRC rule\n"); err != nil {
		t.Fatalf("save root memory: %v", err)
	}
	if err := SaveScopedMemory(sessionDir, workDir, channel, "# Channel memory\n- Channel routing rule\n"); err != nil {
		t.Fatalf("save channel memory: %v", err)
	}
	if err := SaveScopedMemory(sessionDir, workDir, query, "# Query memory\n- Query-only scratchpad\n"); err != nil {
		t.Fatalf("save query memory: %v", err)
	}
	if err := AppendScopedDailyMemory(sessionDir, workDir, channel, when, memorypkg.SourceCompaction, "- Channel durable note"); err != nil {
		t.Fatalf("append scoped daily memory: %v", err)
	}

	results, err := SearchScopedMemory(sessionDir, workDir, query, "Channel routing rule", 10)
	if err != nil {
		t.Fatalf("search scoped memory: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 channel result, got %d", len(results))
	}
	if !strings.Contains(results[0].Source, filepath.Join("contexts", "channels", "lounge", "HOT.md")) {
		t.Fatalf("expected scoped channel source, got %q", results[0].Source)
	}

	results, err = SearchScopedMemory(sessionDir, workDir, query, "Root IRC rule", 10)
	if err != nil {
		t.Fatalf("search inherited root memory: %v", err)
	}
	if len(results) != 1 || results[0].Source != "MEMORY.md" {
		t.Fatalf("expected inherited root MEMORY.md result, got %#v", results)
	}

	results, err = SearchScopedMemory(sessionDir, workDir, root, "Query-only scratchpad", 10)
	if err != nil {
		t.Fatalf("search root scope memory: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected root scope to ignore child-only writes, got %#v", results)
	}
}

func TestRenderMemorySearchResults(t *testing.T) {
	rendered := RenderMemorySearchResults("irc metaphor", []MemorySearchResult{{
		Source:  "MEMORY.md",
		Heading: "Working memory",
		Snippet: "- Keep the IRC metaphor intact",
	}})

	if !strings.Contains(rendered, `Memory matches for "irc metaphor":`) {
		t.Fatalf("missing query heading: %q", rendered)
	}
	if !strings.Contains(rendered, "Source: MEMORY.md") {
		t.Fatalf("missing source: %q", rendered)
	}
	if !strings.Contains(rendered, "Heading: Working memory") {
		t.Fatalf("missing heading: %q", rendered)
	}
}

func TestExpandFileRefs(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("file contents here"), 0644)

	result := ExpandFileRefs("fix @test.txt please", dir)

	if !strings.Contains(result, "file contents here") {
		t.Fatalf("expected file contents in expansion, got: %s", result)
	}
	if !strings.Contains(result, "@test.txt") {
		t.Fatalf("expected @reference preserved, got: %s", result)
	}
}

func TestExpandFileRefsNotFound(t *testing.T) {
	dir := t.TempDir()
	result := ExpandFileRefs("fix @nonexistent.txt", dir)

	if !strings.Contains(result, "file not found") {
		t.Fatalf("expected file not found error, got: %s", result)
	}
}

func TestExpandFileRefsNoRefs(t *testing.T) {
	dir := t.TempDir()
	result := ExpandFileRefs("just a normal message", dir)

	if result != "just a normal message" {
		t.Fatalf("expected unchanged message, got: %s", result)
	}
}
