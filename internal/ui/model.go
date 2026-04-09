package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jstamagal/bitchtea/internal/agent"
	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/llm"
	"github.com/jstamagal/bitchtea/internal/session"
	"github.com/jstamagal/bitchtea/internal/sound"
)

// agentEventMsg wraps agent events for the bubbletea message loop
type agentEventMsg struct {
	event agent.Event
}

// agentDoneMsg signals the agent event channel is closed
type agentDoneMsg struct{}

// SignalMsg wraps an OS signal for the bubbletea message loop
type SignalMsg struct {
	Signal os.Signal
}

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
	viewport  viewport.Model
	input     textarea.Model
	spinner   spinner.Model
	toolPanel *ToolPanel
	mp3       *mp3Controller

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

	// Session
	session         *session.Session
	lastSavedMsgIdx int // index into agent.Messages() of last saved entry
	transcript      *TranscriptLogger

	// Debug mode - verbose API logging
	debugMode bool
}

// NewModel creates the initial model
func NewModel(cfg *config.Config) Model {
	ta := textarea.New()
	ta.Placeholder = "type something, coward..."
	ta.Focus()
	ta.CharLimit = 8192
	ta.SetWidth(80)
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.Prompt = ">> "
	ta.FocusedStyle.Prompt = InputPromptStyle
	ta.FocusedStyle.Text = InputTextStyle
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.BlurredStyle.Prompt = InputPromptStyle
	ta.BlurredStyle.Text = InputTextStyle

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(ColorMagenta)

	ag := agent.NewAgent(cfg)

	// Create session
	sess, err := session.New(cfg.SessionDir)
	if err != nil {
		// Non-fatal: app works without session persistence
		fmt.Fprintf(os.Stderr, "warning: session init failed: %v\n", err)
	}

	transcript, err := NewTranscriptLogger(cfg.LogDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: transcript init failed: %v\n", err)
	}

	return Model{
		config:       cfg,
		agent:        ag,
		agentState:   agent.StateIdle,
		input:        ta,
		spinner:      sp,
		toolPanel:    NewToolPanel(),
		mp3:          newMP3Controller(),
		messages:     []ChatMessage{},
		history:      []string{},
		historyIdx:   -1,
		streamBuffer: &strings.Builder{},
		session:      sess,
		transcript:   transcript,
	}
}

