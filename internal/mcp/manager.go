package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Default lifecycle timeouts. The Manager exposes these as fields so tests
// can drop them way down without globally messing with package state, but
// the production defaults match docs/phase-6-mcp-contract.md ("each stdio
// server gets a 5s connect deadline" etc).
const (
	DefaultPerServerStartTimeout = 5 * time.Second
	DefaultManagerStartTimeout   = 10 * time.Second
	DefaultStopTimeout           = 5 * time.Second
)

// NamespacedTool pairs a server-relative Tool with the prefixed name the
// LLM sees. Per the contract: mcp__<server>__<tool>. The double-underscore
// separator matches Claude Code's convention so prompts written for that
// host stay portable.
type NamespacedTool struct {
	Server string
	Tool   Tool
	// Name is the fully-qualified name the LLM tool registry should use.
	// Always equal to "mcp__" + Server + "__" + Tool.Name.
	Name string
}

// healthState tracks per-server lifecycle for the Manager. We don't need
// a full state machine — Manager only ever cares whether a server is
// "available for ListAllTools / CallTool" (running and not unhealthy).
type healthState int

const (
	stateRunning healthState = iota
	stateUnhealthy
)

type entry struct {
	server   Server
	state    healthState
	reason   string // populated when state == stateUnhealthy
}

// Manager owns the lifecycle of a set of MCP Servers, the audit/auth
// hooks they all share, and the namespacing of tool calls.
//
// A zero-value Manager is not usable; construct via NewManager.
//
// Manager is safe for concurrent use after Start returns.
type Manager struct {
	cfg   Config
	auth  Authorizer
	audit AuditHook

	// Per-server start deadline. Defaults to DefaultPerServerStartTimeout.
	PerServerStartTimeout time.Duration
	// Aggregate cap on Start. Servers race against this AND their per-
	// server timeout; whichever fires first marks them unhealthy.
	ManagerStartTimeout time.Duration
	// Aggregate cap on Stop.
	StopTimeout time.Duration

	// newServer is overridable by tests so they can swap in fakeServer
	// for the otherwise-config-driven NewServer.
	newServer func(ServerConfig) (Server, error)

	mu      sync.RWMutex
	entries map[string]*entry
}

// NewManager constructs a Manager from a parsed Config. nil auth or audit
// fall back to the package defaults so the manager never has to nil-check
// its hooks at dispatch time. Pass mcp.DefaultAuthorizer() / DefaultAuditHook()
// explicitly if you want to be loud about the choice.
func NewManager(cfg Config, auth Authorizer, audit AuditHook) *Manager {
	if auth == nil {
		auth = DefaultAuthorizer()
	}
	if audit == nil {
		audit = DefaultAuditHook()
	}
	return &Manager{
		cfg:                   cfg,
		auth:                  auth,
		audit:                 audit,
		PerServerStartTimeout: DefaultPerServerStartTimeout,
		ManagerStartTimeout:   DefaultManagerStartTimeout,
		StopTimeout:           DefaultStopTimeout,
		newServer:             NewServer,
		entries:               map[string]*entry{},
	}
}

// SetServerFactory swaps the constructor used to build Server impls. It
// exists so tests can inject fakeServer instances without spinning up
// real subprocesses. Production callers never need this.
func (m *Manager) SetServerFactory(f func(ServerConfig) (Server, error)) {
	m.newServer = f
}

