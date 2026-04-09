package memory

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

type ScopeKind string

const (
	ScopeRoot    ScopeKind = "root"
	ScopeChannel ScopeKind = "channel"
	ScopeQuery   ScopeKind = "query"
)

// Scope identifies a memory namespace for the current worktree.
// Root scope remains the legacy top-level MEMORY.md in the repo.
type Scope struct {
	Kind   ScopeKind
	Name   string
	Parent *Scope
}

func RootScope() Scope {
	return Scope{Kind: ScopeRoot}
}

func ChannelScope(name string, parent *Scope) Scope {
	return Scope{Kind: ScopeChannel, Name: name, Parent: parent}
}

func QueryScope(name string, parent *Scope) Scope {
	return Scope{Kind: ScopeQuery, Name: name, Parent: parent}
}

// Load reads MEMORY.md from workDir if it exists.
func Load(workDir string) string {
	path := filepath.Join(workDir, "MEMORY.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// Save writes MEMORY.md to workDir.
func Save(workDir string, content string) error {
	path := filepath.Join(workDir, "MEMORY.md")
	return os.WriteFile(path, []byte(content), 0644)
}

// HotPath returns the hot-memory path for a context scope. Root scope keeps the
// legacy MEMORY.md in the repo for backward compatibility.
func HotPath(sessionDir, workDir string, scope Scope) string {
	if scope.Kind == "" || scope.Kind == ScopeRoot {
		return filepath.Join(workDir, "MEMORY.md")
	}
	return filepath.Join(memoryBaseDir(sessionDir, workDir), "contexts", scope.relativePath(), "HOT.md")
}

// LoadScoped reads hot memory for a scoped context if it exists.
func LoadScoped(sessionDir, workDir string, scope Scope) string {
	path := HotPath(sessionDir, workDir, scope)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// SaveScoped writes hot memory for a scoped context.
func SaveScoped(sessionDir, workDir string, scope Scope, content string) error {
	path := HotPath(sessionDir, workDir, scope)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create scoped hot memory dir: %w", err)
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// DailyPath returns the markdown file used for durable daily memory for the
// current worktree scope.
func DailyPath(sessionDir, workDir string, when time.Time) string {
	return DailyPathForScope(sessionDir, workDir, RootScope(), when)
}

// DailyPathForScope returns the durable daily-memory path for a context scope.
func DailyPathForScope(sessionDir, workDir string, scope Scope, when time.Time) string {
	if scope.Kind == "" || scope.Kind == ScopeRoot {
		return filepath.Join(memoryBaseDir(sessionDir, workDir), when.Format("2006-01-02")+".md")
	}
	return filepath.Join(
		memoryBaseDir(sessionDir, workDir),
		"contexts",
		scope.relativePath(),
		"daily",
		when.Format("2006-01-02")+".md",
	)
}

// AppendDaily appends a dated durable-memory checkpoint for later recall.
func AppendDaily(sessionDir, workDir string, when time.Time, content string) error {
	return AppendDailyForScope(sessionDir, workDir, RootScope(), when, content)
}

// AppendDailyForScope appends a dated durable-memory checkpoint for a scope.
func AppendDailyForScope(sessionDir, workDir string, scope Scope, when time.Time, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	path := DailyPathForScope(sessionDir, workDir, scope, when)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create daily memory dir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open daily memory file: %w", err)
	}
	defer f.Close()

	entry := fmt.Sprintf("## %s pre-compaction flush\n\n%s\n\n", when.Format(time.RFC3339), content)
	if _, err := f.WriteString(entry); err != nil {
		return fmt.Errorf("append daily memory: %w", err)
	}

	return nil
}

// SearchResult is a single recall hit from hot or durable markdown memory.
type SearchResult struct {
	Source  string
	Heading string
	Snippet string
}

// Search searches hot MEMORY.md and durable daily markdown memory for the
// current worktree scope.
func Search(sessionDir, workDir, query string, limit int) ([]SearchResult, error) {
	return SearchInScope(sessionDir, workDir, RootScope(), query, limit)
}

// SearchInScope searches hot and durable markdown memory for the current scope
// and then walks parent scopes outward to support inherited reads.
func SearchInScope(sessionDir, workDir string, scope Scope, query string, limit int) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if limit <= 0 {
		limit = 5
	}

	terms := queryTerms(query)
	candidates, err := candidatePaths(sessionDir, workDir, scope)
	if err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, limit)
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read memory file %s: %w", path, err)
		}

		content := string(data)
		lowerContent := strings.ToLower(content)
		if !containsAllTerms(lowerContent, terms) {
			continue
		}

		matchIdx := strings.Index(lowerContent, strings.ToLower(query))
		if matchIdx < 0 {
			for _, term := range terms {
				matchIdx = strings.Index(lowerContent, term)
				if matchIdx >= 0 {
					break
				}
			}
		}
		if matchIdx < 0 {
			continue
		}

		results = append(results, SearchResult{
			Source:  formatSource(sessionDir, workDir, path),
			Heading: nearestMarkdownHeading(content, matchIdx),
			Snippet: extractSnippet(content, matchIdx, 260),
		})
		if len(results) >= limit {
			break
		}
	}

	return results, nil
}

