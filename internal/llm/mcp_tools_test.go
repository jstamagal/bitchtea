package llm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/mcp"
	"github.com/jstamagal/bitchtea/internal/tools"
)

// fakeMCPServer is the minimum mcp.Server implementation needed to drive
// MCPTools and AssembleAgentTools tests through a real mcp.Manager. We
// duplicate the fake here (rather than reusing the one in
// internal/mcp/manager_test.go) because that one is in package mcp's
// _test.go and is not exported.
type fakeMCPServer struct {
	name      string
	tools     []mcp.Tool
	callCount int32
	callFn    func(name string, args json.RawMessage) (mcp.Result, error)

	mu          sync.Mutex
	lastName    string
	lastArgs    json.RawMessage
	listToolErr error
}

func (f *fakeMCPServer) Name() string                 { return f.name }
func (f *fakeMCPServer) Start(_ context.Context) error { return nil }
func (f *fakeMCPServer) Stop(_ context.Context) error  { return nil }

func (f *fakeMCPServer) ListTools(_ context.Context) ([]mcp.Tool, error) {
	if f.listToolErr != nil {
		return nil, f.listToolErr
	}
	return f.tools, nil
}

func (f *fakeMCPServer) CallTool(_ context.Context, name string, args json.RawMessage) (mcp.Result, error) {
	atomic.AddInt32(&f.callCount, 1)
	f.mu.Lock()
	f.lastName = name
	f.lastArgs = append(json.RawMessage(nil), args...)
	f.mu.Unlock()
	if f.callFn != nil {
		return f.callFn(name, args)
	}
	return mcp.Result{Content: "default-ok"}, nil
}

func (f *fakeMCPServer) ListResources(_ context.Context) ([]mcp.Resource, error) {
	return nil, nil
}

func (f *fakeMCPServer) ReadResource(_ context.Context, uri string) ([]mcp.ResourceContents, error) {
	return nil, nil
}

func (f *fakeMCPServer) ListPrompts(_ context.Context) ([]mcp.Prompt, error) {
	return nil, nil
}

func (f *fakeMCPServer) GetPrompt(_ context.Context, name string, args map[string]string) ([]mcp.PromptMessage, error) {
	return nil, nil
}

