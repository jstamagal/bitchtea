package ui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/jstamagal/bitchtea/internal/agent"
	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/session"
)

// TestContextRoundTrip_joinSendSaveResume exercises the full per-context cycle:
// create a model, /join #dev, send a message, agent runs (fake streamer),
// session is saved with context label, simulate process restart by reloading
// session, verify messages are in #dev context, verify focus restored to #dev.
func TestContextRoundTrip_joinSendSaveResume(t *testing.T) {
	sessionDir := t.TempDir()
	workDir := t.TempDir()

	// --- Phase 1: Create model, /join #dev, send message, save session ---

	sessPath := filepath.Join(sessionDir, "test-session.jsonl")
	sess := &session.Session{Path: sessPath}

	cfg := config.DefaultConfig()
	cfg.WorkDir = workDir
	cfg.SessionDir = sessionDir
	cfg.APIKey = "test-key"
	cfg.Model = "test-model"

	// Use a streamer that returns a single assistant text reply.
	replyText := "got it, working on #dev"
	streamer := singleReplyStreamer{text: replyText}

	model := NewModel(&cfg)
	model.agent = agent.NewAgentWithStreamer(&cfg, streamer)
	// Replace the session with our manually-created one so we can inspect Entries.
	model.session = sess

	// Simulate /join #dev by setting focus.
	model.focus.SetFocus(Channel("dev"))
	devCtxKey := ircContextToKey(Channel("dev"))

	// Verify focus is #dev.
	if got := model.focus.ActiveLabel(); got != "#dev" {
		t.Fatalf("expected focus #dev, got %q", got)
	}

	// Run a turn: manually send message to agent, drain events, then simulate done.
	eventCh := make(chan agent.Event, 16)
	go model.agent.SendMessage(context.Background(), "do the thing", eventCh)
	var gotText string
	for ev := range eventCh {
		if ev.Type == "text" {
			gotText += ev.Text
		}
	}
	if gotText != replyText {
		t.Fatalf("expected reply %q, got %q", replyText, gotText)
	}

	// Manually set up the state that would exist during a real turn.
	model.streaming = true
	model.turnContext = Channel("dev")

	// Trigger agentDoneMsg to run per-context session save.
	updated, _ := model.Update(agentDoneMsg{})
	got := updated.(Model)

	if got.streaming {
		t.Fatal("expected streaming to stop after done")
	}

	// Verify session entries have context "#dev".
	if len(sess.Entries) == 0 {
		t.Fatal("expected session entries after agent done")
	}
	hasDevContext := false
	for _, e := range sess.Entries {
		if e.Context == "#dev" {
			hasDevContext = true
			break
		}
	}
	if !hasDevContext {
		t.Fatalf("expected entries with context '#dev', got entries: %+v", sess.Entries)
	}

	// Verify focus state was persisted.
	fs, err := session.LoadFocus(sessionDir)
	if err != nil {
		t.Fatalf("LoadFocus: %v", err)
	}
	foundDev := false
	for _, c := range fs.Contexts {
		if c.Kind == "channel" && c.Channel == "dev" {
			foundDev = true
			break
		}
	}
	if !foundDev {
		t.Fatalf("expected focus state to contain dev channel, got: %+v", fs.Contexts)
	}

	// --- Phase 2: Simulate restart — reload session and verify ---

	// Reload the session file.
	reloaded, err := session.Load(sessPath)
	if err != nil {
		t.Fatalf("session.Load: %v", err)
	}
	if len(reloaded.Entries) == 0 {
		t.Fatal("expected reloaded session to have entries")
	}

	// Create a new model (simulating restart).
	cfg2 := config.DefaultConfig()
	cfg2.WorkDir = workDir
	cfg2.SessionDir = sessionDir
	cfg2.APIKey = "test-key"
	cfg2.Model = "test-model"

	model2 := NewModel(&cfg2)
	model2.agent = agent.NewAgentWithStreamer(&cfg2, stubStreamer{})

	// Resume the session.
	model2.ResumeSession(reloaded)

	// Verify focus was restored to #dev.
	if got := model2.focus.ActiveLabel(); got != "#dev" {
		t.Fatalf("after reload, expected focus #dev, got %q", got)
	}

	// Agent's active context stays #main after resume (lazy switch).
	// The per-context messages ARE in the context map but the agent
	// doesn't switch until the next sendToAgent via startAgentTurn.
	gotCtxKey := model2.agent.ContextKey()
	if gotCtxKey != agent.DefaultContextKey {
		t.Fatalf("expected agent context %q, got %q", agent.DefaultContextKey, gotCtxKey)
	}

	// Verify messages are in #dev context (not just root).
	// Switch to root context and verify it's just the bootstrap.
	model2.agent.SetContext(agent.DefaultContextKey)
	rootMsgs := model2.agent.Messages()
	// Root should only have bootstrap (system prompt, context files, persona).
	// The actual conversation should be in #dev.
	bootstrapCount := model2.agent.BootstrapMessageCount()
	if len(rootMsgs) < bootstrapCount {
		t.Fatalf("expected root to have at least bootstrap count %d, got %d", bootstrapCount, len(rootMsgs))
	}
	// Root should have exactly bootstrap messages (no turn messages).
	if len(rootMsgs) != bootstrapCount {
		t.Fatalf("expected root context to have only bootstrap messages (%d), got %d", bootstrapCount, len(rootMsgs))
	}

	// Switch to #dev and verify conversation messages are there.
	model2.agent.SetContext(devCtxKey)
	devMsgs := model2.agent.Messages()
	// #dev should have bootstrap + turn messages.
	if len(devMsgs) <= bootstrapCount {
		t.Fatalf("expected #dev to have more than bootstrap count, got %d (bootstrap %d)", len(devMsgs), bootstrapCount)
	}
	// The last assistant message should contain our reply text.
	lastAssistantText := ""
	for i := len(devMsgs) - 1; i >= 0; i-- {
		if devMsgs[i].Role == fantasy.MessageRoleAssistant {
			for _, part := range devMsgs[i].Content {
				if tp, ok := part.(fantasy.TextPart); ok {
					lastAssistantText = tp.Text
				}
			}
			break
		}
	}
	if !strings.Contains(lastAssistantText, replyText) {
		t.Fatalf("expected last assistant msg in #dev to contain %q, got %q", replyText, lastAssistantText)
	}

	// Verify that /join saved focus contains #dev as active.
	fs2, err := session.LoadFocus(sessionDir)
	if err != nil {
		t.Fatalf("LoadFocus after resume: %v", err)
	}
	if fs2.ActiveIndex >= len(fs2.Contexts) {
		t.Fatalf("active index %d out of range (%d contexts)", fs2.ActiveIndex, len(fs2.Contexts))
	}
	activeCtx := fs2.Contexts[fs2.ActiveIndex]
	if activeCtx.Kind != "channel" || activeCtx.Channel != "dev" {
		t.Fatalf("expected active focus to be channel:dev, got %+v", activeCtx)
	}
}
