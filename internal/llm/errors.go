package llm

import (
	"context"
	"errors"
	"net"
	"strings"

	"charm.land/fantasy"
)

// ErrorHint returns a short, user-friendly hint for a given error, or "" if
// the error doesn't match a known pattern. The hint is intended to be
// rendered as a single status-bar line in the UI.
func ErrorHint(err error) string {
	if err == nil {
		return ""
	}

	// Caller-cancelled requests are silent.
	if errors.Is(err, context.Canceled) {
		return ""
	}

	// fantasy surfaces upstream HTTP errors via *fantasy.ProviderError.
	var pe *fantasy.ProviderError
	if errors.As(err, &pe) {
		if pe.IsContextTooLarge() {
			return "context too large — try /compact"
		}
		switch pe.StatusCode {
		case 401:
			return "auth failed — check /set apikey"
		case 403:
			return "access denied — your API key may lack permissions for this model"
		case 404:
			return "model not found — check the model name with your provider"
		case 408:
			return "request timeout — try again"
		case 429:
			return "rate limited — too many requests; slow down or upgrade tier"
		case 500, 502, 503, 504:
			return "provider error — try again in a moment"
		}
	}

	// Transport-level dial failures (local model not running, etc.)
	var netErr *net.OpError
	if errors.As(err, &netErr) && netErr.Op == "dial" {
		return "cannot reach the server — is it running? (for local models, try: ollama serve)"
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "no such host"):
		return "DNS lookup failed — check /set baseurl"
	case strings.Contains(msg, "connection refused"):
		return "connection refused — is the local server running?"
	case strings.Contains(msg, "x509") || strings.Contains(msg, "certificate") || strings.Contains(msg, "tls"):
		return "TLS error — check /set baseurl or your network proxy"
	case strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "timed out"):
		return "request timed out — model may be slow or network unstable"
	}
	return ""
}