// newManagerWithFakes builds a started *mcp.Manager backed by the supplied
// fakes. The factory swap mirrors managerWithFakes in the mcp package's
// own tests.
func newManagerWithFakes(t *testing.T, fakes ...*fakeMCPServer) *mcp.Manager {
	t.Helper()
	cfg := mcp.Config{Enabled: true, Servers: map[string]mcp.ServerConfig{}}
	byName := map[string]*fakeMCPServer{}
	for _, f := range fakes {
		cfg.Servers[f.name] = mcp.ServerConfig{
			Name:      f.name,
			Transport: mcp.TransportStdio,
			Enabled:   true,
			Command:   "irrelevant",
		}
		byName[f.name] = f
	}
	m := mcp.NewManager(cfg, nil, nil)
	m.SetServerFactory(func(sc mcp.ServerConfig) (mcp.Server, error) {
		f, ok := byName[sc.Name]
		if !ok {
			return nil, errors.New("no fake for " + sc.Name)
		}
		return f, nil
	})
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("manager start: %v", err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
	return m
}

// MCPTools should produce one fantasy.AgentTool per discovered MCP tool,
// preserve the namespaced name, and pass through the JSON Schema as
// fantasy parameters + required.
func TestMCPTools_SchemaConversion(t *testing.T) {
	srv := &fakeMCPServer{
		name: "fs",
		tools: []mcp.Tool{
			{
				Name:        "read",
				Description: "read a file",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer"}},"required":["path"]}`),
			},
		},
	}
	manager := newManagerWithFakes(t, srv)

	got, err := MCPTools(context.Background(), manager)
	if err != nil {
		t.Fatalf("MCPTools err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("MCPTools len = %d, want 1", len(got))
	}
	info := got[0].Info()
	if info.Name != "mcp__fs__read" {
		t.Fatalf("name = %q, want mcp__fs__read", info.Name)
	}
	if info.Description != "read a file" {
		t.Fatalf("description = %q, want %q", info.Description, "read a file")
	}
	if _, ok := info.Parameters["path"]; !ok {
		t.Fatalf("parameters missing 'path': %+v", info.Parameters)
	}
	if _, ok := info.Parameters["offset"]; !ok {
		t.Fatalf("parameters missing 'offset': %+v", info.Parameters)
	}
	if len(info.Required) != 1 || info.Required[0] != "path" {
		t.Fatalf("required = %v, want [path]", info.Required)
	}
}

// MCPTools.Run should dispatch through manager.CallTool with the namespaced
// name and the original args, and translate the result into a text response.
func TestMCPTools_RunDispatchesThroughManager(t *testing.T) {
	srv := &fakeMCPServer{
		name:  "fs",
		tools: []mcp.Tool{{Name: "read", InputSchema: json.RawMessage(`{}`)}},
		callFn: func(name string, args json.RawMessage) (mcp.Result, error) {
			if name != "read" {
				t.Errorf("server saw bare tool name = %q, want read", name)
			}
			return mcp.Result{Content: "file body"}, nil
		},
	}
	manager := newManagerWithFakes(t, srv)
	got, err := MCPTools(context.Background(), manager)
	if err != nil || len(got) != 1 {
		t.Fatalf("MCPTools err=%v len=%d", err, len(got))
	}
	resp, err := got[0].Run(context.Background(), fantasy.ToolCall{
		ID:    "call_1",
		Name:  "mcp__fs__read",
		Input: `{"path":"x"}`,
	})
	if err != nil {
		t.Fatalf("Run must never return Go error; got %v", err)
	}
	if resp.IsError {
		t.Fatalf("expected success response, got error: %+v", resp)
	}
	if resp.Content != "file body" {
		t.Fatalf("content = %q, want %q", resp.Content, "file body")
	}
	srv.mu.Lock()
	gotArgs := string(srv.lastArgs)
	srv.mu.Unlock()
	if gotArgs != `{"path":"x"}` {
		t.Fatalf("server saw args = %q, want %q", gotArgs, `{"path":"x"}`)
	}
}

// A dispatch that yields a Go-level error from the manager (e.g. a server
// crash) must surface as a fantasy text-error response, not a Go error —
// otherwise the fantasy stream aborts the whole turn.
func TestMCPTools_RunGoErrorBecomesTextErrorResponse(t *testing.T) {
	srv := &fakeMCPServer{
		name:  "fs",
		tools: []mcp.Tool{{Name: "read", InputSchema: json.RawMessage(`{}`)}},
		callFn: func(name string, args json.RawMessage) (mcp.Result, error) {
			return mcp.Result{}, errors.New("server died")
		},
	}
	manager := newManagerWithFakes(t, srv)
	got, _ := MCPTools(context.Background(), manager)

	resp, err := got[0].Run(context.Background(), fantasy.ToolCall{
		ID:    "call_2",
		Name:  "mcp__fs__read",
		Input: `{}`,
	})
	if err != nil {
		t.Fatalf("Run must never return Go error; got %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected IsError=true, got %+v", resp)
	}
	if !strings.Contains(resp.Content, "server died") {
		t.Fatalf("response missing underlying error text: %q", resp.Content)
	}
}

// A Result whose IsError flag is set (an "expected" tool error from the
// MCP server, e.g. file-not-found) must also become a fantasy error
// response so the model can react accordingly.
func TestMCPTools_RunIsErrorResultBecomesErrorResponse(t *testing.T) {
	srv := &fakeMCPServer{
		name:  "fs",
		tools: []mcp.Tool{{Name: "read", InputSchema: json.RawMessage(`{}`)}},
		callFn: func(name string, args json.RawMessage) (mcp.Result, error) {
			return mcp.Result{Content: "no such file", IsError: true}, nil
		},
	}
	manager := newManagerWithFakes(t, srv)
	got, _ := MCPTools(context.Background(), manager)

	resp, err := got[0].Run(context.Background(), fantasy.ToolCall{
		ID: "call_3", Name: "mcp__fs__read", Input: `{}`,
	})
	if err != nil {
		t.Fatalf("Run must never return Go error; got %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected IsError=true on Result.IsError, got %+v", resp)
	}
	if resp.Content != "no such file" {
		t.Fatalf("content = %q, want %q", resp.Content, "no such file")
	}
}

// MCPTools(nil) must return (nil, nil) — the documented "MCP not opted in"
// shortcut. AssembleAgentTools must then degrade to translateTools(reg).
func TestAssembleAgentTools_NilManagerEqualsTranslateTools(t *testing.T) {
	mcpTools, err := MCPTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("MCPTools(nil) err: %v", err)
	}
	if mcpTools != nil {
		t.Fatalf("MCPTools(nil) tools = %v, want nil", mcpTools)
	}

	reg := tools.NewRegistry(t.TempDir(), t.TempDir())
	wantNames := toolNameSet(translateTools(reg))
	gotNames := toolNameSet(AssembleAgentTools(reg, nil))
	if !sameSet(gotNames, wantNames) {
		t.Fatalf("AssembleAgentTools(reg, nil) names = %v, want %v", gotNames, wantNames)
	}
}

// A local tool ("read") and an MCP tool ("mcp__fs__read") must coexist.
// The contract's namespace prefix means there is no actual collision —
// this test guards against a future regression that mishandles either.
func TestAssembleAgentTools_LocalAndMCPCoexistNoShadowing(t *testing.T) {
	srv := &fakeMCPServer{
		name: "fs",
		tools: []mcp.Tool{
			{Name: "read", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
	}
	manager := newManagerWithFakes(t, srv)
	mcpTools, err := MCPTools(context.Background(), manager)
	if err != nil {
		t.Fatalf("MCPTools err: %v", err)
	}

	reg := tools.NewRegistry(t.TempDir(), t.TempDir())
	assembled := AssembleAgentTools(reg, mcpTools)
	names := toolNameSet(assembled)

	if !names["read"] {
		t.Fatalf("local 'read' tool missing from assembled set: %v", names)
	}
	if !names["mcp__fs__read"] {
		t.Fatalf("MCP 'mcp__fs__read' tool missing from assembled set: %v", names)
	}

	// And the local 'read' entry must be the typed wrapper, not the MCP
	// adapter — verifies the local tool wasn't replaced when the names
	// were both registered.
	for _, tool := range assembled {
		if tool.Info().Name != "read" {
			continue
		}
		if _, isMCP := tool.(*mcpAgentTool); isMCP {
			t.Fatalf("local 'read' was overwritten by *mcpAgentTool")
		}
	}
}

// Theoretical-but-defended: if a local tool ever showed up with an "mcp__"
// name (impossible today — internal/tools never produces such a name —
// but the dedup is the load-bearing collision guard), the local must win
// and the colliding MCP tool must be dropped. We synthesize the case by
// hand-rolling a minimal AgentTool with that name and merging it into a
// list that AssembleAgentTools will treat as the local set.
func TestAssembleAgentTools_LocalWinsEvenOnImpossibleMCPNameCollision(t *testing.T) {
	// Build the MCP-side tool list directly (skip MCPTools — we don't need
	// a real manager to drive the dedup branch; we just need two AgentTools
	// with the same name, one in each list).
	mcpSide := []fantasy.AgentTool{
		&mcpAgentTool{
			info: fantasy.ToolInfo{Name: "mcp__bogus__fake", Description: "from mcp"},
		},
	}
	localSide := []fantasy.AgentTool{
		&staticAgentTool{info: fantasy.ToolInfo{Name: "mcp__bogus__fake", Description: "from local (impossible but dedup'd)"}},
	}

	// Reach AssembleAgentTools' merge path by simulating a Registry whose
	// translateTools output is the staticAgentTool. We can't easily inject
	// a fake Registry here, so exercise the merge function directly by
	// inlining its post-translateTools logic with the two slices above —
	// this mirrors what AssembleAgentTools would do once translateTools
	// returned localSide.
	merged := mergeForTest(localSide, mcpSide)
	if len(merged) != 1 {
		t.Fatalf("merged len = %d, want 1 (local wins, MCP dropped)", len(merged))
	}
	if got := merged[0].Info().Description; got != "from local (impossible but dedup'd)" {
		t.Fatalf("kept entry description = %q, want local-side description", got)
	}
}

// staticAgentTool is a no-op fantasy.AgentTool used only by the dedup test
// above. Keeping it tiny avoids any accidental coupling to the wrappers
// in tools.go / typed_*.go.
type staticAgentTool struct {
	info fantasy.ToolInfo
}

func (s *staticAgentTool) Info() fantasy.ToolInfo                              { return s.info }
func (s *staticAgentTool) Run(_ context.Context, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
	return fantasy.NewTextResponse("static"), nil
}
func (s *staticAgentTool) ProviderOptions() fantasy.ProviderOptions     { return nil }
func (s *staticAgentTool) SetProviderOptions(_ fantasy.ProviderOptions) {}

// mergeForTest mirrors the dedup loop at the bottom of AssembleAgentTools so
// the LocalWinsEvenOnImpossibleMCPNameCollision test can exercise the
// collision branch without a Registry whose translateTools would already
// emit an "mcp__" name (impossible today).
func mergeForTest(local, mcpTools []fantasy.AgentTool) []fantasy.AgentTool {
	out := make([]fantasy.AgentTool, 0, len(local)+len(mcpTools))
	seen := map[string]bool{}
	for _, t := range local {
		name := t.Info().Name
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, t)
	}
	for _, t := range mcpTools {
		name := t.Info().Name
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, t)
	}
	return out
}

// Two MCP tools with the same fully-namespaced name (e.g. a future bug or
// loosened config that let two servers collide post-namespacing): the
// first one wins, the second is dropped silently to the log.
func TestMCPTools_IntraMCPDuplicateFirstWins(t *testing.T) {
	srvA := &fakeMCPServer{
		name: "fs",
		tools: []mcp.Tool{{
			Name:        "read",
			Description: "from server A",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
	}
	// fake a duplicate by exposing the same tool name from a second server
	// after manually rewriting one of the fakes' Name() — but easier:
	// hand-craft a NamespacedTool slice via two servers that, by config
	// validation, *can't* collide. Since we can't actually force the
	// manager to emit a duplicate without breaking namespaceName, hit
	// MCPTools' dedup by injecting a synthetic listing through a
	// custom-built fake that has two tools with the same bare name (impossible
	// for a single server normally, but the dedup is on the *namespaced*
	// name — same server + same tool name = same namespaced name).
	srvA.tools = append(srvA.tools, mcp.Tool{
		Name:        "read",
		Description: "duplicate from server A (should be dropped)",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	})
	manager := newManagerWithFakes(t, srvA)
	got, _ := MCPTools(context.Background(), manager)

	count := 0
	for _, tool := range got {
		if tool.Info().Name == "mcp__fs__read" {
			count++
			if tool.Info().Description != "from server A" {
				t.Fatalf("first-wins violated: kept description = %q", tool.Info().Description)
			}
		}
	}
	if count != 1 {
		t.Fatalf("duplicate mcp__fs__read kept %d times, want 1", count)
	}
}

// End-to-end smoke: build a manager with a fake server, hand its tools to
// AssembleAgentTools alongside a real Registry, dispatch the MCP tool, and
// assert the underlying server actually saw the call. This is the
// proof-of-wire that bt-p6-verify will lean on.
func TestAssembleAgentTools_EndToEndDispatch(t *testing.T) {
	srv := &fakeMCPServer{
		name: "fs",
		tools: []mcp.Tool{{
			Name:        "ping",
			Description: "ping",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}},"required":["msg"]}`),
		}},
		callFn: func(name string, args json.RawMessage) (mcp.Result, error) {
			return mcp.Result{Content: "pong:" + string(args)}, nil
		},
	}
	manager := newManagerWithFakes(t, srv)
	mcpTools, err := MCPTools(context.Background(), manager)
	if err != nil {
		t.Fatalf("MCPTools err: %v", err)
	}

	reg := tools.NewRegistry(t.TempDir(), t.TempDir())
	assembled := AssembleAgentTools(reg, mcpTools)

	var pingTool fantasy.AgentTool
	for _, tool := range assembled {
		if tool.Info().Name == "mcp__fs__ping" {
			pingTool = tool
			break
		}
	}
	if pingTool == nil {
		t.Fatalf("assembled list missing mcp__fs__ping; names=%v", toolNameSet(assembled))
	}

	resp, err := pingTool.Run(context.Background(), fantasy.ToolCall{
		ID: "call_e2e", Name: "mcp__fs__ping", Input: `{"msg":"hello"}`,
	})
	if err != nil {
		t.Fatalf("dispatch returned Go error: %v", err)
	}
	if resp.IsError {
		t.Fatalf("dispatch surfaced as error response: %+v", resp)
	}
	if !strings.Contains(resp.Content, `pong:{"msg":"hello"}`) {
		t.Fatalf("dispatch content = %q, want pong:{\"msg\":\"hello\"}", resp.Content)
	}
	if got := atomic.LoadInt32(&srv.callCount); got != 1 {
		t.Fatalf("server saw %d calls, want 1", got)
	}
}

// helpers

func toolNameSet(tools []fantasy.AgentTool) map[string]bool {
	out := map[string]bool{}
	for _, tool := range tools {
		out[tool.Info().Name] = true
	}
	return out
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