// RenderSearchResults formats memory hits for tool output.
func RenderSearchResults(query string, results []SearchResult) string {
	if len(results) == 0 {
		return fmt.Sprintf("No memory matches found for query %q.", query)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Memory matches for %q:\n", query))
	for i, result := range results {
		sb.WriteString(fmt.Sprintf("\n%d. Source: %s\n", i+1, result.Source))
		if result.Heading != "" {
			sb.WriteString("Heading: " + result.Heading + "\n")
		}
		sb.WriteString(result.Snippet + "\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func scopeName(workDir string) string {
	clean := filepath.Clean(workDir)
	base := strings.ToLower(filepath.Base(clean))
	base = nonAlphaNum.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	if base == "" || base == "." {
		base = "root"
	}

	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(clean))

	return fmt.Sprintf("%s-%08x", base, hasher.Sum32())
}

func memoryBaseDir(sessionDir, workDir string) string {
	return filepath.Join(filepath.Dir(sessionDir), "memory", scopeName(workDir))
}

func candidatePaths(sessionDir, workDir string, scope Scope) ([]string, error) {
	var candidates []string
	for _, ancestor := range scope.lineage() {
		candidates = append(candidates, HotPath(sessionDir, workDir, ancestor))

		dailyDir := filepath.Dir(DailyPathForScope(sessionDir, workDir, ancestor, time.Now()))
		entries, err := os.ReadDir(dailyDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read daily memory dir: %w", err)
		}

		var dailyPaths []string
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
				continue
			}
			dailyPaths = append(dailyPaths, filepath.Join(dailyDir, entry.Name()))
		}
		sort.Sort(sort.Reverse(sort.StringSlice(dailyPaths)))
		candidates = append(candidates, dailyPaths...)
	}

	return candidates, nil
}

func queryTerms(query string) []string {
	fields := strings.Fields(strings.ToLower(query))
	if len(fields) == 0 {
		return []string{strings.ToLower(strings.TrimSpace(query))}
	}
	return fields
}

func containsAllTerms(content string, terms []string) bool {
	for _, term := range terms {
		if term == "" {
			continue
		}
		if !strings.Contains(content, term) {
			return false
		}
	}
	return true
}

func formatSource(sessionDir, workDir, path string) string {
	if filepath.Clean(path) == filepath.Join(workDir, "MEMORY.md") {
		return "MEMORY.md"
	}

	base := filepath.Dir(sessionDir)
	if rel, err := filepath.Rel(base, path); err == nil {
		return rel
	}
	return path
}

func (s Scope) lineage() []Scope {
	if s.Kind == "" {
		s = RootScope()
	}

	var stack []Scope
	for cur := &s; cur != nil; cur = cur.Parent {
		stack = append(stack, *cur)
		if cur.Kind == ScopeRoot {
			break
		}
	}

	if len(stack) == 0 || stack[len(stack)-1].Kind != ScopeRoot {
		stack = append(stack, RootScope())
	}

	lineage := make([]Scope, 0, len(stack))
	for i := 0; i < len(stack); i++ {
		lineage = append(lineage, stack[i])
	}
	return lineage
}

func (s Scope) relativePath() string {
	if s.Kind == "" || s.Kind == ScopeRoot {
		return ""
	}

	var segments []string
	lineage := s.lineage()
	for i := len(lineage) - 1; i >= 0; i-- {
		ancestor := lineage[i]
		if ancestor.Kind == "" || ancestor.Kind == ScopeRoot {
			continue
		}

		dir := "channels"
		if ancestor.Kind == ScopeQuery {
			dir = "queries"
		}
		segments = append(segments, dir, sanitizeSegment(ancestor.Name))
	}
	return filepath.Join(segments...)
}

func sanitizeSegment(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = nonAlphaNum.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if name == "" {
		return "unnamed"
	}
	return name
}

func nearestMarkdownHeading(content string, idx int) string {
	if idx < 0 {
		return ""
	}

	lines := strings.Split(content, "\n")
	offset := 0
	heading := ""
	for _, line := range lines {
		lineEnd := offset + len(line) + 1
		if strings.HasPrefix(line, "#") {
			heading = strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
		if idx < lineEnd {
			break
		}
		offset = lineEnd
	}
	return heading
}

func extractSnippet(content string, idx int, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 260
	}

	runes := []rune(content)
	if len(runes) <= maxLen {
		return strings.TrimSpace(content)
	}

	runeIdx := len([]rune(content[:idx]))
	start := runeIdx - maxLen/2
	if start < 0 {
		start = 0
	}
	end := start + maxLen
	if end > len(runes) {
		end = len(runes)
		start = end - maxLen
		if start < 0 {
			start = 0
		}
	}

	snippet := string(runes[start:end])
	snippet = strings.TrimSpace(snippet)
	if start > 0 {
		snippet = "... " + snippet
	}
	if end < len(runes) {
		snippet += " ..."
	}
	return snippet
}
