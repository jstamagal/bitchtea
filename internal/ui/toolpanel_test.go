package ui

import (
	"strings"
	"testing"

	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/llm"
	"github.com/jstamagal/bitchtea/internal/session"
)

func TestToolPanelStartFinish(t *testing.T) {
	p := NewToolPanel()

	p.StartTool("read")
	if len(p.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(p.Tools))
	}
	if p.Tools[0].Status != "running" {
		t.Fatalf("expected running, got %s", p.Tools[0].Status)
	}

	p.FinishTool("read", "file contents here", false)
	if p.Tools[0].Status != "done" {
		t.Fatalf("expected done, got %s", p.Tools[0].Status)
	}
	if p.Tools[0].Duration == 0 {
		t.Fatal("expected non-zero duration")
	}
}

func TestToolPanelError(t *testing.T) {
	p := NewToolPanel()
	p.StartTool("bash")
	p.FinishTool("bash", "command failed", true)

	if p.Tools[0].Status != "error" {
		t.Fatalf("expected error, got %s", p.Tools[0].Status)
	}
}

func TestToolPanelRender(t *testing.T) {
	p := NewToolPanel()
	p.Stats["read"] = 3
	p.Stats["bash"] = 1
	p.Tokens = 4200
	p.StartTool("read")
	p.FinishTool("read", "ok", false)

	rendered := p.Render(20)
	if rendered == "" {
		t.Fatal("expected non-empty render")
	}
	if !strings.Contains(rendered, "Tools") {
		t.Error("expected 'Tools' header in render")
	}
	if !strings.Contains(rendered, "read") {
		t.Error("expected 'read' in render")
	}
	if !strings.Contains(rendered, "4.2k") {
		t.Error("expected token count in render")
	}
}

func TestToolPanelClear(t *testing.T) {
	p := NewToolPanel()
	p.StartTool("write")
	p.Clear()
	if len(p.Tools) != 0 {
		t.Fatalf("expected 0 tools after clear, got %d", len(p.Tools))
	}
}

func TestToolPanelHiddenRender(t *testing.T) {
	p := NewToolPanel()
	p.Visible = false
	rendered := p.Render(20)
	if rendered != "" {
		t.Fatalf("expected empty render when hidden, got: %q", rendered)
	}
}

func TestToolPanelResultTruncation(t *testing.T) {
	p := NewToolPanel()
	p.StartTool("read")

	longResult := strings.Repeat("x", 200)
	p.FinishTool("read", longResult, false)

	if len(p.Tools[0].Result) > 70 {
		t.Fatalf("expected truncated result, got length %d", len(p.Tools[0].Result))
	}
}

func TestResumeSessionRestoresAgentMessagesAndToolNick(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	model := NewModel(&cfg)
	sess := &session.Session{
		Path: "resume.jsonl",
		Entries: []session.Entry{
			{Role: "system", Content: "system prompt"},
			{Role: "user", Content: "check README"},
			{
				Role:    "assistant",
				Content: "Reading it now.",
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_1",
						Type: "function",
						Function: llm.FunctionCall{
							Name:      "read",
							Arguments: `{"path":"README.md"}`,
						},
					},
				},
			},
			{Role: "tool", Content: "README contents", ToolCallID: "call_1"},
			{Role: "assistant", Content: "Done."},
		},
	}

	model.ResumeSession(sess)

	if got := len(model.agent.Messages()); got != len(sess.Entries) {
		t.Fatalf("expected %d restored agent messages, got %d", len(sess.Entries), got)
	}
	if model.lastSavedMsgIdx != len(sess.Entries) {
		t.Fatalf("expected lastSavedMsgIdx=%d, got %d", len(sess.Entries), model.lastSavedMsgIdx)
	}
	if len(model.messages) != len(sess.Entries) {
		t.Fatalf("expected %d chat messages, got %d", len(sess.Entries), len(model.messages))
	}
	if model.messages[3].Nick != "read" {
		t.Fatalf("expected resumed tool nick to be read, got %q", model.messages[3].Nick)
	}
}

func TestResumeSessionHidesBootstrapEntriesFromDisplay(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	model := NewModel(&cfg)
	sess := &session.Session{
		Path: "resume.jsonl",
		Entries: []session.Entry{
			{Role: "system", Content: "system prompt", Bootstrap: true},
			{Role: "user", Content: "bootstrap context", Bootstrap: true},
			{Role: "assistant", Content: "bootstrap ack", Bootstrap: true},
			{Role: "user", Content: "real user turn"},
			{Role: "assistant", Content: "real reply"},
		},
	}

	model.ResumeSession(sess)

	if got := len(model.agent.Messages()); got != len(sess.Entries) {
		t.Fatalf("expected %d restored agent messages, got %d", len(sess.Entries), got)
	}
	if got := len(model.messages); got != 2 {
		t.Fatalf("expected 2 displayed messages, got %d", got)
	}
	if model.messages[0].Content != "real user turn" {
		t.Fatalf("expected first displayed message to be real user turn, got %q", model.messages[0].Content)
	}
}

func TestThemeCommandInvalidTheme(t *testing.T) {
	defer SetTheme("bitchx")

	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	model := NewModel(&cfg)
	updated, _ := model.handleCommand("/theme nonexistent")
	got := updated.(Model)

	// Should still be bitchx
	if CurrentThemeName() != "BitchX" {
		t.Fatalf("expected active theme BitchX, got %q", CurrentThemeName())
	}
	if len(got.messages) == 0 {
		t.Fatal("expected theme command to emit an error message")
	}
}
