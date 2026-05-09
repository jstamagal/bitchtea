package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jstamagal/bitchtea/internal/agent"
	"github.com/jstamagal/bitchtea/internal/catalog"
	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/llm"
	"github.com/jstamagal/bitchtea/internal/session"
)

// loadModelCatalog is a package-level seam so tests can substitute a fixture
// without writing files into ~/.bitchtea/. Production wiring just calls
// catalog.Load with the user's BaseDir.
var loadModelCatalog = func() catalog.Envelope {
	return catalog.Load(catalog.LoadOptions{})
}

type slashCommandHandler func(Model, string, []string) (Model, tea.Cmd)

type slashCommandSpec struct {
	names   []string
	handler slashCommandHandler
}

var slashCommandRegistry = registerSlashCommands(
	slashCommandSpec{names: []string{"/quit", "/q", "/exit"}, handler: handleQuitCommand},
	slashCommandSpec{names: []string{"/help", "/h"}, handler: handleHelpCommand},
	slashCommandSpec{names: []string{"/set"}, handler: handleSetCommand},
	slashCommandSpec{names: []string{"/clear"}, handler: handleClearCommand},
	slashCommandSpec{names: []string{"/restart"}, handler: handleRestartCommand},
	slashCommandSpec{names: []string{"/compact"}, handler: handleCompactCommand},
	slashCommandSpec{names: []string{"/copy"}, handler: handleCopyCommand},
	slashCommandSpec{names: []string{"/tokens"}, handler: handleTokensCommand},
	slashCommandSpec{names: []string{"/status"}, handler: handleStatusCommand},
	slashCommandSpec{names: []string{"/save"}, handler: handleSaveCommand},
	slashCommandSpec{names: []string{"/debug"}, handler: handleDebugCommand},
	slashCommandSpec{names: []string{"/activity"}, handler: handleActivityCommand},
	slashCommandSpec{names: []string{"/mp3"}, handler: handleMP3Command},
	slashCommandSpec{names: []string{"/theme"}, handler: handleThemeCommand},
	slashCommandSpec{names: []string{"/memory"}, handler: handleMemoryCommand},
	slashCommandSpec{names: []string{"/sessions", "/ls"}, handler: handleSessionsCommand},
	slashCommandSpec{names: []string{"/resume"}, handler: handleResumeCommand},
	slashCommandSpec{names: []string{"/tree"}, handler: handleTreeCommand},
	slashCommandSpec{names: []string{"/fork"}, handler: handleForkCommand},
	slashCommandSpec{names: []string{"/profile"}, handler: handleProfileCommand},
	slashCommandSpec{names: []string{"/models"}, handler: handleModelsCommand},
	slashCommandSpec{names: []string{"/join"}, handler: handleJoinCommand},
	slashCommandSpec{names: []string{"/part"}, handler: handlePartCommand},
	slashCommandSpec{names: []string{"/query"}, handler: handleQueryCommand},
	slashCommandSpec{names: []string{"/channels", "/ch"}, handler: handleChannelsCommand},
	slashCommandSpec{names: []string{"/msg"}, handler: handleMsgCommand},
	slashCommandSpec{names: []string{"/invite"}, handler: handleInviteCommand},
	slashCommandSpec{names: []string{"/kick"}, handler: handleKickCommand},
)

func registerSlashCommands(specs ...slashCommandSpec) map[string]slashCommandHandler {
	registry := make(map[string]slashCommandHandler, len(specs))
	for _, spec := range specs {
		for _, name := range spec.names {
			registry[name] = spec.handler
		}
	}
	return registry
}

func lookupSlashCommand(name string) (slashCommandHandler, bool) {
	handler, ok := slashCommandRegistry[strings.ToLower(name)]
	return handler, ok
}

const helpCommandText = "Commands:\n" +
	"  /join <#channel>    Switch focus to channel (creates if new)\n" +
	"  /part [#channel]    Leave context (default: current)\n" +
	"  /query <nick>       Route Enter persistently to nick\n" +
	"  /msg <nick> <text>  One-shot send to nick, no focus change\n" +
	"  /channels           List open contexts\n" +
	"  /set [key [value]]  Show or change a setting\n" +
	"                        keys: provider, model, baseurl, apikey, service,\n" +
	"                              nick, profile, sound, auto-next, auto-idea\n" +
	"                        e.g. /set apikey sk-..., /set provider anthropic\n" +
	"  /profile [cmd]      save/load/show/delete profiles (built-ins: ollama, openrouter, etc.)\n" +
	"                        bare /profile <name> loads the named profile\n" +
	"  /models             Open a fuzzy picker of models for the active service\n" +
	"                        (uses the catwalk catalog cache; offline-safe)\n" +
	"  /compact            Compact conversation context\n" +
	"  /clear              Clear chat display\n" +
	"  /restart            Reset agent and start a fresh conversation\n" +
	"  /copy [n]           Copy last or nth assistant response\n" +
	"  /tokens             Token usage estimate\n" +
	"  /status             Show endpoint, model, context window usage, cost\n" +
	"  /save               Snapshot current settings to bitchtearc (auto-backup)\n" +
	"  /memory             Show MEMORY.md contents\n" +
	"  /sessions           List saved sessions\n" +
	"  /resume <number>    Resume a session by number\n" +
	"  /tree               Show session tree\n" +
	"  /fork               Fork session\n" +
	"  /debug on|off       Toggle verbose API logging\n" +
	"  /activity [clear]   Show or clear queued background activity\n" +
	"  /mp3 [cmd]          Toggle MP3 panel and player\n" +
	"  /quit               Exit\n" +
	"\n" +
	"  Use @filename to include file contents.\n" +
	"  Type while agent works to queue (steering).\n" +
	"  Ctrl+C to interrupt, again to quit."

func handleQuitCommand(m Model, _ string, _ []string) (Model, tea.Cmd) {
	return m, tea.Quit
}

func handleHelpCommand(m Model, _ string, _ []string) (Model, tea.Cmd) {
	m.addMessage(ChatMessage{
		Time:    time.Now(),
		Type:    MsgSystem,
		Content: helpCommandText,
	})
	m.refreshViewport()
	return m, nil
}

