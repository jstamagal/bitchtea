package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/mcp"
	"github.com/jstamagal/bitchtea/internal/tools"
)

// TestMCPIntegration_AgentLoopDispatch verifies the full MCP tool execution
// path through the ChatStreamer boundary — the exact interface the agent loop
// talks to. A fake MCP server is wired through a real Manager; the streamer
// emits a tool_call for a namespaced MCP tool, actually dispatches through
// mcpAgentTool.Run → manager.CallTool → fake server, and emits the result.
// This proves that when the agent loop's StreamChat returns a tool_call, MCP
// dispatch runs correctly end-to-end without a subprocess.
func TestMCPIntegration_AgentLoopDispatch(t *testing.T) {
	// --- 1. Set up a fake MCP server with a tool ---
	srv := &fakeMCPServer{
		name: "weather",
		tools: []mcp.Tool{{
			Name:        "forecast",
			Description: "get weather forecast for a city",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"},"days":{"type":"integer"}},"required":["city"]}`),
		}},
		callFn: func(name string, args json.RawMessage) (mcp.Result, error) {
			if name != "forecast" {
				t.Errorf("server saw bare tool name = %q, want forecast", name)
			}
			var parsed struct {
				City string `json:"city"`
				Days int    `json:"days"`
			}
			if err := json.Unmarshal(args, &parsed); err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{
				Content: "Weather for " + parsed.City + ": sunny, 22°C (3-day forecast)",
			}, nil
		},
	}

	// --- 2. Start a real Manager backed by the fake server ---
	manager := newManagerWithFakes(t, srv)

	// --- 3. Materialize the MCP tools ---
	mcpTools, err := MCPTools(context.Background(), manager)
	if err != nil {
		t.Fatalf("MCPTools err: %v", err)
	}
	if len(mcpTools) != 1 {
		t.Fatalf("expected 1 MCP tool, got %d", len(mcpTools))
	}
	mcpTool := mcpTools[0]
	if name := mcpTool.Info().Name; name != "mcp__weather__forecast" {
		t.Fatalf("namespaced name = %q, want mcp__weather__forecast", name)
	}

	// --- 4. Build a ChatStreamer that dispatches the MCP tool for real ---
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())
	streamer := &mcpDispatchStreamer{
		mcpTool: mcpTool,
	}

	// --- 5. Run through the ChatStreamer boundary ---
	msgs := []Message{
		{Role: "user", Content: "what's the weather in berlin?"},
	}
	events := make(chan StreamEvent, 32)
	ctx := context.Background()
	go streamer.StreamChat(ctx, msgs, reg, events)

	// --- 6. Collect and assert events ---
	var (
		gotToolCall   bool
		gotToolResult bool
		gotText       bool
		gotDone       bool
		resultText    string
	)
	for ev := range events {
		switch ev.Type {
		case "tool_call":
			if ev.ToolName == "mcp__weather__forecast" {
				gotToolCall = true
				if ev.ToolArgs != `{"city":"berlin","days":3}` {
					t.Errorf("tool args = %q, want {\"city\":\"berlin\",\"days\":3}", ev.ToolArgs)
				}
			}
		case "tool_result":
			if ev.ToolName == "mcp__weather__forecast" {
				gotToolResult = true
				resultText = ev.Text
			}
		case "text":
			if strings.Contains(ev.Text, "Weather for berlin") {
				gotText = true
			}
		case "done":
			gotDone = true
		case "error":
			t.Fatalf("unexpected error event: %v", ev.Error)
		}
	}

	// Assertions
	if !gotToolCall {
		t.Fatal("expected tool_call event for mcp__weather__forecast")
	}
	if !gotToolResult {
		t.Fatal("expected tool_result event for mcp__weather__forecast")
	}
	if !strings.Contains(resultText, "Weather for berlin") {
		t.Fatalf("tool result text = %q, want 'Weather for berlin: sunny, 22°C (3-day forecast)'", resultText)
	}
	if !strings.Contains(resultText, "22°C") {
		t.Fatalf("tool result missing temperature: %q", resultText)
	}
	if !gotText {
		t.Fatal("expected text event after tool execution")
	}
	if !gotDone {
		t.Fatal("expected done event")
	}
	if streamer.calls != 1 {
		t.Fatalf("expected 1 streamer call, got %d", streamer.calls)
	}
}

