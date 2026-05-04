package main

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jstamagal/bitchtea/internal/agent"
	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/llm"
	"github.com/jstamagal/bitchtea/internal/tools"
)

type headlessScriptedStreamer struct {
	mu        sync.Mutex
	responses []string
	prompts   []string
	calls     int
}

func (s *headlessScriptedStreamer) StreamChat(_ context.Context, messages []llm.Message, _ *tools.Registry, events chan<- llm.StreamEvent) {
	defer close(events)

	s.mu.Lock()
	idx := s.calls
	s.calls++
	if len(messages) > 0 {
		s.prompts = append(s.prompts, messages[len(messages)-1].Content)
	}
	s.mu.Unlock()

	if idx >= len(s.responses) {
		events <- llm.StreamEvent{Type: "done"}
		return
	}

	events <- llm.StreamEvent{Type: "text", Text: s.responses[idx]}
	events <- llm.StreamEvent{Type: "done"}
}

func TestRunHeadlessLoopFollowsAutoNextAndIdeaFlow(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()
	cfg.AutoNextSteps = true
	cfg.AutoNextIdea = true

	streamer := &headlessScriptedStreamer{
		responses: []string{
			"Implemented the fix and still need to run go test.",
			agentAutoNextDoneToken() + ": tests are green.",
			agentAutoIdeaDoneToken() + ": nothing worthwhile remains.",
		},
	}

	ag := agent.NewAgentWithStreamer(&cfg, streamer)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if err := runHeadlessLoop(context.Background(), ag, "fix it", &stdout, &stderr); err != nil {
		t.Fatalf("runHeadlessLoop: %v", err)
	}

	if strings.Contains(stdout.String(), agentAutoNextDoneToken()) || strings.Contains(stdout.String(), agentAutoIdeaDoneToken()) {
		t.Fatalf("expected headless stdout to hide control tokens, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "[auto] auto-next-steps") {
		t.Fatalf("expected auto-next-steps follow-up in stderr, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "[auto] auto-next-idea") {
		t.Fatalf("expected auto-next-idea follow-up in stderr, got %q", stderr.String())
	}
	if len(streamer.prompts) != 3 {
		t.Fatalf("expected 3 prompts, got %d", len(streamer.prompts))
	}
	if streamer.prompts[0] != "fix it" {
		t.Fatalf("expected original prompt first, got %q", streamer.prompts[0])
	}
	if !strings.Contains(streamer.prompts[1], agentAutoNextDoneToken()) {
		t.Fatalf("expected auto-next prompt second, got %q", streamer.prompts[1])
	}
	if !strings.Contains(streamer.prompts[2], agentAutoIdeaDoneToken()) {
		t.Fatalf("expected auto-idea prompt third, got %q", streamer.prompts[2])
	}
}

func agentAutoNextDoneToken() string { return "AUTONEXT_DONE" }

func agentAutoIdeaDoneToken() string { return "AUTOIDEA_DONE" }

func TestApplyStartupConfigRCProfileOverrideClearsProfile(t *testing.T) {
	dir := t.TempDir()
	origDir := config.ProfilesDir
	config.ProfilesDir = func() string { return dir }
	defer func() { config.ProfilesDir = origDir }()

	if err := config.SaveProfile(config.Profile{
		Name:     "mytest",
		Provider: "anthropic",
		BaseURL:  "https://test.example.com/v1",
		APIKey:   "sk-test-profile-key",
		Model:    "profile-model",
	}); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	opts, rcCommands, err := applyStartupConfig(&cfg, nil, []string{
		"set profile mytest",
		"set model override-model",
		"join #code",
	})
	if err != nil {
		t.Fatalf("applyStartupConfig: %v", err)
	}

	if opts.profileName != "" {
		t.Fatalf("expected no CLI profile, got %q", opts.profileName)
	}
	if cfg.Model != "override-model" {
		t.Fatalf("model = %q, want override-model", cfg.Model)
	}
	if cfg.Profile != "" {
		t.Fatalf("expected manual rc override to clear profile, got %q", cfg.Profile)
	}
	if len(rcCommands) != 1 || rcCommands[0] != "join #code" {
		t.Fatalf("unexpected rc commands: %v", rcCommands)
	}
}

func TestApplyStartupConfigCLIModelOverrideClearsProfile(t *testing.T) {
	dir := t.TempDir()
	origDir := config.ProfilesDir
	config.ProfilesDir = func() string { return dir }
	defer func() { config.ProfilesDir = origDir }()

	if err := config.SaveProfile(config.Profile{
		Name:     "mytest",
		Provider: "anthropic",
		BaseURL:  "https://test.example.com/v1",
		APIKey:   "sk-test-profile-key",
		Model:    "profile-model",
	}); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	opts, rcCommands, err := applyStartupConfig(&cfg, []string{"--profile", "mytest", "--model", "cli-model", "--headless"}, nil)
	if err != nil {
		t.Fatalf("applyStartupConfig: %v", err)
	}

	if !opts.headless {
		t.Fatal("expected headless option")
	}
	if opts.profileName != "mytest" {
		t.Fatalf("profileName = %q, want mytest", opts.profileName)
	}
	if cfg.Model != "cli-model" {
		t.Fatalf("model = %q, want cli-model", cfg.Model)
	}
	if cfg.Profile != "" {
		t.Fatalf("expected CLI model override to clear loaded profile, got %q", cfg.Profile)
	}
	if len(rcCommands) != 0 {
		t.Fatalf("expected no rc commands, got %v", rcCommands)
	}
}

func TestHeadlessFollowUpLoop(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()
	// Only AutoNextSteps is enabled for this test
	cfg.AutoNextSteps = true

	streamer := &headlessScriptedStreamer{
		responses: []string{
			"Implemented the fix but not yet done.",
			agentAutoNextDoneToken() + ": fix complete.",
		},
	}

	ag := agent.NewAgentWithStreamer(&cfg, streamer)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// Give a timeout to ensure no infinite loop
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := runHeadlessLoop(ctx, ag, "do something", &stdout, &stderr); err != nil {
		t.Fatalf("runHeadlessLoop: %v", err)
	}

	// Verify exactly 2 turns ran
	if streamer.calls != 2 {
		t.Fatalf("expected exactly 2 streamer calls, got %d", streamer.calls)
	}

	if !strings.Contains(stderr.String(), "[auto] auto-next-steps") {
		t.Fatalf("expected auto-next-steps follow-up in stderr, got %q", stderr.String())
	}
}

