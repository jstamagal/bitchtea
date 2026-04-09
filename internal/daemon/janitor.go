package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jstamagal/bitchtea/internal/llm"
)

// toolBlockRe matches markdown fenced code blocks tagged as tool, tool_call,
// or tool_result — these accumulate when agents dump tool output into memory.
var toolBlockRe = regexp.MustCompile("(?s)```(?:tool_call|tool_result|tool)\\b[^`]*?```")

// blankLinesRe collapses 3+ consecutive blank lines into 2.
var blankLinesRe = regexp.MustCompile(`\n{3,}`)

// runJanitorInDir walks memDir for every HOT.md, prunes tool blocks, and
// compacts files above hotCompactThreshold using the Opus model in compact.
// Compaction ALWAYS uses the compact clientParams — never the heartbeat model.
func runJanitorInDir(ctx context.Context, memDir string, compact clientParams, logger *log.Logger) error {
	var hotFiles []string
	err := filepath.WalkDir(memDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if !d.IsDir() && filepath.Base(path) == "HOT.md" {
			hotFiles = append(hotFiles, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk memory dir: %w", err)
	}

	for _, path := range hotFiles {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := processHotFile(ctx, path, compact, logger); err != nil {
			logger.Printf("janitor: skipping %s: %v", path, err)
		}
	}
	return nil
}

// processHotFile prunes tool blocks from path and compacts it if oversized.
func processHotFile(ctx context.Context, path string, compact clientParams, logger *log.Logger) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	original := string(data)
	pruned := toolBlockRe.ReplaceAllString(original, "")
	pruned = blankLinesRe.ReplaceAllString(pruned, "\n\n")
	pruned = strings.TrimSpace(pruned)

	if pruned != strings.TrimSpace(original) {
		if err := os.WriteFile(path, []byte(pruned+"\n"), 0644); err != nil {
			return fmt.Errorf("write pruned HOT.md: %w", err)
		}
		logger.Printf("janitor: pruned tool blocks from %s (%d → %d bytes)", path, len(original), len(pruned))
	}

	if len(pruned) > hotCompactThreshold {
		return compactHotFile(ctx, path, pruned, compact, logger)
	}

	logger.Printf("janitor: %s ok (%d bytes)", path, len(pruned))
	return nil
}

// compactHotFile uses Opus 4.6 (compact.Model) to summarize the HOT.md,
// flushes the original to a dated daily file, then writes the summary back.
// This function MUST NOT use the cheap heartbeat model — compact.Model is
// always claude-opus-4-6 as enforced by the Daemon constructor.
func compactHotFile(ctx context.Context, path, content string, compact clientParams, logger *log.Logger) error {
	if compact.APIKey == "" {
		logger.Printf("janitor: skipping compaction of %s — ANTHROPIC_API_KEY not set", path)
		return nil
	}

	logger.Printf("janitor: compacting %s with %s (%d bytes)", path, compact.Model, len(content))

	prompt := "You are compacting a HOT memory file for an AI coding assistant.\n" +
		"Condense the following into concise markdown (target: under 2000 words), preserving:\n" +
		"- All decisions, preferences, and completed work\n" +
		"- Active context: current task, open questions, next steps\n" +
		"- File paths, branch names, and key technical details\n\n" +
		"Strip verbose logs, repetitive tool outputs, and filler text.\n\n" +
		"HOT memory content:\n\n" + content

	client := llm.NewClient(compact.APIKey, compact.BaseURL, compact.Model, compact.Provider)
	evCh := make(chan llm.StreamEvent, 64)
	go client.StreamChat(ctx, []llm.Message{{Role: "user", Content: prompt}}, nil, evCh)

	var sb strings.Builder
	for ev := range evCh {
		if err := ctx.Err(); err != nil {
			return err
		}
		if ev.Type == "error" {
			return fmt.Errorf("compact LLM error: %w", ev.Error)
		}
		if ev.Type == "text" {
			sb.WriteString(ev.Text)
		}
	}

	summary := strings.TrimSpace(sb.String())
	if summary == "" {
		return fmt.Errorf("compact returned empty summary for %s", path)
	}

	// Flush original to a dated daily file alongside HOT.md before overwriting.
	if err := flushToDailyFile(path, content); err != nil {
		// Non-fatal: log and continue — compaction proceeds regardless.
		logger.Printf("janitor: failed to flush daily for %s: %v", path, err)
	}

	if err := os.WriteFile(path, []byte(summary+"\n"), 0644); err != nil {
		return fmt.Errorf("write compacted HOT.md: %w", err)
	}
	logger.Printf("janitor: compacted %s → %d bytes (was %d)", path, len(summary), len(content))
	return nil
}

// flushToDailyFile appends content to a dated daily file alongside HOT.md.
// E.g. .../contexts/channels/foo/HOT.md → .../contexts/channels/foo/daily/2026-04-09.md
func flushToDailyFile(hotPath, content string) error {
	dailyDir := filepath.Join(filepath.Dir(hotPath), "daily")
	if err := os.MkdirAll(dailyDir, 0755); err != nil {
		return fmt.Errorf("create daily dir: %w", err)
	}

	dailyFile := filepath.Join(dailyDir, time.Now().Format("2006-01-02")+".md")
	f, err := os.OpenFile(dailyFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open daily file: %w", err)
	}
	defer f.Close()

	entry := fmt.Sprintf("## %s daemon compaction flush\n\n%s\n\n", time.Now().Format(time.RFC3339), content)
	if _, err := f.WriteString(entry); err != nil {
		return fmt.Errorf("write daily entry: %w", err)
	}
	return nil
}
