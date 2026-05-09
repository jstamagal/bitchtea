package ui

import (
	"strings"
	"testing"

	"github.com/jstamagal/bitchtea/internal/config"
)

func TestSetCommandShowsAllSettings(t *testing.T) {
	m, _ := testModel(t)
	result, _ := m.handleCommand("/set")
	msg := lastMsg(result)
	if msg.Type != MsgSystem {
		t.Fatalf("expected system message, got %v", msg.Type)
	}
	for _, want := range []string{"PROVIDER", "MODEL", "BASEURL", "APIKEY", "NICK"} {
		if !strings.Contains(msg.Content, want) {
			t.Errorf("expected %q in /set output, got %q", want, msg.Content)
		}
	}
}

func TestSetCommandShowsSingleSetting(t *testing.T) {
	m, _ := testModel(t)
	result, _ := m.handleCommand("/set provider")
	msg := lastMsg(result)
	if !strings.Contains(msg.Content, "Value of PROVIDER is openai") {
		t.Errorf("expected 'Value of PROVIDER is openai', got %q", msg.Content)
	}
}

func TestSetCommandSetsProvider(t *testing.T) {
	m, _ := testModel(t)
	result, _ := m.handleCommand("/set provider anthropic")
	model := result.(Model)
	if model.config.Provider != "anthropic" {
		t.Errorf("provider = %q, want anthropic", model.config.Provider)
	}
	text := allMsgText(result)
	if !strings.Contains(text, "*** Value of PROVIDER set to anthropic.") {
		t.Errorf("expected provider set message, got %q", text)
	}
}

func TestSetCommandSetsModel(t *testing.T) {
	m, _ := testModel(t)
	result, _ := m.handleCommand("/set model claude-opus-4-6")
	model := result.(Model)
	if model.config.Model != "claude-opus-4-6" {
		t.Errorf("model = %q, want claude-opus-4-6", model.config.Model)
	}
}

func TestSetCommandSetsAPIKey(t *testing.T) {
	m, _ := testModel(t)
	result, _ := m.handleCommand("/set apikey sk-1234567890abcdef")
	model := result.(Model)
	if model.config.APIKey != "sk-1234567890abcdef" {
		t.Errorf("apikey not set correctly")
	}
}

// TestSetCommandSetsAPIKeyVerbatim confirms /set apikey passes any value through
// without validation — including short proxy tokens like "x".
func TestSetCommandSetsAPIKeyVerbatim(t *testing.T) {
	m, _ := testModel(t)
	result, _ := m.handleCommand("/set apikey x")
	model := result.(Model)
	if model.config.APIKey != "x" {
		t.Errorf("apikey = %q, want %q", model.config.APIKey, "x")
	}
	for _, msg := range allMsgs(result) {
		if msg.Type == MsgError {
			t.Errorf("unexpected error: %q", msg.Content)
		}
	}
}

// TestSetCommandSetsModelVerbatim confirms /set model passes any value through
// without warnings or rejection.
func TestSetCommandSetsModelVerbatim(t *testing.T) {
	m, _ := testModel(t)
	result, _ := m.handleCommand("/set model x")
	model := result.(Model)
	if model.config.Model != "x" {
		t.Errorf("model = %q, want %q", model.config.Model, "x")
	}
	for _, msg := range allMsgs(result) {
		if msg.Type == MsgError {
			t.Errorf("unexpected error: %q", msg.Content)
		}
	}
}

func TestSetCommandSetsBaseURL(t *testing.T) {
	m, _ := testModel(t)
	result, _ := m.handleCommand("/set baseurl https://api.example.com/v1")
	model := result.(Model)
	if model.config.BaseURL != "https://api.example.com/v1" {
		t.Errorf("baseurl = %q", model.config.BaseURL)
	}
}

func TestSetCommandSetsNick(t *testing.T) {
	m, _ := testModel(t)
	result, _ := m.handleCommand("/set nick coolguy")
	model := result.(Model)
	if model.config.UserNick != "coolguy" {
		t.Errorf("nick = %q, want coolguy", model.config.UserNick)
	}
}

func TestSetCommandUnknownKey(t *testing.T) {
	m, _ := testModel(t)
	result, _ := m.handleCommand("/set bogus value")
	msg := lastMsg(result)
	if msg.Type != MsgError {
		t.Fatalf("expected error for unknown key, got %v", msg.Type)
	}
	if !strings.Contains(msg.Content, "Unknown setting") {
		t.Errorf("unexpected error: %q", msg.Content)
	}
}

func TestSetCommandShowUnknownKey(t *testing.T) {
	m, _ := testModel(t)
	result, _ := m.handleCommand("/set bogus")
	msg := lastMsg(result)
	if msg.Type != MsgError {
		t.Fatalf("expected error for unknown key, got %v", msg.Type)
	}
}