func handleSetCommand(m Model, input string, parts []string) (Model, tea.Cmd) {
	if len(parts) == 1 {
		var sb strings.Builder
		sb.WriteString("Settings:\n")
		for _, key := range config.SetKeys() {
			value, _ := config.GetSetting(m.config, key)
			sb.WriteString(fmt.Sprintf("  %s = %s\n", setKeyDisplay(key), value))
		}
		// Service is the upstream identity (openai, ollama, openrouter, ...)
		// distinct from Provider (wire format). Surfaced here so users can see
		// what per-service gates will fire. See docs/phase-9-service-identity.md.
		sb.WriteString(fmt.Sprintf("  %s = %s\n", setKeyDisplay("service"), serviceDisplay(m.config.Service)))
		m.sysMsg(strings.TrimRight(sb.String(), "\n"))
		return m, nil
	}

	key := strings.ToLower(parts[1])
	if len(parts) == 2 {
		// Enumerable keys: bare `/set <key>` opens a picker or lists choices
		// instead of just echoing the current value. The user can still see
		// the current value with bare `/set` (the all-keys listing).
		switch key {
		case "model":
			return handleModelsCommand(m, "/models", []string{"/models"})
		case "profile":
			return handleProfileCommand(m, "/profile", []string{"/profile"})
		case "debug":
			return handleDebugCommand(m, "/debug", []string{"/debug"})
		case "provider":
			m.sysMsg(fmt.Sprintf("%s = %s\n  available: openai, anthropic\n  set: /set provider <name>", setKeyDisplay("provider"), m.config.Provider))
			return m, nil
		case "service":
			names := config.ListServices()
			cur := serviceDisplay(m.config.Service)
			m.sysMsg(fmt.Sprintf("%s = %s\n  available: %s\n  set: /set service <name>", setKeyDisplay("service"), cur, strings.Join(names, ", ")))
			return m, nil
		}
		value, ok := config.GetSetting(m.config, key)
		if !ok {
			m.errMsg(fmt.Sprintf("Unknown setting %q. Valid keys: %s", key, strings.Join(setKeysWithService(), ", ")))
			return m, nil
		}
		m.sysMsg(fmt.Sprintf("%s = %s", setKeyDisplay(key), value))
		return m, nil
	}

	value := strings.TrimSpace(strings.TrimPrefix(input, parts[0]+" "+parts[1]))
	switch key {
	case "provider":
		return handleProviderCommand(m, "/provider "+value, []string{"/provider", value})
	case "model":
		return handleModelCommand(m, "/model "+value, []string{"/model", value})
	case "baseurl":
		return handleBaseURLCommand(m, "/baseurl "+value, []string{"/baseurl", value})
	case "apikey":
		return handleAPIKeyCommand(m, "/apikey "+value, []string{"/apikey", value})
	case "service":
		return handleServiceSet(m, value)
	case "debug":
		return handleDebugCommand(m, "/debug "+value, []string{"/debug", value})
	}

	if !config.ApplySet(m.config, key, value) {
		m.errMsg(fmt.Sprintf("Unknown setting %q. Valid keys: %s", key, strings.Join(setKeysWithService(), ", ")))
		return m, nil
	}

	switch key {
	case "profile":
		if m.config.Profile == "" {
			m.errMsg(profileLookupMessage(value))
			return m, nil
		}
		m.agent.SetProvider(m.config.Provider)
		m.agent.SetModel(m.config.Model)
		m.agent.SetBaseURL(m.config.BaseURL)
		m.agent.SetAPIKey(m.config.APIKey)
	case "nick":
		// Config-only setting; no agent sync required.
	case "sound", "auto-next", "auto-idea":
		// Config-only settings; no agent sync required.
	default:
		m.errMsg(fmt.Sprintf("Unknown setting %q. Valid keys: %s", key, strings.Join(setKeysWithService(), ", ")))
		return m, nil
	}

	display, _ := config.GetSetting(m.config, key)
	m.sysMsg(setValueChangedMsg(key, display))
	return m, nil
}

// setKeyDisplay renders a /set key in BitchX-style UPPERCASE with hyphens
// folded to underscores (auto-next -> AUTO_NEXT). Used for the bare-`/set`
// listing, single-key show, and `*** Value of KEY set to VALUE.` confirms.
func setKeyDisplay(key string) string {
	key = strings.TrimSpace(key)
	key = strings.ReplaceAll(key, "-", "_")
	return strings.ToUpper(key)
}

// setValueChangedMsg formats a BitchX-style confirmation when a /set key has
// been mutated. Mirrors the canonical format `*** Value of FOO set to bar.`.
func setValueChangedMsg(key, value string) string {
	return fmt.Sprintf("*** Value of %s set to %s.", setKeyDisplay(key), value)
}

func handleModelCommand(m Model, _ string, parts []string) (Model, tea.Cmd) {
	if len(parts) < 2 {
		m.addMessage(ChatMessage{
			Time:    time.Now(),
			Type:    MsgSystem,
			Content: fmt.Sprintf("Current model: %s. Usage: /set model <name>", m.agent.Model()),
		})
		m.refreshViewport()
		return m, nil
	}

	newModel := parts[1]
	m.agent.SetModel(newModel)
	clearLoadedProfile(&m)
	m.addMessage(ChatMessage{
		Time:    time.Now(),
		Type:    MsgSystem,
		Content: setValueChangedMsg("model", newModel),
	})
	m.refreshViewport()
	return m, nil
}

func handleClearCommand(m Model, _ string, _ []string) (Model, tea.Cmd) {
	m.messages = []ChatMessage{}
	m.refreshViewport()
	return m, nil
}

func handleRestartCommand(m Model, _ string, _ []string) (Model, tea.Cmd) {
	if m.streaming && m.agent != nil {
		m.cancelActiveTurn("Restart", true)
	}

	m.agent.Reset()

	m.messages = []ChatMessage{}
	if m.streamBuffer != nil {
		m.streamBuffer.Reset()
	}
	m.queued = nil

	// Start a fresh session log so the restarted conversation is persisted
	// independently. Failure is non-fatal — the app still works without it.
	if newSess, err := session.New(m.config.SessionDir); err == nil {
		m.session = newSess
	}
	m.lastSavedMsgIdx = 0
	m.contextSavedIdx = map[string]int{ircContextToKey(m.focus.Active()): 0}

	m.sysMsg("*** Conversation restarted. Fresh context.")
	return m, nil
}

