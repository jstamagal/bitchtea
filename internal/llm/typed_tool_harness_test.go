package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"charm.land/fantasy"
)

// This file is the test harness for typed fantasy tool wrappers (bt-p2-harness).
// It is consumed by the per-tool port tickets bt-p2-bash-memory, bt-p2-edit, and
// bt-p2-read-write so each ported tool can be asserted with a uniform shape:
//
//  - schema generation (assertSchemaHasField)
//  - successful Run     (assertToolReturnsTextResponse)
//  - tool-level errors  (assertToolReturnsErrorResponse)
//  - context cancel     (asserted as a text response — see comment in cancel test)
//
// All helpers live in test files; nothing in production code (tools.go,
// client.go, ...) is touched by this scaffolding.

// runTypedTool marshals args to JSON, builds a fantasy.ToolCall, and invokes the
// wrapper's Run. It returns whatever the wrapper returned. Tests then use the
// assert* helpers below; this runner is intentionally dumb — it doesn't
// pre-judge IsError, doesn't log, doesn't retry — so each acceptance point
// owns its own assertion.
//
// callID is fixed to a sentinel so failures point clearly at this harness when
// a stack trace surfaces it.
func runTypedTool(
	t *testing.T,
	ctx context.Context,
	tool fantasy.AgentTool,
	args any,
) (fantasy.ToolResponse, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("runTypedTool: marshal args: %v", err)
	}
	return tool.Run(ctx, fantasy.ToolCall{
		ID:    "call_harness",
		Name:  tool.Info().Name,
		Input: string(raw),
	})
}

// assertToolReturnsTextResponse asserts a normal (non-error) text response
// whose content contains wantSubstring. It also asserts the wrapper did NOT
// return a Go error — typed wrappers should surface tool failures as
// IsError responses, not as Go errors that would abort the fantasy stream.
func assertToolReturnsTextResponse(
	t *testing.T,
	resp fantasy.ToolResponse,
	runErr error,
	wantSubstring string,
) {
	t.Helper()
	if runErr != nil {
		t.Fatalf("typed tool must not return a Go error on success; got %v", runErr)
	}
	if resp.IsError {
		t.Fatalf("expected IsError=false, got error response %q", resp.Content)
	}
	if resp.Type != "text" {
		t.Fatalf("expected text response type, got %q (resp=%+v)", resp.Type, resp)
	}
	if !strings.Contains(resp.Content, wantSubstring) {
		t.Fatalf("response content %q does not contain %q", resp.Content, wantSubstring)
	}
}

// assertToolReturnsErrorResponse asserts a tool-level error response — i.e.
// the wrapper turned a tool failure into NewTextErrorResponse rather than
// returning a Go error. wantSubstring is matched against resp.Content.
func assertToolReturnsErrorResponse(
	t *testing.T,
	resp fantasy.ToolResponse,
	runErr error,
	wantSubstring string,
) {
	t.Helper()
	if runErr != nil {
		t.Fatalf("typed tool must not return a Go error on tool failure (would abort the fantasy stream); got %v", runErr)
	}
	if !resp.IsError {
		t.Fatalf("expected IsError=true on tool failure, got %+v", resp)
	}
	if resp.Type != "text" {
		t.Fatalf("expected text response type for error, got %q (resp=%+v)", resp.Type, resp)
	}
	if wantSubstring != "" && !strings.Contains(resp.Content, wantSubstring) {
		t.Fatalf("error content %q does not contain %q", resp.Content, wantSubstring)
	}
}

// assertSchemaHasField asserts that the typed wrapper's auto-generated schema
// (Info().Parameters) contains a property named fieldName whose "type" is
// jsonType. This is the minimum guarantee a per-tool port needs: the LLM-facing
// schema actually advertises the inputs the tool consumes.
func assertSchemaHasField(
	t *testing.T,
	info fantasy.ToolInfo,
	fieldName string,
	jsonType string,
) {
	t.Helper()
	property, ok := info.Parameters[fieldName]
	if !ok {
		t.Fatalf("schema for %q missing field %q; have %+v", info.Name, fieldName, info.Parameters)
	}
	propertyMap, ok := property.(map[string]any)
	if !ok {
		t.Fatalf("schema for %q field %q is not a map: %#v", info.Name, fieldName, property)
	}
	gotType, _ := propertyMap["type"].(string)
	if gotType != jsonType {
		t.Fatalf("schema for %q field %q type = %q, want %q", info.Name, fieldName, gotType, jsonType)
	}
}

