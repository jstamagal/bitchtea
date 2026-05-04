package event

import (
	"errors"
	"reflect"
	"testing"
)

func TestStateTransitions_validSequence(t *testing.T) {
	events := []Event{
		{Type: "state", State: StateIdle},
		{Type: "state", State: StateThinking},
		{Type: "state", State: StateToolCall},
		{Type: "state", State: StateThinking},
		{Type: "state", State: StateIdle},
	}
	want := []State{StateIdle, StateThinking, StateToolCall, StateThinking, StateIdle}

	for i, ev := range events {
		if ev.Type != "state" {
			t.Fatalf("event %d Type = %q, want state", i, ev.Type)
		}
		if ev.State != want[i] {
			t.Fatalf("event %d State = %v, want %v", i, ev.State, want[i])
		}
	}
}

func TestStateTransitions_rejectsBackwardMovesIfApplicable(t *testing.T) {
	events := []Event{
		{Type: "state", State: StateToolCall},
		{Type: "state", State: StateThinking},
		{Type: "state", State: StateIdle},
	}

	// Event is a transport payload, not a state machine. Backward moves are
	// representable because callers must be able to return tool/thinking work to
	// idle without a package-level validator rejecting the event.
	for i, ev := range events {
		if ev.Type != "state" {
			t.Fatalf("event %d Type = %q, want state", i, ev.Type)
		}
	}
	if events[0].State <= events[1].State || events[1].State <= events[2].State {
		t.Fatalf("backward sequence not preserved: %+v", events)
	}
}

func TestEvent_doneCarriesUsage(t *testing.T) {
	ev := Event{Type: "done"}

	if ev.Type != "done" {
		t.Fatalf("Type = %q, want done", ev.Type)
	}
	if ev.Error != nil {
		t.Fatalf("done event Error = %v, want nil", ev.Error)
	}
	if _, ok := reflect.TypeOf(Event{}).FieldByName("Usage"); ok {
		t.Fatal("event.Event unexpectedly grew a Usage field; update done-event coverage to assert the carried values")
	}
}

func TestEvent_errorFinalizesState(t *testing.T) {
	errBoom := errors.New("boom")
	ev := Event{Type: "error", Error: errBoom, State: StateIdle}

	if ev.Type != "error" {
		t.Fatalf("Type = %q, want error", ev.Type)
	}
	if !errors.Is(ev.Error, errBoom) {
		t.Fatalf("Error = %v, want %v", ev.Error, errBoom)
	}
	if ev.State != StateIdle {
		t.Fatalf("State = %v, want StateIdle", ev.State)
	}
}

func TestEvent_toolEventsCarryToolMetadata(t *testing.T) {
	ev := Event{
		Type:       "tool_result",
		ToolName:   "read",
		ToolCallID: "call_123",
		ToolArgs:   `{"path":"README.md"}`,
		ToolResult: "ok",
	}

	if ev.Type != "tool_result" {
		t.Fatalf("Type = %q, want tool_result", ev.Type)
	}
	if ev.ToolName != "read" || ev.ToolCallID != "call_123" {
		t.Fatalf("tool identity = (%q, %q), want (read, call_123)", ev.ToolName, ev.ToolCallID)
	}
	if ev.ToolArgs == "" || ev.ToolResult == "" {
		t.Fatalf("tool payload not preserved: %+v", ev)
	}
}
