package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"charm.land/catwalk/pkg/catwalk"

	"github.com/jstamagal/bitchtea/internal/catalog"
)

// stubCatalog swaps loadModelCatalog for the duration of the test and restores
// it on cleanup. Tests that need an empty catalog pass nil.
func stubCatalog(t *testing.T, providers []catwalk.Provider) {
	t.Helper()
	prev := loadModelCatalog
	loadModelCatalog = func() catalog.Envelope {
		return catalog.Envelope{
			SchemaVersion: catalog.SchemaVersion,
			Providers:     providers,
		}
	}
	t.Cleanup(func() { loadModelCatalog = prev })
}

func openrouterFixture() catwalk.Provider {
	return catwalk.Provider{
		Name:                "OpenRouter",
		ID:                  catwalk.InferenceProviderOpenRouter,
		DefaultLargeModelID: "anthropic/claude-sonnet-4",
		Models: []catwalk.Model{
			{ID: "openai/gpt-4o", Name: "GPT-4o"},
			{ID: "anthropic/claude-sonnet-4", Name: "Claude Sonnet 4"},
			{ID: "anthropic/claude-haiku-4", Name: "Claude Haiku 4"},
			{ID: "meta-llama/llama-3.1-70b", Name: "Llama 3.1 70B"},
		},
	}
}

// TestModelsCommandRegistered guards that /models is in the slash registry.
func TestModelsCommandRegistered(t *testing.T) {
	if _, ok := lookupSlashCommand("/models"); !ok {
		t.Fatal("expected /models to be registered")
	}
}

// TestModelsCommandHelpMentionsModels guards that /help mentions /models so
// users discover the command.
func TestModelsCommandHelpMentionsModels(t *testing.T) {
	m, _ := testModel(t)
	result, _ := m.handleCommand("/help")
	if !strings.Contains(lastMsg(result).Content, "/models") {
		t.Fatalf("expected /help output to mention /models, got %q", lastMsg(result).Content)
	}
}

// TestModelsCommandUnsetService surfaces a clear error when the user has no
// service identity wired up.
func TestModelsCommandUnsetService(t *testing.T) {
	stubCatalog(t, []catwalk.Provider{openrouterFixture()})
	m, _ := testModel(t)
	m.config.Service = ""

	result, _ := m.handleCommand("/models")
	msg := lastMsg(result)
	if msg.Type != MsgError {
		t.Fatalf("expected error, got %v: %q", msg.Type, msg.Content)
	}
	if !strings.Contains(msg.Content, "no active service") {
		t.Errorf("expected unset-service hint, got %q", msg.Content)
	}
	if result.(Model).picker != nil {
		t.Errorf("picker should not open when service is unset")
	}
}

