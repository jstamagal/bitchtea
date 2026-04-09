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

// BackgroundActivity captures work happening outside the currently focused chat.
type BackgroundActivity struct {
	Time    time.Time
	Context string
	Sender  string
	Summary string
}

func normalizeContextLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "main"
	}
	return label
}

func formatContextAddress(contextLabel, sender string) string {
	contextLabel = normalizeContextLabel(contextLabel)
	sender = strings.TrimSpace(sender)
	if sender == "" {
		return fmt.Sprintf("[%s]", contextLabel)
	}
	return fmt.Sprintf("[%s] <%s>", contextLabel, sender)
}

func (a BackgroundActivity) displayLine() string {
	address := formatContextAddress(a.Context, a.Sender)
	summary := strings.TrimSpace(a.Summary)
	if summary == "" {
		return address
	}
	return address + " " + summary
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
	messages           []ChatMessage
	viewContent        string // rendered viewport content
	width              int
	height             int
	ready              bool
	streaming          bool
	streamBuffer       *strings.Builder // accumulates current agent response (pointer to avoid copy panic)
	focus              *FocusManager
	membership         *MembershipManager
	backgroundActivity []BackgroundActivity
	backgroundUnread   int

	// Input history
	history    []string
	historyIdx int

	// Queued messages (steering - typed while agent is working)
	queued []string

	// Session
	session         *session.Session
	lastSavedMsgIdx int        // index into agent.Messages() of last saved entry
	turnContext     IRCContext // active context when current/last turn was submitted
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
	fm := LoadFocusManager(cfg.SessionDir)
	mm := LoadMembershipManager(cfg.SessionDir)

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

	// Set initial memory scope from restored focus.
	ag.SetScope(ircContextToMemoryScope(fm.Active()))

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
		focus:        fm,
		membership:   mm,
		session:      sess,
		transcript:   transcript,
	}
}

