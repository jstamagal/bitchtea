package llm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
)

func TestErrorHintNil(t *testing.T) {
	if h := ErrorHint(nil); h != "" {
		t.Fatalf("nil error should return empty hint, got %q", h)
	}
}

func TestErrorHintAuthErrors(t *testing.T) {
	for _, code := range []string{"API 401", "API 403"} {
		err := fmt.Errorf("after 3 attempts: %s: ...", code)
		h := ErrorHint(err)
		if h == "" {
			t.Fatalf("expected hint for %q, got empty string", code)
		}
	}
}

func TestErrorHintModelNotFound(t *testing.T) {
	err := fmt.Errorf("API 404: model not found")
	h := ErrorHint(err)
	if !contains(h, "model") {
		t.Fatalf("expected model-related hint for 404, got %q", h)
	}
}

func TestErrorHintRateLimit(t *testing.T) {
	err := fmt.Errorf("API 429: too many requests")
	h := ErrorHint(err)
	if !contains(h, "rate") {
		t.Fatalf("expected rate-limit hint for 429, got %q", h)
	}
}

func TestErrorHintServerError(t *testing.T) {
	err := fmt.Errorf("API 500: internal server error")
	h := ErrorHint(err)
	if h == "" {
		t.Fatalf("expected hint for 500, got empty string")
	}
}

func TestErrorHintTimeout(t *testing.T) {
	err := fmt.Errorf("context deadline exceeded")
	h := ErrorHint(err)
	if !contains(h, "timed out") {
		t.Fatalf("expected timeout hint, got %q", h)
	}
}

func TestErrorHintCancelled(t *testing.T) {
	err := context.Canceled
	h := ErrorHint(err)
	// context.Canceled message is "context canceled"
	if h == "" {
		t.Fatalf("expected hint for context.Canceled, got empty string")
	}
}

func TestErrorHintDialError(t *testing.T) {
	err := &net.OpError{
		Op:  "dial",
		Err: errors.New("connection refused"),
	}
	h := ErrorHint(err)
	if !contains(h, "running") {
		t.Fatalf("expected server-not-running hint for dial error, got %q", h)
	}
}

func TestErrorHintUnknown(t *testing.T) {
	err := errors.New("some completely unknown error")
	h := ErrorHint(err)
	// should return empty string — no false positives
	if h != "" {
		t.Fatalf("expected empty hint for unknown error, got %q", h)
	}
}

func contains(s, sub string) bool {
	return len(s) > 0 && len(sub) > 0 && containsAny(s, sub)
}
