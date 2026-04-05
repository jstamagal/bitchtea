package ui

import (
	"strings"
	"testing"
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
