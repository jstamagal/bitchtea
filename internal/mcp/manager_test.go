package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeServer is the in-test Server implementation. It deliberately does
// NOT spin up a real subprocess or HTTP listener — manager-level tests
// only care that lifecycle/dispatch hooks fire in the right order.
//
// Each behavior knob is a field rather than a constructor option so tests
// stay readable as struct literals.
type fakeServer struct {
	name string
	// Tools returned from ListTools.
	tools     []Tool
	resources []Resource
	prompts   []Prompt
	// resourceData maps URI -> contents for ReadResource.
	resourceData map[string][]ResourceContents
	// promptData maps name -> messages for GetPrompt.
	promptData map[string][]PromptMessage

	// hooks
	startErr  error
	startHang bool          // if set, Start blocks until ctx is done
	stopErr   error
	listErr   error // if set, ListTools returns this error
	callFn    func(name string, args json.RawMessage) (Result, error)

	mu          sync.Mutex
	startCalls  int32
	stopCalls   int32
	callCount   int32
	listCalls   int32 // counts ListTools invocations — used by cache tests
	startedAt   time.Time
}

func (f *fakeServer) Name() string { return f.name }

func (f *fakeServer) Start(ctx context.Context) error {
	atomic.AddInt32(&f.startCalls, 1)
	if f.startHang {
		<-ctx.Done()
		return ctx.Err()
	}
	if f.startErr != nil {
		return f.startErr
	}
	f.mu.Lock()
	f.startedAt = time.Now()
	f.mu.Unlock()
	return nil
}

func (f *fakeServer) Stop(_ context.Context) error {
	atomic.AddInt32(&f.stopCalls, 1)
	return f.stopErr
}

func (f *fakeServer) ListTools(_ context.Context) ([]Tool, error) {
	atomic.AddInt32(&f.listCalls, 1)
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.tools, nil
}

func (f *fakeServer) CallTool(_ context.Context, name string, args json.RawMessage) (Result, error) {
	atomic.AddInt32(&f.callCount, 1)
	if f.callFn != nil {
		return f.callFn(name, args)
	}
	return Result{Content: "ok"}, nil
}

func (f *fakeServer) ListResources(_ context.Context) ([]Resource, error) {
	return f.resources, nil
}

func (f *fakeServer) ReadResource(_ context.Context, uri string) ([]ResourceContents, error) {
	if f.resourceData != nil {
		if contents, ok := f.resourceData[uri]; ok {
			return contents, nil
		}
	}
	return nil, fmt.Errorf("resource not found: %s", uri)
}

func (f *fakeServer) ListPrompts(_ context.Context) ([]Prompt, error) {
	return f.prompts, nil
}

func (f *fakeServer) GetPrompt(_ context.Context, name string, args map[string]string) ([]PromptMessage, error) {
	if f.promptData != nil {
		if msgs, ok := f.promptData[name]; ok {
			return msgs, nil
		}
	}
	return nil, fmt.Errorf("prompt not found: %s", name)
}

// recordingAuthorizer records every Authorize call and lets a test
// reject calls by name. Used to assert authorize-before-dispatch order.
type recordingAuthorizer struct {
	mu      sync.Mutex
	calls   []string
	denyTool string
}

func (r *recordingAuthorizer) Authorize(_ context.Context, server, tool string, _ json.RawMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, server+":"+tool)
	if tool == r.denyTool {
		return fmt.Errorf("denied: %s", tool)
	}
	return nil
}

// recordingAudit records OnToolStart/OnToolEnd events with a single
// channel-of-strings so tests can assert ordering relative to other
// hooks (e.g. that Authorize ran before OnToolStart).
type recordingAudit struct {
	events chan string
}

func newRecordingAudit() *recordingAudit {
	return &recordingAudit{events: make(chan string, 8)}
}

func (r *recordingAudit) OnToolStart(_ context.Context, ev ToolCallStart) {
	r.events <- "start:" + ev.Server + ":" + ev.Tool
}

func (r *recordingAudit) OnToolEnd(_ context.Context, ev ToolCallEnd) {
	r.events <- "end:" + ev.Server + ":" + ev.Tool
}

