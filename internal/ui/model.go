package ui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jstamagal/bitchtea/internal/agent"
	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/session"
)

// agentEventMsg wraps agent events for the bubbletea message loop
type agentEventMsg struct {
	event agent.Event
}

// agentDoneMsg signals the agent event channel is closed
type agentDoneMsg struct{}

// Model is the top-level bubbletea model
type Model struct {
	// Config
	config *config.Config

	// Agent
	agent      *agent.Agent
	agentState agent.State
	cancel     context.CancelFunc
	eventCh    chan agent.Event // channel for receiving agent events

	// UI components
	viewport viewport.Model
	input    textinput.Model
	spinner  spinner.Model

	// State
	messages     []ChatMessage
	viewContent  string // rendered viewport content
	width        int
	height       int
	ready        bool
	streaming    bool
	streamBuffer *strings.Builder // accumulates current agent response (pointer to avoid copy panic)

	// Input history
	history    []string
	historyIdx int

	// Queued messages (steering - typed while agent is working)
	queued []string

	// Stats
	toolStats map[string]int

	// Session
	session         *session.Session
	lastSavedMsgIdx int // index into agent.Messages() of last saved entry

	// Auto-next tracking
	autoNextPending bool
}

// NewModel creates the initial model
func NewModel(cfg *config.Config) Model {
	ti := textinput.New()
	ti.Placeholder = "type something, coward..."
	ti.Focus()
	ti.CharLimit = 4096
	ti.Width = 80
	ti.Prompt = ">> "
	ti.PromptStyle = InputPromptStyle
	ti.TextStyle = InputTextStyle

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(ColorMagenta)

	ag := agent.NewAgent(cfg)

	// Create session
	sess, _ := session.New(cfg.SessionDir)

	return Model{
		config:     cfg,
		agent:      ag,
		agentState: agent.StateIdle,
		input:      ti,
		spinner:    sp,
		messages:   []ChatMessage{},
		history:    []string{},
		historyIdx: -1,
		streamBuffer: &strings.Builder{},
		toolStats:    make(map[string]int),
		session:      sess,
	}
}

// ResumeSession loads a previous session's messages into the chat display
func (m *Model) ResumeSession(sess *session.Session) {
	m.session = sess
	for _, e := range sess.Entries {
		var msgType MsgType
		nick := ""
		switch e.Role {
		case "user":
			msgType = MsgUser
			nick = m.config.UserNick
		case "assistant":
			msgType = MsgAgent
			nick = m.config.AgentNick
		case "tool":
			msgType = MsgTool
			nick = e.ToolName
		case "system":
			msgType = MsgSystem
		default:
			msgType = MsgSystem
		}

		content := e.Content
		if len(content) > 500 {
			content = content[:500] + "... (truncated from session)"
		}

		m.messages = append(m.messages, ChatMessage{
			Time:    e.Timestamp,
			Type:    msgType,
			Nick:    nick,
			Content: content,
		})
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		m.spinner.Tick,
		tea.EnterAltScreen,
		m.showSplash(),
	)
}

func (m Model) showSplash() tea.Cmd {
	return func() tea.Msg {
		return splashMsg{}
	}
}

