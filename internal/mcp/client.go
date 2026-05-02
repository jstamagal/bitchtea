package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sync"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool is the bitchtea-internal view of a single MCP tool, decoupled from
// the SDK's protocol type so callers above this package don't have to
// import the SDK directly.
//
// The shape mirrors what the agent boundary needs: a name, a human
// description, and a JSON-Schema input definition the LLM can be told
// about. The schema is left as json.RawMessage on purpose — we pass the
// server's schema through as-is per docs/phase-6-mcp-contract.md.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// Result is the bitchtea-internal view of a tool-call response. It is the
// minimum the agent boundary needs to render a tool result to the model:
// the raw text content (concatenated from CallToolResult.Content), an
// optional structured payload, and an IsError flag.
//
// Implementations of Server are responsible for populating Content with
// something the model can read even when IsError is true.
type Result struct {
	Content           string          `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
}

// Server is the lifecycle/dispatch interface the Manager talks to. There
// is one implementation per transport (stdio, http) plus a fakeServer in
// tests.
//
// Method contracts:
//   - Start: connect to the underlying transport and complete the MCP
//     initialize handshake. Must respect ctx cancellation. Calling Start
//     twice is an error.
//   - Stop:  tear down the transport. Idempotent; calling Stop on a
//     never-started or already-stopped server returns nil.
//   - ListTools: returns the live tool catalog. Must not be called
//     before Start succeeds.
//   - CallTool: dispatches a single tool call. The args payload is the
//     resolved JSON object the model produced (after Authorize ran).
//   - Name: stable identifier from ServerConfig.Name. Used to build the
//     mcp__<server>__<tool> namespacing prefix.
type Server interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	ListTools(ctx context.Context) ([]Tool, error)
	CallTool(ctx context.Context, name string, args json.RawMessage) (Result, error)
	Name() string
}

// NewServer builds the right Server implementation for cfg.Transport.
// Caller is responsible for having validated cfg through LoadConfig
// already; this constructor only panics on a transport string Manager
// itself would not have stored.
func NewServer(cfg ServerConfig) (Server, error) {
	switch cfg.Transport {
	case TransportStdio:
		return newStdioServer(cfg), nil
	case TransportHTTP:
		return newHTTPServer(cfg), nil
	default:
		return nil, fmt.Errorf("mcp: server %q: unsupported transport %q", cfg.Name, cfg.Transport)
	}
}

// sdkClient is the small slice of the SDK ClientSession that stdioServer
// and httpServer both use. Pulling it behind an interface keeps the two
// implementations parallel and (more importantly) lets a future
// integration test swap in a recorder without touching the manager.
type sdkClient interface {
	ListTools(ctx context.Context, params *mcpsdk.ListToolsParams) (*mcpsdk.ListToolsResult, error)
	CallTool(ctx context.Context, params *mcpsdk.CallToolParams) (*mcpsdk.CallToolResult, error)
	Close() error
}

// baseServer holds the wiring shared between stdioServer and httpServer:
// the session pointer + connect mutex. Both transports differ only in
// how they build the SDK Transport for Connect().
type baseServer struct {
	cfg     ServerConfig
	mu      sync.Mutex
	session sdkClient
	started bool
}

func (b *baseServer) Name() string { return b.cfg.Name }

func (b *baseServer) Stop(_ context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session == nil {
		return nil
	}
	err := b.session.Close()
	b.session = nil
	b.started = false
	return err
}

func (b *baseServer) ListTools(ctx context.Context) ([]Tool, error) {
	b.mu.Lock()
	s := b.session
	b.mu.Unlock()
	if s == nil {
		return nil, fmt.Errorf("mcp: server %q not started", b.cfg.Name)
	}
	res, err := s.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := make([]Tool, 0, len(res.Tools))
	for _, t := range res.Tools {
		schemaBytes, _ := json.Marshal(t.InputSchema)
		out = append(out, Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schemaBytes,
		})
	}
	return out, nil
}

func (b *baseServer) CallTool(ctx context.Context, name string, args json.RawMessage) (Result, error) {
	b.mu.Lock()
	s := b.session
	b.mu.Unlock()
	if s == nil {
		return Result{}, fmt.Errorf("mcp: server %q not started", b.cfg.Name)
	}
	var argMap any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &argMap); err != nil {
			return Result{}, fmt.Errorf("mcp: server %q tool %q: bad args: %w", b.cfg.Name, name, err)
		}
	}
	res, err := s.CallTool(ctx, &mcpsdk.CallToolParams{Name: name, Arguments: argMap})
	if err != nil {
		return Result{}, err
	}
	return resultFromSDK(res), nil
}

// resultFromSDK collapses the SDK's []Content slice into a single string
// so the bitchtea agent boundary can treat MCP results the same as
// built-in tool results. Non-text Content entries are summarized rather
// than dropped silently.
func resultFromSDK(res *mcpsdk.CallToolResult) Result {
	out := Result{IsError: res.IsError}
	for _, c := range res.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			out.Content += tc.Text
			continue
		}
		out.Content += fmt.Sprintf("[mcp non-text content: %T]", c)
	}
	if res.StructuredContent != nil {
		if b, err := json.Marshal(res.StructuredContent); err == nil {
			out.StructuredContent = b
		}
	}
	return out
}

// stdioServer wraps a CommandTransport. The subprocess is launched by
// the SDK on Connect; we just hand it a pre-built exec.Cmd so we control
// env-var inheritance and arg handling.
type stdioServer struct {
	baseServer
}

func newStdioServer(cfg ServerConfig) *stdioServer {
	return &stdioServer{baseServer: baseServer{cfg: cfg}}
}

func (s *stdioServer) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return fmt.Errorf("mcp: server %q already started", s.cfg.Name)
	}
	if s.cfg.Command == "" {
		return fmt.Errorf("mcp: server %q: stdio transport requires command", s.cfg.Name)
	}
	cmd := exec.CommandContext(ctx, s.cfg.Command, s.cfg.Args...)
	cmd.Env = mergeEnv(s.cfg.Env)
	t := &mcpsdk.CommandTransport{Command: cmd}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "bitchtea", Version: "0"}, nil)
	sess, err := client.Connect(ctx, t, nil)
	if err != nil {
		return fmt.Errorf("mcp: server %q connect: %w", s.cfg.Name, err)
	}
	s.session = sess
	s.started = true
	return nil
}

// httpServer wraps a StreamableClientTransport. Headers go on a custom
// http.Client via a roundTripper so we don't have to thread them through
// every CallTool.
type httpServer struct {
	baseServer
}

func newHTTPServer(cfg ServerConfig) *httpServer {
	return &httpServer{baseServer: baseServer{cfg: cfg}}
}

func (h *httpServer) Start(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.started {
		return fmt.Errorf("mcp: server %q already started", h.cfg.Name)
	}
	if h.cfg.URL == "" {
		return fmt.Errorf("mcp: server %q: http transport requires url", h.cfg.Name)
	}
	httpClient := &http.Client{Transport: &headerRoundTripper{
		base:    http.DefaultTransport,
		headers: h.cfg.Headers,
	}}
	t := &mcpsdk.StreamableClientTransport{
		Endpoint:   h.cfg.URL,
		HTTPClient: httpClient,
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "bitchtea", Version: "0"}, nil)
	sess, err := client.Connect(ctx, t, nil)
	if err != nil {
		return fmt.Errorf("mcp: server %q connect: %w", h.cfg.Name, err)
	}
	h.session = sess
	h.started = true
	return nil
}

// headerRoundTripper attaches a fixed set of headers to every outbound
// request. It exists so http transport can carry resolved bearer tokens
// without putting them on the wire URL.
type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}
	return h.base.RoundTrip(req)
}

// mergeEnv returns the current process environment with the supplied
// extras appended. exec.Cmd treats Env==nil as "inherit", so we have to
// expand the inheritance manually when extras are present.
func mergeEnv(extra map[string]string) []string {
	if len(extra) == 0 {
		return nil
	}
	env := append([]string{}, getEnv()...)
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

// getEnv is a var so a future test can stub it. Production behavior is
// the same as os.Environ.
var getEnv = os.Environ