// TestModelsCommandEmptyCatalog surfaces a "catalog is empty" error when
// catalog.Load yields zero providers.
func TestModelsCommandEmptyCatalog(t *testing.T) {
	stubCatalog(t, nil)
	m, _ := testModel(t)
	m.config.Service = "openrouter"

	result, _ := m.handleCommand("/models")
	msg := lastMsg(result)
	if msg.Type != MsgError {
		t.Fatalf("expected error, got %v: %q", msg.Type, msg.Content)
	}
	if !strings.Contains(msg.Content, "catalog is empty") {
		t.Errorf("expected empty-catalog hint, got %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "BITCHTEA_CATWALK_AUTOUPDATE") {
		t.Errorf("expected env-var hint, got %q", msg.Content)
	}
}

// TestModelsCommandUnknownService surfaces a clear error and lists the
// services that *do* have catalog data, so the user can fix their /set
// service value.
func TestModelsCommandUnknownService(t *testing.T) {
	stubCatalog(t, []catwalk.Provider{openrouterFixture()})
	m, _ := testModel(t)
	m.config.Service = "totally-fake-service"

	result, _ := m.handleCommand("/models")
	msg := lastMsg(result)
	if msg.Type != MsgError {
		t.Fatalf("expected error, got %v: %q", msg.Type, msg.Content)
	}
	for _, want := range []string{
		`"totally-fake-service"`,
		"no catalog data",
		"available services:",
		"openrouter",
	} {
		if !strings.Contains(msg.Content, want) {
			t.Errorf("expected %q in error, got %q", want, msg.Content)
		}
	}
	if result.(Model).picker != nil {
		t.Errorf("picker should not open for unknown service")
	}
}

// TestModelsCommandOpensPicker confirms a matching service opens the picker
// in a state we can inspect.
func TestModelsCommandOpensPicker(t *testing.T) {
	stubCatalog(t, []catwalk.Provider{openrouterFixture()})
	m, _ := testModel(t)
	m.config.Service = "openrouter"

	result, _ := m.handleCommand("/models")
	model := result.(Model)
	if model.picker == nil {
		t.Fatal("expected picker to be open after /models")
	}
	if got, want := len(model.picker.models), 4; got != want {
		t.Errorf("picker has %d models, want %d", got, want)
	}
	// DefaultLargeModelID floats to the front so the initial cursor lands
	// on the most-likely choice.
	if model.picker.models[0] != "anthropic/claude-sonnet-4" {
		t.Errorf("first model = %q, want default-large", model.picker.models[0])
	}
	if model.pickerOnSelect == nil {
		t.Error("pickerOnSelect should be wired")
	}
}

// TestModelsCommandCaseInsensitiveServiceMatch confirms the join is
// case-insensitive — /set service OPENROUTER must still find the catalog row.
func TestModelsCommandCaseInsensitiveServiceMatch(t *testing.T) {
	stubCatalog(t, []catwalk.Provider{openrouterFixture()})
	m, _ := testModel(t)
	m.config.Service = "OpenRouter"

	result, _ := m.handleCommand("/models")
	model := result.(Model)
	if model.picker == nil {
		t.Fatalf("expected picker for case-insensitive service match, last msg: %q", lastMsg(result).Content)
	}
}

// TestModelsCommandPickerSelectionInvokesSetModel drives the picker through
// Update with a synthetic Enter key event and asserts the agent's model
// changed and the loaded profile tag was cleared.
func TestModelsCommandPickerSelectionInvokesSetModel(t *testing.T) {
	stubCatalog(t, []catwalk.Provider{openrouterFixture()})
	m, _ := testModel(t)
	m.config.Service = "openrouter"
	m.config.Profile = "openrouter" // pretend a profile was loaded

	result, _ := m.handleCommand("/models")
	model := result.(Model)
	if model.picker == nil {
		t.Fatalf("expected picker open")
	}

	// Cursor defaults to position 0 = anthropic/claude-sonnet-4 (default-large).
	want := "anthropic/claude-sonnet-4"
	if got := model.picker.selected(); got != want {
		t.Fatalf("initial selection = %q, want %q", got, want)
	}

	// Synthetic Enter via Update — this is the closest end-to-end test we can
	// run without spinning a real bubbletea program.
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	finalModel := next.(Model)

	if finalModel.picker != nil {
		t.Error("picker should be closed after Enter")
	}
	if got := finalModel.agent.Model(); got != want {
		t.Errorf("agent.Model() = %q after selection, want %q", got, want)
	}
	if finalModel.config.Profile != "" {
		t.Errorf("expected loaded profile tag cleared, got %q", finalModel.config.Profile)
	}
	if !strings.Contains(allMsgText(finalModel), "*** Value of MODEL set to "+want+".") {
		t.Errorf("expected confirmation message, got %q", allMsgText(finalModel))
	}
}

// TestModelsCommandPickerEscCancels confirms Esc closes the picker without
// touching agent.Model().
func TestModelsCommandPickerEscCancels(t *testing.T) {
	stubCatalog(t, []catwalk.Provider{openrouterFixture()})
	m, _ := testModel(t)
	m.config.Service = "openrouter"
	originalModel := m.agent.Model()

	result, _ := m.handleCommand("/models")
	model := result.(Model)
	if model.picker == nil {
		t.Fatalf("expected picker open")
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	finalModel := next.(Model)

	if finalModel.picker != nil {
		t.Error("picker should be closed after Esc")
	}
	if finalModel.agent.Model() != originalModel {
		t.Errorf("agent.Model() should be unchanged after Esc, got %q -> %q",
			originalModel, finalModel.agent.Model())
	}
	if !strings.Contains(allMsgText(finalModel), "Picker cancelled") {
		t.Errorf("expected cancellation message, got %q", allMsgText(finalModel))
	}
}

// TestModelsCommandPickerFiltersByQuery confirms typing into the picker
// narrows the visible list and the selection respects the filter.
func TestModelsCommandPickerFiltersByQuery(t *testing.T) {
	stubCatalog(t, []catwalk.Provider{openrouterFixture()})
	m, _ := testModel(t)
	m.config.Service = "openrouter"

	result, _ := m.handleCommand("/models")
	model := result.(Model)
	if model.picker == nil {
		t.Fatalf("expected picker open")
	}

	// Type "haiku" — only one match. Use Update so the routing path is
	// exercised, not just the picker internals.
	for _, r := range "haiku" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}

	if got := len(model.picker.filtered); got != 1 {
		t.Fatalf("filtered = %d, want 1 (only haiku matches), filter=%q", got, model.picker.query)
	}
	if got := model.picker.selected(); got != "anthropic/claude-haiku-4" {
		t.Errorf("selected = %q after filter, want anthropic/claude-haiku-4", got)
	}

	// Pick it.
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	finalModel := next.(Model)
	if got := finalModel.agent.Model(); got != "anthropic/claude-haiku-4" {
		t.Errorf("agent.Model() = %q, want anthropic/claude-haiku-4", got)
	}
}

// TestModelsCommandPickerFilterEmptyResult confirms an over-restrictive
// filter does not crash and shows a "no selection" message on Enter.
func TestModelsCommandPickerFilterEmptyResult(t *testing.T) {
	stubCatalog(t, []catwalk.Provider{openrouterFixture()})
	m, _ := testModel(t)
	m.config.Service = "openrouter"

	result, _ := m.handleCommand("/models")
	model := result.(Model)
	for _, r := range "zzznopematch" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}
	if got := len(model.picker.filtered); got != 0 {
		t.Fatalf("expected zero matches for nonsense filter, got %d", got)
	}
	originalModel := model.agent.Model()
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	finalModel := next.(Model)
	if finalModel.agent.Model() != originalModel {
		t.Errorf("Enter on empty filter should not change model, got %q -> %q",
			originalModel, finalModel.agent.Model())
	}
	if !strings.Contains(allMsgText(finalModel), "no selection") {
		t.Errorf("expected 'no selection' message, got %q", allMsgText(finalModel))
	}
}