func handleCompactCommand(m Model, _ string, _ []string) (Model, tea.Cmd) {
	if m.streaming {
		m.sysMsg("Can't compact while agent is working. Be patient.")
		return m, nil
	}
	before := m.agent.EstimateTokens()
	if err := m.agent.Compact(context.Background()); err != nil {
		m.errMsg(fmt.Sprintf("Compaction failed: %v", err))
	} else {
		after := m.agent.EstimateTokens()
		m.sysMsg(fmt.Sprintf("Compacted: ~%s -> ~%s tokens", formatTokens(before), formatTokens(after)))
	}
	return m, nil
}

func handleCopyCommand(m Model, _ string, parts []string) (Model, tea.Cmd) {
	selection := 0
	if len(parts) > 1 {
		n, err := parseCopyIndex(parts[1])
		if err != nil {
			m.errMsg(err.Error())
			return m, nil
		}
		selection = n
	}
	target, copied, err := m.copyAssistantMessage(selection)
	if err != nil {
		m.errMsg(err.Error())
		return m, nil
	}
	m.sysMsg(fmt.Sprintf("Copied %s via %s.", target, copied))
	return m, nil
}

func handleTokensCommand(m Model, _ string, _ []string) (Model, tea.Cmd) {
	tokens := m.agent.EstimateTokens()
	cost := m.agent.Cost()
	msgs := m.agent.MessageCount()
	m.sysMsg(fmt.Sprintf("~%s tokens | $%.4f | %d messages | %d turns",
		formatTokens(tokens), cost, msgs, m.agent.TurnCount))
	return m, nil
}

// handleStatusCommand prints the active connection state plus context-window
// usage. This is the read-only counterpart to /set: where /set lists or mutates
// individual keys, /status snapshots the whole picture in one screen.
func handleStatusCommand(m Model, _ string, _ []string) (Model, tea.Cmd) {
	cfg := m.config
	tokens := m.agent.EstimateTokens()
	ctxWindow := lookupContextWindow(cfg.Service, cfg.Model)
	endpoint := transportEndpointPreview(cfg.Provider, cfg.BaseURL)

	var ctxLine string
	switch {
	case ctxWindow > 0:
		pct := float64(tokens) / float64(ctxWindow) * 100
		ctxLine = fmt.Sprintf("~%s / %s tokens (%.1f%%)", formatTokens(tokens), formatTokens(int(ctxWindow)), pct)
	default:
		ctxLine = fmt.Sprintf("~%s tokens (window unknown)", formatTokens(tokens))
	}

	profile := cfg.Profile
	if profile == "" {
		profile = "<none>"
	}

	var sb strings.Builder
	sb.WriteString("Status:\n")
	sb.WriteString(fmt.Sprintf("  profile:   %s\n", profile))
	sb.WriteString(fmt.Sprintf("  service:   %s\n", serviceDisplay(cfg.Service)))
	sb.WriteString(fmt.Sprintf("  provider:  %s\n", cfg.Provider))
	sb.WriteString(fmt.Sprintf("  model:     %s\n", cfg.Model))
	sb.WriteString(fmt.Sprintf("  baseurl:   %s\n", cfg.BaseURL))
	sb.WriteString(fmt.Sprintf("  endpoint:  %s\n", endpoint))
	sb.WriteString(fmt.Sprintf("  apikey:    %s\n", maskSecret(cfg.APIKey)))
	sb.WriteString(fmt.Sprintf("  context:   %s\n", ctxLine))
	sb.WriteString(fmt.Sprintf("  cost:      $%.4f\n", m.agent.Cost()))
	sb.WriteString(fmt.Sprintf("  messages:  %d\n", m.agent.MessageCount()))
	sb.WriteString(fmt.Sprintf("  turns:     %d", m.agent.TurnCount))
	m.sysMsg(sb.String())
	return m, nil
}

// lookupContextWindow returns the catwalk-reported context window for the
// active model, or 0 when the catalog has no entry. Service is the join key
// (matches catwalk.Provider.ID); model is matched case-insensitively against
// catwalk.Model.ID. Falls back to scanning every provider when service is empty
// so a hand-rolled config still surfaces a window when the model ID is unique.
func lookupContextWindow(service, modelID string) int64 {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return 0
	}
	env := loadModelCatalog()
	if len(env.Providers) == 0 {
		return 0
	}
	svc := strings.TrimSpace(strings.ToLower(service))
	for i := range env.Providers {
		p := &env.Providers[i]
		if svc != "" && !strings.EqualFold(strings.TrimSpace(string(p.ID)), svc) {
			continue
		}
		for j := range p.Models {
			if strings.EqualFold(p.Models[j].ID, modelID) {
				return p.Models[j].ContextWindow
			}
		}
	}
	return 0
}

// handleSaveCommand snapshots the current connection state to the rc file at
// config.RCPath() (typically ~/.bitchtea/bitchtearc). Any existing rc file is
// renamed to bitchtearc.bak-YYYYMMDD-HHMMSS first so the user can roll back.
//
// We deliberately persist raw values (including the API key) — the rc lives
// under ~/.bitchtea/ which is already 0o700 and the user owns the risk per
// CLAUDE.md ("no artificial guardrails"). Same trust model as /profile save.
func handleSaveCommand(m Model, _ string, _ []string) (Model, tea.Cmd) {
	rcPath := config.RCPath()
	if err := os.MkdirAll(filepath.Dir(rcPath), 0o700); err != nil {
		m.errMsg(fmt.Sprintf("save: cannot create %s: %v", filepath.Dir(rcPath), err))
		return m, nil
	}

	var backupNote string
	if _, err := os.Stat(rcPath); err == nil {
		backup := rcPath + ".bak-" + time.Now().Format("20060102-150405")
		if err := os.Rename(rcPath, backup); err != nil {
			m.errMsg(fmt.Sprintf("save: cannot back up existing rc: %v", err))
			return m, nil
		}
		backupNote = fmt.Sprintf(" (previous backed up to %s)", filepath.Base(backup))
	}

	body := buildRCSnapshot(m.config)
	if err := os.WriteFile(rcPath, []byte(body), 0o600); err != nil {
		m.errMsg(fmt.Sprintf("save: cannot write %s: %v", rcPath, err))
		return m, nil
	}
	m.sysMsg(fmt.Sprintf("Saved current config to %s%s.", rcPath, backupNote))
	return m, nil
}

