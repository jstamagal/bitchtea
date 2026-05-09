package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"

	"charm.land/fantasy"
)

// IRC-style three-digit error codes. 4xx/5xx mirror HTTP for upstream
// errors. 6xx is bitchtea-local: transport, DNS, TLS, cancellation.
const (
	ErrCancelled       = 600 // user cancelled / context cancelled (silent)
	ErrConnRefused     = 601 // server unreachable, conn refused
	ErrDNS             = 602 // hostname did not resolve
	ErrTLS             = 603 // TLS / x509 / certificate failure
	ErrTimeout         = 604 // deadline exceeded / timed out
	ErrContextTooLarge = 605 // model rejected the prompt for length
	ErrUnknown         = 699 // fallback for transport errors with no code
)

// ErrorMessage formats an error as an IRC-style numeric reply:
//
//	NNN <message>
//
// where NNN is a three-digit code. Upstream HTTP errors use the actual
// HTTP status (401, 404, 400, 500, ...). Local transport errors use
// the 6xx range. The message is the verbatim upstream string when
// available, parsed from common JSON envelopes (OpenAI, Anthropic,
// cliproxyapi, openai-compatible proxies). No sanitization, no hints.
func ErrorMessage(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	if errors.Is(err, context.Canceled) {
		return ErrCancelled, ""
	}

	var pe *fantasy.ProviderError
	if errors.As(err, &pe) {
		if pe.IsContextTooLarge() {
			return ErrContextTooLarge, fmt.Sprintf("context too large (%d/%d tokens) — try /compact", pe.ContextUsedTokens, pe.ContextMaxTokens)
		}
		code := pe.StatusCode
		if code == 0 {
			code = ErrUnknown
		}
		upstream := extractUpstreamMessage(pe.ResponseBody)
		switch {
		case upstream != "" && pe.URL != "":
			return code, fmt.Sprintf("%s — %s", upstream, errURLHost(pe.URL))
		case upstream != "":
			return code, upstream
		case pe.URL != "":
			return code, fmt.Sprintf("(no body) — %s", errURLHost(pe.URL))
		}
		return code, "(no body)"
	}

	var netErr *net.OpError
	if errors.As(err, &netErr) && netErr.Op == "dial" {
		return ErrConnRefused, err.Error()
	}
	msg := err.Error()
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "no such host"):
		return ErrDNS, msg
	case strings.Contains(low, "connection refused"):
		return ErrConnRefused, msg
	case strings.Contains(low, "x509"), strings.Contains(low, "certificate"), strings.Contains(low, "tls"):
		return ErrTLS, msg
	case strings.Contains(low, "deadline exceeded"), strings.Contains(low, "timed out"):
		return ErrTimeout, msg
	}
	return ErrUnknown, msg
}

// FormatError returns the IRC-style "NNN message" string ready for display,
// or "" for silent errors (cancellation).
func FormatError(err error) string {
	code, msg := ErrorMessage(err)
	if code == 0 || code == ErrCancelled {
		return ""
	}
	if msg == "" {
		return fmt.Sprintf("%d", code)
	}
	return fmt.Sprintf("%d %s", code, msg)
}

func extractUpstreamMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var env struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &env) == nil && len(env.Error) > 0 {
		var s string
		if json.Unmarshal(env.Error, &s) == nil && s != "" {
			return s
		}
		var obj struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		}
		if json.Unmarshal(env.Error, &obj) == nil && obj.Message != "" {
			return obj.Message
		}
	}
	var flat struct {
		Message string `json:"message"`
		Detail  string `json:"detail"`
	}
	if json.Unmarshal(body, &flat) == nil {
		if flat.Message != "" {
			return flat.Message
		}
		if flat.Detail != "" {
			return flat.Detail
		}
	}
	raw := strings.TrimSpace(string(body))
	if len(raw) > 1024 {
		raw = raw[:1024] + "... (truncated)"
	}
	return raw
}

func errURLHost(rawURL string) string {
	if rawURL == "" {
		return "unknown host"
	}
	if i := strings.Index(rawURL, "://"); i >= 0 {
		rawURL = rawURL[i+3:]
	}
	if i := strings.Index(rawURL, "/"); i >= 0 {
		rawURL = rawURL[:i]
	}
	return rawURL
}

// ErrorDetail returns the verbose diagnostic block for /debug on:
// full URL, status, raw response body, request body, and response headers.
func ErrorDetail(err error) string {
	if err == nil {
		return ""
	}
	var pe *fantasy.ProviderError
	if !errors.As(err, &pe) {
		return ""
	}
	var b strings.Builder
	if pe.URL != "" {
		fmt.Fprintf(&b, "  URL:    %s\n", pe.URL)
	}
	if pe.StatusCode != 0 {
		fmt.Fprintf(&b, "  Status: %d\n", pe.StatusCode)
	}
	if len(pe.ResponseHeaders) > 0 {
		fmt.Fprintf(&b, "  Response Headers: %v\n", pe.ResponseHeaders)
	}
	if len(pe.ResponseBody) > 0 {
		body := string(pe.ResponseBody)
		if len(body) > 4096 {
			body = body[:4096] + "... (truncated at 4 KiB)"
		}
		fmt.Fprintf(&b, "  Response Body:\n%s\n", body)
	}
	if len(pe.RequestBody) > 0 {
		req := string(pe.RequestBody)
		if len(req) > 2048 {
			req = req[:2048] + "... (truncated at 2 KiB)"
		}
		fmt.Fprintf(&b, "  Request Body:\n%s", req)
	}
	return strings.TrimRight(b.String(), "\n")
}

// ErrorHint kept for backwards compatibility with tests; deprecated.
// The IRC-style code carries the same information without the patronizing tone.
func ErrorHint(err error) string { return "" }