// managerWithFakes builds a Manager wired to the supplied fakeServers and
// returns the same fakes back keyed by name so the test can assert on
// them after Start.
func managerWithFakes(t *testing.T, auth Authorizer, audit AuditHook, fakes ...*fakeServer) (*Manager, map[string]*fakeServer) {
	t.Helper()
	cfg := Config{Enabled: true, Servers: map[string]ServerConfig{}}
	byName := map[string]*fakeServer{}
	for _, f := range fakes {
		cfg.Servers[f.name] = ServerConfig{
			Name:      f.name,
			Transport: TransportStdio,
			Enabled:   true,
			Command:   "irrelevant", // never reached, factory is overridden
		}
		byName[f.name] = f
	}
	m := NewManager(cfg, auth, audit)
	m.SetServerFactory(func(sc ServerConfig) (Server, error) {
		f, ok := byName[sc.Name]
		if !ok {
			return nil, fmt.Errorf("no fake for %s", sc.Name)
		}
		return f, nil
	})
	// Tighten timeouts so the hang test finishes quickly.
	m.PerServerStartTimeout = 50 * time.Millisecond
	m.ManagerStartTimeout = 200 * time.Millisecond
	m.StopTimeout = 100 * time.Millisecond
	return m, byName
}

// Two healthy fakes both come up running.
func TestManager_Start_TwoHealthyServers(t *testing.T) {
	a := &fakeServer{name: "a", tools: []Tool{{Name: "alpha"}}}
	b := &fakeServer{name: "b", tools: []Tool{{Name: "beta"}}}
	m, _ := managerWithFakes(t, nil, nil, a, b)

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	got := names(m.Servers())
	if want := []string{"a", "b"}; !equalSet(got, want) {
		t.Fatalf("Servers()=%v want %v", got, want)
	}
	if u := m.Unhealthy(); len(u) != 0 {
		t.Fatalf("Unhealthy()=%v want empty", u)
	}
}

// One fake errors on Start: manager continues, the failing server is
// marked unhealthy and excluded from Servers/ListAllTools.
func TestManager_Start_OneFails_OthersContinue(t *testing.T) {
	good := &fakeServer{name: "good", tools: []Tool{{Name: "ok"}}}
	bad := &fakeServer{name: "bad", startErr: errors.New("boom")}
	m, _ := managerWithFakes(t, nil, nil, good, bad)

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got, want := names(m.Servers()), []string{"good"}; !equalSet(got, want) {
		t.Fatalf("Servers()=%v want %v", got, want)
	}
	u := m.Unhealthy()
	if _, ok := u["bad"]; !ok {
		t.Fatalf("expected bad in Unhealthy(), got %v", u)
	}
	tools, _ := m.ListAllTools(context.Background())
	if len(tools) != 1 || tools[0].Server != "good" {
		t.Fatalf("ListAllTools()=%v want one tool from good", tools)
	}
}

// Hanging Start must not block the manager past the per-server timeout.
func TestManager_Start_TimeoutMarksUnhealthy(t *testing.T) {
	hang := &fakeServer{name: "hang", startHang: true}
	good := &fakeServer{name: "good", tools: []Tool{{Name: "ok"}}}
	m, _ := managerWithFakes(t, nil, nil, hang, good)

	t0 := time.Now()
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	elapsed := time.Since(t0)
	// Manager-level timeout is 200ms in the helper; we should be under
	// that by a comfortable margin.
	if elapsed > m.ManagerStartTimeout+100*time.Millisecond {
		t.Fatalf("Start took %v, expected <= %v", elapsed, m.ManagerStartTimeout)
	}
	if got, want := names(m.Servers()), []string{"good"}; !equalSet(got, want) {
		t.Fatalf("Servers()=%v want %v (hang should be unhealthy)", got, want)
	}
	if _, ok := m.Unhealthy()["hang"]; !ok {
		t.Fatalf("expected hang in Unhealthy()")
	}
}

