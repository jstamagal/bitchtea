package agent

import (
	"os"
	"path/filepath"
	"strings"
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
