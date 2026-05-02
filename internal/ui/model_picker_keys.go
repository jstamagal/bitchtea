package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// openPicker shows a picker overlay and remembers the selection callback.
// The initial render is appended to the chat scrollback as a system message
// so it lives in the existing viewport rather than requiring a sub-pane.
//
// The onSelect callback receives a *Model rather than a closure-captured
// pointer because Update hands each call a fresh value copy of Model — the
// callback fires from a future Update where the live receiver is the only
// pointer that mutates the *current* state.
func (m *Model) openPicker(p *modelPicker, onSelect func(*Model, string)) {
	m.picker = p
	m.pickerOnSelect = onSelect
	// Append a placeholder; rerenderPicker will overwrite it on every update.
	m.addMessage(ChatMessage{
		Time:    time.Now(),
		Type:    MsgRaw,
		Content: p.view(pickerVisibleRows),
	})
	m.pickerMsgIdx = len(m.messages) - 1
	m.refreshViewport()
}

// closePicker drops the overlay and clears its state. Caller is responsible
// for any user-facing confirmation message.
func (m *Model) closePicker() {
	m.picker = nil
	m.pickerOnSelect = nil
	m.pickerMsgIdx = 0
}

// rerenderPicker rewrites the picker's chat block in place so the scrollback
// shows live filter/cursor state without piling new copies.
func (m *Model) rerenderPicker() {
	if m.picker == nil {
		return
	}
	if m.pickerMsgIdx < 0 || m.pickerMsgIdx >= len(m.messages) {
		return
	}
	m.messages[m.pickerMsgIdx].Content = m.picker.view(pickerVisibleRows)
	m.refreshViewport()
}

// handlePickerKey routes a key event to the active picker. Enter selects,
// Esc cancels, Up/Down move, Backspace deletes one rune from the filter,
// printable runes append to the filter.
func (m *Model) handlePickerKey(msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyEnter:
		choice := m.picker.selected()
		cb := m.pickerOnSelect
		m.closePicker()
		if choice != "" && cb != nil {
			cb(m, choice)
		} else if choice == "" {
			m.sysMsg("Picker: no selection (filter excludes everything).")
		}
		return
	case tea.KeyEsc, tea.KeyCtrlC:
		m.closePicker()
		m.sysMsg("Picker cancelled.")
		return
	case tea.KeyUp:
		m.picker.moveCursor(-1)
		m.rerenderPicker()
		return
	case tea.KeyDown:
		m.picker.moveCursor(1)
		m.rerenderPicker()
		return
	case tea.KeyPgUp:
		m.picker.moveCursor(-pickerVisibleRows)
		m.rerenderPicker()
		return
	case tea.KeyPgDown:
		m.picker.moveCursor(pickerVisibleRows)
		m.rerenderPicker()
		return
	case tea.KeyBackspace:
		m.picker.backspace()
		m.rerenderPicker()
		return
	case tea.KeyRunes, tea.KeySpace:
		// Append printable runes (including space) to the filter.
		runes := msg.Runes
		if msg.Type == tea.KeySpace && len(runes) == 0 {
			runes = []rune{' '}
		}
		if len(runes) == 0 {
			return
		}
		m.picker.appendQuery(string(runes))
		m.rerenderPicker()
		return
	}
	// Unhandled keys are dropped silently — the picker should not leak input
	// back to the textarea.
}

// pickerVisibleRows controls how many model entries are rendered around the
// cursor. Picked to fit comfortably in a single screen of scrollback.
const pickerVisibleRows = 12