// Stop calls Stop() on every server, in parallel, even when one errors.
func TestManager_Stop_AllInParallel(t *testing.T) {
	a := &fakeServer{name: "a"}
	b := &fakeServer{name: "b", stopErr: errors.New("boom")}
	c := &fakeServer{name: "c"}
	m, _ := managerWithFakes(t, nil, nil, a, b, c)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	err := m.Stop(context.Background())
	if err == nil {
		t.Fatalf("expected error from Stop (b should fail)")
	}
	for _, f := range []*fakeServer{a, b, c} {
		if got := atomic.LoadInt32(&f.stopCalls); got != 1 {
			t.Errorf("server %s: stopCalls=%d want 1", f.name, got)
		}
	}
	if len(m.Servers()) != 0 {
		t.Fatalf("Servers() should be empty after Stop, got %v", names(m.Servers()))
	}
}

// ListAllTools returns names prefixed with mcp__<server>__<tool>.
func TestManager_ListAllTools_Namespaced(t *testing.T) {
	a := &fakeServer{name: "fs", tools: []Tool{{Name: "read"}, {Name: "write"}}}
	b := &fakeServer{name: "issues", tools: []Tool{{Name: "create"}}}
	m, _ := managerWithFakes(t, nil, nil, a, b)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	tools, err := m.ListAllTools(context.Background())
	if err != nil {
		t.Fatalf("ListAllTools: %v", err)
	}
	got := []string{}
	for _, nt := range tools {
		got = append(got, nt.Name)
	}
	want := []string{"mcp__fs__read", "mcp__fs__write", "mcp__issues__create"}
	if !equalSet(got, want) {
		t.Fatalf("ListAllTools names=%v want %v", got, want)
	}
}

// ListAllTools skips tools whose names contain disallowed chars rather
// than failing the whole call.
func TestManager_ListAllTools_SkipsInvalidNames(t *testing.T) {
	bad := &fakeServer{name: "fs", tools: []Tool{
		{Name: "good"},
		{Name: "bad-name"}, // hyphen disallowed
		{Name: ""},
	}}
	m, _ := managerWithFakes(t, nil, nil, bad)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	tools, err := m.ListAllTools(context.Background())
	if err == nil {
		t.Fatalf("expected aggregated error for skipped tools")
	}
	if len(tools) != 1 || tools[0].Tool.Name != "good" {
		t.Fatalf("got tools=%v, want only [good]", tools)
	}
}

