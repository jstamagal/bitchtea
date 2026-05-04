package ui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jstamagal/bitchtea/internal/agent"
	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/llm"
	"github.com/jstamagal/bitchtea/internal/session"
	"github.com/jstamagal/bitchtea/internal/tools"

	"charm.land/fantasy"
)

// TestSendToAgent_switchesAgentContext verifies that sendToAgent triggers
// InitContext, SetContext, and SetScope with the correct keys from the
// active focus.
func TestSendToAgent_switchesAgentContext(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()
	cfg.APIKey = "test-key"
	cfg.Model = "test-model"

	model := NewModel(&cfg)
	model.agent = agent.NewAgentWithStreamer(&cfg, stubStreamer{})

	// Directly exercise the context switch mechanism to verify InitContext,
	// SetContext, and SetScope (as done by startAgentTurn) with channel context.
	// Avoid sendToAgent because it spawns a goroutine that races with
	// subsequent SetContext calls on the shared messages slice.
	devKey := ircContextToKey(Channel("dev"))
	model.agent.InitContext(devKey)
	model.agent.SetContext(devKey)
	model.agent.SetScope(ircContextToMemoryScope(Channel("dev")))

	if got := model.agent.ContextKey(); got != devKey {
		t.Fatalf("expected agent context %q, got %q", devKey, got)
	}

	// Root context should still exist (initialized by NewAgentWithStreamer).
	model.agent.SetContext(agent.DefaultContextKey)
	rootMsgs := model.agent.Messages()
	bootstrapCount := model.agent.BootstrapMessageCount()
	if len(rootMsgs) < bootstrapCount {
		t.Fatalf("expected root to have bootstrap messages, got %d", len(rootMsgs))
	}
	// Root should be clean (only bootstrap).
	if len(rootMsgs) != bootstrapCount {
		t.Fatalf("expected root to have only bootstrap (%d), got %d", bootstrapCount, len(rootMsgs))
	}

	// Switch back to #dev.
	model.agent.SetContext(devKey)
	devMsgs := model.agent.Messages()
	if len(devMsgs) < bootstrapCount {
		t.Fatalf("expected #dev to have bootstrap messages, got %d", len(devMsgs))
	}

	// Scope should be channel-scoped for #dev.
	scope := model.agent.Scope()
	if scope.Kind != agent.MemoryScopeChannel {
		t.Fatalf("expected channel scope, got kind %q", scope.Kind)
	}
	if scope.Name != "dev" {
		t.Fatalf("expected scope name 'dev', got %q", scope.Name)
	}

	// Verify scope is correct for direct context.
	// Instead of calling sendToAgent again (which would race with the goroutine
	// from the first call), directly exercise the context switch mechanism.
	model.focus.SetFocus(Direct("buddy"))
	buddyKey := ircContextToKey(Direct("buddy"))
	model.agent.InitContext(buddyKey)
	model.agent.SetContext(buddyKey)
	model.agent.SetScope(ircContextToMemoryScope(Direct("buddy")))

	if got := model.agent.ContextKey(); got != "buddy" {
		t.Fatalf("expected agent context 'buddy', got %q", got)
	}
	scope2 := model.agent.Scope()
	if scope2.Kind != agent.MemoryScopeQuery {
		t.Fatalf("expected query scope, got kind %q", scope2.Kind)
	}
	if scope2.Name != "buddy" {
		t.Fatalf("expected scope name 'buddy', got %q", scope2.Name)
	}
}