// Start brings up every server in cfg.Servers in parallel. A failure to
// start any one server is logged into that server's entry as unhealthy
// and the manager continues — the contract requires that "a misbehaving
// MCP server must never take down the agent loop".
//
// Per-server start runs under PerServerStartTimeout. The aggregate Start
// returns (with the slow servers marked unhealthy) once ManagerStartTimeout
// expires, even if some servers are still trying to come up.
//
// If cfg.Enabled is false, Start is a no-op and returns nil — Manager
// trusts the disabled-by-default gating that LoadConfig already did.
func (m *Manager) Start(ctx context.Context) error {
	if !m.cfg.Enabled {
		return nil
	}
	startCtx, cancel := context.WithTimeout(ctx, m.ManagerStartTimeout)
	defer cancel()

	var wg sync.WaitGroup
	for _, sc := range m.cfg.Servers {
		sc := sc // capture
		srv, err := m.newServer(sc)
		if err != nil {
			m.markUnhealthy(sc.Name, nil, err.Error())
			continue
		}
		// Pre-record the entry so a startup that runs past the manager
		// timeout still has somewhere to land.
		m.mu.Lock()
		m.entries[sc.Name] = &entry{server: srv, state: stateUnhealthy, reason: "starting"}
		m.mu.Unlock()

		wg.Add(1)
		go func() {
			defer wg.Done()
			perCtx, perCancel := context.WithTimeout(startCtx, m.PerServerStartTimeout)
			defer perCancel()
			if err := srv.Start(perCtx); err != nil {
				m.markUnhealthy(sc.Name, srv, fmt.Sprintf("start: %v", err))
				return
			}
			m.markRunning(sc.Name, srv)
		}()
	}

	// Wait for either every server to settle or the manager-level
	// deadline to fire. Whichever wins, we return — slow servers are
	// already pre-marked unhealthy and will flip to running asynchronously
	// if they ever finish.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-startCtx.Done():
	}
	return nil
}

func (m *Manager) markRunning(name string, srv Server) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[name] = &entry{server: srv, state: stateRunning}
}

func (m *Manager) markUnhealthy(name string, srv Server, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[name] = &entry{server: srv, state: stateUnhealthy, reason: reason}
}

// Stop tears down every started server in parallel under StopTimeout.
// Servers that error on Stop have their error joined into the returned
// error; Stop still attempts every other server. Calling Stop on a
// Manager that never Started is a no-op.
func (m *Manager) Stop(ctx context.Context) error {
	stopCtx, cancel := context.WithTimeout(ctx, m.StopTimeout)
	defer cancel()

	m.mu.RLock()
	servers := make([]Server, 0, len(m.entries))
	for _, e := range m.entries {
		if e.server != nil {
			servers = append(servers, e.server)
		}
	}
	m.mu.RUnlock()

	var (
		errMu sync.Mutex
		errs  []error
		wg    sync.WaitGroup
	)
	for _, srv := range servers {
		srv := srv
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := srv.Stop(stopCtx); err != nil {
				errMu.Lock()
				errs = append(errs, fmt.Errorf("mcp: server %q stop: %w", srv.Name(), err))
				errMu.Unlock()
			}
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-stopCtx.Done():
		errMu.Lock()
		errs = append(errs, fmt.Errorf("mcp: stop timeout: %w", stopCtx.Err()))
		errMu.Unlock()
	}

	m.mu.Lock()
	m.entries = map[string]*entry{}
	m.mu.Unlock()

	return errors.Join(errs...)
}

// Servers returns the live, running Server set. Unhealthy/failed servers
// are excluded — callers iterating Servers can assume each result is
// safe to dispatch to.
func (m *Manager) Servers() []Server {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Server, 0, len(m.entries))
	for _, e := range m.entries {
		if e.state == stateRunning && e.server != nil {
			out = append(out, e.server)
		}
	}
	return out
}

// Unhealthy returns name -> reason for every server that failed to start
// or otherwise transitioned out of the running state. Useful for the
// transcript "mcp: server <name> failed to start: <reason>" line.
func (m *Manager) Unhealthy() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := map[string]string{}
	for name, e := range m.entries {
		if e.state == stateUnhealthy {
			out[name] = e.reason
		}
	}
	return out
}