// ResumeSession loads a previous session's messages into the chat display.
// Focus state is already restored in NewModel via LoadFocusManager; this call
// records the session and replays the display messages only.
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
		providerName := m.config.Provider
		if m.config.Profile != "" {
			providerName = m.config.Profile
		}
		m.addMessage(ChatMessage{Time: time.Now(), Type: MsgRaw, Content: SplashArt()})
		m.addMessage(ChatMessage{Time: time.Now(), Type: MsgRaw, Content: SplashTagline})
		m.addMessage(ChatMessage{Time: time.Now(), Type: MsgRaw, Content: fmt.Sprintf(ConnectMsg, providerName, m.config.Model, m.config.WorkDir)})

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

			// Slash commands always work regardless of focus.
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

		// Save new messages to session (incremental), stamping the turn context.
		if m.session != nil {
			msgs := m.agent.Messages()
			ctxLabel := m.turnContext.Label()
			for i := m.lastSavedMsgIdx; i < len(msgs); i++ {
				e := session.EntryFromMessageWithBootstrap(
					msgs[i],
					i < m.agent.BootstrapMessageCount(),
				)
				e.Context = ctxLabel
				_ = m.session.Append(e)
			}
			m.lastSavedMsgIdx = len(msgs)
		}

		// Persist open contexts and current focus for restart restoration.
		if err := m.focus.Save(m.config.SessionDir); err != nil {
			m.errMsg(fmt.Sprintf("focus save failed: %v", err))
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
		// If the thinking placeholder is still showing, replace it with a
		// real streaming agent message before writing the first token.
		if len(m.messages) > 0 && m.messages[len(m.messages)-1].Type == MsgThink {
			m.messages[len(m.messages)-1] = ChatMessage{
				Time:    m.messages[len(m.messages)-1].Time,
				Type:    MsgAgent,
				Nick:    m.config.AgentNick,
				Content: "",
				Context: m.focus.Active(),
			}
		}
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
			// Show a styled thinking placeholder in the scroll buffer.
			// It will be replaced with MsgAgent once the first token arrives.
			m.streamBuffer.Reset()
			m.addMessage(ChatMessage{
				Time:    time.Now(),
				Type:    MsgThink,
				Content: "thinking...",
			})
			m.refreshViewport()
		}

	case "error":
		errText := fmt.Sprintf("Error: %v", ev.Error)
		if hint := llm.ErrorHint(ev.Error); hint != "" {
			errText += "\n  hint: " + hint
		}
		m.addMessage(ChatMessage{
			Time:    time.Now(),
			Type:    MsgError,
			Content: errText,
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

func ircContextToMemoryScope(ctx IRCContext) agent.MemoryScope {
	switch ctx.Kind {
	case KindChannel:
		return agent.ChannelMemoryScope(ctx.Channel, nil)
	case KindSubchannel:
		parent := agent.ChannelMemoryScope(ctx.Channel, nil)
		return agent.ChannelMemoryScope(ctx.Sub, &parent)
	case KindDirect:
		return agent.QueryMemoryScope(ctx.Target, nil)
	default:
		return agent.RootMemoryScope()
	}
}

func (m *Model) sendToAgent(input string) tea.Cmd {
	if m.cancel != nil {
		m.cancel()
	}
	m.turnContext = m.focus.Active()
	m.agent.SetScope(ircContextToMemoryScope(m.turnContext))
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.streaming = true

	ch := make(chan agent.Event, 100)
	m.eventCh = ch
	go m.agent.SendMessage(ctx, input, ch)

	return waitForAgentEvent(ch)
}

func (m *Model) addMessage(msg ChatMessage) {
	msg.Context = m.focus.Active()
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
	topBarProvider := m.config.Provider
	if m.config.Profile != "" {
		topBarProvider = m.config.Profile
	}
	topLeft := TopBarStyle.Render(fmt.Sprintf(" bitchtea — %s/%s [%s]%s%s ", topBarProvider, m.config.Model, m.focus.ActiveLabel(), flags, queuedStr))
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
	if bg := m.backgroundStatus(); bg != "" {
		statsStr += bg + " | "
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

// SetActiveContext switches focus to the named channel. Exists for compatibility
// with callers that only know a string label; prefer focus.SetFocus directly.
func (m *Model) SetActiveContext(label string) {
	m.focus.SetFocus(Channel(label))
}

func (m *Model) NotifyBackgroundActivity(activity BackgroundActivity) {
	activity.Context = normalizeContextLabel(activity.Context)
	if activity.Time.IsZero() {
		activity.Time = time.Now()
	}
	if strings.TrimSpace(activity.Summary) == "" {
		activity.Summary = "activity waiting"
	}
	m.backgroundActivity = append(m.backgroundActivity, activity)
	m.backgroundUnread++
}

func (m Model) backgroundStatus() string {
	if len(m.backgroundActivity) == 0 {
		return ""
	}
	latest := truncateRunes(m.backgroundActivity[len(m.backgroundActivity)-1].displayLine(), 22)
	return fmt.Sprintf("bg:%d /activity %s", m.backgroundUnread, latest)
}

func (m Model) backgroundActivityReport() string {
	if len(m.backgroundActivity) == 0 {
		return "No background activity queued."
	}

	var sb strings.Builder
	sb.WriteString("Background activity:\n")
	for _, activity := range m.backgroundActivity {
		sb.WriteString("  ")
		sb.WriteString(activity.Time.Format("15:04"))
		sb.WriteString(" ")
		sb.WriteString(activity.displayLine())
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}

// handleCommand processes slash commands
func (m Model) handleCommand(input string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return m, nil
	}

	handler, ok := lookupSlashCommand(parts[0])
	if !ok {
		m.errMsg(fmt.Sprintf("Unknown command: %s. Try /help, genius.", parts[0]))
		return m, nil
	}

	updated, cmd := handler(m, input, parts)
	return updated, cmd
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
