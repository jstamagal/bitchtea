package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jstamagal/bitchtea/internal/config"
)

func TestUndoPreviewShowsDiffStat(t *testing.T) {
	repo := newGitCommandTestRepo(t)
	model := newGitCommandTestModel(t, repo)

	writeGitCommandFile(t, repo, "tracked.txt", "one\nchanged\n")

	result, _ := model.handleCommand("/undo")
	msg := lastMsg(result)

	if msg.Type != MsgRaw {
		t.Fatalf("expected raw preview message, got %v", msg.Type)
	}
	if !strings.Contains(msg.Content, "--- /undo preview ---") {
		t.Fatalf("expected undo preview header, got %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "tracked.txt") {
		t.Fatalf("expected changed file in preview, got %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "/undo confirm") {
		t.Fatalf("expected usage hint, got %q", msg.Content)
	}
}

func TestUndoConfirmRevertsTrackedChanges(t *testing.T) {
	repo := newGitCommandTestRepo(t)
	model := newGitCommandTestModel(t, repo)

	path := filepath.Join(repo, "tracked.txt")
	writeGitCommandFile(t, repo, "tracked.txt", "one\nchanged\n")

	result, _ := model.handleCommand("/undo confirm")
	msg := lastMsg(result)

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read reverted file: %v", err)
	}
	if string(content) != "one\n" {
		t.Fatalf("expected file reverted to committed content, got %q", string(content))
	}
	if !strings.Contains(msg.Content, "Reverted all unstaged tracked changes.") {
		t.Fatalf("expected confirmation message, got %q", msg.Content)
	}
}

func TestUndoFileRevertsOnlyRequestedPath(t *testing.T) {
	repo := newGitCommandTestRepo(t)
	model := newGitCommandTestModel(t, repo)

	trackedPath := filepath.Join(repo, "tracked.txt")
	otherPath := filepath.Join(repo, "other.txt")
	writeGitCommandFile(t, repo, "tracked.txt", "one\nchanged\n")
	writeGitCommandFile(t, repo, "other.txt", "two\nchanged\n")

	result, _ := model.handleCommand("/undo tracked.txt")
	msg := lastMsg(result)

	trackedContent, err := os.ReadFile(trackedPath)
	if err != nil {
		t.Fatalf("read tracked file: %v", err)
	}
	otherContent, err := os.ReadFile(otherPath)
	if err != nil {
		t.Fatalf("read other file: %v", err)
	}
	if string(trackedContent) != "one\n" {
		t.Fatalf("expected tracked file reverted, got %q", string(trackedContent))
	}
	if string(otherContent) != "two\nchanged\n" {
		t.Fatalf("expected other file unchanged, got %q", string(otherContent))
	}
	if !strings.Contains(msg.Content, "tracked.txt") {
		t.Fatalf("expected reverted path in message, got %q", msg.Content)
	}
}

func TestCommitPreviewShowsTrackedAndUntrackedSections(t *testing.T) {
	repo := newGitCommandTestRepo(t)
	model := newGitCommandTestModel(t, repo)

	writeGitCommandFile(t, repo, "tracked.txt", "one\nchanged\n")
	writeGitCommandFile(t, repo, "new.txt", "brand new\n")

	result, _ := model.handleCommand("/commit")
	msg := lastMsg(result)

	if msg.Type != MsgRaw {
		t.Fatalf("expected raw preview message, got %v", msg.Type)
	}
	if !strings.Contains(msg.Content, "Tracked changes only will be committed.") {
		t.Fatalf("expected tracked-only warning, got %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "Unstaged:\n  tracked.txt") {
		t.Fatalf("expected unstaged tracked file in preview, got %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "Untracked:\n  new.txt") {
		t.Fatalf("expected untracked file in preview, got %q", msg.Content)
	}
}

func TestCommitStagesTrackedChangesOnly(t *testing.T) {
	repo := newGitCommandTestRepo(t)
	model := newGitCommandTestModel(t, repo)

	writeGitCommandFile(t, repo, "tracked.txt", "one\nchanged\n")
	writeGitCommandFile(t, repo, "new.txt", "brand new\n")

	result, _ := model.handleCommand("/commit update tracked file")
	msg := lastMsg(result)

	logOutput := runGit(repo, "log", "-1", "--pretty=%s")
	statusOutput := runGit(repo, "status", "--short")

	if !strings.Contains(msg.Content, "Committed:") {
		t.Fatalf("expected commit confirmation, got %q", msg.Content)
	}
	if logOutput != "update tracked file" {
		t.Fatalf("expected commit message to match, got %q", logOutput)
	}
	if !strings.Contains(statusOutput, "?? new.txt") {
		t.Fatalf("expected untracked file to remain unstaged, got %q", statusOutput)
	}
	if strings.Contains(statusOutput, "tracked.txt") {
		t.Fatalf("expected tracked file to be committed cleanly, got %q", statusOutput)
	}
}

func newGitCommandTestModel(t *testing.T, workDir string) Model {
	t.Helper()
	dataDir := t.TempDir()
	cfg := &config.Config{
		APIKey:     "sk-test-key-12345",
		BaseURL:    "https://api.openai.com/v1",
		Model:      "gpt-4o",
		Provider:   "openai",
		SessionDir: filepath.Join(dataDir, "sessions"),
		LogDir:     filepath.Join(dataDir, "logs"),
		WorkDir:    workDir,
	}
	return NewModel(cfg)
}

func newGitCommandTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()

	runGitCommand(t, repo, "init")
	runGitCommand(t, repo, "config", "user.name", "Test User")
	runGitCommand(t, repo, "config", "user.email", "test@example.com")

	writeGitCommandFile(t, repo, "tracked.txt", "one\n")
	writeGitCommandFile(t, repo, "other.txt", "two\n")

	runGitCommand(t, repo, "add", "tracked.txt", "other.txt")
	runGitCommand(t, repo, "commit", "-m", "initial state")

	return repo
}

func writeGitCommandFile(t *testing.T, repo, name, content string) {
	t.Helper()
	path := filepath.Join(repo, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func runGitCommand(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return strings.TrimSpace(string(out))
}