// ListAllTools collects tools from every running server, prefixing each
// with mcp__<server>__<tool>. Errors from individual servers are joined
// but do NOT prevent other servers' tools from being returned —
// best-effort aggregation matches the contract's "schema error drops
// that one tool" stance.
func (m *Manager) ListAllTools(ctx context.Context) ([]NamespacedTool, error) {
	servers := m.Servers()
	var (
		out  []NamespacedTool
		errs []error
	)
	for _, srv := range servers {
		tools, err := srv.ListTools(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("mcp: server %q list_tools: %w", srv.Name(), err))
			continue
		}
		for _, t := range tools {
			if !validToolName(t.Name) {
				// Per contract: a server-reported tool whose name
				// contains characters outside [A-Za-z0-9_] is logged
				// and skipped.
				errs = append(errs, fmt.Errorf("mcp: server %q tool %q: invalid name, skipping", srv.Name(), t.Name))
				continue
			}
			out = append(out, NamespacedTool{
				Server: srv.Name(),
				Tool:   t,
				Name:   namespaceName(srv.Name(), t.Name),
			})
		}
	}
	return out, errors.Join(errs...)
}

// CallTool dispatches a namespaced tool call: parses the mcp__<server>__<tool>
// name, runs it through the Authorizer, emits an OnToolStart, dispatches
// to the underlying Server, and emits OnToolEnd.
//
// A non-nil error from Authorize is returned to the caller untouched and
// no dispatch happens. Errors from the dispatch itself are wrapped and
// returned alongside whatever partial Result the server produced (the
// caller may still want to surface IsError content to the model).
func (m *Manager) CallTool(ctx context.Context, namespacedName string, args json.RawMessage) (Result, error) {
	server, tool, ok := SplitNamespacedName(namespacedName)
	if !ok {
		return Result{}, fmt.Errorf("mcp: %q is not a namespaced MCP tool name", namespacedName)
	}
	srv := m.lookupServer(server)
	if srv == nil {
		return Result{}, fmt.Errorf("mcp: server %q not running", server)
	}

	if err := m.auth.Authorize(ctx, server, tool, args); err != nil {
		return Result{}, err
	}

	start := time.Now()
	m.audit.OnToolStart(ctx, ToolCallStart{
		Server: server,
		Tool:   tool,
		Args:   args,
		When:   start,
	})

	res, err := srv.CallTool(ctx, tool, args)

	end := ToolCallEnd{
		Server:     server,
		Tool:       tool,
		Result:     nil,
		Err:        err,
		DurationMS: time.Since(start).Milliseconds(),
		When:       time.Now(),
	}
	if err == nil {
		if b, marshalErr := json.Marshal(res); marshalErr == nil {
			end.Result = b
		}
	}
	m.audit.OnToolEnd(ctx, end)

	return res, err
}

func (m *Manager) lookupServer(name string) Server {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.entries[name]
	if !ok || e.state != stateRunning {
		return nil
	}
	return e.server
}

// namespacePrefix is the fixed prefix every MCP-routed tool name carries,
// per docs/phase-6-mcp-contract.md "Tool naming". Built-in tools never
// start with this string, which is what makes the prefix a safe routing
// key in the tool registry.
const namespacePrefix = "mcp__"

func namespaceName(server, tool string) string {
	return namespacePrefix + server + "__" + tool
}

// SplitNamespacedName reverses namespaceName. Returns ("", "", false) if
// the input is not a valid mcp__<server>__<tool> string. The split is on
// the first "__" after the prefix so server names with no underscores
// are unambiguous; server names containing underscores would round-trip
// wrong but config validation bans them via the [a-z0-9_-]+ rule.
func SplitNamespacedName(name string) (server, tool string, ok bool) {
	if !strings.HasPrefix(name, namespacePrefix) {
		return "", "", false
	}
	rest := name[len(namespacePrefix):]
	idx := strings.Index(rest, "__")
	if idx <= 0 || idx == len(rest)-2 {
		return "", "", false
	}
	return rest[:idx], rest[idx+2:], true
}

// validToolName enforces the contract's "characters outside [A-Za-z0-9_]"
// rule. Empty names are also rejected.
func validToolName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return false
		}
	}
	return true
}
