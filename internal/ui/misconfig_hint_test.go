package ui

import (
	"strings"
	"testing"
)

// TestCLIProxyAPIPortTypoHint covers the digit-transposition warnings:
// 8713 / 8137 / 8371 / 8731 should all hint that 8317 is the canonical
// CLIProxyAPI port. Other ports must NOT trigger the warning.
func TestCLIProxyAPIPortTypoHint(t *testing.T) {
	cases := []struct {
		name     string
		baseURL  string
		wantHint bool
	}{
		{"8713 transposed", "http://127.0.0.1:8713/v1", true},
		{"8137 transposed", "http://127.0.0.1:8137/v1", true},
		{"8371 transposed", "http://127.0.0.1:8371/v1", true},
		{"8731 transposed", "http://127.0.0.1:8731/v1", true},
		{"8713 on localhost", "http://localhost:8713/v1", true},
		{"8317 canonical (no warning)", "http://127.0.0.1:8317/v1", false},
		{"3456 unrelated (no warning)", "http://127.0.0.1:3456/v1", false},
		{"8713 but not local (no warning)", "https://example.com:8713/v1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cliproxyapiPortTypoHint(strings.ToLower(tc.baseURL))
			if tc.wantHint && got == "" {
				t.Errorf("expected typo hint for %q, got empty", tc.baseURL)
			}
			if !tc.wantHint && got != "" {
				t.Errorf("unexpected hint for %q: %q", tc.baseURL, got)
			}
			if tc.wantHint && !strings.Contains(got, "8317") {
				t.Errorf("hint should mention canonical 8317, got %q", got)
			}
		})
	}
}

// TestProviderTransportHintCLIProxyAPILocal covers the cliproxyapi-shaped local
// URL flag: provider=anthropic against http://127.0.0.1:8317/v1 must warn
// because cliproxyapi is OpenAI-compatible.
func TestProviderTransportHintCLIProxyAPILocal(t *testing.T) {
	got := providerTransportHint("anthropic", "http://127.0.0.1:8317/v1")
	if !strings.Contains(got, "cliproxyapi is OpenAI-compatible") {
		t.Errorf("expected cliproxyapi+anthropic warning, got %q", got)
	}

	// Same URL with provider=openai must NOT raise the cliproxyapi warning.
	got = providerTransportHint("openai", "http://127.0.0.1:8317/v1")
	if strings.Contains(got, "cliproxyapi is OpenAI-compatible") {
		t.Errorf("openai+cliproxyapi should not warn, got %q", got)
	}
}

// TestServiceMisconfigHintCLIProxyAPIWithAnthropic covers the canonical
// example from the ticket: service=cliproxyapi + provider=anthropic.
func TestServiceMisconfigHintCLIProxyAPIWithAnthropic(t *testing.T) {
	got := serviceMisconfigHint("cliproxyapi", "anthropic", "http://127.0.0.1:8317/v1")
	if !strings.Contains(got, "service=cliproxyapi expects provider=openai") {
		t.Errorf("expected cliproxyapi+anthropic mismatch warning, got %q", got)
	}

	// Correct combo must produce no warning.
	got = serviceMisconfigHint("cliproxyapi", "openai", "http://127.0.0.1:8317/v1")
	if got != "" {
		t.Errorf("cliproxyapi+openai+local should be silent, got %q", got)
	}
}

// TestServiceMisconfigHintCLIProxyAPIRemote flags a cliproxyapi service name
// pointed at a non-local baseurl — almost always a copy/paste mistake.
func TestServiceMisconfigHintCLIProxyAPIRemote(t *testing.T) {
	got := serviceMisconfigHint("cliproxyapi", "openai", "https://api.openai.com/v1")
	if !strings.Contains(got, "normally points at a local daemon") {
		t.Errorf("expected remote-baseurl warning for cliproxyapi, got %q", got)
	}
}