// TestMCPIntegration_MultiTurnDispatch tests two consecutive StreamChat calls
// where the first turn calls an MCP tool, and a second turn uses the result.
// This mirrors the real agent loop: first StreamChat emits tool_call + result,
// second StreamChat carries forward the conversation with the result in context.
func TestMCPIntegration_MultiTurnDispatch(t *testing.T) {
	srv := &fakeMCPServer{
		name: "calc",
		tools: []mcp.Tool{{
			Name:        "add",
			Description: "add two numbers",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}},"required":["a","b"]}`),
		}},
		callFn: func(name string, args json.RawMessage) (mcp.Result, error) {
			var parsed struct {
				A float64 `json:"a"`
				B float64 `json:"b"`
			}
			json.Unmarshal(args, &parsed)
			sum := parsed.A + parsed.B
			return mcp.Result{
				Content: fmt.Sprintf("result: %v", sum),
			}, nil
		},
	}
	manager := newManagerWithFakes(t, srv)
	mcpTools, _ := MCPTools(context.Background(), manager)

	reg := tools.NewRegistry(t.TempDir(), t.TempDir())
	streamer := &mcpDispatchStreamer{
		mcpTool: mcpTools[0],
	}

	// Turn 1: call the MCP tool
	events1 := make(chan StreamEvent, 32)
	go streamer.StreamChat(context.Background(), []Message{
		{Role: "user", Content: "add 2 and 3"},
	}, reg, events1)

	var resultText string
	for ev := range events1 {
		if ev.Type == "error" {
			t.Fatalf("turn 1 error: %v", ev.Error)
		}
		if ev.Type == "tool_result" {
			resultText = ev.Text
		}
	}
	if !strings.Contains(resultText, "result: 5") {
		t.Fatalf("turn 1 result = %q, want 'result: 5'", resultText)
	}
	if streamer.calls != 1 {
		t.Fatalf("turn 1 calls = %d, want 1", streamer.calls)
	}

	// Turn 2: a follow-up that doesn't call a tool — confirms the streamer
	// still works after an MCP dispatch turn.
	streamer.secondTurnText = "The sum is 5, as computed by the calc server."
	events2 := make(chan StreamEvent, 32)
	go streamer.StreamChat(context.Background(), []Message{
		{Role: "user", Content: "add 2 and 3"},
		{Role: "assistant", Content: "ok, let me use the MCP tool", ToolCalls: []ToolCall{
			{ID: "call_1", Function: FunctionCall{Name: "mcp__calc__add", Arguments: `{"a":2,"b":3}`}},
		}},
		{Role: "tool", Content: resultText, ToolCallID: "call_1"},
		{Role: "assistant", Content: "The sum is 5."},
		{Role: "user", Content: "thanks!"},
	}, reg, events2)

	var gotSecondText bool
	for ev := range events2 {
		if ev.Type == "error" {
			t.Fatalf("turn 2 error: %v", ev.Error)
		}
		if ev.Type == "text" && strings.Contains(ev.Text, "computed by the calc server") {
			gotSecondText = true
		}
	}
	if !gotSecondText {
		t.Fatal("turn 2: expected text referencing the calc server")
	}
	if streamer.calls != 2 {
		t.Fatalf("turn 2 calls = %d, want 2", streamer.calls)
	}
}

// TestMCPIntegration_MCPToolErrorResponse verifies that when the fake MCP
// server returns an error (IsError=true or Go error), it surfaces correctly
// as a fantasy text-error response through the ChatStreamer.
func TestMCPIntegration_MCPToolErrorResponse(t *testing.T) {
	srv := &fakeMCPServer{
		name: "fs",
		tools: []mcp.Tool{{
			Name:        "cat",
			Description: "cat a file",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
		}},
		callFn: func(name string, args json.RawMessage) (mcp.Result, error) {
			return mcp.Result{Content: "file not found: noent.txt", IsError: true}, nil
		},
	}
	manager := newManagerWithFakes(t, srv)
	mcpTools, _ := MCPTools(context.Background(), manager)

	reg := tools.NewRegistry(t.TempDir(), t.TempDir())
	streamer := &mcpDispatchStreamer{
		mcpTool:           mcpTools[0],
		errorMode:         true,
		errorModeArgs:     `{"path":"noent.txt"}`,
		errorModeToolName: "mcp__fs__cat",
	}

	events := make(chan StreamEvent, 32)
	go streamer.StreamChat(context.Background(), []Message{
		{Role: "user", Content: "cat noent.txt"},
	}, reg, events)

	var gotToolResult bool
	var resultText string
	for ev := range events {
		if ev.Type == "error" {
			t.Fatalf("unexpected error event: %v", ev.Error)
		}
		if ev.Type == "tool_result" {
			gotToolResult = true
			resultText = ev.Text
		}
	}
	if !gotToolResult {
		t.Fatal("expected tool_result event even for MCP error")
	}
	if !strings.Contains(resultText, "file not found") {
		t.Fatalf("tool result text = %q, want 'Error: file not found: noent.txt'", resultText)
	}
}

// mcpDispatchStreamer is a ChatStreamer that simulates a fantasy turn. On its
// first call (and optionally in errorMode), it emits a tool_call for the MCP
// tool, dispatches through the real mcpAgentTool.Run → manager.CallTool →
// fake server, emits the tool_result, then emits a final text + done. On
// subsequent normal calls it acts as a plain text-only turn so multi-turn
// tests can verify the streamer stays sane after an MCP dispatch.
type mcpDispatchStreamer struct {
	mcpTool           fantasy.AgentTool
	calls             int
	secondTurnText    string
	errorMode         bool
	errorModeArgs     string
	errorModeToolName string
}

func (s *mcpDispatchStreamer) StreamChat(_ context.Context, msgs []Message, _ *tools.Registry, events chan<- StreamEvent) {
	defer close(events)

	s.calls++

	// Determine if this turn should dispatch an MCP tool.
	shouldDispatch := s.calls == 1

	if shouldDispatch {
		callID := "mcp_call_1"
		namespacedName := s.mcpTool.Info().Name
		args := `{"city":"berlin","days":3}`

		// Select args based on the test mode or user message content.
		if s.errorMode {
			namespacedName = s.errorModeToolName
			args = s.errorModeArgs
		} else if len(msgs) > 0 && msgs[0].Role == "user" && strings.Contains(msgs[0].Content, "add 2 and 3") {
			args = `{"a":2,"b":3}`
		}

		// Emit the tool_call event.
		events <- StreamEvent{
			Type:       "tool_call",
			ToolCallID: callID,
			ToolName:   namespacedName,
			ToolArgs:   args,
		}

		// Actually dispatch through the mcpAgentTool.
		resp, err := s.mcpTool.Run(context.Background(), fantasy.ToolCall{
			ID:    callID,
			Name:  namespacedName,
			Input: args,
		})
		if err != nil {
			events <- StreamEvent{Type: "error", Error: err}
			return
		}

		// Emit the tool_result with the actual dispatch result.
		resultText := resp.Content
		if resp.IsError {
			resultText = "Error: " + resp.Content
		}
		events <- StreamEvent{
			Type:       "tool_result",
			ToolCallID: callID,
			ToolName:   namespacedName,
			Text:       resultText,
		}

		// Final text + done (simulates fantasy's second step after tool result).
		events <- StreamEvent{Type: "text", Text: resultText}
		events <- StreamEvent{Type: "done", Messages: []Message{
			{Role: "assistant", Content: resultText},
		}}
		return
	}

	// Subsequent turn: plain text response, no tools.
	text := s.secondTurnText
	if text == "" {
		text = "acknowledged"
	}
	events <- StreamEvent{Type: "text", Text: text}
	events <- StreamEvent{Type: "done"}
}
