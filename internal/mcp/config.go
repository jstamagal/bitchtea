// Package mcp holds the security/permission/audit scaffolding for the
// optional Model Context Protocol (MCP) integration described in
// docs/phase-6-mcp-contract.md.
//
// This package intentionally does NOT contain a real MCP client. The client
// (transports, JSON-RPC framing, lifecycle) lives in a sibling task
// (bt-p6-client). This package provides:
//
//   - Config loading with three-layer disable-by-default semantics.
//   - An Authorizer interface gated in front of every MCP tool call.
//   - An AuditHook interface for tool start/end events.
//   - Helpers for stripping resolved secrets out of values that flow into
//     the transcript or session JSONL.
//
// Per the contract, MCP is OFF unless the user opts in three times: the
// per-workspace mcp.json file exists, top-level enabled is true, and the
// per-server enabled is true.
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ConfigFileName is the per-workspace MCP config file, resolved against
// <WorkDir>/.bitchtea/.
const ConfigFileName = "mcp.json"

// Transport names recognized by Config validation. The actual transport
// implementations live in bt-p6-client; this package only validates the
// declared shape so a typo fails loudly.
const (
	TransportStdio = "stdio"
	TransportHTTP  = "http"
)

// Config is the parsed mcp.json. It is the in-memory shape AFTER applying
// the three disable-by-default layers and after env-var interpolation.
//
// A Config returned by LoadConfig is safe to consult: if Enabled is false,
// no MCP traffic should occur for this session. If Enabled is true, only
// servers in Servers should be considered (already filtered for
// per-server enabled:true).
type Config struct {
	// Enabled is the top-level master switch. When false, the MCP client
	// manager must not start any servers and must not surface any MCP UI.
	Enabled bool `json:"enabled"`

	// Servers is keyed by server name. An empty map with Enabled==true is
	// legal but means "MCP is on, but you configured zero servers".
	Servers map[string]ServerConfig `json:"servers"`
}

// ServerConfig describes a single MCP server. Only fields relevant to its
// declared Transport are populated; the unused ones stay at zero value.
type ServerConfig struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"`
	Enabled   bool              `json:"enabled"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

// rawServer mirrors the on-disk JSON shape, where servers is an array
// rather than a map. The map representation in Config is more convenient
// for callers but a list-of-servers reads more naturally on disk.
type rawServer struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"`
	// Enabled is *bool so we can default to true when the field is absent
	// (per the contract). A literal `false` still disables the server.
	Enabled *bool             `json:"enabled,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type rawConfig struct {
	Enabled bool        `json:"enabled"`
	Servers []rawServer `json:"servers"`
}

// Disabled returns a zero-value Config that signals MCP is off. Callers
// should not need to construct this directly — LoadConfig returns it for
// the missing-file and top-level-disabled cases.
func Disabled() Config {
	return Config{Enabled: false, Servers: map[string]ServerConfig{}}
}

// LoadConfig applies the three-layer disable-by-default contract:
//
//  1. If <workDir>/.bitchtea/mcp.json does not exist, returns a disabled
//     Config and a nil error. (User has never opted in.)
//  2. If the file exists but top-level "enabled" is false, returns a
//     disabled Config; the file is still parsed so syntax errors surface.
//  3. Per-server "enabled:false" entries are filtered out before return.
//
// In addition LoadConfig:
//   - Performs ${env:VARNAME} interpolation on env values, headers, args,
//     command, and url. A reference to a variable that is unset in the
//     current process environment is a load-time error for that server.
//   - Refuses to load any value that contains a recognized inline-secret
//     prefix (sk-..., ghp_..., etc.). The contract says secrets must come
//     from ${env:...} interpolation, never inline.
//
// On parse error the returned Config is Disabled() and the error is
// non-nil; callers should surface the error once and otherwise behave as
// if MCP were off.
func LoadConfig(workDir string) (Config, error) {
	path := filepath.Join(workDir, ".bitchtea", ConfigFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Layer 1: no opt-in. Disabled, no error.
			return Disabled(), nil
		}
		return Disabled(), fmt.Errorf("mcp: read %s: %w", path, err)
	}

	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var raw rawConfig
	if err := dec.Decode(&raw); err != nil {
		return Disabled(), fmt.Errorf("mcp: parse %s: %w", path, err)
	}

	if !raw.Enabled {
		// Layer 2: top-level kill switch. We parsed for syntax so the
		// user gets feedback when they later flip it on.
		return Disabled(), nil
	}

	out := Config{Enabled: true, Servers: map[string]ServerConfig{}}
	for _, rs := range raw.Servers {
		// Layer 3: per-server toggle. Default is true when omitted.
		enabled := true
		if rs.Enabled != nil {
			enabled = *rs.Enabled
		}
		if !enabled {
			continue
		}
		if rs.Name == "" {
			return Disabled(), fmt.Errorf("mcp: server entry missing name in %s", path)
		}
		if _, dup := out.Servers[rs.Name]; dup {
			return Disabled(), fmt.Errorf("mcp: duplicate server name %q in %s", rs.Name, path)
		}
		switch rs.Transport {
		case TransportStdio, TransportHTTP:
		default:
			return Disabled(), fmt.Errorf("mcp: server %q: unknown transport %q (want %q or %q)",
				rs.Name, rs.Transport, TransportStdio, TransportHTTP)
		}

		sc := ServerConfig{
			Name:      rs.Name,
			Transport: rs.Transport,
			Enabled:   true,
			Args:      append([]string(nil), rs.Args...),
		}

		// Resolve scalar string fields. Each call both interpolates
		// ${env:...} and rejects inline-secret patterns.
		var ierr error
		if sc.Command, ierr = resolveString(rs.Command, rs.Name, "command"); ierr != nil {
			return Disabled(), ierr
		}
		if sc.URL, ierr = resolveString(rs.URL, rs.Name, "url"); ierr != nil {
			return Disabled(), ierr
		}
		for i, a := range sc.Args {
			v, ierr := resolveString(a, rs.Name, fmt.Sprintf("args[%d]", i))
			if ierr != nil {
				return Disabled(), ierr
			}
			sc.Args[i] = v
		}
		if rs.Env != nil {
			sc.Env = map[string]string{}
			for k, v := range rs.Env {
				rv, ierr := resolveString(v, rs.Name, "env."+k)
				if ierr != nil {
					return Disabled(), ierr
				}
				sc.Env[k] = rv
			}
		}
		if rs.Headers != nil {
			sc.Headers = map[string]string{}
			for k, v := range rs.Headers {
				rv, ierr := resolveString(v, rs.Name, "headers."+k)
				if ierr != nil {
					return Disabled(), ierr
				}
				sc.Headers[k] = rv
			}
		}

		out.Servers[rs.Name] = sc
	}

	return out, nil
}

// envRefPattern matches ${env:VARNAME}. VARNAME is letters, digits,
// and underscore, leading char letter or underscore. Anything outside
// that pattern is left as a literal.
var envRefPattern = regexp.MustCompile(`\$\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

