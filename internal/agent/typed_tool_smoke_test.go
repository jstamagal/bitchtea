package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/llm"
)

// This is the bt-p2-verify integration smoke for the Phase 2 typed-tool
// migration. Unlike the fakeStreamer tests in agent_loop_test.go (which
// pre-fake the tool_result), this smoke wires the agent to a real llm.Client
// whose underlying fantasy.LanguageModel is a fake. The agent dispatches the
// real `read` tool through the typed wrapper in internal/llm/typed_read.go,
// which calls into internal/tools.Registry.Execute against a real workspace.
//
// What this proves end-to-end:
//   - typed wrapper routes correctly (read schema is advertised, fantasy
//     dispatches to the typed Run callback in internal/llm)
//   - the tool actually executes against the real filesystem
//   - the result lands back in the agent as a tool_result event
//   - the loop completes cleanly with the expected message sequence
//
// Scope: ONE test. Per-tool semantic coverage lives in
// internal/llm/typed_*_test.go and internal/tools/tools_test.go.

// readReplyLanguageModel is a fake fantasy.LanguageModel that on its first
// Stream call emits a `read` tool call, and on its second Stream call (after
// the tool result is folded back in by fantasy) emits a finishing text. This
// mirrors the captureLanguageModel pattern in internal/llm/tools_test.go but
// is duplicated here so this smoke can live in the agent package without
// reaching into llm internals.
type readReplyLanguageModel struct {
	calls    []fantasy.Call
	filePath string // expected "path" arg the fake will request
}

func (m *readReplyLanguageModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return nil, fmt.Errorf("Generate not implemented")
}

func (m *readReplyLanguageModel) Stream(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.calls = append(m.calls, call)
	switch len(m.calls) {
	case 1:
		return func(yield func(fantasy.StreamPart) bool) {
			if !yield(fantasy.StreamPart{
				Type:          fantasy.StreamPartTypeToolCall,
				ID:            "call_smoke_read",
				ToolCallName:  "read",
				ToolCallInput: fmt.Sprintf(`{"path":%q}`, m.filePath),
			}) {
				return
			}
			yield(fantasy.StreamPart{
				Type:         fantasy.StreamPartTypeFinish,
				FinishReason: fantasy.FinishReasonToolCalls,
				Usage:        fantasy.Usage{InputTokens: 12, OutputTokens: 3, TotalTokens: 15},
			})
		}, nil
	case 2:
		return func(yield func(fantasy.StreamPart) bool) {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "text_1"}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "text_1", Delta: "ack"}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "text_1"}) {
				return
			}
			yield(fantasy.StreamPart{
				Type:         fantasy.StreamPartTypeFinish,
				FinishReason: fantasy.FinishReasonStop,
				Usage:        fantasy.Usage{InputTokens: 4, OutputTokens: 1, TotalTokens: 5},
			})
		}, nil
	default:
		return nil, fmt.Errorf("unexpected stream call %d", len(m.calls))
	}
}

func (m *readReplyLanguageModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, fmt.Errorf("GenerateObject not implemented")
}

func (m *readReplyLanguageModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, fmt.Errorf("StreamObject not implemented")
}

func (m *readReplyLanguageModel) Provider() string { return "smoke" }
func (m *readReplyLanguageModel) Model() string    { return "smoke-model" }

func TestPhase2TypedToolSmoke_ReadFileEndToEnd(t *testing.T) {
	// Build a real workspace with a known file. The agent's tools.Registry
	// will resolve "smoke.txt" against this directory.
	workDir := t.TempDir()
	const fileName = "smoke.txt"
	const fileBody = "phase 2 typed-tool smoke body\n"
	if err := os.WriteFile(filepath.Join(workDir, fileName), []byte(fileBody), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	// Real llm.Client + injected fake fantasy.LanguageModel. Going through
	// the real client means translateTools picks the typed read wrapper from
	// internal/llm/typed_read.go, fantasy advertises its schema, and the
	// dispatch path is the production one — not a test shortcut.
	client := llm.NewClient("", "", "smoke-model", "openai")
	client.InjectLanguageModelForTesting(&readReplyLanguageModel{filePath: fileName})

	cfg := config.DefaultConfig()
	cfg.WorkDir = workDir
	cfg.SessionDir = t.TempDir()

	agent := NewAgentWithStreamer(&cfg, client)
	startMsgCount := agent.MessageCount()

	eventCh := make(chan Event, 32)
	go agent.SendMessage(context.Background(), "read smoke.txt", eventCh)

	var sawToolStart bool
	var sawToolResult bool
	var sawDone bool
	var finalText string
	for ev := range eventCh {
		switch ev.Type {
		case "tool_start":
			if ev.ToolName == "read" {
				sawToolStart = true
			}
		case "tool_result":
			if ev.ToolName == "read" && strings.Contains(ev.ToolResult, fileBody) {
				sawToolResult = true
			}
		case "text":
			finalText += ev.Text
		case "done":
			sawDone = true
		case "error":
			t.Fatalf("unexpected error event: %v", ev.Error)
		}
	}

	if !sawToolStart {
		t.Fatal("expected tool_start event for read")
	}
	if !sawToolResult {
		t.Fatal("expected tool_result event whose content contains the file body")
	}
	if !sawDone {
		t.Fatal("expected done event")
	}
	if finalText != "ack" {
		t.Fatalf("expected final text %q, got %q", "ack", finalText)
	}

	// Message-history sequence: bootstrap messages + user prompt + assistant
	// (with tool call) + tool result + assistant final text. We assert the
	// tail rather than the absolute count because bootstrap composition can
	// change as system-prompt content evolves.
	msgs := agent.Messages()
	if got := len(msgs); got <= startMsgCount {
		t.Fatalf("expected message history to grow past bootstrap (%d), got %d", startMsgCount, got)
	}
	tail := msgs[startMsgCount:]
	if len(tail) < 4 {
		t.Fatalf("expected at least 4 trailing messages (user, assistant+toolcall, tool, assistant), got %d: %+v", len(tail), tail)
	}
	if tail[0].Role != "user" || !strings.Contains(tail[0].Content, "read smoke.txt") {
		t.Fatalf("expected first new message to be the user prompt, got role=%q content=%q", tail[0].Role, tail[0].Content)
	}

	var foundAssistantToolCall bool
	var foundToolResult bool
	var foundFinalAssistant bool
	for _, m := range tail[1:] {
		switch m.Role {
		case "assistant":
			if len(m.ToolCalls) == 1 && m.ToolCalls[0].Function.Name == "read" {
				foundAssistantToolCall = true
			}
			if strings.Contains(m.Content, "ack") {
				foundFinalAssistant = true
			}
		case "tool":
			if m.ToolCallID == "call_smoke_read" && strings.Contains(m.Content, fileBody) {
				foundToolResult = true
			}
		}
	}
	if !foundAssistantToolCall {
		t.Fatalf("expected an assistant message carrying a `read` tool call in history: %+v", tail)
	}
	if !foundToolResult {
		t.Fatalf("expected a tool message with the read result in history: %+v", tail)
	}
	if !foundFinalAssistant {
		t.Fatalf("expected a final assistant text message containing %q in history: %+v", "ack", tail)
	}
}
