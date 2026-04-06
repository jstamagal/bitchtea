package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ToolPanelWidth is the width of the collapsible tool panel
const ToolPanelWidth = 28

// ToolStatus tracks the state of a single tool call
type ToolStatus struct {
	Name      string
	Status    string // "running", "done", "error"
	StartTime time.Time
	Duration  time.Duration
	Result    string // truncated result
}

// ToolPanel manages the collapsible tool sidebar
type ToolPanel struct {
	Visible bool
	Tools   []ToolStatus
	Stats   map[string]int
	Tokens  int
	Elapsed time.Duration
}

// NewToolPanel creates a new tool panel
func NewToolPanel() *ToolPanel {
	return &ToolPanel{
		Visible: true,
		Stats:   make(map[string]int),
	}
}

// StartTool marks a tool as running
func (p *ToolPanel) StartTool(name string) {
	p.Tools = append(p.Tools, ToolStatus{
		Name:      name,
		Status:    "running",
		StartTime: time.Now(),
	})
	p.Stats[name]++
}

// FinishTool marks the last tool with the given name as done
func (p *ToolPanel) FinishTool(name string, result string, isError bool) {
	for i := len(p.Tools) - 1; i >= 0; i-- {
		if p.Tools[i].Name == name && p.Tools[i].Status == "running" {
			p.Tools[i].Duration = time.Since(p.Tools[i].StartTime)
			if isError {
				p.Tools[i].Status = "error"
			} else {
				p.Tools[i].Status = "done"
			}
			if len(result) > 60 {
				result = result[:60] + "..."
			}
			p.Tools[i].Result = result
			break
		}
	}
}

// Clear resets the tool panel for a new agent turn
func (p *ToolPanel) Clear() {
	p.Tools = nil
}

// Render returns the tool panel view, fitting within the given height
func (p *ToolPanel) Render(height int) string {
	if !p.Visible || height < 3 {
		return ""
	}

	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Theme.Cyan).
		Width(ToolPanelWidth - 2).
		Padding(0, 1)

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(Theme.Cyan)

	var lines []string
	lines = append(lines, headerStyle.Render("Tools"))
	lines = append(lines, strings.Repeat("─", ToolPanelWidth-4))

	// Stats summary
	for name, count := range p.Stats {
		lines = append(lines, fmt.Sprintf("  %s(%d)", name, count))
	}

	if p.Tokens > 0 {
		lines = append(lines, "")
		tokenStr := fmt.Sprintf("%d", p.Tokens)
		if p.Tokens >= 1000 {
			tokenStr = fmt.Sprintf("%.1fk", float64(p.Tokens)/1000)
		}
		lines = append(lines, fmt.Sprintf("  Tokens: ~%s", tokenStr))
	}

	if p.Elapsed > 0 {
		lines = append(lines, fmt.Sprintf("  Time: %s", p.Elapsed.Truncate(time.Second)))
	}

	// Recent tool calls
	if len(p.Tools) > 0 {
		lines = append(lines, "")
		lines = append(lines, headerStyle.Render("Recent"))
		lines = append(lines, strings.Repeat("─", ToolPanelWidth-4))

		// Show last N that fit
		start := len(p.Tools) - (height - len(lines) - 2)
		if start < 0 {
			start = 0
		}

		for _, ts := range p.Tools[start:] {
			var icon string
			var style lipgloss.Style
			switch ts.Status {
			case "running":
				icon = "◉"
				style = lipgloss.NewStyle().Foreground(Theme.Yellow)
			case "done":
				icon = "✓"
				style = lipgloss.NewStyle().Foreground(Theme.Green)
			case "error":
				icon = "✗"
				style = lipgloss.NewStyle().Foreground(Theme.Red)
			}

			durStr := ""
			if ts.Duration > 0 {
				durStr = fmt.Sprintf(" %s", ts.Duration.Truncate(time.Millisecond))
			}

			lines = append(lines, style.Render(fmt.Sprintf("  %s %s%s", icon, ts.Name, durStr)))
		}
	}

	// Truncate to fit height
	maxLines := height - 2 // account for border
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}

	content := strings.Join(lines, "\n")
	return panelStyle.Render(content)
}