type splashMsg struct{}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = msg.Width - 6 // account for prompt

		// Layout: topbar(1) + sep(1) + viewport + sep(1) + statusbar(1) + sep(1) + input(1)
		vpHeight := msg.Height - 6
		if vpHeight < 1 {
			vpHeight = 1
		}

		if !m.ready {
			m.viewport = viewport.New(msg.Width, vpHeight)
			m.viewport.SetContent(m.viewContent)
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = vpHeight
		}
		m.refreshViewport()

	case splashMsg:
		// Show the splash screen
		m.addMessage(ChatMessage{Time: time.Now(), Type: MsgRaw, Content: SplashArt()})
		m.addMessage(ChatMessage{Time: time.Now(), Type: MsgRaw, Content: SplashTagline})
		m.addMessage(ChatMessage{Time: time.Now(), Type: MsgRaw, Content: fmt.Sprintf(ConnectMsg, m.config.Provider, m.config.Model, m.config.WorkDir)})

		// Show loaded context files
		ctxFiles := agent.DiscoverContextFiles(m.config.WorkDir)
		if ctxFiles != "" {
			// Count how many files were found
			count := strings.Count(ctxFiles, "# Context from")
			m.addMessage(ChatMessage{
				Time:    time.Now(),
				Type:    MsgSystem,
				Content: fmt.Sprintf("Loaded %d context file(s) from project tree", count),
			})
		}

		// Show memory status
		mem := agent.LoadMemory(m.config.WorkDir)
		if mem != "" {
			m.addMessage(ChatMessage{
				Time:    time.Now(),
				Type:    MsgSystem,
				Content: "Loaded MEMORY.md from working directory",
			})
		}

		if m.session != nil {
			m.addMessage(ChatMessage{
				Time:    time.Now(),
				Type:    MsgSystem,
				Content: fmt.Sprintf("Session: %s", m.session.Path),
			})
		}

		m.addMessage(ChatMessage{Time: time.Now(), Type: MsgRaw, Content: MOTD})
		m.refreshViewport()

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.streaming && m.cancel != nil {
				m.cancel()
				m.streaming = false
				m.agentState = agent.StateIdle
				m.addMessage(ChatMessage{
					Time:    time.Now(),
					Type:    MsgSystem,
					Content: "Interrupted. Like your attention span.",
				})
				m.refreshViewport()
				return m, nil
			}
			return m, tea.Quit

		case "enter":
			input := strings.TrimSpace(m.input.Value())
			if input == "" {
				return m, nil
			}

			m.input.SetValue("")

			// Save to history
			m.history = append(m.history, input)
			m.historyIdx = len(m.history)

			// Handle slash commands
			if strings.HasPrefix(input, "/") {
				return m.handleCommand(input)
			}

			// If agent is busy, queue the message (steering)
			if m.streaming {
				m.queued = append(m.queued, input)
				m.addMessage(ChatMessage{
					Time:    time.Now(),
					Type:    MsgSystem,
					Content: fmt.Sprintf("Queued message (agent is busy): %s", input),
				})
				m.refreshViewport()
				return m, nil
			}

			// Send to agent
			m.addMessage(ChatMessage{
				Time:    time.Now(),
				Type:    MsgUser,
				Nick:    m.config.UserNick,
				Content: input,
			})
			m.refreshViewport()

			return m, m.sendToAgent(input)

		case "up":
			if len(m.history) > 0 && m.historyIdx > 0 {
				m.historyIdx--
				m.input.SetValue(m.history[m.historyIdx])
				m.input.CursorEnd()
			}
			return m, nil

		case "down":
			if m.historyIdx < len(m.history)-1 {
				m.historyIdx++
				m.input.SetValue(m.history[m.historyIdx])
				m.input.CursorEnd()
			} else {
				m.historyIdx = len(m.history)
				m.input.SetValue("")
			}
			return m, nil

		case "pgup":
			m.viewport.ViewUp()
			return m, nil

		case "pgdown":
			m.viewport.ViewDown()
			return m, nil
		}

	case agentEventMsg:
		newModel, cmd := m.handleAgentEvent(msg.event)
		// Chain: after handling this event, wait for the next one
		if m.eventCh != nil {
			return newModel, tea.Batch(cmd, waitForAgentEvent(m.eventCh))
		}
		return newModel, cmd

	case agentDoneMsg:
		m.streaming = false
		m.agentState = agent.StateIdle
		m.eventCh = nil

		// Save new messages to session (incremental)
		if m.session != nil {
			msgs := m.agent.Messages()
			for i := m.lastSavedMsgIdx; i < len(msgs); i++ {
				_ = m.session.Append(session.Entry{
					Role:    msgs[i].Role,
					Content: msgs[i].Content,
				})
			}
			m.lastSavedMsgIdx = len(msgs)
		}

		// Process queued messages first
		if len(m.queued) > 0 {
			next := m.queued[0]
			m.queued = m.queued[1:]
			m.addMessage(ChatMessage{
				Time:    time.Now(),
				Type:    MsgUser,
				Nick:    m.config.UserNick,
				Content: next,
			})
			m.refreshViewport()
			return m, m.sendToAgent(next)
		}

		// Auto-next-steps
		if m.config.AutoNextSteps && !m.autoNextPending {
			m.autoNextPending = true
			prompt := agent.AutoNextPrompt()
			m.addMessage(ChatMessage{
				Time:    time.Now(),
				Type:    MsgSystem,
				Content: "*** auto-next-steps: continuing...",
			})
			m.refreshViewport()
			return m, m.sendToAgent(prompt)
		}

		// Auto-next-idea (only fires once after auto-next-steps completes)
		if m.config.AutoNextIdea && m.autoNextPending {
			m.autoNextPending = false
			prompt := agent.AutoIdeaPrompt()
			m.addMessage(ChatMessage{
				Time:    time.Now(),
				Type:    MsgSystem,
				Content: "*** auto-next-idea: brainstorming...",
			})
			m.refreshViewport()
			return m, m.sendToAgent(prompt)
		}

		m.autoNextPending = false
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	// Update input
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m *Model) handleAgentEvent(ev agent.Event) (tea.Model, tea.Cmd) {
	switch ev.Type {
	case "text":
		m.streamBuffer.WriteString(ev.Text)
		// Update the last agent message in-place for streaming effect
		m.updateStreamingMessage()
		m.refreshViewport()

	case "tool_start":
		m.addMessage(ChatMessage{
			Time:    time.Now(),
			Type:    MsgTool,
			Nick:    ev.ToolName,
			Content: fmt.Sprintf("calling %s...", ev.ToolName),
		})
		m.toolStats[ev.ToolName]++
		m.refreshViewport()

	case "tool_result":
		// Truncate tool output for display
		result := ev.ToolResult
		lines := strings.Split(result, "\n")
		if len(lines) > 20 {
			result = strings.Join(lines[:20], "\n") + fmt.Sprintf("\n... (%d more lines)", len(lines)-20)
		}
		msgType := MsgTool
		if ev.ToolError != nil {
			msgType = MsgError
		}
		m.addMessage(ChatMessage{
			Time:    time.Now(),
			Type:    msgType,
			Nick:    ev.ToolName,
			Content: result,
		})
		m.refreshViewport()

	case "state":
		m.agentState = ev.State
		if ev.State == agent.StateThinking {
			// Start a new streaming message
			m.streamBuffer.Reset()
			m.addMessage(ChatMessage{
				Time:    time.Now(),
				Type:    MsgAgent,
				Nick:    m.config.AgentNick,
				Content: "",
			})
		}

	case "error":
		m.addMessage(ChatMessage{
			Time:    time.Now(),
			Type:    MsgError,
			Content: fmt.Sprintf("Error: %v", ev.Error),
		})
		m.refreshViewport()

	case "done":
		return m, func() tea.Msg { return agentDoneMsg{} }
	}

	return m, nil
}