func TestSetCommandMasksAPIKey(t *testing.T) {
	m, _ := testModel(t)
	result, _ := m.handleCommand("/set apikey")
	msg := lastMsg(result)
	if strings.Contains(msg.Content, "sk-test-key-12345") {
		t.Errorf("API key should be masked, got %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "sk-t...2345") {
		t.Errorf("expected masked key, got %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "Value of APIKEY is ") {
		t.Errorf("expected 'Value of APIKEY is ' prefix, got %q", msg.Content)
	}
}

// TestSetCommandSetsServiceVerbatim confirms /set service writes any string to
// cfg.Service without validation, mirroring the bt-fnt convention used by
// /set provider, /set baseurl, etc.
func TestSetCommandSetsServiceVerbatim(t *testing.T) {
	cases := []string{
		"openai", "ollama", "openrouter", "zai-anthropic",
		"custom", "x", "some-weird-proxy-label",
	}
	for _, value := range cases {
		t.Run(value, func(t *testing.T) {
			m, _ := testModel(t)
			result, _ := m.handleCommand("/set service " + value)
			model := result.(Model)
			if model.config.Service != value {
				t.Errorf("Service = %q, want %q", model.config.Service, value)
			}
			for _, msg := range allMsgs(result) {
				if msg.Type == MsgError {
					t.Errorf("unexpected error: %q", msg.Content)
				}
			}
			if !strings.Contains(allMsgText(result), "*** Value of SERVICE set to "+value+".") {
				t.Errorf("expected confirmation, got %q", allMsgText(result))
			}
		})
	}
}

// TestSetCommandListingHasNoDuplicateService is a regression for bt-brc:
// the bare /set listing and the "Unknown setting" error surface used to
// duplicate the 'service' entry because both config.SetKeys() and a
// UI-side setKeysWithService() helper appended it. Lock in the single
// occurrence on both surfaces.
func TestSetCommandListingHasNoDuplicateService(t *testing.T) {
	m, _ := testModel(t)
	m.config.Service = "openrouter"

	result, _ := m.handleCommand("/set")
	listing := lastMsg(result).Content
	if n := strings.Count(listing, "Value of SERVICE is "); n != 1 {
		t.Errorf("bare /set listing has %d SERVICE entries, want 1: %q", n, listing)
	}

	result, _ = m.handleCommand("/set bogus value")
	errText := lastMsg(result).Content
	if n := strings.Count(errText, "service"); n != 1 {
		t.Errorf("unknown-key error has %d 'service' mentions, want 1: %q", n, errText)
	}
}

// TestSetCommandShowsServiceLine confirms the bare /set listing surfaces the
// service identity alongside the other recognised keys.
func TestSetCommandShowsServiceLine(t *testing.T) {
	m, _ := testModel(t)
	m.config.Service = "openai"
	result, _ := m.handleCommand("/set")
	msg := lastMsg(result)
	if !strings.Contains(msg.Content, "Value of SERVICE is openai") {
		t.Errorf("expected 'Value of SERVICE is openai' in /set output, got %q", msg.Content)
	}
}

// TestSetCommandShowsSingleService confirms /set service (no value) prints the
// current service identity rather than rejecting the key.
func TestSetCommandShowsSingleService(t *testing.T) {
	m, _ := testModel(t)
	m.config.Service = "ollama"
	result, _ := m.handleCommand("/set service")
	msg := lastMsg(result)
	if msg.Type == MsgError {
		t.Fatalf("unexpected error for /set service: %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "Value of SERVICE is ollama") {
		t.Errorf("expected 'Value of SERVICE is ollama', got %q", msg.Content)
	}
}

// TestSetCommandServiceDoesNotClearProfile confirms a /set service edit is
// treated as a metadata relabel, not a transport switch — unlike /set baseurl
// or /set provider, the active profile name should survive.
func TestSetCommandServiceDoesNotClearProfile(t *testing.T) {
	m, _ := testModel(t)
	m.config.Profile = "openrouter"
	result, _ := m.handleCommand("/set service some-other-thing")
	model := result.(Model)
	if model.config.Profile != "openrouter" {
		t.Errorf("Profile = %q, want it preserved as openrouter", model.config.Profile)
	}
}

// TestProfileShowDisplaysServiceWithoutLoading confirms /profile show prints a
// profile's service identity (alongside provider/model/baseurl) without
// mutating the active config.
func TestProfileShowDisplaysServiceWithoutLoading(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-1234567890abcdef")

	m, _ := testModel(t)
	priorProfile := m.config.Profile
	priorBaseURL := m.config.BaseURL

	result, _ := m.handleCommand("/profile show openrouter")
	model := result.(Model)
	msg := lastMsg(result)

	if msg.Type == MsgError {
		t.Fatalf("unexpected error: %q", msg.Content)
	}
	for _, want := range []string{"Profile: openrouter", "service=openrouter", "provider=openai"} {
		if !strings.Contains(msg.Content, want) {
			t.Errorf("expected %q in output, got %q", want, msg.Content)
		}
	}
	if model.config.Profile != priorProfile {
		t.Errorf("/profile show mutated active profile: %q -> %q", priorProfile, model.config.Profile)
	}
	if model.config.BaseURL != priorBaseURL {
		t.Errorf("/profile show mutated baseurl: %q -> %q", priorBaseURL, model.config.BaseURL)
	}
}

// TestProfileLoadVerboseShowsServiceLine confirms the verbose profile-load
// status message surfaces the service identity, not just provider+model.
func TestProfileLoadVerboseShowsServiceLine(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-1234567890abcdef")

	m, _ := testModel(t)
	result, _ := m.handleCommand("/profile load openrouter")
	msg := lastMsg(result)
	if !strings.Contains(msg.Content, "service=openrouter") {
		t.Errorf("expected 'service=openrouter' in verbose load output, got %q", msg.Content)
	}
}

// TestSetCommandBitchXOutputFormat confirms the BitchX-style surface for /set:
//   - bare /set lists keys UPPERCASE
//   - /set <key> <value> emits `*** Value of KEY set to VALUE.`
//   - lowercase input is still accepted (rc-file compatibility)
func TestSetCommandBitchXOutputFormat(t *testing.T) {
	m, _ := testModel(t)

	// Lowercase input must still mutate cfg and emit BitchX-shaped output.
	result, _ := m.handleCommand("/set nick coolguy")
	model := result.(Model)
	if model.config.UserNick != "coolguy" {
		t.Fatalf("lowercase /set nick did not stick: %q", model.config.UserNick)
	}
	if !strings.Contains(allMsgText(result), "*** Value of NICK set to coolguy.") {
		t.Errorf("expected BitchX-style confirm, got %q", allMsgText(result))
	}

	// Hyphenated keys render with underscores in display: auto-next -> AUTO_NEXT.
	result, _ = model.handleCommand("/set auto-next on")
	model = result.(Model)
	if !model.config.AutoNextSteps {
		t.Fatalf("auto-next did not flip on")
	}
	if !strings.Contains(allMsgText(result), "*** Value of AUTO_NEXT set to on.") {
		t.Errorf("expected hyphen folded to underscore in display, got %q", allMsgText(result))
	}

	// Bare /set listing renders all keys UPPERCASE.
	result, _ = model.handleCommand("/set")
	msg := lastMsg(result)
	for _, want := range []string{"Value of PROVIDER is ", "Value of MODEL is ", "Value of NICK is ", "Value of AUTO_NEXT is ", "Value of PERSONA_FILE is ", "Value of SERVICE is "} {
		if !strings.Contains(msg.Content, want) {
			t.Errorf("expected %q in /set output, got %q", want, msg.Content)
		}
	}
	// And NO lowercase key labels leaking through.
	for _, leak := range []string{"Value of provider is", "Value of auto-next is", "Value of persona_file is"} {
		if strings.Contains(msg.Content, leak) {
			t.Errorf("unexpected lowercase key label %q in /set output: %q", leak, msg.Content)
		}
	}
}

// TestProfileSaveLoadRoundtripPreservesService confirms a service identity set
// via /set service survives /profile save -> /profile load. This guards the
// JSON marshal/unmarshal path against silently dropping the field.
func TestProfileSaveLoadRoundtripPreservesService(t *testing.T) {
	dir := t.TempDir()
	orig := config.ProfilesDir
	config.ProfilesDir = func() string { return dir }
	t.Cleanup(func() { config.ProfilesDir = orig })

	m, _ := testModel(t)
	m.config.APIKey = "sk-roundtrip-12345"

	// Stamp a non-derivable service so we can prove it round-trips verbatim
	// (rather than being silently re-derived from name/host on load).
	result, _ := m.handleCommand("/set service my-fancy-proxy")
	model := result.(Model)
	if model.config.Service != "my-fancy-proxy" {
		t.Fatalf("/set service did not stick: %q", model.config.Service)
	}

	result, _ = model.handleCommand("/profile save roundtrip-svc")
	model = result.(Model)
	for _, msg := range allMsgs(result) {
		if msg.Type == MsgError {
			t.Fatalf("save error: %q", msg.Content)
		}
	}

	// Reset Service in-memory to confirm load actually reads it back from disk.
	model.config.Service = ""

	result, _ = model.handleCommand("/profile load roundtrip-svc")
	model = result.(Model)
	if model.config.Service != "my-fancy-proxy" {
		t.Errorf("Service after load = %q, want %q", model.config.Service, "my-fancy-proxy")
	}
	if !strings.Contains(allMsgText(result), "service=my-fancy-proxy") {
		t.Errorf("expected verbose load output to surface restored service, got %q",
			allMsgText(result))
	}
}
