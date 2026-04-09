package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jstamagal/bitchtea/internal/agent"
	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/llm"
	"github.com/jstamagal/bitchtea/internal/session"
	"github.com/jstamagal/bitchtea/internal/sound"
)

type slashCommandHandler func(Model, string, []string) (Model, tea.Cmd)

type slashCommandSpec struct {
	names   []string
	handler slashCommandHandler
}

var slashCommandRegistry = registerSlashCommands(
	slashCommandSpec{names: []string{"/quit", "/q", "/exit"}, handler: handleQuitCommand},
	slashCommandSpec{names: []string{"/help", "/h"}, handler: handleHelpCommand},
	slashCommandSpec{names: []string{"/model"}, handler: handleModelCommand},
	slashCommandSpec{names: []string{"/clear"}, handler: handleClearCommand},
	slashCommandSpec{names: []string{"/compact"}, handler: handleCompactCommand},
	slashCommandSpec{names: []string{"/diff"}, handler: handleDiffCommand},
	slashCommandSpec{names: []string{"/status"}, handler: handleStatusCommand},
	slashCommandSpec{names: []string{"/undo"}, handler: handleUndoCommand},
	slashCommandSpec{names: []string{"/commit"}, handler: handleCommitCommand},
	slashCommandSpec{names: []string{"/copy"}, handler: handleCopyCommand},
	slashCommandSpec{names: []string{"/tokens"}, handler: handleTokensCommand},
	slashCommandSpec{names: []string{"/auto-next"}, handler: handleAutoNextCommand},
	slashCommandSpec{names: []string{"/auto-idea"}, handler: handleAutoIdeaCommand},
	slashCommandSpec{names: []string{"/debug"}, handler: handleDebugCommand},
	slashCommandSpec{names: []string{"/sound"}, handler: handleSoundCommand},
	slashCommandSpec{names: []string{"/activity"}, handler: handleActivityCommand},
	slashCommandSpec{names: []string{"/mp3"}, handler: handleMP3Command},
	slashCommandSpec{names: []string{"/theme"}, handler: handleThemeCommand},
	slashCommandSpec{names: []string{"/memory"}, handler: handleMemoryCommand},
	slashCommandSpec{names: []string{"/sessions", "/ls"}, handler: handleSessionsCommand},
	slashCommandSpec{names: []string{"/tree"}, handler: handleTreeCommand},
	slashCommandSpec{names: []string{"/fork"}, handler: handleForkCommand},
	slashCommandSpec{names: []string{"/baseurl"}, handler: handleBaseURLCommand},
	slashCommandSpec{names: []string{"/apikey"}, handler: handleAPIKeyCommand},
	slashCommandSpec{names: []string{"/provider"}, handler: handleProviderCommand},
	slashCommandSpec{names: []string{"/profile"}, handler: handleProfileCommand},
	// IRC routing commands (Phase 1b)
	slashCommandSpec{names: []string{"/join"}, handler: handleJoinCommand},
	slashCommandSpec{names: []string{"/part"}, handler: handlePartCommand},
	slashCommandSpec{names: []string{"/query"}, handler: handleQueryCommand},
	slashCommandSpec{names: []string{"/msg"}, handler: handleMsgCommand},
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
	"  /model <name>       Switch LLM model\n" +
	"  /provider <name>    Set provider transport (openai, anthropic)\n" +
	"  /baseurl <url>      Set API base URL\n" +
	"  /apikey <key>       Set API key\n" +
	"  /profile [cmd]      save/load/delete profiles (built-ins: ollama, openrouter, huggingface, xai, copilot, etc.)\n" +
	"  /compact            Compact conversation context\n" +
	"  /clear              Clear chat display\n" +
	"  /diff               Show git diff\n" +
	"  /undo [confirm|file] Preview revert, confirm all, or revert one file\n" +
	"  /commit [msg]       Preview git state or commit tracked changes only\n" +
	"  /copy [n]           Copy last or nth assistant response\n" +
	"  /status             Git status\n" +
	"  /tokens             Token usage estimate\n" +
	"  /memory             Show MEMORY.md contents\n" +
	"  /sessions           List saved sessions\n" +
	"  /tree               Show session tree\n" +
	"  /fork               Fork session\n" +
	"  /auto-next          Toggle auto-next-steps\n" +
	"  /auto-idea          Toggle auto-next-idea\n" +
	"  /debug on|off       Toggle verbose API logging\n" +
	"  /sound              Toggle completion bell\n" +
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

func handleDiffCommand(m Model, _ string, _ []string) (Model, tea.Cmd) {
	output := runGit(m.config.WorkDir, "diff")
	if output == "" {
		output = "No changes. Clean as a whistle."
	}
	m.addMessage(ChatMessage{
		Time:    time.Now(),
		Type:    MsgRaw,
		Content: "\033[1;36m--- git diff ---\033[0m\n" + output,
	})
	m.refreshViewport()
	return m, nil
}

func handleStatusCommand(m Model, _ string, _ []string) (Model, tea.Cmd) {
	output := runGit(m.config.WorkDir, "status", "--short")
	if output == "" {
		output = "Nothing to report. Working tree clean."
	}
	m.addMessage(ChatMessage{
		Time:    time.Now(),
		Type:    MsgRaw,
		Content: "\033[1;36m--- git status ---\033[0m\n" + output,
	})
	m.refreshViewport()
	return m, nil
}

func handleUndoCommand(m Model, input string, parts []string) (Model, tea.Cmd) {
	switch {
	case len(parts) == 1:
		preview := gitUndoPreview(m.config.WorkDir)
		m.addMessage(ChatMessage{
			Time:    time.Now(),
			Type:    MsgRaw,
			Content: "\033[1;36m--- /undo preview ---\033[0m\n" + preview,
		})
		m.refreshViewport()
	case len(parts) == 2 && parts[1] == "confirm":
		output := runGit(m.config.WorkDir, "restore", "--worktree", "--", ".")
		if output == "" {
			output = "Reverted all unstaged tracked changes."
		} else {
			output = "Reverted all unstaged tracked changes.\n" + output
		}
		m.sysMsg(output)
	default:
		target := strings.TrimSpace(strings.TrimPrefix(input, parts[0]))
		output := runGit(m.config.WorkDir, "restore", "--worktree", "--", target)
		if output == "" {
			output = fmt.Sprintf("Reverted unstaged changes for %s.", target)
		} else {
			output = fmt.Sprintf("Reverted unstaged changes for %s.\n%s", target, output)
		}
		m.sysMsg(output)
	}
	return m, nil
}

func handleCommitCommand(m Model, input string, parts []string) (Model, tea.Cmd) {
	if len(parts) == 1 {
		preview := gitCommitPreview(m.config.WorkDir)
		m.addMessage(ChatMessage{
			Time:    time.Now(),
			Type:    MsgRaw,
			Content: "\033[1;36m--- /commit preview ---\033[0m\n" + preview,
		})
		m.refreshViewport()
		return m, nil
	}

	msg := strings.TrimSpace(strings.TrimPrefix(input, parts[0]))
	if msg == "" {
		m.errMsg("Usage: /commit <message>")
		return m, nil
	}

	runGit(m.config.WorkDir, "add", "-u")
	output := runGit(m.config.WorkDir, "commit", "-m", msg)
	if strings.Contains(output, "nothing to commit") || strings.Contains(output, "no changes added to commit") {
		m.sysMsg("Nothing to commit. Only tracked changes are staged by /commit.")
		return m, nil
	}
	m.sysMsg("Committed: " + output)
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

func handleAutoNextCommand(m Model, _ string, _ []string) (Model, tea.Cmd) {
	m.config.AutoNextSteps = !m.config.AutoNextSteps
	status := "OFF"
	if m.config.AutoNextSteps {
		status = "ON"
	}
	m.sysMsg(fmt.Sprintf("Auto-next-steps: %s", status))
	return m, nil
}

func handleAutoIdeaCommand(m Model, _ string, _ []string) (Model, tea.Cmd) {
	m.config.AutoNextIdea = !m.config.AutoNextIdea
	status := "OFF"
	if m.config.AutoNextIdea {
		status = "ON"
	}
	m.sysMsg(fmt.Sprintf("Auto-next-idea: %s", status))
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

func handleSoundCommand(m Model, _ string, _ []string) (Model, tea.Cmd) {
	m.config.NotificationSound = !m.config.NotificationSound
	status := "OFF"
	if m.config.NotificationSound {
		status = "ON"
		sound.Play(m.config.SoundType)
	}
	m.sysMsg(fmt.Sprintf("Notification sound: %s", status))
	return m, nil
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
	mem := agent.LoadMemory(m.config.WorkDir)
	if mem == "" {
		m.sysMsg("No MEMORY.md found in working directory.")
		return m, nil
	}
	if len(mem) > 1000 {
		mem = mem[:1000] + "\n... (truncated)"
	}
	m.addMessage(ChatMessage{
		Time:    time.Now(),
		Type:    MsgRaw,
		Content: "\033[1;36m--- MEMORY.md ---\033[0m\n" + mem,
	})
	m.refreshViewport()
	return m, nil
}

func handleSessionsCommand(m Model, _ string, _ []string) (Model, tea.Cmd) {
	sessions, err := session.List(m.config.SessionDir)
	if err != nil || len(sessions) == 0 {
		m.sysMsg("No saved sessions.")
		return m, nil
	}
	var sb strings.Builder
	sb.WriteString("Sessions:\n")
	limit := len(sessions)
	if limit > 15 {
		limit = 15
	}
	for i, sess := range sessions[:limit] {
		sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, session.Info(sess)))
	}
	if len(sessions) > 15 {
		sb.WriteString(fmt.Sprintf("  ... and %d more\n", len(sessions)-15))
	}
	m.sysMsg(sb.String())
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
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		m.errMsg(fmt.Sprintf("Invalid URL %q. Must start with http:// or https://.", url))
		return m, nil
	}
	m.agent.SetBaseURL(url)
	m.config.BaseURL = url
	m.sysMsg(fmt.Sprintf("*** Base URL set to: %s", url))
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
	masked := key
	if len(masked) > 8 {
		masked = masked[:4] + "..." + masked[len(masked)-4:]
	}
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
	m.sysMsg(fmt.Sprintf("*** Provider set to: %s", prov))
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
			m.errMsg(fmt.Sprintf("Load failed: %v", err))
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
			m.errMsg(fmt.Sprintf("Unknown profile action or profile not found: %s", action))
			return m, nil
		}
		applyProfileToModel(&m, action, p, false)
	}
	return m, nil
}