// TestModelsForServiceNoMatchReturnsNil exercises the join helper in
// isolation — no providers matching service => nil slice.
func TestModelsForServiceNoMatchReturnsNil(t *testing.T) {
	if got := modelsForService(nil, "openrouter"); got != nil {
		t.Errorf("expected nil for empty providers, got %v", got)
	}
	if got := modelsForService([]catwalk.Provider{openrouterFixture()}, "anthropic"); got != nil {
		t.Errorf("expected nil for missing service, got %v", got)
	}
	if got := modelsForService([]catwalk.Provider{openrouterFixture()}, ""); got != nil {
		t.Errorf("expected nil for empty service, got %v", got)
	}
}

// TestAvailableServicesSortedAndLowercased exercises the hint helper.
func TestAvailableServicesSortedAndLowercased(t *testing.T) {
	providers := []catwalk.Provider{
		{ID: catwalk.InferenceProviderOpenRouter},
		{ID: catwalk.InferenceProviderAnthropic},
		{ID: catwalk.InferenceProviderOpenAI},
	}
	got := availableServices(providers)
	want := []string{"anthropic", "openai", "openrouter"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// --- Picker regression tests (bt-p5-verify) ---

// TestModelPickerNewPickerCursorAtDefault confirms a fresh picker starts with
// cursor at position 0 (default-large model if present).
func TestModelPickerNewPickerCursorAtDefault(t *testing.T) {
	ids := []string{"alpha", "beta", "gamma"}
	p := newModelPicker("test", ids)
	if p.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", p.cursor)
	}
	if p.selected() != "alpha" {
		t.Fatalf("selected = %q, want alpha", p.selected())
	}
}

// TestModelPickerCursorClampedOnRefilter verifies the cursor is clamped after
// narrowing the filter.
func TestModelPickerCursorClampedOnRefilter(t *testing.T) {
	ids := []string{"openai/gpt-4o", "anthropic/claude-sonnet-4", "anthropic/claude-haiku-4"}
	p := newModelPicker("test", ids)
	p.cursor = 2 // point at haiku
	p.appendQuery("openai")
	// Only one match now.
	if len(p.filtered) != 1 {
		t.Fatalf("filtered = %d, want 1", len(p.filtered))
	}
	if p.cursor != 0 {
		t.Fatalf("cursor not clamped after refilter, got %d", p.cursor)
	}
}

// TestModelPickerEmptyModelsList confirms the picker handles an empty model
// list gracefully.
func TestModelPickerEmptyModelsList(t *testing.T) {
	p := newModelPicker("empty", nil)
	if p.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", p.cursor)
	}
	if p.selected() != "" {
		t.Fatalf("selected = %q, want empty", p.selected())
	}
	p.moveCursor(1)
	if p.cursor != 0 {
		t.Fatalf("cursor should stay at 0 on empty list")
	}
}

// TestModelPickerBackspaceFullCycle tests backspace through an empty query.
func TestModelPickerBackspaceFullCycle(t *testing.T) {
	ids := []string{"a", "b"}
	p := newModelPicker("test", ids)
	p.appendQuery("something")
	p.backspace()
	p.backspace()
	// backspace on empty query is a no-op, not a panic.
	for i := 0; i < 20; i++ {
		p.backspace()
	}
	if p.query != "" {
		t.Fatalf("query should be empty after full backspace, got %q", p.query)
	}
	if len(p.filtered) != 2 {
		t.Fatalf("empty query should match all, got %d filtered", len(p.filtered))
	}
}