// dummyEchoInput is the typed input shape for the in-test dummy tool used to
// exercise the harness itself. Keeping it a distinct named type makes the
// schema-generation test crisp: we know exactly which fields and types to
// expect because we declared them right here.
type dummyEchoInput struct {
	Message string `json:"message"`
	Times   int    `json:"times"`
	Fail    bool   `json:"fail"`
}

// newDummyEchoTool builds a typed fantasy tool that:
//   - on Fail=true, returns a tool-level error response (NewTextErrorResponse)
//   - on ctx.Done, returns the cancellation cause as a text error response
//     (matches current bitchteaTool behavior — bt-p8-state will define a
//     synthetic cancellation contract; this harness asserts what code does NOW)
//   - otherwise, echoes Message repeated Times times.
func newDummyEchoTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		"dummy_echo",
		"echoes its input; for harness tests only",
		func(ctx context.Context, in dummyEchoInput, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if err := ctx.Err(); err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: %v", err)), nil
			}
			if in.Fail {
				return fantasy.NewTextErrorResponse(
					fmt.Sprintf("Error: %v", errors.New("dummy_echo asked to fail")),
				), nil
			}
			times := in.Times
			if times <= 0 {
				times = 1
			}
			return fantasy.NewTextResponse(strings.Repeat(in.Message, times)), nil
		},
	)
}

// --- Acceptance: schema generation -----------------------------------------

func TestTypedToolHarness_SchemaGeneration(t *testing.T) {
	tool := newDummyEchoTool()
	info := tool.Info()

	if info.Name != "dummy_echo" {
		t.Fatalf("info.Name = %q, want dummy_echo", info.Name)
	}
	assertSchemaHasField(t, info, "message", "string")
	assertSchemaHasField(t, info, "times", "integer")
	assertSchemaHasField(t, info, "fail", "boolean")

	// Sanity: the schema must not nest "type"/"properties"/"required" inside
	// the property map — that's the same shape constraint we already enforce
	// for the untyped translateTools adapter.
	for _, bogus := range []string{"type", "properties", "required"} {
		if _, ok := info.Parameters[bogus]; ok {
			t.Fatalf("typed schema must not include nested key %q at properties root: %+v", bogus, info.Parameters)
		}
	}
}

// --- Acceptance: successful Run --------------------------------------------

func TestTypedToolHarness_SuccessfulRun(t *testing.T) {
	tool := newDummyEchoTool()
	resp, err := runTypedTool(t, context.Background(), tool, dummyEchoInput{
		Message: "ok",
		Times:   3,
	})
	assertToolReturnsTextResponse(t, resp, err, "okokok")
}

// --- Acceptance: tool error wrapped as fantasy text error -------------------

func TestTypedToolHarness_ToolErrorBecomesTextErrorResponse(t *testing.T) {
	tool := newDummyEchoTool()
	resp, err := runTypedTool(t, context.Background(), tool, dummyEchoInput{
		Message: "ignored",
		Fail:    true,
	})
	assertToolReturnsErrorResponse(t, resp, err, "dummy_echo asked to fail")
}

// --- Acceptance: context cancellation ---------------------------------------
//
// Asserts what the code does TODAY: a cancelled ctx surfaces through the tool
// as a text error response (NewTextErrorResponse wrapping ctx.Err()), not as
// a Go error. bt-p8-state will redesign this contract — when that lands this
// test should be updated alongside it, not preserved as-is.
func TestTypedToolHarness_CancelledContextSurfacesAsTextErrorResponse(t *testing.T) {
	tool := newDummyEchoTool()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resp, err := runTypedTool(t, ctx, tool, dummyEchoInput{Message: "won't run"})
	assertToolReturnsErrorResponse(t, resp, err, "context canceled")
}
