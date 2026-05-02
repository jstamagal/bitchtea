package mcp

import (
	"strings"
	"testing"
)

// RedactServer drops the env map and replaces every header value with
// the placeholder. Command, args, URL, and identity fields stay so the
// transcript can still say "fs (stdio): npx -y ..." without leaking
// the resolved bearer token.
func TestRedactServer_StripsEnvAndHeaders(t *testing.T) {
	in := ServerConfig{
		Name:      "issues",
		Transport: TransportHTTP,
		Enabled:   true,
		URL:       "https://mcp.example.local/v1",
		Headers:   map[string]string{"Authorization": "Bearer SECRET-TOKEN-VALUE"},
		Env:       map[string]string{"X": "leaky"},
		Command:   "",
	}
	out := RedactServer(in)

	if out.Env != nil {
		t.Errorf("expected env to be dropped, got %v", out.Env)
	}
	if v := out.Headers["Authorization"]; v != redactedPlaceholder {
		t.Errorf("expected header redacted, got %q", v)
	}
	if out.Name != "issues" || out.URL != "https://mcp.example.local/v1" {
		t.Errorf("expected identity fields preserved, got %+v", out)
	}
	// Make sure the placeholder is what we think it is — guard against
	// someone changing the constant without updating the contract.
	if !strings.Contains(redactedPlaceholder, "redacted") {
		t.Errorf("placeholder should mention 'redacted', got %q", redactedPlaceholder)
	}
}

// RedactString sweeps a free-form payload (e.g. an error message) for
// any resolved secret values from the supplied Config.
func TestRedactString_ScrubsResolvedSecrets(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Servers: map[string]ServerConfig{
			"issues": {
				Name:      "issues",
				Transport: TransportHTTP,
				Enabled:   true,
				Headers:   map[string]string{"Authorization": "Bearer abc-123-token"},
				Env:       map[string]string{"X": "envleak"},
			},
		},
	}
	in := "request failed: token Bearer abc-123-token used; envleak set"
	out := RedactString(in, cfg)
	if strings.Contains(out, "abc-123-token") {
		t.Errorf("header secret leaked through: %q", out)
	}
	if strings.Contains(out, "envleak") {
		t.Errorf("env secret leaked through: %q", out)
	}
	if !strings.Contains(out, redactedPlaceholder) {
		t.Errorf("expected placeholder in output, got %q", out)
	}
}

// Empty Headers/Env values must not collapse the entire string into the
// placeholder. (strings.ReplaceAll with "" matches every position.)
func TestRedactString_IgnoresEmptySecrets(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Servers: map[string]ServerConfig{
			"x": {
				Name:    "x",
				Headers: map[string]string{"Empty": ""},
				Env:     map[string]string{"Empty": ""},
			},
		},
	}
	in := "hello"
	out := RedactString(in, cfg)
	if out != "hello" {
		t.Errorf("empty secret should not affect output, got %q", out)
	}
}
