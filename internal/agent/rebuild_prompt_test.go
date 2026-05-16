package agent

import (
	"strings"
	"testing"

	"github.com/jstamagal/bitchtea/internal/config"
)

func TestRebuildSystemPromptSwapsMessagesZero(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	streamer := &fakeStreamer{}
	agent := NewAgentWithStreamer(&cfg, streamer)

	// Verify initial system prompt contains the default persona marker.
	if len(agent.messages) == 0 {
		t.Fatal("expected at least one message (system prompt)")
	}
	if agent.messages[0].Role != "system" {
		t.Fatalf("expected messages[0] to be system, got %s", agent.messages[0].Role)
	}
	origPrompt := messageText(agent.messages[0])
	if !strings.Contains(origPrompt, "<persona>") {
		t.Fatal("initial system prompt should contain <persona> block")
	}
	// The default persona is the safe public-repo one; it should not contain
	// the test marker we're about to inject.
	if strings.Contains(origPrompt, "CHILL_PERSONA_TEST_MARKER") {
		t.Fatal("initial system prompt should not contain the chill persona marker")
	}

	// Change persona_file to the test fixture and rebuild.
	cfg.PersonaFile = "testdata/chill_persona_test.md"
	agent.RebuildSystemPrompt()

	newPrompt := messageText(agent.messages[0])
	if !strings.Contains(newPrompt, "CHILL_PERSONA_TEST_MARKER") {
		t.Fatalf("after RebuildSystemPrompt with persona_file, system prompt should contain CHILL_PERSONA_TEST_MARKER; got %s", truncate(newPrompt, 200))
	}
	// The old prompt is gone.
	if newPrompt == origPrompt {
		t.Fatal("expected system prompt to have changed after RebuildSystemPrompt, but it was unchanged")
	}
}

func TestRebuildSystemPromptInsertsIfMissing(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	streamer := &fakeStreamer{}
	agent := NewAgentWithStreamer(&cfg, streamer)

	// Remove the system message entirely to simulate a weird state.
	origLen := len(agent.messages)
	agent.messages = agent.messages[1:] // drop messages[0] (system)
	if len(agent.messages) != origLen-1 {
		t.Fatalf("expected %d messages after dropping system, got %d", origLen-1, len(agent.messages))
	}

	cfg.PersonaFile = "testdata/chill_persona_test.md"
	agent.RebuildSystemPrompt()

	// Should have prepended a system message.
	if len(agent.messages) != origLen {
		t.Fatalf("expected %d messages (system re-prepended), got %d", origLen, len(agent.messages))
	}
	if agent.messages[0].Role != "system" {
		t.Fatalf("expected messages[0] to be system, got %s", agent.messages[0].Role)
	}
	if !strings.Contains(messageText(agent.messages[0]), "CHILL_PERSONA_TEST_MARKER") {
		t.Fatal("re-prepended system prompt should contain CHILL_PERSONA_TEST_MARKER")
	}
}

// truncate returns the first n characters of s for error messages.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}