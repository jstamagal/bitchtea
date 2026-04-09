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

// DailyPath returns the markdown file used for durable daily memory for the
// current worktree scope.
func DailyPath(sessionDir, workDir string, when time.Time) string {
	return filepath.Join(
		filepath.Dir(sessionDir),
		"memory",
		scopeName(workDir),
		when.Format("2006-01-02")+".md",
	)
}

// AppendDaily appends a dated durable-memory checkpoint for later recall.
func AppendDaily(sessionDir, workDir string, when time.Time, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	path := DailyPath(sessionDir, workDir, when)
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
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if limit <= 0 {
		limit = 5
	}

	terms := queryTerms(query)
	candidates := []string{filepath.Join(workDir, "MEMORY.md")}

	dailyDir := filepath.Join(filepath.Dir(sessionDir), "memory", scopeName(workDir))
	if entries, err := os.ReadDir(dailyDir); err == nil {
		var dailyPaths []string
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
				continue
			}
			dailyPaths = append(dailyPaths, filepath.Join(dailyDir, entry.Name()))
		}
		sort.Sort(sort.Reverse(sort.StringSlice(dailyPaths)))
		candidates = append(candidates, dailyPaths...)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read daily memory dir: %w", err)
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