// TestServiceMisconfigHintOpenAICompatibleServices covers the wider class of
// OpenAI-compatible services (ollama, openrouter) wired against the wrong
// provider transport.
func TestServiceMisconfigHintOpenAICompatibleServices(t *testing.T) {
	for _, svc := range []string{"ollama", "openrouter"} {
		got := serviceMisconfigHint(svc, "anthropic", "")
		if !strings.Contains(got, "expects provider=openai") {
			t.Errorf("service=%s + provider=anthropic should warn, got %q", svc, got)
		}
	}
}

// TestServiceMisconfigHintZAIAnthropic covers the inverse: a service that
// expects the Anthropic wire format wired against provider=openai.
func TestServiceMisconfigHintZAIAnthropic(t *testing.T) {
	got := serviceMisconfigHint("zai-anthropic", "openai", "")
	if !strings.Contains(got, "expects provider=anthropic") {
		t.Errorf("service=zai-anthropic + provider=openai should warn, got %q", got)
	}
}

// TestServiceMisconfigHintEmptyService is a guard: an unset service must not
// trigger any service-shaped warning regardless of provider/baseurl.
func TestServiceMisconfigHintEmptyService(t *testing.T) {
	got := serviceMisconfigHint("", "anthropic", "http://127.0.0.1:8317/v1")
	if got != "" {
		t.Errorf("empty service should be silent, got %q", got)
	}
}

// TestSetServiceEmitsMisconfigHint confirms /set service surfaces the
// misconfig warning when the resulting (service, provider, baseurl) triple
// looks wrong.
func TestSetServiceEmitsMisconfigHint(t *testing.T) {
	m, _ := testModel(t)
	m.config.Provider = "anthropic"
	m.agent.SetProvider("anthropic")

	result, _ := m.handleCommand("/set service cliproxyapi")
	text := allMsgText(result)
	if !strings.Contains(text, "*** Value of SERVICE set to cliproxyapi.") {
		t.Errorf("missing BitchX confirm, got %q", text)
	}
	if !strings.Contains(text, "service=cliproxyapi expects provider=openai") {
		t.Errorf("missing misconfig warning on /set service, got %q", text)
	}
}

// TestSetBaseURLEmitsServiceMisconfigHint covers the other direction:
// a remote baseurl after service is already set to cliproxyapi.
func TestSetBaseURLEmitsServiceMisconfigHint(t *testing.T) {
	m, _ := testModel(t)
	m.config.Service = "cliproxyapi"

	result, _ := m.handleCommand("/set baseurl https://api.openai.com/v1")
	text := allMsgText(result)
	if !strings.Contains(text, "normally points at a local daemon") {
		t.Errorf("expected remote-baseurl warning, got %q", text)
	}
}

// TestSetBaseURLEmitsPortTypoHint covers the digit-transposition warning
// surfaced through the /set baseurl command path.
func TestSetBaseURLEmitsPortTypoHint(t *testing.T) {
	m, _ := testModel(t)

	result, _ := m.handleCommand("/set baseurl http://127.0.0.1:8713/v1")
	text := allMsgText(result)
	if !strings.Contains(text, "looks like a typo for :8317") {
		t.Errorf("expected port-typo hint on /set baseurl, got %q", text)
	}
}

// TestSetProviderEmitsServiceMisconfigHint confirms the misconfig warning
// fires when provider is changed after service is already pinned.
func TestSetProviderEmitsServiceMisconfigHint(t *testing.T) {
	m, _ := testModel(t)
	m.config.Service = "cliproxyapi"
	m.config.BaseURL = "http://127.0.0.1:8317/v1"
	m.agent.SetBaseURL(m.config.BaseURL)

	result, _ := m.handleCommand("/set provider anthropic")
	text := allMsgText(result)
	if !strings.Contains(text, "service=cliproxyapi expects provider=openai") {
		t.Errorf("expected service-misconfig warning on /set provider, got %q", text)
	}
}