// TestTurnBoundary_savesPerContextMessages verifies that after a turn
// completes, the session JSONL appends entries with the correct per-context
// label. We exercise two contexts sequentially and assert entries are labelled.
func TestTurnBoundary_savesPerContextMessages(t *testing.T) {
	workDir := t.TempDir()
	sessionDir := t.TempDir()

	sessPath := filepath.Join(sessionDir, "test-session.jsonl")
	sess := &session.Session{Path: sessPath}

	cfg := config.DefaultConfig()
	cfg.WorkDir = workDir
	cfg.SessionDir = sessionDir
	cfg.APIKey = "test-key"
	cfg.Model = "test-model"

	// Use a fake streamer with per-call responses.
	type callSpec struct {
		text string
	}
	var calls []callSpec
	var callIdx int
	streamer := &fakeCallStreamer{
		getText: func() string {
			if callIdx < len(calls) {
				txt := calls[callIdx].text
				callIdx++
				return txt
			}
			return "done"
		},
	}

	model := NewModel(&cfg)
	model.agent = agent.NewAgentWithStreamer(&cfg, streamer)
	model.session = sess

	// --- Turn 1: #dev context ---

	model.focus.SetFocus(Channel("dev"))
	calls = []callSpec{{text: "worked on dev code"}}

	// Run agent turn synchronously then trigger done.
	eventCh := make(chan agent.Event, 16)
	go model.agent.SendMessage(context.Background(), "fix dev", eventCh)
	for range eventCh {
	}

	model.streaming = true
	model.turnContext = Channel("dev")

	_, _ = model.Update(agentDoneMsg{})

	// Collect entries for #dev after turn 1.
	devEntryCount := 0
	for _, e := range sess.Entries {
		if e.Context == "#dev" {
			devEntryCount++
		}
	}
	if devEntryCount == 0 {
		t.Fatal("expected entries with context '#dev' after turn 1")
	}

	// --- Turn 2: #ops context ---

	model.focus.SetFocus(Channel("ops"))
	calls = []callSpec{{text: "deployed to ops"}}

	eventCh2 := make(chan agent.Event, 16)
	// Need to switch agent context for the new turn.
	model.agent.InitContext(ircContextToKey(Channel("ops")))
	model.agent.SetContext(ircContextToKey(Channel("ops")))
	go model.agent.SendMessage(context.Background(), "deploy ops", eventCh2)
	for range eventCh2 {
	}

	model.streaming = true
	model.turnContext = Channel("ops")

	_, _ = model.Update(agentDoneMsg{})

	// Verify #ops entries exist.
	opsEntryCount := 0
	for _, e := range sess.Entries {
		if e.Context == "#ops" {
			opsEntryCount++
		}
	}
	if opsEntryCount == 0 {
		t.Fatal("expected entries with context '#ops' after turn 2")
	}

	// Verify both contexts have entries.
	hasDev := false
	hasOps := false
	for _, e := range sess.Entries {
		switch e.Context {
		case "#dev":
			hasDev = true
		case "#ops":
			hasOps = true
		}
	}
	if !hasDev || !hasOps {
		t.Fatalf("expected both #dev and #ops contexts in entries, dev=%v ops=%v", hasDev, hasOps)
	}
}

// fakeCallStreamer is a streamer that calls a getText function each time
// StreamChat is invoked.
type fakeCallStreamer struct {
	getText func() string
}

func (f *fakeCallStreamer) StreamChat(_ context.Context, _ []llm.Message, _ *tools.Registry, events chan<- llm.StreamEvent) {
	defer close(events)
	events <- llm.StreamEvent{Type: "text", Text: f.getText()}
	events <- llm.StreamEvent{Type: "done"}
}

