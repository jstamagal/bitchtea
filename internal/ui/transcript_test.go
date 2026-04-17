package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTranscriptLoggerWritesDailyReadableLog(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewTranscriptLogger(dir)
	if err != nil {
		t.Fatalf("new transcript logger: %v", err)
	}
	logger.now = func() time.Time {
		return time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	}

	now := time.Date(2026, 4, 8, 12, 34, 0, 0, time.UTC)
	messages := []ChatMessage{
		{Time: now, Type: MsgUser, Nick: "tj", Content: "hello"},
		{Time: now, Type: MsgSystem, Content: "connected"},
		{Time: now, Type: MsgTool, Nick: "bash", Content: "line one\nline two"},
		{Time: now, Type: MsgRaw, Content: "\x1b[1;36mansi\x1b[0m raw"},
	}

	for _, msg := range messages {
		if err := logger.LogMessage(msg); err != nil {
			t.Fatalf("log message: %v", err)
		}
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("close transcript logger: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "2026-04-08.log"))
	if err != nil {
		t.Fatalf("read transcript log: %v", err)
	}

	got := string(data)
	for _, want := range []string{
		"[12:34] <tj> hello",
		"[12:34] *** connected",
		"[12:34] -> bash:",
		"    line one",
		"    line two",
		"ansi raw",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected transcript to contain %q, got:\n%s", want, got)
		}
	}
}

func TestTranscriptLoggerStreamsAssistantMessage(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewTranscriptLogger(dir)
	if err != nil {
		t.Fatalf("new transcript logger: %v", err)
	}
	logger.now = func() time.Time {
		return time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	}

	at := time.Date(2026, 4, 8, 12, 35, 0, 0, time.UTC)
	if err := logger.LogMessage(ChatMessage{Time: at, Type: MsgAgent, Nick: "bitchtea"}); err != nil {
		t.Fatalf("log placeholder agent message: %v", err)
	}
	if err := logger.AppendAgentChunk(at, "bitchtea", "Hello"); err != nil {
		t.Fatalf("append first chunk: %v", err)
	}
	if err := logger.AppendAgentChunk(at, "bitchtea", " world"); err != nil {
		t.Fatalf("append second chunk: %v", err)
	}
	if err := logger.FinishAgentMessage(); err != nil {
		t.Fatalf("finish streaming message: %v", err)
	}
	if err := logger.LogMessage(ChatMessage{Time: at, Type: MsgSystem, Content: "done"}); err != nil {
		t.Fatalf("log trailing message: %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("close transcript logger: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "2026-04-08.log"))
	if err != nil {
		t.Fatalf("read transcript log: %v", err)
	}

	got := string(data)
	if !strings.Contains(got, "[12:35] <bitchtea> Hello world\n[12:35] *** done\n") {
		t.Fatalf("unexpected transcript content:\n%s", got)
	}
}

func TestTranscriptLoggerFinalizesStreamBeforeToolOutput(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewTranscriptLogger(dir)
	if err != nil {
		t.Fatalf("new transcript logger: %v", err)
	}
	logger.now = func() time.Time {
		return time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	}

	at := time.Date(2026, 4, 8, 12, 36, 0, 0, time.UTC)
	if err := logger.AppendAgentChunk(at, "bitchtea", "Need a tool"); err != nil {
		t.Fatalf("append chunk: %v", err)
	}
	if err := logger.LogMessage(ChatMessage{Time: at, Type: MsgTool, Nick: "read", Content: "file.txt"}); err != nil {
		t.Fatalf("log tool message: %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("close transcript logger: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "2026-04-08.log"))
	if err != nil {
		t.Fatalf("read transcript log: %v", err)
	}

	got := string(data)
	if !strings.Contains(got, "[12:36] <bitchtea> Need a tool\n[12:36] -> read:\n    file.txt\n") {
		t.Fatalf("unexpected transcript content:\n%s", got)
	}
}

func TestTranscriptLoggerOmitsThinkingMessages(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewTranscriptLogger(dir)
	if err != nil {
		t.Fatalf("new transcript logger: %v", err)
	}
	logger.now = func() time.Time {
		return time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	}

	at := time.Date(2026, 4, 8, 12, 37, 0, 0, time.UTC)
	if err := logger.LogMessage(ChatMessage{Time: at, Type: MsgThink, Content: "thinking..."}); err != nil {
		t.Fatalf("log placeholder thinking message: %v", err)
	}
	if err := logger.LogMessage(ChatMessage{Time: at, Type: MsgThink, Content: "plan: inspect file"}); err != nil {
		t.Fatalf("log updated thinking message: %v", err)
	}
	if err := logger.LogMessage(ChatMessage{Time: at, Type: MsgSystem, Content: "done"}); err != nil {
		t.Fatalf("log trailing system message: %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("close transcript logger: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "2026-04-08.log"))
	if err != nil {
		t.Fatalf("read transcript log: %v", err)
	}

	got := string(data)
	if strings.Contains(got, "thinking") {
		t.Fatalf("expected thinking messages to be omitted, got:\n%s", got)
	}
	if !strings.Contains(got, "[12:37] *** done\n") {
		t.Fatalf("expected non-thinking messages to remain, got:\n%s", got)
	}
}

func TestTranscriptLoggerIgnoringThinkingDoesNotBreakAgentStream(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewTranscriptLogger(dir)
	if err != nil {
		t.Fatalf("new transcript logger: %v", err)
	}
	logger.now = func() time.Time {
		return time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	}

	at := time.Date(2026, 4, 8, 12, 38, 0, 0, time.UTC)
	if err := logger.AppendAgentChunk(at, "bitchtea", "Hello"); err != nil {
		t.Fatalf("append first chunk: %v", err)
	}
	if err := logger.LogMessage(ChatMessage{Time: at, Type: MsgThink, Content: "thinking..."}); err != nil {
		t.Fatalf("log ignored thinking message: %v", err)
	}
	if err := logger.AppendAgentChunk(at, "bitchtea", " world"); err != nil {
		t.Fatalf("append second chunk: %v", err)
	}
	if err := logger.FinishAgentMessage(); err != nil {
		t.Fatalf("finish streaming message: %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("close transcript logger: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "2026-04-08.log"))
	if err != nil {
		t.Fatalf("read transcript log: %v", err)
	}

	got := string(data)
	if !strings.Contains(got, "[12:38] <bitchtea> Hello world\n") {
		t.Fatalf("expected uninterrupted agent stream, got:\n%s", got)
	}
}
