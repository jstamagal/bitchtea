package ui

import (
	"context"
	"fmt"
	neturl "net/url"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jstamagal/bitchtea/internal/agent"
	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/llm"
	"github.com/jstamagal/bitchtea/internal/session"
)

type slashCommandHandler func(Model, string, []string) (Model, tea.Cmd)

type slashCommandSpec struct {
	names   []string
	handler slashCommandHandler
}

var slashCommandRegistry = registerSlashCommands(
	slashCommandSpec{names: []string{"/quit", "/q", "/exit"}, handler: handleQuitCommand},
	slashCommandSpec{names: []string{"/help", "/h"}, handler: handleHelpCommand},
	slashCommandSpec{names: []string{"/set"}, handler: handleSetCommand},
	slashCommandSpec{names: []string{"/model"}, handler: handleModelCommand},
	slashCommandSpec{names: []string{"/clear"}, handler: handleClearCommand},
	slashCommandSpec{names: []string{"/restart"}, handler: handleRestartCommand},
	slashCommandSpec{names: []string{"/compact"}, handler: handleCompactCommand},
	slashCommandSpec{names: []string{"/copy"}, handler: handleCopyCommand},
	slashCommandSpec{names: []string{"/tokens"}, handler: handleTokensCommand},
	slashCommandSpec{names: []string{"/debug"}, handler: handleDebugCommand},
	slashCommandSpec{names: []string{"/activity"}, handler: handleActivityCommand},
	slashCommandSpec{names: []string{"/mp3"}, handler: handleMP3Command},
	slashCommandSpec{names: []string{"/theme"}, handler: handleThemeCommand},
	slashCommandSpec{names: []string{"/memory"}, handler: handleMemoryCommand},
	slashCommandSpec{names: []string{"/sessions", "/ls"}, handler: handleSessionsCommand},
	slashCommandSpec{names: []string{"/resume"}, handler: handleResumeCommand},
	slashCommandSpec{names: []string{"/tree"}, handler: handleTreeCommand},
	slashCommandSpec{names: []string{"/fork"}, handler: handleForkCommand},
	slashCommandSpec{names: []string{"/baseurl"}, handler: handleBaseURLCommand},
	slashCommandSpec{names: []string{"/apikey"}, handler: handleAPIKeyCommand},
	slashCommandSpec{names: []string{"/provider"}, handler: handleProviderCommand},
	slashCommandSpec{names: []string{"/profile"}, handler: handleProfileCommand},
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
	handler, ok := slashCommandRegistry[name]
	return handler, ok
}

const helpCommandText = "Commands:\n" +
	"  /join <#channel>    Switch focus to channel (creates if new)\n" +
	"  /part [#channel]    Leave context (default: current)\n" +
	"  /query <nick>       Route Enter persistently to nick\n" +
	"  /msg <nick> <text>  One-shot send to nick, no focus change\n" +
	"  /channels           List open contexts\n" +
	"  /set [key [value]]  Show or change a setting\n" +
	"                        keys: provider, model, baseurl, apikey, nick,\n" +
	"                              profile, sound, auto-next, auto-idea\n" +
	"  /profile [cmd]      save/load/delete profiles (built-ins: ollama, openrouter, etc.)\n" +
	"  /compact            Compact conversation context\n" +
	"  /clear              Clear chat display\n" +
	"  /restart            Reset agent and start a fresh conversation\n" +
	"  /copy [n]           Copy last or nth assistant response\n" +
	"  /tokens             Token usage estimate\n" +
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
			sb.WriteString(fmt.Sprintf("  %s = %s\n", key, value))
		}
		m.sysMsg(strings.TrimRight(sb.String(), "\n"))
		return m, nil
	}

	key := strings.ToLower(parts[1])
	if len(parts) == 2 {
		value, ok := config.GetSetting(m.config, key)
		if !ok {
			m.errMsg(fmt.Sprintf("Unknown setting %q. Valid keys: %s", key, strings.Join(config.SetKeys(), ", ")))
			return m, nil
		}
		m.sysMsg(fmt.Sprintf("%s = %s", key, value))
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
	}

	if !config.ApplySet(m.config, key, value) {
		m.errMsg(fmt.Sprintf("Unknown setting %q. Valid keys: %s", key, strings.Join(config.SetKeys(), ", ")))
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
		m.errMsg(fmt.Sprintf("Unknown setting %q. Valid keys: %s", key, strings.Join(config.SetKeys(), ", ")))
		return m, nil
	}

	display, _ := config.GetSetting(m.config, key)
	m.sysMsg(fmt.Sprintf("%s set to: %s", settingLabel(key), display))
	return m, nil
}

