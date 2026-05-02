package ui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"charm.land/catwalk/pkg/catwalk"
)

// modelPicker is a minimal substring-filter picker overlay. When active it
// intercepts key events from Model.Update: typing filters the list, ↑/↓ moves
// the cursor, Enter selects the highlighted entry, Esc cancels.
//
// We intentionally do not pull in a third-party fuzzy ranker. Substring match
// + insertion-order ranking is good enough for ~hundreds of model IDs.
type modelPicker struct {
	// title is rendered above the input — typically "models for openrouter".
	title string

	// models is the full set of selectable IDs in insertion order.
	models []string

	// query is the live filter string. Empty matches everything.
	query string

	// cursor is the index into the *filtered* slice.
	cursor int

	// filtered is the current view, recomputed when query changes.
	filtered []string
}

// newModelPicker constructs a picker over the given model IDs. The list is
// not deduped or sorted — callers control ordering.
func newModelPicker(title string, models []string) *modelPicker {
	p := &modelPicker{
		title:  title,
		models: append([]string(nil), models...),
	}
	p.refilter()
	return p
}

// modelsForService returns the model IDs from the catwalk Provider whose ID
// matches service (case-insensitive). Returns nil if no provider matches.
// Selection ordering: provider's own Models slice (catwalk's preferred order),
// with the default-large model floated to the front when present so the
// most-likely choice is the initial cursor target.
func modelsForService(providers []catwalk.Provider, service string) []string {
	service = strings.TrimSpace(strings.ToLower(service))
	if service == "" {
		return nil
	}
	for _, p := range providers {
		if strings.EqualFold(string(p.ID), service) {
			ids := make([]string, 0, len(p.Models))
			seen := make(map[string]bool, len(p.Models))
			if def := strings.TrimSpace(p.DefaultLargeModelID); def != "" {
				for _, m := range p.Models {
					if m.ID == def {
						ids = append(ids, def)
						seen[def] = true
						break
					}
				}
			}
			for _, m := range p.Models {
				if seen[m.ID] || m.ID == "" {
					continue
				}
				ids = append(ids, m.ID)
				seen[m.ID] = true
			}
			return ids
		}
	}
	return nil
}

// availableServices returns the sorted, lowercased provider IDs for a hint
// when the active service has no catalog entry.
func availableServices(providers []catwalk.Provider) []string {
	out := make([]string, 0, len(providers))
	for _, p := range providers {
		id := strings.ToLower(strings.TrimSpace(string(p.ID)))
		if id != "" {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// refilter recomputes filtered from query. Substring match, case-insensitive.
// Cursor clamps to the new range.
func (p *modelPicker) refilter() {
	q := strings.ToLower(strings.TrimSpace(p.query))
	if q == "" {
		p.filtered = append([]string(nil), p.models...)
	} else {
		p.filtered = p.filtered[:0]
		for _, m := range p.models {
			if strings.Contains(strings.ToLower(m), q) {
				p.filtered = append(p.filtered, m)
			}
		}
	}
	if p.cursor >= len(p.filtered) {
		p.cursor = len(p.filtered) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

// moveCursor shifts the cursor by delta with clamping (no wrap).
func (p *modelPicker) moveCursor(delta int) {
	if len(p.filtered) == 0 {
		p.cursor = 0
		return
	}
	p.cursor += delta
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.cursor >= len(p.filtered) {
		p.cursor = len(p.filtered) - 1
	}
}

// selected returns the currently highlighted model ID, or "" if the filter
// excludes everything.
func (p *modelPicker) selected() string {
	if len(p.filtered) == 0 {
		return ""
	}
	return p.filtered[p.cursor]
}

// appendQuery adds a literal character to the filter and refilters.
func (p *modelPicker) appendQuery(s string) {
	p.query += s
	p.cursor = 0
	p.refilter()
}

// backspace removes the last rune from the filter and refilters.
func (p *modelPicker) backspace() {
	if p.query == "" {
		return
	}
	r := []rune(p.query)
	p.query = string(r[:len(r)-1])
	p.cursor = 0
	p.refilter()
}

// view renders a one-message dump suitable for stuffing into the chat
// scrollback. We deliberately don't paint over the viewport — opening a new
// pane would mean wiring a whole sub-model. Instead we re-render the current
// list as a system message every time the state changes; the agent loop
// already handles incremental viewport refresh well.
//
// Keep the line count bounded so the picker doesn't dominate the screen.
func (p *modelPicker) view(maxRows int) string {
	if maxRows < 5 {
		maxRows = 5
	}
	var sb strings.Builder
	sb.WriteString(pickerTitleStyle.Render(p.title))
	sb.WriteString("\n")
	sb.WriteString(pickerPromptStyle.Render("filter> "))
	sb.WriteString(p.query)
	sb.WriteString("\n")
	if len(p.filtered) == 0 {
		sb.WriteString(pickerHintStyle.Render("  (no matches — backspace to widen)"))
		sb.WriteString("\n")
	} else {
		// Window the list around the cursor so the user always sees the
		// highlighted entry even with hundreds of rows.
		start := 0
		if p.cursor > maxRows/2 {
			start = p.cursor - maxRows/2
		}
		end := start + maxRows
		if end > len(p.filtered) {
			end = len(p.filtered)
			start = end - maxRows
			if start < 0 {
				start = 0
			}
		}
		for i := start; i < end; i++ {
			marker := "  "
			line := p.filtered[i]
			if i == p.cursor {
				marker = "> "
				line = pickerCursorStyle.Render(line)
			}
			sb.WriteString(marker)
			sb.WriteString(line)
			sb.WriteString("\n")
		}
		if end < len(p.filtered) {
			sb.WriteString(pickerHintStyle.Render("  ..."))
			sb.WriteString("\n")
		}
	}
	sb.WriteString(pickerHintStyle.Render("  enter=pick  esc=cancel  type=filter"))
	return sb.String()
}

// Picker styles — kept local because they're only used by the picker view.
var (
	pickerTitleStyle  = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	pickerPromptStyle = lipgloss.NewStyle().Foreground(ColorMagenta).Bold(true)
	pickerCursorStyle = lipgloss.NewStyle().Foreground(ColorYellow).Bold(true)
	pickerHintStyle   = lipgloss.NewStyle().Foreground(ColorGray)
)