// --- IRC routing commands (Phase 1b) ---

func handleJoinCommand(m Model, _ string, parts []string) (Model, tea.Cmd) {
	if len(parts) < 2 {
		m.errMsg("Usage: /join <#channel>")
		return m, nil
	}
	ctx := Channel(parts[1])
	m.focus.SetFocus(ctx)
	m.sysMsg(fmt.Sprintf("*** Now in %s", ctx.Label()))
	return m, nil
}

func handlePartCommand(m Model, _ string, parts []string) (Model, tea.Cmd) {
	var ctx IRCContext
	if len(parts) >= 2 {
		ctx = Channel(parts[1])
	} else {
		ctx = m.focus.Active()
	}
	label := ctx.Label()
	if !m.focus.Remove(ctx) {
		if len(m.focus.All()) <= 1 {
			m.errMsg("Can't part the last context.")
		} else {
			m.errMsg(fmt.Sprintf("Not in %s.", label))
		}
		return m, nil
	}
	m.sysMsg(fmt.Sprintf("*** Parted %s — now in %s", label, m.focus.ActiveLabel()))
	return m, nil
}

func handleQueryCommand(m Model, _ string, parts []string) (Model, tea.Cmd) {
	if len(parts) < 2 {
		m.errMsg("Usage: /query <nick>")
		return m, nil
	}
	ctx := Direct(parts[1])
	m.focus.SetFocus(ctx)
	m.sysMsg(fmt.Sprintf("*** Routing Enter to %s", ctx.Label()))
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

	if verbose {
		m.sysMsg(fmt.Sprintf("*** Profile loaded: %s (provider=%s model=%s)", name, p.Provider, p.Model))
	} else {
		m.sysMsg(fmt.Sprintf("*** Profile loaded: %s\n  provider=%s model=%s\n  baseurl=%s\n  apikey=%s",
			name, p.Provider, p.Model, p.BaseURL, p.APIKey))
	}

	m.agent.SetModel(p.Model)
	m.agent.SetBaseURL(p.BaseURL)
	m.agent.SetAPIKey(p.APIKey)
	m.agent.SetProvider(p.Provider)

	if !verbose {
		m.sysMsg(fmt.Sprintf("*** Profile loaded: %s (provider=%s model=%s)", name, p.Provider, p.Model))
		return
	}

	masked := p.APIKey
	if len(masked) > 8 {
		masked = masked[:4] + "..." + masked[len(masked)-4:]
	}
	m.sysMsg(fmt.Sprintf("*** Profile loaded: %s\n  provider=%s model=%s\n  baseurl=%s\n  apikey=%s",
		name, p.Provider, p.Model, p.BaseURL, masked))
}
