package llm

import (
	"context"
	"crypto/x509"
	"errors"
	"testing"

	"charm.land/fantasy"
)

func TestFormatErrorProviderErrorStringEnvelope(t *testing.T) {
	err := &fantasy.ProviderError{
		StatusCode:   401,
		URL:          "http://127.0.0.1:8317/v1/messages",
		ResponseBody: []byte(`{"error":"Missing Authentication header"}`),
	}

	got := FormatError(err)
	want := "401 Missing Authentication header — 127.0.0.1:8317"
	if got != want {
		t.Fatalf("FormatError() = %q, want %q", got, want)
	}
}

func TestFormatErrorProviderErrorNestedEnvelope(t *testing.T) {
	err := &fantasy.ProviderError{
		StatusCode:   400,
		URL:          "http://127.0.0.1:8317/v1/messages",
		ResponseBody: []byte(`{"error":{"message":"claude-opus-4-7 is not a valid model ID","type":"invalid_request_error"}}`),
	}

	got := FormatError(err)
	want := "400 claude-opus-4-7 is not a valid model ID — 127.0.0.1:8317"
	if got != want {
		t.Fatalf("FormatError() = %q, want %q", got, want)
	}
}

func TestFormatErrorTransportNumerics(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "connection refused",
			err:  errors.New("dial tcp 127.0.0.1:8317: connect: connection refused"),
			want: "601 dial tcp 127.0.0.1:8317: connect: connection refused",
		},
		{
			name: "dns",
			err:  errors.New("dial tcp: lookup api.example.invalid: no such host"),
			want: "602 dial tcp: lookup api.example.invalid: no such host",
		},
		{
			name: "tls",
			err:  &x509.UnknownAuthorityError{},
			want: "603 x509: certificate signed by unknown authority",
		},
		{
			name: "timeout",
			err:  context.DeadlineExceeded,
			want: "604 context deadline exceeded",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatError(tt.err); got != tt.want {
				t.Fatalf("FormatError() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatErrorContextTooLarge(t *testing.T) {
	err := &fantasy.ProviderError{
		StatusCode:         400,
		ContextUsedTokens:  210000,
		ContextMaxTokens:   200000,
		ContextTooLargeErr: true,
	}

	got := FormatError(err)
	want := "605 context too large (210000/200000 tokens) — try /compact"
	if got != want {
		t.Fatalf("FormatError() = %q, want %q", got, want)
	}
}

func TestFormatErrorCanceledIsSilent(t *testing.T) {
	if got := FormatError(context.Canceled); got != "" {
		t.Fatalf("FormatError(context.Canceled) = %q, want empty string", got)
	}
}