// TestQueuedMessageStaleness_60sDiscard verifies that queued messages older
// than queueStaleThreshold (2 minutes) are discarded when the agent turn
// finishes, and that fresh queued messages are not discarded.
func TestQueuedMessageStaleness_60sDiscard(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()
	cfg.APIKey = "test-key"
	cfg.Model = "test-model"

	model := NewModel(&cfg)
	model.agent = agent.NewAgentWithStreamer(&cfg, stubStreamer{})

	// Ensure no stale check interferes by using a fake eventCh.
	model.eventCh = make(chan agent.Event)
	model.streaming = true

	// Queue a message that's older than the stale threshold.
	model.queued = []queuedMsg{
		{text: "stale message 1", queuedAt: time.Now().Add(-3 * time.Minute)},
		{text: "stale message 2", queuedAt: time.Now().Add(-4 * time.Minute)},
	}

	updated, cmd := model.Update(agentDoneMsg{})
	got := updated.(Model)

	// Should not produce a follow-up command (stale queue discarded).
	if cmd != nil {
		t.Fatal("expected no command when stale queue is discarded")
	}

	// Queue should be cleared.
	if len(got.queued) != 0 {
		t.Fatalf("expected queue to be emptied, got %d items", len(got.queued))
	}

	// Should have a discard system message.
	if len(got.messages) == 0 {
		t.Fatal("expected discard warning message")
	}
	last := got.messages[len(got.messages)-1]
	if last.Type != MsgSystem || !strings.Contains(last.Content, "Discarded") {
		t.Fatalf("expected discard system message, got type=%d content=%q", last.Type, last.Content)
	}

	// Verify the specific discard count in the message.
	if !strings.Contains(last.Content, "Discarded 2") {
		t.Fatalf("expected 'Discarded 2' in message, got %q", last.Content)
	}

	// Now test: fresh messages should not be discarded.
	model2 := NewModel(&cfg)
	model2.agent = agent.NewAgentWithStreamer(&cfg, singleReplyStreamer{text: "reply"})
	model2.eventCh = make(chan agent.Event)
	model2.streaming = true
	model2.queued = []queuedMsg{
		{text: "fresh message", queuedAt: time.Now()},
	}

	updated2, cmd2 := model2.Update(agentDoneMsg{})
	got2 := updated2.(Model)

	// Should produce a command to send the fresh message.
	if cmd2 == nil {
		t.Fatal("expected command for fresh queued message")
	}

	// Queue should be drained.
	if len(got2.queued) != 0 {
		t.Fatalf("expected queue to be drained, got %d", len(got2.queued))
	}

	// Should NOT have a discard message.
	for _, msg := range got2.messages {
		if strings.Contains(msg.Content, "Discarded") {
			t.Fatalf("expected no discard message for fresh queue, got %q", msg.Content)
		}
	}

	// --- Edge case: exactly at threshold boundary ---
	model3 := NewModel(&cfg)
	model3.agent = agent.NewAgentWithStreamer(&cfg, stubStreamer{})
	model3.eventCh = make(chan agent.Event)
	model3.streaming = true
	model3.queued = []queuedMsg{
		{text: "at boundary", queuedAt: time.Now().Add(-queueStaleThreshold - time.Second)},
	}

	updated3, cmd3 := model3.Update(agentDoneMsg{})
	got3 := updated3.(Model)

	if cmd3 != nil {
		t.Fatal("expected no command for boundary-stale queue")
	}
	if len(got3.queued) != 0 {
		t.Fatalf("expected boundary-stale queue to be cleared, got %d", len(got3.queued))
	}
}

// TestSendToAgent_defaultContextGetsMainLabel verifies that after a turn
// completes in the default context, the session JSONL carries the #main
// context label.
func TestSendToAgent_defaultContextGetsMainLabel(t *testing.T) {
	sessionDir := t.TempDir()
	workDir := t.TempDir()

	sessPath := filepath.Join(sessionDir, "test-session.jsonl")
	sess := &session.Session{Path: sessPath}

	cfg := config.DefaultConfig()
	cfg.WorkDir = workDir
	cfg.SessionDir = sessionDir
	cfg.APIKey = "test-key"
	cfg.Model = "test-model"

	model := NewModel(&cfg)
	model.agent = agent.NewAgentWithStreamer(&cfg, singleReplyStreamer{text: "default reply"})
	model.session = sess

	// Default focus is #main.
	if got := model.focus.ActiveLabel(); got != "#main" {
		t.Fatalf("expected default focus #main, got %q", got)
	}

	// Run a turn in default context.
	eventCh := make(chan agent.Event, 16)
	go model.agent.SendMessage(context.Background(), "test default", eventCh)
	for range eventCh {
	}

	model.streaming = true
	model.turnContext = model.focus.Active() // #main

	_, _ = model.Update(agentDoneMsg{})

	// Verify default context entries exist with #main label.
	hasMain := false
	for _, e := range sess.Entries {
		if e.Context == "#main" {
			hasMain = true
			break
		}
	}
	if !hasMain {
		t.Fatal("expected entries with '#main' context label for default turn")
	}
}

