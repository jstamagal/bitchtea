package mcp

import "strings"

// Redaction policy
//
// Per docs/phase-6-mcp-contract.md, MCP tool args and results flow
// through the transcript path the same way bash output does — they are
// NOT redacted by default. The user accepts that risk by enabling MCP
// at all.
//
// What MUST stay out of the transcript and the session JSONL:
//
//  1. Resolved env-var maps for stdio servers.
//  2. Resolved HTTP headers for http servers.
//  3. The connection config itself (command line, URL with embedded
//     credentials, etc).
//
// The helpers in this file are the chokepoint. Any code path that emits
// MCP-related state to a user-visible surface (transcript) or a
// persistent surface (session JSONL) should run its payload through
// RedactServer / RedactString first.

// RedactServer returns a copy of sc with every field that may carry a
// resolved secret stripped or replaced with a fixed placeholder. The
// returned value is safe to embed in a transcript line or a session
// JSONL record.
//
// Specifically it:
//
//   - Drops the env map.
//   - Replaces every header value with "<redacted>".
//   - Leaves Name, Transport, Enabled, Command, Args, and URL alone:
//     command and args may contain a binary name and switches the user
//     wants to see ("npx -y @modelcontextprotocol/server-filesystem ."),
//     URL is the public endpoint — neither is a secret on its own. If a
//     user smuggles a token into Args or URL, the inline-secret check
//     in LoadConfig refuses to load the file in the first place.
//
// We deliberately avoid trying to scrub Command/Args/URL post-load: by
// the time we are emitting transcript, we have already resolved the
// env references in those fields, so a token would no longer match the
// inline-secret screen. The first line of defense (LoadConfig) is the
// one we trust here.
func RedactServer(sc ServerConfig) ServerConfig {
	out := ServerConfig{
		Name:      sc.Name,
		Transport: sc.Transport,
		Enabled:   sc.Enabled,
		Command:   sc.Command,
		URL:       sc.URL,
	}
	if sc.Args != nil {
		out.Args = append([]string(nil), sc.Args...)
	}
	if sc.Headers != nil {
		out.Headers = map[string]string{}
		for k := range sc.Headers {
			out.Headers[k] = redactedPlaceholder
		}
	}
	// Env intentionally omitted — there is no safe rendering of a
	// resolved env map for transcript.
	return out
}

// RedactString takes a free-form string (e.g. a log line a tool produced)
// and replaces any substring that matches a resolved secret value from
// the supplied Config with the placeholder.
//
// The intended use is for places where the MCP client manager wants to
// emit a status line that could conceivably contain config values
// (typically because it interpolated them itself).
//
// For tool args and results — which the contract says we DO show — do
// NOT run them through this. That's the whole point of the
// "args not redacted by default" rule.
func RedactString(in string, cfg Config) string {
	out := in
	for _, sc := range cfg.Servers {
		for _, v := range sc.Env {
			if v == "" {
				continue
			}
			out = strings.ReplaceAll(out, v, redactedPlaceholder)
		}
		for _, v := range sc.Headers {
			if v == "" {
				continue
			}
			out = strings.ReplaceAll(out, v, redactedPlaceholder)
		}
	}
	return out
}

const redactedPlaceholder = "<redacted>"