// buildRCSnapshot serializes cfg as a sequence of `set <key> <value>` lines
// that ApplyRCSetCommands will replay at next startup. Order mirrors
// config.SetKeys() so the file reads top-to-bottom in the same order /set
// lists them. Empty values are skipped — a missing apikey is more useful as
// "fall back to env" than as a recorded blank that would clobber it.
func buildRCSnapshot(cfg *config.Config) string {
	var sb strings.Builder
	sb.WriteString("# bitchtea startup commands — written by /save on ")
	sb.WriteString(time.Now().Format(time.RFC3339))
	sb.WriteString("\n")

	write := func(key, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		sb.WriteString("set ")
		sb.WriteString(key)
		sb.WriteString(" ")
		sb.WriteString(value)
		sb.WriteString("\n")
	}

	// Profile first: ApplyProfile clobbers provider/model/baseurl/apikey/service,
	// so writing it before the explicit overrides means subsequent set lines
	// can refine on top. Also drops the profile tag if the snapshot mixes hand
	// edits — /set profile re-applies the named profile cleanly at boot.
	write("profile", cfg.Profile)
	write("provider", cfg.Provider)
	write("model", cfg.Model)
	write("baseurl", cfg.BaseURL)
	write("apikey", cfg.APIKey)
	write("nick", cfg.UserNick)
	write("sound", boolRCValue(cfg.NotificationSound))
	write("auto-next", boolRCValue(cfg.AutoNextSteps))
	write("auto-idea", boolRCValue(cfg.AutoNextIdea))
	return sb.String()
}