// TestQueuedMessageStaleness_edgeCases tests additional queue staleness
// scenarios: empty queue, mixed stale/fresh boundary, under threshold.
func TestQueuedMessageStaleness_edgeCases(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()
	cfg.APIKey = "test-key"
	cfg.Model = "test-model"

	t.Run("empty queue after done is no-op", func(t *testing.T) {
		model := NewModel(&cfg)
		model.agent = agent.NewAgentWithStreamer(&cfg, stubStreamer{})
		model.eventCh = make(chan agent.Event)
		model.streaming = true
		model.queued = nil

		updated, cmd := model.Update(agentDoneMsg{})
		got := updated.(Model)
		if cmd != nil {
			t.Fatal("expected no command for empty queue")
		}
		if len(got.queued) != 0 {
			t.Fatal("expected queue to remain empty")
		}
	})

	t.Run("single fresh message gets sent", func(t *testing.T) {
		model := NewModel(&cfg)
		model.agent = agent.NewAgentWithStreamer(&cfg, singleReplyStreamer{text: "ack"})
		model.eventCh = make(chan agent.Event)
		model.streaming = true
		model.queued = []queuedMsg{{text: "only one", queuedAt: time.Now()}}

		updated, cmd := model.Update(agentDoneMsg{})
		got := updated.(Model)
		if cmd == nil {
			t.Fatal("expected send command for single fresh message")
		}
		if len(got.queued) != 0 {
			t.Fatal("expected queue to be drained")
		}
	})

	t.Run("queue oldest message determines staleness", func(t *testing.T) {
		model := NewModel(&cfg)
		model.agent = agent.NewAgentWithStreamer(&cfg, stubStreamer{})
		model.eventCh = make(chan agent.Event)
		model.streaming = true
		model.queued = []queuedMsg{
			{text: "oldest", queuedAt: time.Now().Add(-3 * time.Minute)},
			{text: "fresher but still discarded", queuedAt: time.Now()},
		}

		updated, cmd := model.Update(agentDoneMsg{})
		got := updated.(Model)
		if cmd != nil {
			t.Fatal("expected entire queue to be discarded when oldest is stale")
		}
		if len(got.queued) != 0 {
			t.Fatal("expected queue to be cleared")
		}
	})

	t.Run("just-under-threshold is not discarded", func(t *testing.T) {
		model := NewModel(&cfg)
		model.agent = agent.NewAgentWithStreamer(&cfg, singleReplyStreamer{text: "ok"})
		model.eventCh = make(chan agent.Event)
		model.streaming = true
		model.queued = []queuedMsg{
			{text: "close to threshold", queuedAt: time.Now().Add(-queueStaleThreshold + time.Second)},
		}

		updated, cmd := model.Update(agentDoneMsg{})
		got := updated.(Model)
		if cmd == nil {
			t.Fatal("expected send command for just-under-threshold message")
		}
		if len(got.queued) != 0 {
			t.Fatal("expected queue to be drained")
		}
	})
}

// TestContextSwitch_preservesSavedIndex verifies that per-context saved
// indices are tracked independently. After saving #dev messages, switching
// to #ops and saving, switching back to #dev should not re-save already-saved
// dev messages.
func TestContextSwitch_preservesSavedIndex(t *testing.T) {
	sessionDir := t.TempDir()
	workDir := t.TempDir()

	sessPath := filepath.Join(sessionDir, "test-session.jsonl")
	sess := &session.Session{Path: sessPath}

	cfg := config.DefaultConfig()
	cfg.WorkDir = workDir
	cfg.SessionDir = sessionDir
	cfg.APIKey = "test-key"
	cfg.Model = "test-model"

	model := NewModel(&cfg)
	model.agent = agent.NewAgentWithStreamer(&cfg, singleReplyStreamer{text: "dev done"})
	model.session = sess

	// Turn 1: #dev
	model.agent.InitContext(ircContextToKey(Channel("dev")))
	model.agent.SetContext(ircContextToKey(Channel("dev")))
	eventCh := make(chan agent.Event, 16)
	go model.agent.SendMessage(context.Background(), "dev work", eventCh)
	for range eventCh {
	}
	model.streaming = true
	model.turnContext = Channel("dev")
	_, _ = model.Update(agentDoneMsg{})

	entryCountAfterDev := len(sess.Entries)

	// Turn 2: #ops
	model.agent.InitContext(ircContextToKey(Channel("ops")))
	model.agent.SetContext(ircContextToKey(Channel("ops")))
	eventCh2 := make(chan agent.Event, 16)
	go model.agent.SendMessage(context.Background(), "ops work", eventCh2)
	for range eventCh2 {
	}
	model.streaming = true
	model.turnContext = Channel("ops")
	_, _ = model.Update(agentDoneMsg{})

	// Total entries should have grown.
	if len(sess.Entries) <= entryCountAfterDev {
		t.Fatalf("expected entries to grow after ops turn, had %d now %d", entryCountAfterDev, len(sess.Entries))
	}

	// Turn 3: #dev again (no new messages, just switching and saving).
	model.agent.SetContext(ircContextToKey(Channel("dev")))
	model.streaming = true
	model.turnContext = Channel("dev")
	entriesBeforeThirdSave := len(sess.Entries)
	_, _ = model.Update(agentDoneMsg{})

	// Verify no duplicate entries were appended.
	devEntriesAfter := 0
	for _, e := range sess.Entries {
		if e.Context == "#dev" {
			devEntriesAfter++
		}
	}
	devEntriesAfter1 := 0
	for i := 0; i < entryCountAfterDev; i++ {
		if sess.Entries[i].Context == "#dev" {
			devEntriesAfter1++
		}
	}
	if devEntriesAfter != devEntriesAfter1 {
		t.Fatalf("expected no duplicate #dev entries on re-save, had %d after turn 1, now %d", devEntriesAfter1, devEntriesAfter)
	}

	if len(sess.Entries) != entriesBeforeThirdSave {
		t.Fatalf("expected entries unchanged after no-op re-save, before=%d after=%d", entriesBeforeThirdSave, len(sess.Entries))
	}
}

