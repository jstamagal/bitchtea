package agent

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	memorypkg "github.com/jstamagal/bitchtea/internal/memory"
)

// DiscoverContextFiles walks up from workDir looking for AGENTS.md, CLAUDE.md,
// and similar context files. Returns their contents concatenated.
func DiscoverContextFiles(workDir string) string {
	filenames := []string{"AGENTS.md", "CLAUDE.md", ".agents.md", ".claude.md"}
	var found []string

	dir := workDir
	for {
		for _, name := range filenames {
			path := filepath.Join(dir, name)
			data, err := os.ReadFile(path)
			if err == nil {
				found = append(found, "# Context from "+path+"\n\n"+string(data))
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached root
		}
		dir = parent
	}

	if len(found) == 0 {
		return ""
	}

	return strings.Join(found, "\n\n---\n\n")
}

// LoadMemory reads MEMORY.md from workDir if it exists
func LoadMemory(workDir string) string {
	return memorypkg.Load(workDir)
}

type MemoryScope = memorypkg.Scope
type MemoryScopeKind = memorypkg.ScopeKind

const (
	MemoryScopeRoot    = memorypkg.ScopeRoot
	MemoryScopeChannel = memorypkg.ScopeChannel
	MemoryScopeQuery   = memorypkg.ScopeQuery
)

func RootMemoryScope() MemoryScope {
	return memorypkg.RootScope()
}

func ChannelMemoryScope(name string, parent *MemoryScope) MemoryScope {
	return memorypkg.ChannelScope(name, parent)
}

func QueryMemoryScope(name string, parent *MemoryScope) MemoryScope {
	return memorypkg.QueryScope(name, parent)
}

// SaveMemory writes MEMORY.md to workDir
func SaveMemory(workDir string, content string) error {
	return memorypkg.Save(workDir, content)
}

// LoadScopedMemory reads scoped hot memory if it exists.
func LoadScopedMemory(sessionDir, workDir string, scope MemoryScope) string {
	return memorypkg.LoadScoped(sessionDir, workDir, scope)
}

// SaveScopedMemory writes scoped hot memory.
func SaveScopedMemory(sessionDir, workDir string, scope MemoryScope, content string) error {
	return memorypkg.SaveScoped(sessionDir, workDir, scope, content)
}

// ScopedHotMemoryPath returns the scoped hot-memory path.
func ScopedHotMemoryPath(sessionDir, workDir string, scope MemoryScope) string {
	return memorypkg.HotPath(sessionDir, workDir, scope)
}

// DailyMemoryPath returns the markdown file used for durable daily memory for
// the current worktree scope.
func DailyMemoryPath(sessionDir, workDir string, when time.Time) string {
	return memorypkg.DailyPath(sessionDir, workDir, when)
}

// ScopedDailyMemoryPath returns the durable daily-memory path for a scope.
func ScopedDailyMemoryPath(sessionDir, workDir string, scope MemoryScope, when time.Time) string {
	return memorypkg.DailyPathForScope(sessionDir, workDir, scope, when)
}

// AppendDailyMemory appends a dated durable-memory checkpoint for later recall.
func AppendDailyMemory(sessionDir, workDir string, when time.Time, content string) error {
	return memorypkg.AppendDaily(sessionDir, workDir, when, content)
}

// AppendScopedDailyMemory appends durable memory for a context scope.
func AppendScopedDailyMemory(sessionDir, workDir string, scope MemoryScope, when time.Time, content string) error {
	return memorypkg.AppendDailyForScope(sessionDir, workDir, scope, when, content)
}

// MemorySearchResult is a single recall hit from hot or durable markdown memory.
type MemorySearchResult = memorypkg.SearchResult

// SearchMemory searches hot MEMORY.md and durable daily markdown memory for the
// current worktree scope.
func SearchMemory(sessionDir, workDir, query string, limit int) ([]MemorySearchResult, error) {
	return memorypkg.Search(sessionDir, workDir, query, limit)
}

// SearchScopedMemory searches scoped memory and inherited parent scopes.
func SearchScopedMemory(sessionDir, workDir string, scope MemoryScope, query string, limit int) ([]MemorySearchResult, error) {
	return memorypkg.SearchInScope(sessionDir, workDir, scope, query, limit)
}

// RenderMemorySearchResults formats memory hits for the recall tool output.
func RenderMemorySearchResults(query string, results []MemorySearchResult) string {
	return memorypkg.RenderSearchResults(query, results)
}

// ExpandFileRefs replaces @file references in input with file contents.
// e.g., "fix @main.go" becomes "fix <contents of main.go>"
func ExpandFileRefs(input string, workDir string) string {
	words := strings.Fields(input)
	var result []string

	for _, word := range words {
		if strings.HasPrefix(word, "@") && len(word) > 1 {
			filename := word[1:]
			path := filename
			if !filepath.IsAbs(path) {
				path = filepath.Join(workDir, path)
			}
			data, err := os.ReadFile(path)
			if err == nil {
				// Truncate large files
				content := string(data)
				const maxSize = 30 * 1024
				if len(content) > maxSize {
					content = content[:maxSize] + "\n... (truncated at 30KB)"
				}
				result = append(result, word+" (file contents below):\n```\n"+content+"\n```")
			} else {
				result = append(result, word+" (file not found: "+err.Error()+")")
			}
		} else {
			result = append(result, word)
		}
	}

	return strings.Join(result, " ")
}
