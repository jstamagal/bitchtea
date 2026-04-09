package llm

import (
	"errors"
	"net"
	"strings"
)

// ErrorHint returns a short, user-friendly hint for common LLM API errors.
// Returns "" when no specific hint is available.
func ErrorHint(err error) string {
	if err == nil {
		return ""
	}

	msg := err.Error()

	// Network errors — local model not running
	var netErr *net.OpError
	if errors.As(err, &netErr) && netErr.Op == "dial" {
		return "cannot reach the server — is it running? (for local models, try: ollama serve)"
	}

	// Detect status codes embedded in error strings by the client
	switch {
	case containsAny(msg, "API 401", "status 401"):
		return "authentication failed — check your API key"
	case containsAny(msg, "API 403", "status 403"):
		return "access denied — your API key may lack permissions for this model"
	case containsAny(msg, "API 404", "status 404"):
		return "model not found — check the model name with your provider"
	case containsAny(msg, "API 429", "status 429"):
		return "rate limited — too many requests; slow down or upgrade your tier"
	case containsAny(msg, "API 500", "status 500"):
		return "provider internal error — try again in a moment"
	case containsAny(msg, "API 502", "status 502", "API 503", "status 503"):
		return "provider unavailable — try again shortly"
	}

	// Context / timeout
	if containsAny(msg, "context deadline exceeded", "deadline exceeded", "timed out") {
		return "request timed out — the model may be slow or the network is unstable"
	}
	if containsAny(msg, "context canceled") {
		return "request was cancelled"
	}

	// TLS / cert issues
	if containsAny(msg, "certificate", "tls", "x509") {
		return "TLS error — check the base URL or your network proxy settings"
	}

	return ""
}

func containsAny(s string, subs ...string) bool {
	lower := strings.ToLower(s)
	for _, sub := range subs {
		if strings.Contains(lower, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}