// resolveString applies env interpolation and inline-secret screening to a
// single config value. The (server, field) pair is only used to make load
// errors locatable.
//
// A reference to an unset env var is treated as an error rather than
// silently substituting an empty string. This matches the contract:
// "we do not substitute empty string". Callers (= LoadConfig) treat any
// such error as fatal for the whole config so a half-configured server
// never starts.
func resolveString(in, server, field string) (string, error) {
	if in == "" {
		return "", nil
	}
	var resolveErr error
	resolved := envRefPattern.ReplaceAllStringFunc(in, func(match string) string {
		// Pull the VARNAME back out of the match.
		name := envRefPattern.FindStringSubmatch(match)[1]
		v, ok := os.LookupEnv(name)
		if !ok {
			resolveErr = fmt.Errorf("mcp: server %q field %s references unset env var %s", server, field, name)
			return match
		}
		return v
	})
	if resolveErr != nil {
		return "", resolveErr
	}
	if pat := looksLikeInlineSecret(in); pat != "" {
		// Screen the *raw* input — if the user pasted a token literal,
		// we want to flag it whether or not it would also have been
		// substituted away.
		return "", fmt.Errorf("mcp: server %q field %s contains inline secret pattern %q; use ${env:VAR} instead",
			server, field, pat)
	}
	return resolved, nil
}

// inlineSecretPrefixes is a small heuristic list of well-known token
// prefixes that should never appear in mcp.json. It is intentionally
// minimal: it catches obvious paste-ins (OpenAI, GitHub, Anthropic,
// generic Bearer literals) without trying to be a full secret scanner.
// Users who hit a false positive can rename the variable; the contract
// says secrets always go through ${env:...}.
var inlineSecretPrefixes = []string{
	"sk-",       // OpenAI / many providers
	"ghp_",      // GitHub personal access token
	"github_pat_",
	"gho_",      // GitHub OAuth
	"sk-ant-",   // Anthropic
	"xoxb-",     // Slack bot
	"xoxp-",     // Slack user
	"AKIA",      // AWS access key id
	"AIza",      // Google API key
}

// looksLikeInlineSecret returns the matched prefix if the value appears
// to embed one of the known token prefixes anywhere in the string, and
// the empty string otherwise. The check is substring-based so
// "Bearer sk-abc" trips it just like "sk-abc".
func looksLikeInlineSecret(value string) string {
	for _, p := range inlineSecretPrefixes {
		if strings.Contains(value, p) {
			return p
		}
	}
	return ""
}
