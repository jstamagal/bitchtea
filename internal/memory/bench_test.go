package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// BenchmarkSearchInScope measures SearchInScope against a populated on-disk
// layout: a hot MEMORY.md plus two daily files, all containing a matchable
// query term.
func BenchmarkSearchInScope(b *testing.B) {
	base := b.TempDir()
	sessionDir := filepath.Join(base, "sessions")
	workDir := filepath.Join(base, "work")

	// Create workDir so scopeName has a real path to hash.
	if err := os.MkdirAll(workDir, 0755); err != nil {
		b.Fatal(err)
	}

	// Hot memory (root scope → workDir/MEMORY.md).
	hot := filepath.Join(workDir, "MEMORY.md")
	if err := os.WriteFile(hot, []byte("# Project Notes\n\nbenchmark target\n\nMore notes.\n"), 0644); err != nil {
		b.Fatal(err)
	}

	// Durable daily memory.
	dailyDir := filepath.Join(filepath.Dir(sessionDir), "memory", scopeName(workDir))
	if err := os.MkdirAll(dailyDir, 0755); err != nil {
		b.Fatal(err)
	}
	content := strings.Repeat("Line with benchmark keyword.\n", 100)
	for _, name := range []string{"2026-05-03.md", "2026-05-04.md"} {
		if err := os.WriteFile(filepath.Join(dailyDir, name), []byte(content), 0644); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := SearchInScope(sessionDir, workDir, RootScope(), "benchmark", 5)
		if err != nil {
			b.Fatal(err)
		}
		if len(res) == 0 {
			b.Fatal("no results")
		}
	}
}
