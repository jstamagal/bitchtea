package agent

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
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
	path := filepath.Join(workDir, "MEMORY.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// SaveMemory writes MEMORY.md to workDir
func SaveMemory(workDir string, content string) error {
	path := filepath.Join(workDir, "MEMORY.md")
	return os.WriteFile(path, []byte(content), 0644)
}

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

// DailyMemoryPath returns the markdown file used for durable daily memory for
// the current worktree scope.
func DailyMemoryPath(sessionDir, workDir string, when time.Time) string {
	return filepath.Join(
		filepath.Dir(sessionDir),
		"memory",
		memoryScopeName(workDir),
		when.Format("2006-01-02")+".md",
	)
}

// AppendDailyMemory appends a dated durable-memory checkpoint for later recall.
func AppendDailyMemory(sessionDir, workDir string, when time.Time, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	path := DailyMemoryPath(sessionDir, workDir, when)
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

func memoryScopeName(workDir string) string {
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