// waitForAgentEvent returns a Cmd that waits for the next event on the channel
func waitForAgentEvent(ch chan agent.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return agentDoneMsg{}
		}
		return agentEventMsg{event: ev}
	}
}

func (m *Model) sendToAgent(input string) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.streaming = true

	ch := make(chan agent.Event, 100)
	m.eventCh = ch
	go m.agent.SendMessage(ctx, input, ch)

	return waitForAgentEvent(ch)
}

func (m *Model) addMessage(msg ChatMessage) {
	m.messages = append(m.messages, msg)
}

func (m *Model) updateStreamingMessage() {
	if len(m.messages) == 0 {
		return
	}
	last := &m.messages[len(m.messages)-1]
	if last.Type == MsgAgent {
		last.Content = m.streamBuffer.String()
	}
}

func (m *Model) refreshViewport() {
	if !m.ready {
		return
	}

	var sb strings.Builder
	for _, msg := range m.messages {
		sb.WriteString(msg.Format())
		sb.WriteString("\n")
	}

	m.viewContent = sb.String()
	m.viewport.SetContent(m.viewContent)
	m.viewport.GotoBottom()
}

func (m Model) View() string {
	if !m.ready {
		return "initializing bitchtea..."
	}

	// Top bar
	flags := ""
	if m.config.AutoNextSteps {
		flags += " [auto]"
	}
	if m.config.AutoNextIdea {
		flags += " [idea]"
	}
	topLeft := TopBarStyle.Render(fmt.Sprintf(" bitchtea — %s%s ", m.config.Model, flags))
	topRight := TopBarStyle.Render(fmt.Sprintf(" %s ", time.Now().Format("3:04pm")))
	topPad := m.width - lipgloss.Width(topLeft) - lipgloss.Width(topRight)
	if topPad < 0 {
		topPad = 0
	}
	topBar := topLeft + TopBarStyle.Render(strings.Repeat(" ", topPad)) + topRight

	// Viewport
	vpView := m.viewport.View()

	// Status bar
	stateStr := "idle"
	switch m.agentState {
	case agent.StateThinking:
		stateStr = m.spinner.View() + " thinking..."
	case agent.StateToolCall:
		stateStr = m.spinner.View() + " running tools..."
	}

	elapsed := m.agent.Elapsed().Truncate(time.Second)
	tokens := m.agent.EstimateTokens()
	tokenStr := formatTokens(tokens)

	statusLeft := BottomBarStyle.Render(fmt.Sprintf(" [%s] %s ", m.config.AgentNick, stateStr))

	// Tool stats + tokens + elapsed
	var statsItems []string
	for name, count := range m.toolStats {
		statsItems = append(statsItems, fmt.Sprintf("%s(%d)", name, count))
	}
	statsStr := strings.Join(statsItems, " ")
	if statsStr != "" {
		statsStr += " | "
	}
	statsStr += fmt.Sprintf("~%s tok | %s", tokenStr, elapsed)

	statusRight := BottomBarStyle.Render(fmt.Sprintf(" %s ", statsStr))
	statusPad := m.width - lipgloss.Width(statusLeft) - lipgloss.Width(statusRight)
	if statusPad < 0 {
		statusPad = 0
	}
	statusBar := statusLeft + BottomBarStyle.Render(strings.Repeat(" ", statusPad)) + statusRight

	// Input
	inputView := " " + m.input.View()

	// Assemble
	return topBar + "\n" +
		Separator(m.width) + "\n" +
		vpView + "\n" +
		Separator(m.width) + "\n" +
		statusBar + "\n" +
		Separator(m.width) + "\n" +
		inputView
}