func boolRCValue(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func handleDebugCommand(m Model, _ string, parts []string) (Model, tea.Cmd) {
	if len(parts) < 2 {
		status := "OFF"
		if m.debugMode {
			status = "ON"
		}
		m.sysMsg(fmt.Sprintf("Debug mode: %s. Usage: /debug on|off", status))
		return m, nil
	}

	switch strings.ToLower(parts[1]) {
	case "on":
		m.debugMode = true
		m.agent.SetDebugHook(func(info llm.DebugInfo) {
			m.addMessage(ChatMessage{
				Time: time.Now(),
				Type: MsgSystem,
				Content: fmt.Sprintf("[DEBUG] %s %s\nRequest Headers: %v\nRequest Body: %s\nResponse Status: %d",
					info.Method, info.URL, info.RequestHeaders, info.RequestBody, info.StatusCode),
			})
			m.refreshViewport()
		})
		m.sysMsg("Debug mode: ON")
	case "off":
		m.debugMode = false
		m.agent.SetDebugHook(nil)
		m.sysMsg("Debug mode: OFF")
	default:
		m.errMsg("Usage: /debug on|off")
	}
	return m, nil
}

func handleActivityCommand(m Model, _ string, parts []string) (Model, tea.Cmd) {
	switch {
	case len(parts) == 1:
		report := m.backgroundActivityReport()
		if len(m.backgroundActivity) > 0 {
			m.backgroundUnread = 0
		}
		m.addMessage(ChatMessage{
			Time:    time.Now(),
			Type:    MsgSystem,
			Content: report,
		})
		m.refreshViewport()
		return m, nil
	case len(parts) == 2 && strings.EqualFold(parts[1], "clear"):
		cleared := len(m.backgroundActivity)
		m.backgroundActivity = nil
		m.backgroundUnread = 0
		m.sysMsg(fmt.Sprintf("Cleared %d background activity notice(s).", cleared))
		return m, nil
	default:
		m.errMsg("Usage: /activity [clear]")
		return m, nil
	}
}

func handleMP3Command(m Model, _ string, parts []string) (Model, tea.Cmd) {
	if m.mp3 == nil {
		m.errMsg("MP3 controller unavailable.")
		return m, nil
	}
	if len(parts) == 1 {
		status, cmd := m.mp3.toggle()
		m.sysMsg(status)
		return m, cmd
	}

	var (
		status string
		cmd    tea.Cmd
	)

	switch strings.ToLower(parts[1]) {
	case "rescan":
		status = m.mp3.rescan()
	case "play":
		status, cmd = m.mp3.playIndex(m.mp3.current)
	case "pause", "toggle":
		status = m.mp3.togglePause()
	case "next":
		status, cmd = m.mp3.next()
	case "prev", "previous":
		status, cmd = m.mp3.prev()
	default:
		m.errMsg("Usage: /mp3 [rescan|play|pause|next|prev]")
		return m, nil
	}
	if strings.Contains(strings.ToLower(status), "failed") || strings.HasPrefix(status, "No MP3s") || strings.HasPrefix(status, "Usage:") {
		m.errMsg(status)
	} else {
		m.sysMsg(status)
	}
	return m, cmd
}

func handleThemeCommand(m Model, _ string, _ []string) (Model, tea.Cmd) {
	m.sysMsg(fmt.Sprintf("Theme switching is disabled. Built-in theme: %s.", CurrentThemeName()))
	return m, nil
}

func handleMemoryCommand(m Model, _ string, _ []string) (Model, tea.Cmd) {
	// Show root MEMORY.md.
	mem := agent.LoadMemory(m.config.WorkDir)
	if mem != "" {
		if len(mem) > 1000 {
			mem = mem[:1000] + "\n... (truncated)"
		}
		m.addMessage(ChatMessage{
			Time:    time.Now(),
			Type:    MsgRaw,
			Content: "\033[1;36m--- MEMORY.md ---\033[0m\n" + mem,
		})
	}

	// Show scoped HOT.md when not in root context.
	active := m.focus.Active()
	scope := ircContextToMemoryScope(active)
	if scope.Kind != agent.MemoryScopeRoot {
		hot := agent.LoadScopedMemory(m.config.SessionDir, m.config.WorkDir, scope)
		label := active.Label()
		if hot == "" {
			m.sysMsg(fmt.Sprintf("No HOT.md for %s yet.", label))
		} else {
			if len(hot) > 1000 {
				hot = hot[:1000] + "\n... (truncated)"
			}
			m.addMessage(ChatMessage{
				Time:    time.Now(),
				Type:    MsgRaw,
				Content: fmt.Sprintf("\033[1;36m--- HOT.md (%s) ---\033[0m\n", label) + hot,
			})
		}
	}

	if mem == "" && scope.Kind == agent.MemoryScopeRoot {
		m.sysMsg("No MEMORY.md found in working directory.")
	}

	m.refreshViewport()
	return m, nil
}

func handleSessionsCommand(m Model, _ string, parts []string) (Model, tea.Cmd) {
	sessions, err := session.List(m.config.SessionDir)
	if err != nil || len(sessions) == 0 {
		m.sysMsg("No saved sessions.")
		return m, nil
	}

	const pageSize = 20
	page := 1
	if len(parts) >= 2 {
		if _, err := fmt.Sscanf(parts[1], "%d", &page); err != nil || page < 1 {
			page = 1
		}
	}

	totalPages := (len(sessions) + pageSize - 1) / pageSize
	if page > totalPages {
		page = totalPages
	}

	start := (page - 1) * pageSize
	end := start + pageSize
	if end > len(sessions) {
		end = len(sessions)
	}

	var sb strings.Builder
	if totalPages > 1 {
		sb.WriteString(fmt.Sprintf("Sessions (page %d/%d):\n", page, totalPages))
	} else {
		sb.WriteString("Sessions:\n")
	}
	for i, sess := range sessions[start:end] {
		sb.WriteString(fmt.Sprintf("  %d. %s\n", start+i+1, session.Info(sess)))
	}
	if totalPages > 1 && page < totalPages {
		sb.WriteString(fmt.Sprintf("  ... use /sessions %d for next page\n", page+1))
	}
	sb.WriteString(fmt.Sprintf("  Resume: /resume <number>\n"))
	m.sysMsg(sb.String())
	return m, nil
}

func handleResumeCommand(m Model, _ string, parts []string) (Model, tea.Cmd) {
	if len(parts) < 2 {
		m.sysMsg("Usage: /resume <number>  (use /sessions to list)")
		return m, nil
	}

	var num int
	if _, err := fmt.Sscanf(parts[1], "%d", &num); err != nil || num < 1 {
		m.sysMsg(fmt.Sprintf("Invalid session number: %s", parts[1]))
		return m, nil
	}

	sessions, err := session.List(m.config.SessionDir)
	if err != nil || len(sessions) == 0 {
		m.sysMsg("No saved sessions.")
		return m, nil
	}
	if num > len(sessions) {
		m.sysMsg(fmt.Sprintf("Session %d not found. %d sessions available.", num, len(sessions)))
		return m, nil
	}

	if m.streaming && m.agent != nil {
		m.cancelActiveTurn("Session resume", true)
	}

	sess, err := session.Load(sessions[num-1])
	if err != nil {
		m.sysMsg(fmt.Sprintf("Error loading session: %v", err))
		return m, nil
	}

	m.ResumeSession(sess)
	m.sysMsg(fmt.Sprintf("Resumed session %d: %s", num, filepath.Base(sessions[num-1])))
	return m, nil
}

func handleTreeCommand(m Model, _ string, _ []string) (Model, tea.Cmd) {
	if m.session == nil {
		m.sysMsg("No active session.")
		return m, nil
	}
	m.addMessage(ChatMessage{
		Time:    time.Now(),
		Type:    MsgRaw,
		Content: "\033[1;36m" + m.session.Tree() + "\033[0m",
	})
	m.refreshViewport()
	return m, nil
}

func handleForkCommand(m Model, _ string, _ []string) (Model, tea.Cmd) {
	if m.session == nil || len(m.session.Entries) == 0 {
		m.sysMsg("No session to fork from.")
		return m, nil
	}
	lastID := m.session.Entries[len(m.session.Entries)-1].ID
	newSess, err := m.session.Fork(lastID)
	if err != nil {
		m.errMsg(fmt.Sprintf("Fork failed: %v", err))
		return m, nil
	}
	m.session = newSess
	m.sysMsg(fmt.Sprintf("Forked to new session: %s", newSess.Path))
	return m, nil
}

func handleBaseURLCommand(m Model, _ string, parts []string) (Model, tea.Cmd) {
	if len(parts) < 2 {
		m.sysMsg(fmt.Sprintf("Base URL: %s\nUsage: /set baseurl <url>", m.config.BaseURL))
		return m, nil
	}
	url := parts[1]
	m.agent.SetBaseURL(url)
	m.config.BaseURL = url
	clearLoadedProfile(&m)
	m.sysMsg(fmt.Sprintf("%s\n  requests -> %s", setValueChangedMsg("baseurl", url), transportEndpointPreview(m.config.Provider, url)))
	if note := providerTransportHint(m.config.Provider, url); note != "" {
		m.sysMsg(note)
	}
	if note := serviceMisconfigHint(m.config.Service, m.config.Provider, url); note != "" {
		m.sysMsg(note)
	}
	return m, nil
}

func handleAPIKeyCommand(m Model, _ string, parts []string) (Model, tea.Cmd) {
	if len(parts) < 2 {
		masked := m.config.APIKey
		if len(masked) > 8 {
			masked = masked[:4] + "..." + masked[len(masked)-4:]
		}
		m.sysMsg(fmt.Sprintf("API Key: %s\nUsage: /set apikey <key>", masked))
		return m, nil
	}

	key := parts[1]
	m.agent.SetAPIKey(key)
	m.config.APIKey = key
	clearLoadedProfile(&m)
	masked := maskSecret(key)
	m.sysMsg(setValueChangedMsg("apikey", masked))
	return m, nil
}

func handleProviderCommand(m Model, _ string, parts []string) (Model, tea.Cmd) {
	if len(parts) < 2 {
		m.sysMsg(fmt.Sprintf("Provider: %s\nUsage: /set provider <openai|anthropic>", m.config.Provider))
		return m, nil
	}
	prov := parts[1]
	m.config.Provider = prov
	m.agent.SetProvider(prov)
	clearLoadedProfile(&m)
	m.sysMsg(fmt.Sprintf("%s\n  requests -> %s", setValueChangedMsg("provider", prov), transportEndpointPreview(prov, m.config.BaseURL)))
	if note := providerTransportHint(prov, m.config.BaseURL); note != "" {
		m.sysMsg(note)
	}
	if note := serviceMisconfigHint(m.config.Service, prov, m.config.BaseURL); note != "" {
		m.sysMsg(note)
	}
	return m, nil
}

func handleProfileCommand(m Model, _ string, parts []string) (Model, tea.Cmd) {
	if len(parts) < 2 {
		names := config.ListProfiles()
		m.sysMsg("Profiles: " + strings.Join(names, ", ") +
			"\nUsage: /profile save <name> | /profile load <name> | /profile show <name> | /profile delete <name>")
		return m, nil
	}

	action := parts[1]
	switch action {
	case "show":
		if len(parts) < 3 {
			m.errMsg("Usage: /profile show <name>")
			return m, nil
		}
		name := parts[2]
		p, err := config.ResolveProfile(name)
		if err != nil {
			m.errMsg(profileLookupMessage(name))
			return m, nil
		}
		m.sysMsg(fmt.Sprintf("Profile: %s\n  provider=%s service=%s model=%s\n  baseurl=%s\n  endpoint=%s\n  apikey=%s",
			name, p.Provider, serviceDisplay(p.Service), p.Model, p.BaseURL,
			transportEndpointPreview(p.Provider, p.BaseURL), maskSecret(p.APIKey)))
		return m, nil
	case "save":
		if len(parts) < 3 {
			m.errMsg("Usage: /profile save <name>")
			return m, nil
		}
		name := parts[2]
		p := config.Profile{
			Name:     name,
			Provider: m.config.Provider,
			Service:  m.config.Service,
			BaseURL:  m.config.BaseURL,
			APIKey:   m.config.APIKey,
			Model:    m.config.Model,
		}
		if err := config.SaveProfile(p); err != nil {
			m.errMsg(fmt.Sprintf("Save failed: %v", err))
		} else {
			m.sysMsg(fmt.Sprintf("*** Profile saved: %s (provider=%s service=%s model=%s)",
				name, p.Provider, serviceDisplay(p.Service), p.Model))
		}
	case "load":
		if len(parts) < 3 {
			m.errMsg("Usage: /profile load <name>")
			return m, nil
		}
		name := parts[2]
		p, err := config.ResolveProfile(name)
		if err != nil {
			m.errMsg(profileLookupMessage(name))
			return m, nil
		}
		applyProfileToModel(&m, name, p, true)
	case "delete":
		if len(parts) < 3 {
			m.errMsg("Usage: /profile delete <name>")
			return m, nil
		}
		name := parts[2]
		if err := config.DeleteProfile(name); err != nil {
			m.errMsg(fmt.Sprintf("Delete failed: %v", err))
		} else {
			m.sysMsg(fmt.Sprintf("*** Profile deleted: %s", name))
		}
	default:
		p, err := config.ResolveProfile(action)
		if err != nil {
			m.errMsg(profileLookupMessage(action))
			return m, nil
		}
		applyProfileToModel(&m, action, p, false)
	}
	return m, nil
}

// handleJoinCommand joins a channel context and persists the change.
// Usage: /join #channel  or  /join channel
func handleJoinCommand(m Model, _ string, parts []string) (Model, tea.Cmd) {
	if len(parts) < 2 {
		m.errMsg("Usage: /join <#channel>")
		return m, nil
	}
	ctx := Channel(parts[1])
	m.focus.SetFocus(ctx)
	m.syncAgentContextIfIdle(ctx)
	if err := m.focus.Save(m.config.SessionDir); err != nil {
		m.errMsg(fmt.Sprintf("focus save: %v", err))
		return m, nil
	}
	m.sysMsg(fmt.Sprintf("Joined %s", ctx.Label()))
	return m, nil
}

// handlePartCommand leaves the current or a named context and persists.
// Usage: /part  or  /part #channel  or  /part persona
func handlePartCommand(m Model, _ string, parts []string) (Model, tea.Cmd) {
	var target IRCContext
	if len(parts) >= 2 {
		name := parts[1]
		if strings.HasPrefix(name, "#") {
			target = Channel(name)
		} else {
			// Try direct first; caller can always be explicit with #
			target = Direct(name)
		}
	} else {
		target = m.focus.Active()
	}

	if !m.focus.Remove(target) {
		if len(m.focus.All()) <= 1 {
			m.errMsg("Can't part the last context.")
		} else {
			m.errMsg(fmt.Sprintf("Not in context %s.", target.Label()))
		}
		return m, nil
	}
	m.syncAgentContextIfIdle(m.focus.Active())
	if err := m.focus.Save(m.config.SessionDir); err != nil {
		m.errMsg(fmt.Sprintf("focus save: %v", err))
		return m, nil
	}
	m.sysMsg(fmt.Sprintf("Parted %s — now in %s", target.Label(), m.focus.ActiveLabel()))
	return m, nil
}

// handleQueryCommand opens a direct-message context to a persona and persists.
// Usage: /query <persona>
func handleQueryCommand(m Model, _ string, parts []string) (Model, tea.Cmd) {
	if len(parts) < 2 {
		m.errMsg("Usage: /query <persona>")
		return m, nil
	}
	ctx := Direct(parts[1])
	m.focus.SetFocus(ctx)
	m.syncAgentContextIfIdle(ctx)
	if err := m.focus.Save(m.config.SessionDir); err != nil {
		m.errMsg(fmt.Sprintf("focus save: %v", err))
		return m, nil
	}
	m.sysMsg(fmt.Sprintf("Query open: %s", ctx.Label()))
	return m, nil
}

// handleChannelsCommand lists all open contexts with their members.
func handleChannelsCommand(m Model, _ string, _ []string) (Model, tea.Cmd) {
	all := m.focus.All()
	active := m.focus.Active()
	var sb strings.Builder
	sb.WriteString("Open contexts:\n")
	for _, ctx := range all {
		marker := "  "
		if ctx == active {
			marker = "* "
		}
		sb.WriteString(marker + ctx.Label())
		if key, ok := channelKeyFromCtx(ctx); ok {
			if members := m.membership.Members(key); len(members) > 0 {
				sb.WriteString(" [" + strings.Join(members, ", ") + "]")
			}
		}
		sb.WriteString("\n")
	}
	m.addMessage(ChatMessage{
		Time:    time.Now(),
		Type:    MsgSystem,
		Content: strings.TrimRight(sb.String(), "\n"),
	})
	m.refreshViewport()
	return m, nil
}

func handleMsgCommand(m Model, input string, parts []string) (Model, tea.Cmd) {
	if len(parts) < 3 {
		m.errMsg("Usage: /msg <nick> <text>")
		return m, nil
	}
	nick := parts[1]
	prefix := parts[0] + " " + parts[1] + " "
	text := strings.TrimSpace(strings.TrimPrefix(input, prefix))
	if text == "" {
		m.errMsg("Usage: /msg <nick> <text>")
		return m, nil
	}
	if m.streaming {
		m.queued = append(m.queued, queuedMsg{text: fmt.Sprintf("[to:%s] %s", nick, text), queuedAt: time.Now()})
		m.sysMsg(fmt.Sprintf("Queued /msg to %s (agent busy).", nick))
		return m, nil
	}
	m.addMessage(ChatMessage{
		Time:    time.Now(),
		Type:    MsgUser,
		Nick:    m.config.UserNick,
		Content: fmt.Sprintf("→%s: %s", nick, text),
	})
	m.refreshViewport()
	return m, m.sendToAgent(fmt.Sprintf("[to:%s] %s", nick, text))
}

func applyProfileToModel(m *Model, name string, p *config.Profile, verbose bool) {
	config.ApplyProfile(m.config, p)
	m.config.Profile = name

	// Order matters: Service controls provider routing inside buildProvider
	// (openai+service="cliproxyapi" → openaicompat instead of openai-direct),
	// so push it FIRST so subsequent invalidations all rebuild against the
	// right routing. SetAPIKey runs last so the freshly-built transport is
	// guaranteed to carry the new key. bt-vwm.
	m.agent.SetService(p.Service)
	m.agent.SetProvider(p.Provider)
	m.agent.SetModel(p.Model)
	m.agent.SetBaseURL(p.BaseURL)
	m.agent.SetAPIKey(p.APIKey)

	masked := maskSecret(p.APIKey)
	if verbose {
		m.sysMsg(fmt.Sprintf("*** Profile loaded: %s\n  provider=%s service=%s model=%s\n  baseurl=%s\n  endpoint=%s\n  apikey=%s",
			name, p.Provider, serviceDisplay(p.Service), p.Model, p.BaseURL,
			transportEndpointPreview(p.Provider, p.BaseURL), masked))
	} else {
		m.sysMsg(fmt.Sprintf("*** Profile loaded: %s (provider=%s service=%s model=%s)",
			name, p.Provider, serviceDisplay(p.Service), p.Model))
	}
	if p.APIKey == "" {
		m.sysMsg("This profile did not provide an API key. Set one with /set apikey <key> or the matching env var before connecting.")
	}
	if note := providerTransportHint(p.Provider, p.BaseURL); note != "" {
		m.sysMsg(note)
	}
}

func clearLoadedProfile(m *Model) {
	m.config.Profile = ""
}

// handleServiceSet writes the user-supplied service identity verbatim. Per the
// bt-fnt convention used for /set provider, /set baseurl, etc., no validation
// is performed — any string is accepted, including arbitrary proxy labels.
// Service is informational metadata used to gate per-service behavior (cache
// control, reasoning forwarding, ...); see docs/phase-9-service-identity.md.
func handleServiceSet(m Model, value string) (Model, tea.Cmd) {
	if value == "" {
		m.errMsg("Usage: /set service <value>")
		return m, nil
	}
	m.config.Service = value
	// Don't clear cfg.Profile here — relabeling Service is a metadata edit, not
	// a transport switch. Provider/baseurl/apikey already drop the profile tag.
	m.sysMsg(setValueChangedMsg("service", value))
	if note := serviceMisconfigHint(value, m.config.Provider, m.config.BaseURL); note != "" {
		m.sysMsg(note)
	}
	return m, nil
}

// setKeysWithService returns the canonical /set key list with "service"
// appended. Kept here (rather than in config.SetKeys) because /set service
// is handled UI-side via verbatim routing, like /set provider.
func setKeysWithService() []string {
	keys := config.SetKeys()
	return append(keys, "service")
}

// serviceDisplay renders cfg.Service for /set output. Built-ins fill the
// field on load and DetectProvider sets it for env-detected providers, so an
// empty value here means a hand-rolled config that hasn't been derived yet.
func serviceDisplay(value string) string {
	if value == "" {
		return "<unset>"
	}
	return value
}

func maskSecret(value string) string {
	if strings.TrimSpace(value) == "" {
		return "<unset>"
	}
	if len(value) <= 8 {
		return value
	}
	return value[:4] + "..." + value[len(value)-4:]
}

func profileLookupMessage(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "openai" || name == "anthropic" {
		return fmt.Sprintf("%s is a provider, not a profile. Use /set provider %s or /profile to list profiles.", name, name)
	}
	return fmt.Sprintf("Unknown profile %q. Use /profile load <name> or /profile to list profiles.", name)
}

func transportEndpointPreview(provider, baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	switch provider {
	case "anthropic":
		return baseURL + "/messages"
	default:
		return baseURL + "/chat/completions"
	}
}

func providerTransportHint(provider, baseURL string) string {
	lowerURL := strings.TrimRight(strings.TrimSpace(strings.ToLower(baseURL)), "/")
	if lowerURL == "" {
		return ""
	}

	var warnings []string
	switch {
	case strings.HasSuffix(lowerURL, "/chat/completions"):
		warnings = append(warnings, "warning -> base URL already includes /chat/completions; omit /chat/completions because bitchtea appends endpoint paths automatically.")
	case strings.HasSuffix(lowerURL, "/messages"):
		warnings = append(warnings, "warning -> base URL already includes /messages; omit /messages because bitchtea appends endpoint paths automatically.")
	}

	looksLocal := strings.Contains(lowerURL, "localhost") || strings.Contains(lowerURL, "127.0.0.1")
	looksAnthropic := strings.Contains(lowerURL, "anthropic")
	looksGenericV1 := strings.Contains(lowerURL, "/v1")
	looksOpenAIStyle := strings.Contains(lowerURL, "api.openai.com") || strings.Contains(lowerURL, "openrouter.ai") || strings.Contains(lowerURL, "/openai/")

	if typo := cliproxyapiPortTypoHint(lowerURL); typo != "" {
		warnings = append(warnings, typo)
	}

	switch strings.TrimSpace(strings.ToLower(provider)) {
	case "anthropic":
		if looksCLIProxyAPILocal(lowerURL) {
			warnings = append(warnings, "warning -> anthropic transport against a CLIProxyAPI-shaped local URL is wrong. cliproxyapi is OpenAI-compatible: switch with /set provider openai (or /profile load cliproxyapi).")
		} else if looksOpenAIStyle {
			warnings = append(warnings, "warning -> Anthropic transport with an OpenAI-style base URL looks suspicious. Requests will go to /messages. If this endpoint is OpenAI-compatible, switch with /set provider openai.")
		} else if !looksAnthropic && (looksLocal || looksGenericV1) {
			warnings = append(warnings, "warning -> anthropic transport sends requests to /messages. If this server is OpenAI-compatible, switch with /set provider openai.")
		}
	case "openai":
		if looksAnthropic {
			warnings = append(warnings, "warning -> openai transport sends requests to /chat/completions. If this endpoint is Anthropic-compatible, switch with /set provider anthropic.")
		}
	}

	return strings.Join(warnings, "\n")
}

// looksCLIProxyAPILocal reports whether lowerURL looks like a CLIProxyAPI
// daemon endpoint — local host on the canonical 8317 port. Used to flag
// provider=anthropic combos that will silently route to /messages against an
// OpenAI-compatible upstream.
func looksCLIProxyAPILocal(lowerURL string) bool {
	local := strings.Contains(lowerURL, "localhost") || strings.Contains(lowerURL, "127.0.0.1")
	return local && strings.Contains(lowerURL, ":8317")
}

// cliproxyapiPortTypoHint surfaces a hint when the base URL looks like a
// transposed-digit typo of the canonical CLIProxyAPI port (8317). Common
// transpositions LO has hit: 8713, 8137, 8371, 8731. Also flags 8713
// specifically because the ticket calls it out as the recurring miss.
func cliproxyapiPortTypoHint(lowerURL string) string {
	if !strings.Contains(lowerURL, "localhost") && !strings.Contains(lowerURL, "127.0.0.1") {
		return ""
	}
	for _, typo := range []string{":8713", ":8137", ":8371", ":8731"} {
		if strings.Contains(lowerURL, typo) {
			return fmt.Sprintf("warning -> port %s looks like a typo for :8317 (CLIProxyAPI canonical). If you meant cliproxyapi, /profile load cliproxyapi resets it.", strings.TrimPrefix(typo, ":"))
		}
	}
	return ""
}

// serviceMisconfigHint flags combinations of (service, provider, baseURL)
// that route requests in ways the user almost certainly did not intend.
// Returned string is empty when nothing looks wrong. Surfaced from /set
// service, /set baseurl, /set provider, and /profile load so the warning
// fires regardless of which key the user touched last.
func serviceMisconfigHint(service, provider, baseURL string) string {
	service = strings.TrimSpace(strings.ToLower(service))
	provider = strings.TrimSpace(strings.ToLower(provider))
	lowerURL := strings.TrimRight(strings.TrimSpace(strings.ToLower(baseURL)), "/")
	if service == "" {
		return ""
	}

	var warnings []string
	switch service {
	case "cliproxyapi":
		if provider == "anthropic" {
			warnings = append(warnings, "warning -> service=cliproxyapi expects provider=openai (CLIProxyAPI is OpenAI-compatible). Switch with /set provider openai.")
		}
		if lowerURL != "" && !strings.Contains(lowerURL, "127.0.0.1") && !strings.Contains(lowerURL, "localhost") {
			warnings = append(warnings, "warning -> service=cliproxyapi normally points at a local daemon (http://127.0.0.1:8317/v1). Current baseurl is remote — confirm the proxy is reachable.")
		}
	case "ollama":
		if provider == "anthropic" {
			warnings = append(warnings, "warning -> service=ollama expects provider=openai (Ollama exposes an OpenAI-compatible API). Switch with /set provider openai.")
		}
	case "openrouter":
		if provider == "anthropic" {
			warnings = append(warnings, "warning -> service=openrouter expects provider=openai (OpenRouter speaks OpenAI). Switch with /set provider openai.")
		}
	case "zai-anthropic":
		if provider == "openai" {
			warnings = append(warnings, "warning -> service=zai-anthropic expects provider=anthropic. Switch with /set provider anthropic.")
		}
	}
	return strings.Join(warnings, "\n")
}

// handleModelsCommand opens a fuzzy picker over the model IDs for the active
// service. Wiring:
//
//   - Resolve the join key from cfg.Service (per docs/phase-9-service-identity.md
//     and docs/phase-5-catalog-audit.md "join on Service ↔ InferenceProvider").
//   - Load the catalog via the package-level loadModelCatalog seam (cache or
//     embedded fallback — never blocks, never errors).
//   - Surface a clear error if the service is unset or no provider matches.
//   - On selection, route through agent.SetModel + clear the loaded profile
//     tag (mirroring /set model semantics).
func handleModelsCommand(m Model, _ string, _ []string) (Model, tea.Cmd) {
	service := strings.TrimSpace(m.config.Service)
	if service == "" {
		m.errMsg("models: no active service — set one with /set service <name> or load a profile (e.g. /profile openrouter).")
		return m, nil
	}

	env := loadModelCatalog()
	if len(env.Providers) == 0 {
		m.errMsg("models: catalog is empty — try BITCHTEA_CATWALK_AUTOUPDATE=true with BITCHTEA_CATWALK_URL set, or wait for the embedded snapshot.")
		return m, nil
	}

	ids := modelsForService(env.Providers, service)
	if len(ids) == 0 {
		hint := ""
		if names := availableServices(env.Providers); len(names) > 0 {
			hint = "\n  available services: " + strings.Join(names, ", ")
		}
		m.errMsg(fmt.Sprintf(
			"models: no catalog data for service %q — try BITCHTEA_CATWALK_AUTOUPDATE=true or check /profile.%s",
			service, hint,
		))
		return m, nil
	}

	title := fmt.Sprintf("models for %s (%d total) — type to filter", service, len(ids))
	picker := newModelPicker(title, ids)

	m.openPicker(picker, applyModelSelection)
	return m, nil
}

// applyModelSelection is the picker callback for /models. It mirrors
// /set model semantics: route through the agent's cache-invalidating setter
// and drop the loaded profile tag so the topbar reflects the manual override.
func applyModelSelection(m *Model, choice string) {
	m.agent.SetModel(choice)
	clearLoadedProfile(m)
	m.sysMsg(setValueChangedMsg("model", choice))
}