// TestModelPickerViewBoundedRows confirms the view doesn't exceed maxRows.
func TestModelPickerViewBoundedRows(t *testing.T) {
	ids := make([]string, 50)
	for i := range ids {
		ids[i] = "model-" + string(rune('a'+i%26)) + "-v" + string(rune('0'+i/26))
	}
	p := newModelPicker("test", ids)
	view := p.view(5)
	lines := strings.Split(view, "\n")
	// title + prompt + up to maxRows entries + optional hint + footer
	// Should be roughly: title(1) + prompt(1) + entries(≤maxRows) + "...\n(1)" + footer(1)
	if len(lines) > 12 {
		t.Fatalf("view too tall: %d lines for maxRows=5", len(lines))
	}
}

// TestModelPickerMoveCursorPage confirms PgUp/PgDown move by page increments.
func TestModelPickerMoveCursorPage(t *testing.T) {
	ids := make([]string, 30)
	for i := range ids {
		ids[i] = "model-" + string(rune('a'+i%26))
	}
	p := newModelPicker("test", ids)
	p.cursor = 20
	p.moveCursor(-pickerVisibleRows) // PgUp
	if p.cursor != 8 {
		t.Fatalf("PgUp: cursor = %d, want 8", p.cursor)
	}
	p.moveCursor(pickerVisibleRows) // PgDown
	if p.cursor != 20 {
		t.Fatalf("PgDown: cursor = %d, want 20", p.cursor)
	}
}

// TestModelsForServiceDefaultLargeModelDedup confirms the default-large model
// appears only once (at front) and the rest follow in catalog order.
func TestModelsForServiceDefaultLargeModelDedup(t *testing.T) {
	p := catwalk.Provider{
		ID:                  "openrouter",
		DefaultLargeModelID: "alpha",
		Models: []catwalk.Model{
			{ID: "alpha"}, {ID: "beta"}, {ID: "gamma"},
		},
	}
	got := modelsForService([]catwalk.Provider{p}, "openrouter")
	want := []string{"alpha", "beta", "gamma"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestModelsForServiceDefaultLargeNotInList does not crash or insert when
// DefaultLargeModelID is not present in any model.
func TestModelsForServiceDefaultLargeNotInList(t *testing.T) {
	p := catwalk.Provider{
		ID:                  "openrouter",
		DefaultLargeModelID: "nonexistent-model",
		Models: []catwalk.Model{
			{ID: "alpha"}, {ID: "beta"},
		},
	}
	got := modelsForService([]catwalk.Provider{p}, "openrouter")
	if len(got) != 2 {
		t.Fatalf("expected 2 models, got %v", got)
	}
}

// TestModelsForServiceSkipsEmptyIDs confirms model entries with empty IDs
// are filtered out.
func TestModelsForServiceSkipsEmptyIDs(t *testing.T) {
	p := catwalk.Provider{
		ID: "openrouter",
		Models: []catwalk.Model{
			{ID: ""}, {ID: "valid"}, {ID: ""},
		},
	}
	got := modelsForService([]catwalk.Provider{p}, "openrouter")
	if len(got) != 1 || got[0] != "valid" {
		t.Fatalf("expected only 'valid', got %v", got)
	}
}

// TestHandleModelsCommandPickerStateAfterFilter verifies the picker state is
// consistent after opening and typing into it via Update.
func TestHandleModelsCommandPickerStateAfterFilter(t *testing.T) {
	stubCatalog(t, []catwalk.Provider{openrouterFixture()})
	m, _ := testModel(t)
	m.config.Service = "openrouter"

	result, _ := m.handleCommand("/models")
	model := result.(Model)
	if model.picker == nil {
		t.Fatalf("expected picker open")
	}

	// Type two chars then backspace one — filter should narrow then widen.
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	final, _ := next.(Model).Update(tea.KeyMsg{Type: tea.KeyBackspace})
	pickerModel := final.(Model)
	if pickerModel.picker == nil {
		t.Fatal("picker should still be open")
	}
	// Filter is "h" — should match haiku-4 at minimum.
	if len(pickerModel.picker.filtered) < 1 {
		t.Fatalf("filtered too narrow: %d entries for query=%q", len(pickerModel.picker.filtered), pickerModel.picker.query)
	}
}

// TestHandleModelsCommandServiceStripped confirms the service join is robust
// against surrounding whitespace.
func TestHandleModelsCommandServiceStripped(t *testing.T) {
	stubCatalog(t, []catwalk.Provider{openrouterFixture()})
	m, _ := testModel(t)
	m.config.Service = "  openrouter  "

	result, _ := m.handleCommand("/models")
	model := result.(Model)
	if model.picker == nil {
		t.Fatal("picker should open with whitespace-stripped service")
	}
}