// handleCommand processes slash commands
func (m Model) handleCommand(input string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(input)
	cmd := parts[0]

	switch cmd {
	case "/quit", "/q", "/exit":
		return m, tea.Quit

	case "/help", "/h":
		m.addMessage(ChatMessage{
			Time: time.Now(),
			Type: MsgSystem,
			Content: "Commands:\n" +
				"  /model <name>       Switch LLM model\n" +
				"  /provider <name>    Set provider (openai, anthropic)\n" +
				"  /baseurl <url>      Set API base URL\n" +
				"  /apikey <key>       Set API key\n" +
				"  /profile [cmd]      save/load/delete named profiles\n" +
				"  /compact            Compact conversation context\n" +
				"  /clear              Clear chat display\n" +
				"  /diff               Show git diff\n" +
				"  /undo               Undo last git change\n" +
				"  /commit [msg]       Git commit\n" +
				"  /status             Git status\n" +
				"  /tokens             Token usage estimate\n" +
				"  /sessions           List saved sessions\n" +
				"  /tree               Show session tree\n" +
				"  /fork               Fork session\n" +
				"  /auto-next          Toggle auto-next-steps\n" +
				"  /auto-idea          Toggle auto-next-idea\n" +
				"  /quit               Exit\n" +
				"\n" +
				"  Use @filename to include file contents.\n" +
				"  Type while agent works to queue (steering).\n" +
				"  Ctrl+C to interrupt, again to quit.",
		})
		m.refreshViewport()
		return m, nil

	case "/model":
		if len(parts) < 2 {
			m.addMessage(ChatMessage{
				Time:    time.Now(),
				Type:    MsgSystem,
				Content: fmt.Sprintf("Current model: %s. Usage: /model <name>", m.agent.Model()),
			})
		} else {
			newModel := parts[1]
			m.agent.SetModel(newModel)
			m.addMessage(ChatMessage{
				Time:    time.Now(),
				Type:    MsgSystem,
				Content: fmt.Sprintf("*** Model switched to: %s", newModel),
			})
		}
		m.refreshViewport()
		return m, nil

	case "/clear":
		m.messages = []ChatMessage{}
		m.refreshViewport()
		return m, nil

	case "/compact":
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

	case "/diff":
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

	case "/status":
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

	case "/undo":
		// git checkout -- . (revert all unstaged changes)
		output := runGit(m.config.WorkDir, "checkout", "--", ".")
		m.sysMsg("Reverted all unstaged changes. " + output)
		return m, nil

	case "/commit":
		var msg string
		if len(parts) > 1 {
			msg = strings.Join(parts[1:], " ")
		} else {
			// Auto-generate commit message
			diff := runGit(m.config.WorkDir, "diff", "--cached", "--stat")
			if diff == "" {
				// Stage everything first
				runGit(m.config.WorkDir, "add", "-A")
				diff = runGit(m.config.WorkDir, "diff", "--cached", "--stat")
			}
			if diff == "" {
				m.sysMsg("Nothing to commit.")
				return m, nil
			}
			msg = "bitchtea: auto-commit"
		}
		runGit(m.config.WorkDir, "add", "-A")
		output := runGit(m.config.WorkDir, "commit", "-m", msg)
		m.sysMsg("Committed: " + output)
		return m, nil

	case "/tokens":
		tokens := m.agent.EstimateTokens()
		msgs := m.agent.MessageCount()
		m.sysMsg(fmt.Sprintf("~%s tokens | %d messages | %d turns",
			formatTokens(tokens), msgs, m.agent.TurnCount))
		return m, nil

	case "/auto-next":
		m.config.AutoNextSteps = !m.config.AutoNextSteps
		status := "OFF"
		if m.config.AutoNextSteps {
			status = "ON"
		}
		m.sysMsg(fmt.Sprintf("Auto-next-steps: %s", status))
		return m, nil

	case "/auto-idea":
		m.config.AutoNextIdea = !m.config.AutoNextIdea
		status := "OFF"
		if m.config.AutoNextIdea {
			status = "ON"
		}
		m.sysMsg(fmt.Sprintf("Auto-next-idea: %s", status))
		return m, nil

	case "/sessions", "/ls":
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
		for i, s := range sessions[:limit] {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, session.Info(s)))
		}
		if len(sessions) > 15 {
			sb.WriteString(fmt.Sprintf("  ... and %d more\n", len(sessions)-15))
		}
		m.sysMsg(sb.String())
		return m, nil

	case "/tree":
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

	case "/fork":
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

	case "/baseurl":
		if len(parts) < 2 {
			m.sysMsg(fmt.Sprintf("Base URL: %s\nUsage: /baseurl <url>", m.config.BaseURL))
		} else {
			url := parts[1]
			m.agent.SetBaseURL(url)
			m.config.BaseURL = url
			m.sysMsg(fmt.Sprintf("*** Base URL set to: %s", url))
		}
		return m, nil

	case "/apikey":
		if len(parts) < 2 {
			masked := m.config.APIKey
			if len(masked) > 8 {
				masked = masked[:4] + "..." + masked[len(masked)-4:]
			}
			m.sysMsg(fmt.Sprintf("API Key: %s\nUsage: /apikey <key>", masked))
		} else {
			key := parts[1]
			m.agent.SetAPIKey(key)
			m.config.APIKey = key
			masked := key
			if len(masked) > 8 {
				masked = masked[:4] + "..." + masked[len(masked)-4:]
			}
			m.sysMsg(fmt.Sprintf("*** API key set: %s", masked))
		}
		return m, nil

	case "/provider":
		if len(parts) < 2 {
			m.sysMsg(fmt.Sprintf("Provider: %s\nUsage: /provider <openai|anthropic>", m.config.Provider))
		} else {
			prov := parts[1]
			m.config.Provider = prov
			m.sysMsg(fmt.Sprintf("*** Provider set to: %s", prov))
		}
		return m, nil

	case "/profile":
		if len(parts) < 2 {
			// List profiles
			names := config.ListProfiles()
			if len(names) == 0 {
				m.sysMsg("No saved profiles.\nUsage: /profile save <name> | /profile load <name> | /profile delete <name>")
			} else {
				m.sysMsg("Profiles: " + strings.Join(names, ", ") +
					"\nUsage: /profile save <name> | /profile load <name> | /profile delete <name>")
			}
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
			p, err := config.LoadProfile(name)
			if err != nil {
				m.errMsg(fmt.Sprintf("Load failed: %v", err))
				return m, nil
			}
			config.ApplyProfile(m.config, p)
			m.agent.SetModel(p.Model)
			m.agent.SetBaseURL(p.BaseURL)
			m.agent.SetAPIKey(p.APIKey)
			masked := p.APIKey
			if len(masked) > 8 {
				masked = masked[:4] + "..." + masked[len(masked)-4:]
			}
			m.sysMsg(fmt.Sprintf("*** Profile loaded: %s\n  provider=%s model=%s\n  baseurl=%s\n  apikey=%s",
				name, p.Provider, p.Model, p.BaseURL, masked))

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
			// Treat as shorthand for /profile load <name>
			p, err := config.LoadProfile(action)
			if err != nil {
				m.errMsg(fmt.Sprintf("Unknown profile action or profile not found: %s", action))
				return m, nil
			}
			config.ApplyProfile(m.config, p)
			m.agent.SetModel(p.Model)
			m.agent.SetBaseURL(p.BaseURL)
			m.agent.SetAPIKey(p.APIKey)
			m.sysMsg(fmt.Sprintf("*** Profile loaded: %s (provider=%s model=%s)", action, p.Provider, p.Model))
		}
		return m, nil

	default:
		m.errMsg(fmt.Sprintf("Unknown command: %s. Try /help, genius.", cmd))
		return m, nil
	}
}

// sysMsg is a shorthand for adding a system message and refreshing
func (m *Model) sysMsg(content string) {
	m.addMessage(ChatMessage{Time: time.Now(), Type: MsgSystem, Content: content})
	m.refreshViewport()
}

// errMsg is a shorthand for adding an error message and refreshing
func (m *Model) errMsg(content string) {
	m.addMessage(ChatMessage{Time: time.Now(), Type: MsgError, Content: content})
	m.refreshViewport()
}

// runGit runs a git command and returns its output
func runGit(workDir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = workDir
	out, _ := cmd.CombinedOutput()
	return strings.TrimSpace(string(out))
}

// formatTokens formats a token count nicely
func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000.0)
}