// CallTool: Authorize runs first, then OnToolStart, then dispatch, then
// OnToolEnd. We assert the order with a single events channel.
func TestManager_CallTool_OrderAuthorizeStartDispatchEnd(t *testing.T) {
	auth := &recordingAuthorizer{}
	audit := newRecordingAudit()
	dispatched := make(chan struct{}, 1)
	srv := &fakeServer{name: "fs", tools: []Tool{{Name: "read"}}, callFn: func(name string, args json.RawMessage) (Result, error) {
		// Authorize must have already recorded a call when we land here.
		auth.mu.Lock()
		got := len(auth.calls)
		auth.mu.Unlock()
		if got == 0 {
			t.Errorf("dispatch ran before Authorize")
		}
		dispatched <- struct{}{}
		return Result{Content: "hi"}, nil
	}}
	m, _ := managerWithFakes(t, auth, audit, srv)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	res, err := m.CallTool(context.Background(), "mcp__fs__read", json.RawMessage(`{"path":"x"}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.Content != "hi" {
		t.Fatalf("Result.Content=%q want %q", res.Content, "hi")
	}
	select {
	case <-dispatched:
	default:
		t.Fatalf("dispatch did not run")
	}
	if got, want := auth.calls, []string{"fs:read"}; !equalSlice(got, want) {
		t.Fatalf("Authorize calls=%v want %v", got, want)
	}
	got := []string{<-audit.events, <-audit.events}
	want := []string{"start:fs:read", "end:fs:read"}
	if !equalSlice(got, want) {
		t.Fatalf("audit events=%v want %v", got, want)
	}
}

// CallTool: Authorize denial returns the error, no dispatch happens, and
// OnToolStart/OnToolEnd are NOT fired (auth is the gate that decides
// whether the call is even visible to audit).
func TestManager_CallTool_DenialBlocksDispatch(t *testing.T) {
	auth := &recordingAuthorizer{denyTool: "read"}
	audit := newRecordingAudit()
	srv := &fakeServer{name: "fs", tools: []Tool{{Name: "read"}}}
	m, _ := managerWithFakes(t, auth, audit, srv)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	_, err := m.CallTool(context.Background(), "mcp__fs__read", json.RawMessage(`{}`))
	if err == nil {
		t.Fatalf("expected denial error")
	}
	if c := atomic.LoadInt32(&srv.callCount); c != 0 {
		t.Fatalf("dispatch ran despite denial: callCount=%d", c)
	}
	select {
	case ev := <-audit.events:
		t.Fatalf("unexpected audit event after denial: %q", ev)
	default:
	}
}

// CallTool: unknown server name returns an error, no panic.
func TestManager_CallTool_UnknownServer(t *testing.T) {
	m, _ := managerWithFakes(t, nil, nil, &fakeServer{name: "fs", tools: []Tool{{Name: "read"}}})
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_, err := m.CallTool(context.Background(), "mcp__missing__do", json.RawMessage(`{}`))
	if err == nil {
		t.Fatalf("expected error for unknown server")
	}
}

// CallTool: malformed namespaced name (missing prefix) returns an error.
func TestManager_CallTool_BadName(t *testing.T) {
	m, _ := managerWithFakes(t, nil, nil)
	_, err := m.CallTool(context.Background(), "not_namespaced", json.RawMessage(`{}`))
	if err == nil {
		t.Fatalf("expected error for non-namespaced name")
	}
}

// Disabled config: Start is a no-op and Servers is empty.
func TestManager_DisabledConfig(t *testing.T) {
	m := NewManager(Disabled(), nil, nil)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(m.Servers()) != 0 {
		t.Fatalf("Servers should be empty when disabled")
	}
}

// SplitNamespacedName behavior — covers the round-trip that CallTool
// relies on plus the rejection cases.
func TestSplitNamespacedName(t *testing.T) {
	cases := []struct {
		in, server, tool string
		ok               bool
	}{
		{"mcp__fs__read", "fs", "read", true},
		{"mcp__fs__do_thing", "fs", "do_thing", true},
		{"read", "", "", false},
		{"mcp__fs", "", "", false},
		{"mcp____tool", "", "tool", false}, // empty server
		{"mcp__fs__", "", "", false},        // empty tool
	}
	for _, c := range cases {
		s, tl, ok := SplitNamespacedName(c.in)
		if ok != c.ok || (ok && (s != c.server || tl != c.tool)) {
			t.Errorf("Split(%q)=(%q,%q,%v) want (%q,%q,%v)", c.in, s, tl, ok, c.server, c.tool, c.ok)
		}
	}
}

// ListAllResources collects resources from every running server.
func TestManager_ListAllResources(t *testing.T) {
	a := &fakeServer{
		name: "fs",
		resources: []Resource{
			{URI: "file:///a.txt", Name: "a.txt", MIMEType: "text/plain"},
			{URI: "file:///b.txt", Name: "b.txt", MIMEType: "text/plain"},
		},
	}
	b := &fakeServer{
		name: "db",
		resources: []Resource{
			{URI: "db://schema", Name: "schema", MIMEType: "application/json"},
		},
	}
	m, _ := managerWithFakes(t, nil, nil, a, b)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	res, err := m.ListAllResources(context.Background())
	if err != nil {
		t.Fatalf("ListAllResources: %v", err)
	}
	if len(res["fs"]) != 2 {
		t.Fatalf("fs resources: got %d, want 2", len(res["fs"]))
	}
	if len(res["db"]) != 1 {
		t.Fatalf("db resources: got %d, want 1", len(res["db"]))
	}
}

// ReadResource returns contents when under the size cap.
func TestManager_ReadResource(t *testing.T) {
	srv := &fakeServer{
		name: "fs",
		resourceData: map[string][]ResourceContents{
			"file:///data.txt": {{URI: "file:///data.txt", Text: "hello", MIMEType: "text/plain"}},
		},
	}
	m, _ := managerWithFakes(t, nil, nil, srv)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	contents, err := m.ReadResource(context.Background(), "fs", "file:///data.txt")
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(contents) != 1 || contents[0].Text != "hello" {
		t.Fatalf("contents: %+v", contents)
	}
}

// ReadResource rejects content exceeding MaxResourceBytes.
func TestManager_ReadResource_ExceedsLimit(t *testing.T) {
	big := make([]byte, DefaultMaxResourceBytes+1)
	for i := range big {
		big[i] = 'x'
	}
	srv := &fakeServer{
		name: "fs",
		resourceData: map[string][]ResourceContents{
			"file:///big.txt": {{URI: "file:///big.txt", Text: string(big), MIMEType: "text/plain"}},
		},
	}
	m, _ := managerWithFakes(t, nil, nil, srv)
	m.MaxResourceBytes = 100
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	_, err := m.ReadResource(context.Background(), "fs", "file:///big.txt")
	if err == nil {
		t.Fatal("expected error for oversized resource")
	}
}

// ReadResource from an unknown server returns an error.
func TestManager_ReadResource_UnknownServer(t *testing.T) {
	m, _ := managerWithFakes(t, nil, nil)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_, err := m.ReadResource(context.Background(), "missing", "file:///x")
	if err == nil {
		t.Fatal("expected error for unknown server")
	}
}

// ListAllPrompts collects prompts from every running server.
func TestManager_ListAllPrompts(t *testing.T) {
	a := &fakeServer{
		name: "fs",
		prompts: []Prompt{
			{Name: "review", Description: "Code review", Arguments: []PromptArgument{{Name: "file", Required: true}}},
		},
	}
	b := &fakeServer{
		name: "db",
		prompts: []Prompt{
			{Name: "query", Description: "Run query"},
			{Name: "schema", Description: "Show schema"},
		},
	}
	m, _ := managerWithFakes(t, nil, nil, a, b)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	res, err := m.ListAllPrompts(context.Background())
	if err != nil {
		t.Fatalf("ListAllPrompts: %v", err)
	}
	if len(res["fs"]) != 1 {
		t.Fatalf("fs prompts: got %d, want 1", len(res["fs"]))
	}
	if len(res["db"]) != 2 {
		t.Fatalf("db prompts: got %d, want 2", len(res["db"]))
	}
}

// GetPrompt fetches a prompt from a running server.
func TestManager_GetPrompt(t *testing.T) {
	srv := &fakeServer{
		name: "fs",
		promptData: map[string][]PromptMessage{
			"review": {{Role: "user", Content: "Review this code"}},
		},
	}
	m, _ := managerWithFakes(t, nil, nil, srv)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	msgs, err := m.GetPrompt(context.Background(), "fs", "review", map[string]string{"file": "main.go"})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "Review this code" {
		t.Fatalf("messages: %+v", msgs)
	}
}

// GetPrompt from an unknown server returns an error.
func TestManager_GetPrompt_UnknownServer(t *testing.T) {
	m, _ := managerWithFakes(t, nil, nil)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_, err := m.GetPrompt(context.Background(), "missing", "review", nil)
	if err == nil {
		t.Fatal("expected error for unknown server")
	}
}

// ListAllResources from unhealthy servers is excluded.
func TestManager_ListAllResources_ExcludesUnhealthy(t *testing.T) {
	good := &fakeServer{name: "good", resources: []Resource{{URI: "x", Name: "x"}}}
	bad := &fakeServer{name: "bad", startErr: errors.New("nope")}
	m, _ := managerWithFakes(t, nil, nil, good, bad)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	res, err := m.ListAllResources(context.Background())
	if err != nil {
		t.Fatalf("ListAllResources: %v", err)
	}
	if _, ok := res["bad"]; ok {
		t.Fatal("expected bad server excluded from resources")
	}
	if len(res["good"]) != 1 {
		t.Fatalf("good resources: got %d, want 1", len(res["good"]))
	}
}

// --- helpers ---

func names(servers []Server) []string {
	out := make([]string, 0, len(servers))
	for _, s := range servers {
		out = append(out, s.Name())
	}
	return out
}

func equalSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ac := append([]string(nil), a...)
	bc := append([]string(nil), b...)
	sort.Strings(ac)
	sort.Strings(bc)
	for i := range ac {
		if ac[i] != bc[i] {
			return false
		}
	}
	return true
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- ListAllTools cache (MED #6 / bt-45z) -----------------------------------
//
// The cache is a per-Manager memoization of ListAllTools' result, valid for
// ToolsCacheTTL. Tests pin: cache hit avoids re-listing servers; TTL expiry
// triggers a rebuild; partial-failure responses are NOT cached so a flaky
// server doesn't poison the cache; InvalidateToolsCache forces a rebuild;
// ToolsCacheTTL=0 disables caching.

func TestListAllTools_CachesWithinTTL(t *testing.T) {
	srv := &fakeServer{name: "s1", tools: []Tool{{Name: "t1"}}}
	mgr, _ := managerWithFakes(t, nil, nil, srv)
	mgr.ToolsCacheTTL = 60 * time.Second
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop(context.Background()) })

	for i := 0; i < 5; i++ {
		got, err := mgr.ListAllTools(context.Background())
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if len(got) != 1 || got[0].Name != "mcp__s1__t1" {
			t.Fatalf("call %d: unexpected tools %+v", i, got)
		}
	}
	if got := atomic.LoadInt32(&srv.listCalls); got != 1 {
		t.Fatalf("expected 1 ListTools call (cached), got %d", got)
	}
}

func TestListAllTools_RebuildAfterTTLExpires(t *testing.T) {
	srv := &fakeServer{name: "s1", tools: []Tool{{Name: "t1"}}}
	mgr, _ := managerWithFakes(t, nil, nil, srv)
	mgr.ToolsCacheTTL = 50 * time.Millisecond
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop(context.Background()) })

	if _, err := mgr.ListAllTools(context.Background()); err != nil {
		t.Fatalf("first call: %v", err)
	}
	time.Sleep(60 * time.Millisecond) // outside TTL
	if _, err := mgr.ListAllTools(context.Background()); err != nil {
		t.Fatalf("post-expiry call: %v", err)
	}
	if got := atomic.LoadInt32(&srv.listCalls); got != 2 {
		t.Fatalf("expected 2 ListTools calls (rebuild after TTL), got %d", got)
	}
}

func TestListAllTools_DoesNotCacheErrors(t *testing.T) {
	srv := &fakeServer{name: "s1", listErr: fmt.Errorf("upstream gone")}
	mgr, _ := managerWithFakes(t, nil, nil, srv)
	mgr.ToolsCacheTTL = 60 * time.Second
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop(context.Background()) })

	for i := 0; i < 3; i++ {
		_, err := mgr.ListAllTools(context.Background())
		if err == nil {
			t.Fatalf("call %d: expected error", i)
		}
	}
	// Each call must hit the server because the error path doesn't cache —
	// otherwise a flaky server would lock out future success for the TTL.
	if got := atomic.LoadInt32(&srv.listCalls); got != 3 {
		t.Fatalf("expected 3 ListTools calls (errors not cached), got %d", got)
	}
}

func TestInvalidateToolsCache_ForcesRebuild(t *testing.T) {
	srv := &fakeServer{name: "s1", tools: []Tool{{Name: "t1"}}}
	mgr, _ := managerWithFakes(t, nil, nil, srv)
	mgr.ToolsCacheTTL = 60 * time.Second
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop(context.Background()) })

	if _, err := mgr.ListAllTools(context.Background()); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := mgr.ListAllTools(context.Background()); err != nil {
		t.Fatalf("second (cached): %v", err)
	}
	if got := atomic.LoadInt32(&srv.listCalls); got != 1 {
		t.Fatalf("setup expected 1 call, got %d", got)
	}

	mgr.InvalidateToolsCache()

	if _, err := mgr.ListAllTools(context.Background()); err != nil {
		t.Fatalf("post-invalidate: %v", err)
	}
	if got := atomic.LoadInt32(&srv.listCalls); got != 2 {
		t.Fatalf("expected 2 ListTools calls (rebuild after invalidate), got %d", got)
	}
}

func TestListAllTools_TTLZeroDisablesCaching(t *testing.T) {
	srv := &fakeServer{name: "s1", tools: []Tool{{Name: "t1"}}}
	mgr, _ := managerWithFakes(t, nil, nil, srv)
	mgr.ToolsCacheTTL = 0 // explicit disable
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop(context.Background()) })

	for i := 0; i < 4; i++ {
		if _, err := mgr.ListAllTools(context.Background()); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&srv.listCalls); got != 4 {
		t.Fatalf("expected 4 ListTools calls (caching disabled), got %d", got)
	}
}
