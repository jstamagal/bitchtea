package llm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"charm.land/fantasy"
)

func TestErrorHintNil(t *testing.T) {
	if got := ErrorHint(nil); got != "" {
		t.Fatalf("nil error should return empty hint, got %q", got)
	}
}

func TestErrorHintCanceledIsSilent(t *testing.T) {
	if got := ErrorHint(context.Canceled); got != "" {
		t.Fatalf("context.Canceled should be silent, got %q", got)
	}
}

func TestErrorHintProviderStatusCodes(t *testing.T) {
	cases := []struct {
		status   int
		contains string
	}{
		{401, "auth"},
		{403, "access"},
		{404, "model not found"},
		{408, "timeout"},
		{429, "rate"},
		{500, "provider error"},
		{503, "provider error"},
	}
	for _, c := range cases {
		err := &fantasy.ProviderError{StatusCode: c.status, Message: "boom"}
		got := ErrorHint(err)
		if !strings.Contains(strings.ToLower(got), c.contains) {
			t.Errorf("status %d: expected hint containing %q, got %q", c.status, c.contains, got)
		}
	}
}

func TestErrorHintContextTooLargeBeatsStatus(t *testing.T) {
	err := &fantasy.ProviderError{
		StatusCode:         400,
		ContextTooLargeErr: true,
	}
	got := ErrorHint(err)
	if !strings.Contains(got, "/compact") {
		t.Fatalf("expected context-too-large hint mentioning /compact, got %q", got)
	}
}

func TestErrorHintProviderErrorWrapped(t *testing.T) {
	pe := &fantasy.ProviderError{StatusCode: 429, Message: "limited"}
	wrapped := fmt.Errorf("during stream: %w", pe)
	got := ErrorHint(wrapped)
	if !strings.Contains(got, "rate") {
		t.Fatalf("wrapped ProviderError should still match by errors.As, got %q", got)
	}
}

func TestErrorHintDialError(t *testing.T) {
	err := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	got := ErrorHint(err)
	if !strings.Contains(got, "running") {
		t.Fatalf("dial error should hint about server running, got %q", got)
	}
}

func TestErrorHintMessageFallbacks(t *testing.T) {
	cases := []struct {
		err      error
		contains string
	}{
		{errors.New("dial tcp: lookup api.example.com: no such host"), "DNS"},
		{errors.New("connection refused"), "refused"},
		{errors.New("x509: certificate signed by unknown authority"), "TLS"},
		{errors.New("context deadline exceeded"), "timed out"},
		{errors.New("operation timed out"), "timed out"},
	}
	for _, c := range cases {
		got := ErrorHint(c.err)
		if !strings.Contains(got, c.contains) {
			t.Errorf("err %q: expected hint containing %q, got %q", c.err, c.contains, got)
		}
	}
}

func TestErrorHintUnknownIsEmpty(t *testing.T) {
	if got := ErrorHint(errors.New("entirely opaque failure mode")); got != "" {
		t.Fatalf("unknown error should produce no hint, got %q", got)
	}
}