// ResumeSession loads a previous session's messages into the chat display
func (m *Model) ResumeSession(sess *session.Session) {
	m.session = sess
	messages := session.MessagesFromEntries(sess.Entries)
	if len(messages) > 0 {
		m.agent.RestoreMessages(messages)
		m.lastSavedMsgIdx = len(messages)
	}

	toolNames := make(map[string]string)
	for _, e := range sess.Entries {
		for _, tc := range e.ToolCalls {
			toolNames[tc.ID] = tc.Function.Name
		}
	}

	for _, e := range session.DisplayEntries(sess.Entries) {
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
			if nick == "" {
				nick = toolNames[e.ToolCallID]
			}
			if nick == "" {
				nick = "tool"
			}
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
		textarea.Blink,
		m.spinner.Tick,
		mp3TickCmd(),
		tea.EnterAltScreen,
		tea.EnableMouseCellMotion,
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

		// Input area height: 3 lines for textarea
		inputHeight := 3
		m.input.SetWidth(msg.Width - 4)
		m.input.SetHeight(inputHeight)

		// Layout: topbar(1) + sep(1) + viewport + sep(1) + statusbar(1) + sep(1) + input(3)
		vpHeight := msg.Height - 4 - inputHeight
		if vpHeight < 1 {
			vpHeight = 1
		}

		// Account for side panel width
		vpWidth := msg.Width
		if m.mp3 != nil && m.mp3.visible && msg.Width > 90 {
			vpWidth = msg.Width - mp3PanelWidth
			if vpWidth < 40 {
				vpWidth = msg.Width
			}
		} else if m.toolPanel != nil && m.toolPanel.Visible && m.streaming {
			vpWidth = msg.Width - ToolPanelWidth
			if vpWidth < 40 {
				vpWidth = msg.Width // too narrow, hide panel
			}
		}

		if !m.ready {
			m.viewport = viewport.New(vpWidth, vpHeight)
			m.viewport.SetContent(m.viewContent)
			m.viewport.MouseWheelEnabled = true
			m.viewport.MouseWheelDelta = 3
			m.ready = true
		} else {
			m.viewport.Width = vpWidth
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
		// Up/down arrow: history navigation when cursor is on first/last line,
		// otherwise fall through to textarea for normal multiline cursor movement.
		switch msg.String() {
		case "up":
			if m.input.Line() == 0 && len(m.history) > 0 && m.historyIdx > 0 {
				m.historyIdx--
				m.input.SetValue(m.history[m.historyIdx])
				m.input.SetCursor(len(m.history[m.historyIdx]))
				return m, nil
			}

		case "down":
			if m.input.Line() >= m.input.LineCount()-1 {
				if m.historyIdx < len(m.history)-1 {
					m.historyIdx++
					m.input.SetValue(m.history[m.historyIdx])
					m.input.SetCursor(len(m.history[m.historyIdx]))
				} else if m.historyIdx == len(m.history)-1 {
					m.historyIdx = len(m.history)
					m.input.SetValue("")
				}
				return m, nil
			}

		case "ctrl+c":
			if m.streaming && m.cancel != nil {
				m.cancel()
				_ = m.transcript.FinishAgentMessage()
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
			// Enter sends; Shift+Enter / Alt+Enter adds newline
			input := strings.TrimSpace(m.input.Value())
			if input == "" {
				return m, nil
			}

			m.input.Reset()

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

		case "ctrl+p":
			// History up
			if len(m.history) > 0 && m.historyIdx > 0 {
				m.historyIdx--
				m.input.SetValue(m.history[m.historyIdx])
			}
			return m, nil

		case "ctrl+n":
			// History down
			if m.historyIdx < len(m.history)-1 {
				m.historyIdx++
				m.input.SetValue(m.history[m.historyIdx])
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

		case "ctrl+t":
			// Toggle tool panel
			if m.toolPanel != nil {
				m.toolPanel.Visible = !m.toolPanel.Visible
				m.refreshViewport()
			}
			return m, nil
		}

		if handled, cmd := m.handleMP3Key(msg); handled {
			return m, cmd
		}

	case SignalMsg:
		// Handle OS signals (SIGINT, SIGTERM) sent from main.go
		if m.streaming && m.cancel != nil {
			m.cancel()
			_ = m.transcript.FinishAgentMessage()
			m.streaming = false
			m.agentState = agent.StateIdle
			m.addMessage(ChatMessage{
				Time:    time.Now(),
				Type:    MsgSystem,
				Content: "Interrupted by signal.",
			})
			m.refreshViewport()
			return m, nil
		}
		return m, tea.Quit
	case tea.SuspendMsg:
		// Ctrl+Z: suspend gracefully - bubbletea handles terminal restoration
		return m, tea.Suspend

	case tea.QuitMsg:
		// SIGINT/SIGTERM from OS: cancel any active streaming before quitting
		if m.streaming && m.cancel != nil {
			m.cancel()
			_ = m.transcript.FinishAgentMessage()
			m.streaming = false
		}
		if m.mp3 != nil {
			m.mp3.stop()
		}
		_ = m.transcript.Close()
		return m, tea.Quit

	case agentEventMsg:
		newModel, cmd := m.handleAgentEvent(msg.event)
		// Chain: after handling this event, wait for the next one
		if m.eventCh != nil {
			return newModel, tea.Batch(cmd, waitForAgentEvent(m.eventCh))
		}
		return newModel, cmd

	case agentDoneMsg:
		_ = m.transcript.FinishAgentMessage()
		m.streaming = false
		m.agentState = agent.StateIdle
		m.eventCh = nil
		m.cancel = nil

		// Play notification sound if enabled
		if m.config.NotificationSound {
			sound.Play(m.config.SoundType)
		}

		// Update tool panel
		if m.toolPanel != nil {
			m.toolPanel.Tokens = m.agent.EstimateTokens()
			m.toolPanel.Elapsed = m.agent.Elapsed()
		}

		// Save new messages to session (incremental)
		if m.session != nil {
			msgs := m.agent.Messages()
			for i := m.lastSavedMsgIdx; i < len(msgs); i++ {
				_ = m.session.Append(session.EntryFromMessageWithBootstrap(
					msgs[i],
					i < m.agent.BootstrapMessageCount(),
				))
			}
			m.lastSavedMsgIdx = len(msgs)
		}

		if err := session.SaveCheckpoint(m.config.SessionDir, session.Checkpoint{
			TurnCount: m.agent.TurnCount,
			ToolCalls: cloneToolStats(m.agent.ToolCalls),
			Model:     m.agent.Model(),
		}); err != nil {
			m.errMsg(fmt.Sprintf("checkpoint save failed: %v", err))
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

		if followUp := m.agent.MaybeQueueFollowUp(); followUp != nil {
			m.addMessage(ChatMessage{
				Time:    time.Now(),
				Type:    MsgSystem,
				Content: fmt.Sprintf("*** %s: continuing...", followUp.Label),
			})
			m.refreshViewport()
			return m, m.sendToAgent(followUp.Prompt)
		}

		return m, nil

	case mp3TickMsg:
		return m, mp3TickCmd()

	case mp3DoneMsg:
		if m.mp3 != nil {
			status, cmd := m.mp3.handleDone(msg)
			if status != "" {
				m.sysMsg(status)
			}
			return m, cmd
		}
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
		if len(m.messages) > 0 {
			last := m.messages[len(m.messages)-1]
			if last.Type == MsgAgent {
				_ = m.transcript.AppendAgentChunk(last.Time, last.Nick, ev.Text)
			}
		}
		m.refreshViewport()

	case "tool_start":
		m.addMessage(ChatMessage{
			Time:    time.Now(),
			Type:    MsgTool,
			Nick:    ev.ToolName,
			Content: fmt.Sprintf("calling %s...", ev.ToolName),
		})
		if m.toolPanel != nil {
			m.toolPanel.StartTool(ev.ToolName)
		}
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
		if m.toolPanel != nil {
			m.toolPanel.FinishTool(ev.ToolName, result, ev.ToolError != nil)
		}
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
	if m.cancel != nil {
		m.cancel()
	}
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
	_ = m.transcript.LogMessage(msg)
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

	wrapWidth := m.viewport.Width - 2
	if wrapWidth < 20 {
		wrapWidth = 20
	}

	var sb strings.Builder
	for i := range m.messages {
		m.messages[i].Width = wrapWidth
		formatted := m.messages[i].Format()
		// Word-wrap everything except raw messages (ANSI art)
		if m.messages[i].Type != MsgRaw {
			formatted = WrapText(formatted, wrapWidth)
		}
		sb.WriteString(formatted)
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
	queuedStr := ""
	if len(m.queued) > 0 {
		queuedStr = fmt.Sprintf(" [queued:%d]", len(m.queued))
	}
	topLeft := TopBarStyle.Render(fmt.Sprintf(" bitchtea — %s/%s%s%s ", m.config.Provider, m.config.Model, flags, queuedStr))
	topRight := TopBarStyle.Render(fmt.Sprintf(" %s ", time.Now().Format("3:04pm")))
	topPad := m.width - lipgloss.Width(topLeft) - lipgloss.Width(topRight)
	if topPad < 0 {
		topPad = 0
	}
	topBar := topLeft + TopBarStyle.Render(strings.Repeat(" ", topPad)) + topRight

	// Viewport + optional tool panel
	vpView := m.viewport.View()

	showPanel := m.toolPanel != nil && m.toolPanel.Visible && m.streaming && m.width > 80
	if m.mp3 != nil && m.mp3.visible && m.width > 90 {
		panel := m.mp3.renderPanel(m.viewport.Height)
		vpView = lipgloss.JoinHorizontal(lipgloss.Top, vpView, panel)
	} else if showPanel {
		panel := m.toolPanel.Render(m.viewport.Height)
		vpView = lipgloss.JoinHorizontal(lipgloss.Top, vpView, panel)
	}

	// Status bar
	stateStr := "idle"
	agentActive := false
	switch m.agentState {
	case agent.StateThinking:
		stateStr = m.spinner.View() + " thinking..."
		agentActive = true
	case agent.StateToolCall:
		stateStr = m.spinner.View() + " running tools..."
		agentActive = true
	}

	elapsed := m.agent.Elapsed().Truncate(time.Second)
	tokens := m.agent.EstimateTokens()
	tokenStr := formatTokens(tokens)

	barStyle := BottomBarStyle
	if agentActive {
		barStyle = ThinkingBarStyle
	}

	statusLeft := barStyle.Render(fmt.Sprintf(" [%s] %s ", m.config.AgentNick, stateStr))

	// Tool stats + tokens + elapsed
	var statsItems []string
	for name, count := range m.toolPanel.Stats {
		statsItems = append(statsItems, fmt.Sprintf("%s(%d)", name, count))
	}
	statsStr := strings.Join(statsItems, " ")
	if statsStr != "" {
		statsStr += " | "
	}
	if m.mp3 != nil && m.mp3.hasTracks() {
		mp3Status := truncateRunes(m.mp3.statusText(), mp3StatusBarWidth)
		statsStr += mp3Status + " | "
	}
	statsStr += fmt.Sprintf("~%s tok | %s", tokenStr, elapsed)

	statusRight := barStyle.Render(fmt.Sprintf(" %s ", statsStr))
	statusPad := m.width - lipgloss.Width(statusLeft) - lipgloss.Width(statusRight)
	if statusPad < 0 {
		statusPad = 0
	}
	statusBar := statusLeft + barStyle.Render(strings.Repeat(" ", statusPad)) + statusRight

	// Input
	inputView := m.input.View()

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
				"  /provider <name>    Set provider transport (openai, anthropic)\n" +
				"  /baseurl <url>      Set API base URL\n" +
				"  /apikey <key>       Set API key\n" +
				"  /profile [cmd]      save/load/delete profiles (built-ins: ollama, openrouter, zai-openai, zai-anthropic)\n" +
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
				"  /mp3 [cmd]          Toggle MP3 panel and player\n" +
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
			if !strings.Contains(newModel, ".") && !strings.Contains(newModel, "-") || len(newModel) < 3 || strings.Contains(newModel, " ") {
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
			target := strings.TrimSpace(strings.TrimPrefix(input, cmd))
			output := runGit(m.config.WorkDir, "restore", "--worktree", "--", target)
			if output == "" {
				output = fmt.Sprintf("Reverted unstaged changes for %s.", target)
			} else {
				output = fmt.Sprintf("Reverted unstaged changes for %s.\n%s", target, output)
			}
			m.sysMsg(output)
		}
		return m, nil

	case "/commit":
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

		msg := strings.TrimSpace(strings.TrimPrefix(input, cmd))
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

	case "/copy":
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

	case "/tokens":
		tokens := m.agent.EstimateTokens()
		cost := m.agent.Cost()
		msgs := m.agent.MessageCount()
		m.sysMsg(fmt.Sprintf("~%s tokens | $%.4f | %d messages | %d turns",
			formatTokens(tokens), cost, msgs, m.agent.TurnCount))
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

	case "/debug":
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

	case "/sound":
		m.config.NotificationSound = !m.config.NotificationSound
		status := "OFF"
		if m.config.NotificationSound {
			status = "ON"
			sound.Play(m.config.SoundType)
		}
		m.sysMsg(fmt.Sprintf("Notification sound: %s", status))
		return m, nil

	case "/mp3":
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

	case "/theme":
		m.sysMsg(fmt.Sprintf("Theme switching is disabled. Built-in theme: %s.", CurrentThemeName()))
		return m, nil

	case "/memory":
		mem := agent.LoadMemory(m.config.WorkDir)
		if mem == "" {
			m.sysMsg("No MEMORY.md found in working directory.")
		} else {
			if len(mem) > 1000 {
				mem = mem[:1000] + "\n... (truncated)"
			}
			m.addMessage(ChatMessage{
				Time:    time.Now(),
				Type:    MsgRaw,
				Content: "\033[1;36m--- MEMORY.md ---\033[0m\n" + mem,
			})
			m.refreshViewport()
		}
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
			if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
				m.errMsg(fmt.Sprintf("Invalid URL %q. Must start with http:// or https://.", url))
				return m, nil
			}
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
		}
		return m, nil

	case "/provider":
		if len(parts) < 2 {
			m.sysMsg(fmt.Sprintf("Provider: %s\nUsage: /provider <openai|anthropic>", m.config.Provider))
		} else {
			prov := parts[1]
			if prov != "openai" && prov != "anthropic" {
				m.errMsg(fmt.Sprintf("Invalid provider %q. Must be openai or anthropic.", prov))
				return m, nil
			}
			m.config.Provider = prov
			m.agent.SetProvider(prov)
			m.sysMsg(fmt.Sprintf("*** Provider set to: %s", prov))
		}
		return m, nil

	case "/profile":
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
			config.ApplyProfile(m.config, p)
			m.agent.SetModel(p.Model)
			m.agent.SetBaseURL(p.BaseURL)
			m.agent.SetAPIKey(p.APIKey)
			m.agent.SetProvider(p.Provider)
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
			p, err := config.ResolveProfile(action)
			if err != nil {
				m.errMsg(fmt.Sprintf("Unknown profile action or profile not found: %s", action))
				return m, nil
			}
			config.ApplyProfile(m.config, p)
			m.agent.SetModel(p.Model)
			m.agent.SetBaseURL(p.BaseURL)
			m.agent.SetAPIKey(p.APIKey)
			m.agent.SetProvider(p.Provider)
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

func (m *Model) handleMP3Key(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m.mp3 == nil || !m.mp3.visible {
		return false, nil
	}
	if strings.TrimSpace(m.input.Value()) != "" {
		return false, nil
	}

	var (
		status string
		cmd    tea.Cmd
	)

	switch msg.String() {
	case " ":
		status = m.mp3.togglePause()
	case "left", "j":
		status, cmd = m.mp3.prev()
	case "right", "k":
		status, cmd = m.mp3.next()
	default:
		return false, nil
	}

	if strings.Contains(strings.ToLower(status), "failed") || strings.HasPrefix(status, "No MP3s") {
		m.errMsg(status)
	} else {
		m.sysMsg(status)
	}
	return true, cmd
}

// runGit runs a git command and returns its output
func runGit(workDir string, args ...string) string {
	return strings.TrimSpace(runGitRaw(workDir, args...))
}

func runGitRaw(workDir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = workDir
	out, _ := cmd.CombinedOutput()
	return strings.TrimRight(string(out), "\n")
}

func gitUndoPreview(workDir string) string {
	diff := runGit(workDir, "diff", "--stat", "--")
	if diff == "" {
		return "No unstaged tracked changes to revert.\nUsage: /undo confirm to revert all, or /undo <file> to revert one file."
	}
	return diff + "\nUsage: /undo confirm to revert all, or /undo <file> to revert one file."
}

func gitCommitPreview(workDir string) string {
	status := runGitRaw(workDir, "status", "--short")
	if status == "" {
		return "Nothing to commit. Working tree clean."
	}

	var staged []string
	var unstaged []string
	var untracked []string

	for _, line := range strings.Split(status, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		path := strings.TrimSpace(line[2:])
		switch {
		case strings.HasPrefix(line, "??"):
			untracked = append(untracked, path)
		default:
			if len(line) >= 1 && line[0] != ' ' {
				staged = append(staged, path)
			}
			if len(line) >= 2 && line[1] != ' ' {
				unstaged = append(unstaged, path)
			}
		}
	}

	var b strings.Builder
	b.WriteString("Tracked changes only will be committed.\n")
	writeGitPreviewSection(&b, "Staged", staged)
	writeGitPreviewSection(&b, "Unstaged", unstaged)
	writeGitPreviewSection(&b, "Untracked", untracked)
	b.WriteString("Run /commit <message> to stage tracked changes with git add -u and commit.")
	return b.String()
}

func writeGitPreviewSection(b *strings.Builder, heading string, items []string) {
	b.WriteString(heading)
	b.WriteString(":\n")
	if len(items) == 0 {
		b.WriteString("  (none)\n")
		return
	}
	for _, item := range items {
		b.WriteString("  ")
		b.WriteString(item)
		b.WriteByte('\n')
	}
}

// formatTokens formats a token count nicely
func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000.0)
}

func cloneToolStats(stats map[string]int) map[string]int {
	cloned := make(map[string]int, len(stats))
	for name, count := range stats {
		cloned[name] = count
	}
	return cloned
}
