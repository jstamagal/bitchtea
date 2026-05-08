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
	Args      string // tool arguments (for verbose mode)
	Status    string // "running", "done", "error"
	StartTime time.Time
	Duration  time.Duration
	Result    string // truncated result
}

// ToolPanel manages the collapsible tool sidebar
type ToolPanel struct {
	Visible   bool
	Tools     []ToolStatus
	Stats     map[string]int
	Tokens    int
	Elapsed   time.Duration
	Verbosity string // "terse", "normal" (default), "verbose"
}

// NewToolPanel creates a new tool panel
func NewToolPanel() *ToolPanel {
	return &ToolPanel{
		Visible: true,
		Stats:   make(map[string]int),
	}
}

// StartTool marks a tool as running. args is stored for verbose display.
func (p *ToolPanel) StartTool(name string, args ...string) {
	ts := ToolStatus{
		Name:      name,
		Status:    "running",
		StartTime: time.Now(),
	}
	if len(args) > 0 {
		ts.Args = args[0]
	}
	p.Tools = append(p.Tools, ts)
	p.Stats[name]++
}

// FinishTool marks the last tool with the given name as done.
// Result storage length depends on Verbosity: verbose=500, normal=60, terse=0.
func (p *ToolPanel) FinishTool(name string, result string, isError bool) {
	verbosity := p.Verbosity
	if verbosity == "" {
		verbosity = "normal"
	}
	for i := len(p.Tools) - 1; i >= 0; i-- {
		if p.Tools[i].Name == name && p.Tools[i].Status == "running" {
			p.Tools[i].Duration = time.Since(p.Tools[i].StartTime)
			if isError {
				p.Tools[i].Status = "error"
			} else {
				p.Tools[i].Status = "done"
			}
			switch verbosity {
			case "terse":
				// terse: no result text stored at all
			case "verbose":
				if len(result) > 500 {
					result = result[:500] + "..."
				}
				p.Tools[i].Result = result
			default: // "normal"
				if len(result) > 60 {
					result = result[:60] + "..."
				}
				p.Tools[i].Result = result
			}
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
		avail := height - len(lines) - 2
		if avail < 0 {
			avail = 0
		}
		start := len(p.Tools) - avail
		if start < 0 {
			start = 0
		}

		verbosity := p.Verbosity
		if verbosity == "" {
			verbosity = "normal"
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

			switch verbosity {
			case "terse":
				// terse: just icon + name, no duration, no result
				lines = append(lines, style.Render(fmt.Sprintf("  %s %s", icon, ts.Name)))
			case "verbose":
				// verbose: icon + name + duration + args + result
				durStr := ""
				if ts.Duration > 0 {
					durStr = fmt.Sprintf(" %s", ts.Duration.Truncate(time.Millisecond))
				}
				lines = append(lines, style.Render(fmt.Sprintf("  %s %s%s", icon, ts.Name, durStr)))
				if ts.Args != "" {
					lines = append(lines, fmt.Sprintf("    args: %s", ts.Args))
				}
				if ts.Result != "" {
					lines = append(lines, fmt.Sprintf("    result: %s", ts.Result))
				}
			default: // "normal"
				durStr := ""
				if ts.Duration > 0 {
					durStr = fmt.Sprintf(" %s", ts.Duration.Truncate(time.Millisecond))
				}
				lines = append(lines, style.Render(fmt.Sprintf("  %s %s%s", icon, ts.Name, durStr)))
			}
		}
	}

	// Truncate to fit height
	maxLines := height - 2 // account for border
	if maxLines < 0 {
		maxLines = 0
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}

	content := strings.Join(lines, "\n")
	return panelStyle.Render(content)
}