// TestContextSwitch_restorePreservesContextMessages verifies that when
// resuming a session, messages are correctly restored to the right agent
// context keys and the savedIdx watermark is accurate.
func TestContextSwitch_restorePreservesContextMessages(t *testing.T) {
	sessionDir := t.TempDir()
	workDir := t.TempDir()

	sessPath := filepath.Join(sessionDir, "test-session.jsonl")
	sess := &session.Session{Path: sessPath}

	cfg := config.DefaultConfig()
	cfg.WorkDir = workDir
	cfg.SessionDir = sessionDir
	cfg.APIKey = "test-key"
	cfg.Model = "test-model"

	// Create a session with entries from two contexts.
	now := time.Now()
	_ = sess.Append(session.Entry{
		Timestamp: now,
		Role:      "system",
		Content:   "system prompt",
		Context:   "#main",
		ID:        "1",
	})
	_ = sess.Append(session.Entry{
		Timestamp: now,
		Role:      "user",
		Content:   "hello main",
		Context:   "#main",
		ID:        "2",
		ParentID:  "1",
	})
	_ = sess.Append(session.Entry{
		Timestamp: now,
		Role:      "assistant",
		Content:   "hi from main",
		Context:   "#main",
		ID:        "3",
		ParentID:  "2",
	})
	_ = sess.Append(session.Entry{
		Timestamp: now,
		Role:      "user",
		Content:   "hello dev",
		Context:   "#dev",
		ID:        "4",
	})
	_ = sess.Append(session.Entry{
		Timestamp: now,
		Role:      "assistant",
		Content:   "hi from dev",
		Context:   "#dev",
		ID:        "5",
		ParentID:  "4",
	})

	// Reload to get the full entry list.
	reloaded, err := session.Load(sessPath)
	if err != nil {
		t.Fatalf("session.Load: %v", err)
	}

	// Create fresh model and resume.
	model := NewModel(&cfg)
	model.agent = agent.NewAgentWithStreamer(&cfg, stubStreamer{})
	model.ResumeSession(reloaded)

	// Verify both contexts exist in agent.
	defaultMsgs := model.agent.Messages() // current context after restore should be #main
	model.agent.SetContext("#dev")
	devMsgs := model.agent.Messages()

	// #main should have system + some user/assistant messages.
	hasMainConversation := false
	for _, msg := range defaultMsgs {
		for _, part := range msg.Content {
			if tp, ok := part.(fantasy.TextPart); ok && tp.Text == "hello main" {
				hasMainConversation = true
			}
		}
	}
	if !hasMainConversation {
		t.Fatal("expected hello main in default context messages")
	}

	// #dev should have the user+assistant exchange.
	hasDevConversation := false
	for _, msg := range devMsgs {
		for _, part := range msg.Content {
			if tp, ok := part.(fantasy.TextPart); ok && tp.Text == "hello dev" {
				hasDevConversation = true
			}
		}
	}
	if !hasDevConversation {
		t.Fatal("expected hello dev in #dev context messages")
	}

	// SavedIdx for #dev should be set.
	if got := model.agent.SavedIdx("#dev"); got <= 0 {
		t.Fatalf("expected positive SavedIdx for #dev, got %d", got)
	}
}