func settingLabel(key string) string {
	parts := strings.Split(key, "-")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func handleModelCommand(m Model, _ string, parts []string) (Model, tea.Cmd) {
	if len(parts) < 2 {
		m.addMessage(ChatMessage{
			Time:    time.Now(),
			Type:    MsgSystem,
			Content: fmt.Sprintf("Current model: %s. Usage: /model <name>", m.agent.Model()),
		})
		m.refreshViewport()
		return m, nil
	}

	newModel := parts[1]
	if (!strings.Contains(newModel, ".") && !strings.Contains(newModel, "-")) || len(newModel) < 3 || strings.Contains(newModel, " ") {
		m.addMessage(ChatMessage{
			Time:    time.Now(),
			Type:    MsgError,
			Content: fmt.Sprintf("Warning: model name %q looks suspicious. Expected something like gpt-4o, claude-3.5-sonnet, etc.", newModel),
		})
	}
	m.agent.SetModel(newModel)
	clearLoadedProfile(&m)
	m.addMessage(ChatMessage{
		Time:    time.Now(),
		Type:    MsgSystem,
		Content: fmt.Sprintf("*** Model switched to: %s", newModel),
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
		m.sysMsg(fmt.Sprintf("Base URL: %s\nUsage: /baseurl <url>", m.config.BaseURL))
		return m, nil
	}
	url := parts[1]
	parsed, err := neturl.Parse(url)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		m.errMsg(fmt.Sprintf("Invalid URL %q. Must start with http:// or https://.", url))
		return m, nil
	}
	m.agent.SetBaseURL(url)
	m.config.BaseURL = url
	clearLoadedProfile(&m)
	m.sysMsg(fmt.Sprintf("*** Base URL set to: %s\n  requests -> %s", url, transportEndpointPreview(m.config.Provider, url)))
	if note := providerTransportHint(m.config.Provider, url); note != "" {
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
		m.sysMsg(fmt.Sprintf("API Key: %s\nUsage: /apikey <key>", masked))
		return m, nil
	}

	key := parts[1]
	if len(key) < 10 {
		m.errMsg(fmt.Sprintf("API key too short (%d chars). Must be at least 10 characters.", len(key)))
		return m, nil
	}
	m.agent.SetAPIKey(key)
	m.config.APIKey = key
	clearLoadedProfile(&m)
	masked := maskSecret(key)
	m.sysMsg(fmt.Sprintf("*** API key set: %s", masked))
	return m, nil
}

func handleProviderCommand(m Model, _ string, parts []string) (Model, tea.Cmd) {
	if len(parts) < 2 {
		m.sysMsg(fmt.Sprintf("Provider: %s\nUsage: /provider <openai|anthropic>", m.config.Provider))
		return m, nil
	}
	prov := parts[1]
	if prov != "openai" && prov != "anthropic" {
		m.errMsg(fmt.Sprintf("Invalid provider %q. Must be openai or anthropic.", prov))
		return m, nil
	}
	m.config.Provider = prov
	m.agent.SetProvider(prov)
	clearLoadedProfile(&m)
	m.sysMsg(fmt.Sprintf("*** Provider set to: %s\n  requests -> %s", prov, transportEndpointPreview(prov, m.config.BaseURL)))
	if note := providerTransportHint(prov, m.config.BaseURL); note != "" {
		m.sysMsg(note)
	}
	return m, nil
}

func handleProfileCommand(m Model, _ string, parts []string) (Model, tea.Cmd) {
	if len(parts) < 2 {
		names := config.ListProfiles()
		m.sysMsg("Profiles: " + strings.Join(names, ", ") +
			"\nUsage: /profile save <name> | /profile load <name> | /profile delete <name>")
		return m, nil
	}

	action := parts[1]
	switch action {
	case "save":
		if len(parts) < 3 {
			m.errMsg("Usage: /profile save <name>")
			return m, nil
		}
		name := parts[2]
		p := config.Profile{
			Name:     name,
			Provider: m.config.Provider,
			BaseURL:  m.config.BaseURL,
			APIKey:   m.config.APIKey,
			Model:    m.config.Model,
		}
		if err := config.SaveProfile(p); err != nil {
			m.errMsg(fmt.Sprintf("Save failed: %v", err))
		} else {
			m.sysMsg(fmt.Sprintf("*** Profile saved: %s (provider=%s model=%s)", name, p.Provider, p.Model))
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
		m.queued = append(m.queued, fmt.Sprintf("[to:%s] %s", nick, text))
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

	m.agent.SetModel(p.Model)
	m.agent.SetBaseURL(p.BaseURL)
	m.agent.SetAPIKey(p.APIKey)
	m.agent.SetProvider(p.Provider)

	masked := maskSecret(p.APIKey)
	if verbose {
		m.sysMsg(fmt.Sprintf("*** Profile loaded: %s\n  provider=%s model=%s\n  baseurl=%s\n  endpoint=%s\n  apikey=%s",
			name, p.Provider, p.Model, p.BaseURL, transportEndpointPreview(p.Provider, p.BaseURL), masked))
	} else {
		m.sysMsg(fmt.Sprintf("*** Profile loaded: %s (provider=%s model=%s)", name, p.Provider, p.Model))
	}
	if p.APIKey == "" {
		m.sysMsg("This profile did not provide an API key. Set one with /apikey or the matching env var before connecting.")
	}
	if note := providerTransportHint(p.Provider, p.BaseURL); note != "" {
		m.sysMsg(note)
	}
}

func clearLoadedProfile(m *Model) {
	m.config.Profile = ""
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
		return fmt.Sprintf("%s is a provider, not a profile. Use /provider %s or /profile to list profiles.", name, name)
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

	switch strings.TrimSpace(strings.ToLower(provider)) {
	case "anthropic":
		if looksOpenAIStyle {
			warnings = append(warnings, "warning -> Anthropic transport with an OpenAI-style base URL looks suspicious. Requests will go to /messages. If this endpoint is OpenAI-compatible, switch with /provider openai.")
		} else if !looksAnthropic && (looksLocal || looksGenericV1) {
			warnings = append(warnings, "warning -> anthropic transport sends requests to /messages. If this server is OpenAI-compatible, switch with /provider openai.")
		}
	case "openai":
		if looksAnthropic {
			warnings = append(warnings, "warning -> openai transport sends requests to /chat/completions. If this endpoint is Anthropic-compatible, switch with /provider anthropic.")
		}
	}

	return strings.Join(warnings, "\n")
}
