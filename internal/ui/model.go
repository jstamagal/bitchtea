package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jstamagal/bitchtea/internal/agent"
	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/daemon"
	daemonjobs "github.com/jstamagal/bitchtea/internal/daemon/jobs"
	"github.com/jstamagal/bitchtea/internal/llm"
	"github.com/jstamagal/bitchtea/internal/session"
	"github.com/jstamagal/bitchtea/internal/sound"
)

// agentEventMsg wraps agent events for the bubbletea message loop
type agentEventMsg struct {
	ch    chan agent.Event
	event agent.Event
}

// agentDoneMsg signals the agent event channel is closed
type agentDoneMsg struct {
	ch chan agent.Event
}

const (
	escGraduationWindow   = 1500 * time.Millisecond
	ctrlCGraduationWindow = 3 * time.Second

	// queueStaleThreshold is how long queued messages can sit before they're
	// considered stale. When the agent finishes a turn, any queue older than
	// this is discarded rather than sent as out-of-date context.
	queueStaleThreshold = 2 * time.Minute
)

// queuedMsg holds a message typed while the agent was busy, along with the
// time it was queued so staleness can be detected on drain.
type queuedMsg struct {
	text     string
	queuedAt time.Time
}

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
	activeToolName     string
	activeToolCallID   string // fantasy tool call ID for per-tool cancellation
	focus              *FocusManager
	membership         *MembershipManager
	backgroundActivity []BackgroundActivity
	backgroundUnread   int

	// Input history
	history    []string
	historyIdx int

	// Queued messages (steering - typed while agent is working)
	queued          []queuedMsg
	queueClearArmed bool
	escStage        int
	escLast         time.Time
	ctrlCStage      int
	ctrlCLast       time.Time

	// Session
	session         *session.Session
	lastSavedMsgIdx int            // index into agent.Messages() of last saved entry
	contextSavedIdx map[string]int // per-context session-save watermark
	turnContext     IRCContext     // active context when current/last turn was submitted
	transcript      *TranscriptLogger

	// Debug mode - verbose API logging
	debugMode bool

	// Suppresses visible/logged command output during silent startup execution.
	suppressMessages bool

	// Active model picker overlay (nil when not picking). When set, key
	// events are routed to the picker instead of the textarea / slash router.
	// See model_picker.go and handleModelsCommand.
	picker *modelPicker

	// pickerMsgIdx tracks the index of the current picker render in messages
	// so refreshes overwrite in place rather than spamming new entries.
	pickerMsgIdx int

	// pickerOnSelect is invoked with the chosen model ID when the user hits
	// Enter. The callback receives a *Model so it can mutate state on the
	// live receiver (the closure cannot capture &m from the slash handler —
	// bubbletea hands the next Update a fresh value copy). Reset to nil on
	// close.
	pickerOnSelect func(*Model, string)

	// daemonBaseDir gates daemon IPC submission. Empty (default) means no
	// daemon submissions are issued from this Model — tests rely on this so
	// they don't accidentally write to a developer's real ~/.bitchtea/daemon
	// mailbox. Production code (main.buildStartupModel) sets this to
	// config.BaseDir() so submitDaemonCheckpointCmd() actually fires.
	daemonBaseDir string
}

// SetDaemonBaseDir enables daemon-mailbox submissions from this Model and
// pins the base dir used to resolve the lock and mailbox paths. Pass
// config.BaseDir() in production. Tests should leave this unset (the zero
// value) so they don't pollute a real daemon's mailbox if one happens to
// be running on the host.
func (m *Model) SetDaemonBaseDir(base string) {
	m.daemonBaseDir = base
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
	ta.Cursor.SetMode(cursor.CursorStatic)
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
		config:          cfg,
		agent:           ag,
		agentState:      agent.StateIdle,
		input:           ta,
		spinner:         sp,
		toolPanel: func() *ToolPanel {
			tp := NewToolPanel()
			if cfg.ToolVerbosity != "" {
				tp.Verbosity = cfg.ToolVerbosity
			}
			return tp
		}(),
		mp3:             newMP3Controller(),
		messages:        []ChatMessage{},
		history:         []string{},
		historyIdx:      -1,
		streamBuffer:    &strings.Builder{},
		focus:           fm,
		membership:      mm,
		session:         sess,
		transcript:      transcript,
		contextSavedIdx: map[string]int{ircContextToKey(fm.Active()): 0},
	}
}

// ResumeSession loads a previous session's messages into the chat display.
// Focus state is already restored in NewModel via LoadFocusManager; this call
// records the session and replays the display messages only.
//
// Phase 3: the agent's canonical message history is fantasy-native, so we
// restore via session.FantasyFromEntries — which round-trips v1 entries
// verbatim (preserving reasoning/media parts) and lifts v0 entries into
// fantasy parts the same way the in-flight conversion does.
func (m *Model) ResumeSession(sess *session.Session) {
	m.session = sess

	// Group entries by context label for per-context restore.
	type ctxGroup struct {
		entries []session.Entry
	}
	groups := map[string]*ctxGroup{}
	for _, e := range sess.Entries {
		key := e.Context
		if key == "" {
			key = agent.DefaultContextKey
		}
		if _, ok := groups[key]; !ok {
			groups[key] = &ctxGroup{}
		}
		groups[key].entries = append(groups[key].entries, e)
	}

	// Restore default context via RestoreMessages (sets bootstrap prefix).
	defaultKey := agent.DefaultContextKey
	if dg, ok := groups[defaultKey]; ok {
		msgs := session.FantasyFromEntries(dg.entries)
		if len(msgs) > 0 {
			m.agent.RestoreMessages(msgs)
			m.agent.SetSavedIdx(defaultKey, len(msgs))
			m.lastSavedMsgIdx = len(msgs)
		}
	}

	// Restore other contexts via RestoreContextMessages.
	for key, g := range groups {
		if key == defaultKey {
			continue
		}
		msgs := session.FantasyFromEntries(g.entries)
		if len(msgs) > 0 {
			m.agent.RestoreContextMessages(key, msgs)
			m.agent.SetSavedIdx(key, len(msgs))
		}
	}

	// Initialize contextSavedIdx for the active context.
	m.contextSavedIdx = map[string]int{ircContextToKey(m.focus.Active()): 0}

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

// ExecuteStartupCommand runs a slash command derived from bitchtearc without
// emitting visible startup chatter.
func (m *Model) ExecuteStartupCommand(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if !strings.HasPrefix(line, "/") {
		line = "/" + line
	}

	m.suppressMessages = true
	updated, _ := m.handleCommand(line)
	next := updated.(Model)
	next.suppressMessages = false
	*m = next
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		mp3TickCmd(),
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
		// Banner toggle: suppress splash art / *** lines when cfg.Banner is false.
		if m.config.Banner {
			m.addMessage(ChatMessage{Time: time.Now(), Type: MsgRaw, Content: SplashArt()})
			m.addMessage(ChatMessage{Time: time.Now(), Type: MsgRaw, Content: SplashTagline})
			m.addMessage(ChatMessage{Time: time.Now(), Type: MsgRaw, Content: fmt.Sprintf(ConnectMsg, providerName, m.config.Model, m.config.WorkDir)})
		}

		// Show loaded context files
		ctxFiles := agent.DiscoverContextFiles(m.config.WorkDir)
		if ctxFiles != "" {
			if m.debugMode {
				m.addMessage(ChatMessage{
					Time:    time.Now(),
					Type:    MsgSystem,
					Content: ctxFiles,
				})
			}
			// Extract individual file names from "# Context from <path>" headers
			for _, line := range strings.Split(ctxFiles, "\n") {
				if after, ok := strings.CutPrefix(line, "# Context from "); ok {
					name := filepath.Base(after)
					m.addMessage(ChatMessage{
						Time:    time.Now(),
						Type:    MsgSystem,
						Content: fmt.Sprintf("*** Injected %s", name),
					})
				}
			}
		}

		// Show memory status
		mem := agent.LoadMemory(m.config.WorkDir)
		if mem != "" {
			if m.debugMode {
				m.addMessage(ChatMessage{
					Time:    time.Now(),
					Type:    MsgSystem,
					Content: fmt.Sprintf("*** Injected MEMORY.md\n%s", mem),
				})
			} else {
				m.addMessage(ChatMessage{
					Time:    time.Now(),
					Type:    MsgSystem,
					Content: "*** Injected MEMORY.md",
				})
			}
		}

		if m.session != nil {
			m.addMessage(ChatMessage{
				Time:    time.Now(),
				Type:    MsgSystem,
				Content: fmt.Sprintf("Session: %s", m.session.Path),
			})
		}

		m.addMessage(ChatMessage{Time: time.Now(), Type: MsgRaw, Content: MOTD})

		// Print system prompt
		if sp := m.agent.SystemPrompt(); sp != "" {
			if m.debugMode {
				m.addMessage(ChatMessage{
					Time:    time.Now(),
					Type:    MsgSystem,
					Content: sp,
				})
			} else {
				m.addMessage(ChatMessage{
					Time:    time.Now(),
					Type:    MsgSystem,
					Content: "*** Injected system prompt",
				})
			}
		}

		m.refreshViewport()

	case tea.KeyMsg:
		// Picker overlay swallows all keys until it closes. Done first so
		// /models can shadow Enter / Up / Down / Esc without fighting the
		// textarea or history handlers below.
		if m.picker != nil {
			m.handlePickerKey(msg)
			return m, nil
		}

		// Up/down arrow: history navigation when cursor is on first/last line,
		// otherwise fall through to textarea for normal multiline cursor movement.
		switch msg.String() {
		case "up":
			if strings.TrimSpace(m.input.Value()) == "" && len(m.queued) > 0 {
				lastIdx := len(m.queued) - 1
				input := m.queued[lastIdx].text
				m.queued = m.queued[:lastIdx]
				m.input.SetValue(input)
				m.input.SetCursor(len(input))
				m.addMessage(ChatMessage{
					Time:    time.Now(),
					Type:    MsgSystem,
					Content: fmt.Sprintf("Unqueued message: %s", input),
				})
				m.refreshViewport()
				return m, nil
			}
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
			return m.handleCtrlCKey()

		case "esc":
			m.handleEscKey()
			return m, nil

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
				m.queued = append(m.queued, queuedMsg{text: input, queuedAt: time.Now()})
				m.addMessage(ChatMessage{
					Time:    time.Now(),
					Type:    MsgSystem,
					Content: fmt.Sprintf("Queued message (agent is busy): %s", input),
				})
				m.queueClearArmed = false
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
			m.cancelActiveTurn("Interrupted by signal.", true)
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
			m.queued = nil
		}
		m.queued = nil
		if m.mp3 != nil {
			m.mp3.stop()
		}
		_ = m.transcript.Close()
		return m, tea.Quit

	case agentEventMsg:
		if msg.ch != m.eventCh {
			return m, nil
		}
		newModel, cmd := m.handleAgentEvent(msg.event)
		// Chain: after handling this event, wait for the next one
		if m.eventCh != nil {
			return newModel, tea.Batch(cmd, waitForAgentEvent(m.eventCh))
		}
		return newModel, cmd

	case agentDoneMsg:
		if msg.ch != nil && msg.ch != m.eventCh {
			return m, nil
		}
		_ = m.transcript.FinishAgentMessage()
		m.streaming = false
		m.agentState = agent.StateIdle
		m.eventCh = nil
		m.cancel = nil
		m.activeToolName = ""
		m.activeToolCallID = ""
		m.escStage = 0
		m.escLast = time.Time{}
		m.ctrlCStage = 0
		m.ctrlCLast = time.Time{}
		m.syncLastAssistantMessage()

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
		// Uses per-context savedIdx so each context's session watermark is tracked
		// independently across /join and /query switches.
		if m.session != nil {
			ctxKey := ircContextToKey(m.turnContext)
			msgs := m.agent.Messages()
			savedIdx := m.agent.SavedIdx(ctxKey)
			ctxLabel := m.turnContext.Label()
			for i := savedIdx; i < len(msgs); i++ {
				e := session.EntryFromFantasyWithBootstrap(
					msgs[i],
					i < m.agent.BootstrapMessageCount(),
				)
				e.Context = ctxLabel
				_ = m.session.Append(e)
			}
			m.agent.SetSavedIdx(ctxKey, len(msgs))
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

		// Mirror the inline checkpoint to the daemon mailbox if a daemon is
		// running. The cmd is nil when daemonBaseDir isn't set (tests, or
		// when no session is initialised), so this is a no-op outside
		// production. The actual flock probe + mailbox write happens off
		// the bubbletea goroutine — see submitDaemonCheckpointCmd.
		daemonCmd := m.submitDaemonCheckpointCmd()

		// Process queued messages: batch all of them into one turn so the agent
		// sees the full context of what was said, not one orphaned message at a time.
		// But first check for staleness — if the queue has been sitting for longer
		// than the threshold, the conversation has moved on and the messages are
		// no longer useful context.
		if len(m.queued) > 0 {
			if time.Since(m.queued[0].queuedAt) > queueStaleThreshold {
				cleared := len(m.queued)
				m.queued = nil
				m.queueClearArmed = false
				m.addMessage(ChatMessage{
					Time:    time.Now(),
					Type:    MsgSystem,
					Content: fmt.Sprintf("Discarded %d queued message(s) older than %v — context changed. Re-send if still relevant.", cleared, queueStaleThreshold),
				})
				m.refreshViewport()
				return m, daemonCmd
			}
			var combined strings.Builder
			for i, msg := range m.queued {
				if i > 0 {
					combined.WriteString("\n")
				}
				combined.WriteString(fmt.Sprintf("[queued msg %d]: %s", i+1, msg.text))
			}
			m.queued = nil
			m.queueClearArmed = false
			m.addMessage(ChatMessage{
				Time:    time.Now(),
				Type:    MsgUser,
				Nick:    m.config.UserNick,
				Content: combined.String(),
			})
			m.refreshViewport()
			return m, tea.Batch(daemonCmd, m.sendToAgent(combined.String()))
		}

		if followUp := m.agent.MaybeQueueFollowUp(); followUp != nil {
			m.addMessage(ChatMessage{
				Time:    time.Now(),
				Type:    MsgSystem,
				Content: fmt.Sprintf("*** %s: continuing...", followUp.Label),
			})
			m.refreshViewport()
			return m, tea.Batch(daemonCmd, m.sendFollowUpToAgent(followUp))
		}

		return m, daemonCmd

	case daemonCheckpointSubmittedMsg:
		// Result of submitDaemonCheckpointCmd. Dispatched on the bubbletea
		// event-loop goroutine so it's safe to mutate Model state here.
		if msg.skipped {
			return m, nil
		}
		if msg.err != nil {
			m.errMsg(fmt.Sprintf("daemon submit failed: %v", msg.err))
			return m, nil
		}
		if msg.jobID == "" {
			return m, nil
		}
		m.NotifyBackgroundActivity(BackgroundActivity{
			Time:    time.Now(),
			Context: msg.context,
			Summary: fmt.Sprintf("session-checkpoint submitted to daemon (%s)", msg.jobID),
		})
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

	case "thinking":
		if len(m.messages) > 0 && m.messages[len(m.messages)-1].Type == MsgThink {
			if m.messages[len(m.messages)-1].Content == "thinking..." {
				m.messages[len(m.messages)-1].Content = ev.Text
			} else {
				m.messages[len(m.messages)-1].Content += ev.Text
			}
		} else {
			m.addMessage(ChatMessage{
				Time:    time.Now(),
				Type:    MsgThink,
				Content: ev.Text,
			})
		}
		m.refreshViewport()

	case "tool_start":
		m.activeToolName = ev.ToolName
		m.activeToolCallID = ev.ToolCallID
		m.addMessage(ChatMessage{
			Time:    time.Now(),
			Type:    MsgTool,
			Nick:    ev.ToolName,
			Content: fmt.Sprintf("calling %s...", ev.ToolName),
		})
		if m.toolPanel != nil {
			// Sync verbosity from live config so /set tool_verbosity takes effect without restart.
			if m.config != nil && m.config.ToolVerbosity != "" {
				m.toolPanel.Verbosity = m.config.ToolVerbosity
			}
			m.toolPanel.StartTool(ev.ToolName, ev.ToolArgs)
		}
		m.refreshViewport()

	case "tool_result":
		if m.activeToolName == ev.ToolName {
			m.activeToolName = ""
			m.activeToolCallID = ""
		}
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
			m.streamBuffer.Reset()
			// Show a styled thinking placeholder in the scroll buffer.
			// It will be replaced with MsgAgent once the first token arrives.
			m.addMessage(ChatMessage{
				Time:    time.Now(),
				Type:    MsgThink,
				Content: "thinking...",
			})
			m.refreshViewport()
		}

	case "error":
		m.activeToolName = ""
		m.activeToolCallID = ""
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
		ch := m.eventCh
		return m, func() tea.Msg { return agentDoneMsg{ch: ch} }
	}

	return m, nil
}

// waitForAgentEvent returns a Cmd that waits for the next event on the channel
func waitForAgentEvent(ch chan agent.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return agentDoneMsg{ch: ch}
		}
		return agentEventMsg{ch: ch, event: ev}
	}
}

func ircContextToMemoryScope(ctx IRCContext) agent.MemoryScope {
	switch ctx.Kind {
	case KindChannel:
		return agent.ChannelMemoryScope(ctx.Channel, nil)
	case KindDirect:
		return agent.QueryMemoryScope(ctx.Target, nil)
	default:
		return agent.RootMemoryScope()
	}
}

func (m *Model) syncAgentContextIfIdle(ctx IRCContext) {
	if m.agent == nil {
		return
	}
	if m.streaming {
		// The active turn owns the agent's current message slice and tool scope.
		// Keep /join's focus update, then let the next startAgentTurn apply it.
		return
	}

	ctxKey := ircContextToKey(ctx)
	m.agent.InitContext(ctxKey)
	m.agent.SetContext(ctxKey)
	m.lastSavedMsgIdx = m.agent.SavedIdx(ctxKey)
	m.agent.SetScope(ircContextToMemoryScope(ctx))
}

func (m *Model) sendToAgent(input string) tea.Cmd {
	return m.startAgentTurn(func(ctx context.Context, ch chan agent.Event) {
		go m.agent.SendMessage(ctx, input, ch)
	})
}

func (m *Model) sendFollowUpToAgent(req *agent.FollowUpRequest) tea.Cmd {
	return m.startAgentTurn(func(ctx context.Context, ch chan agent.Event) {
		go m.agent.SendFollowUp(ctx, req, ch)
	})
}

func (m *Model) startAgentTurn(start func(context.Context, chan agent.Event)) tea.Cmd {
	if m.cancel != nil {
		m.cancel()
	}
	m.queueClearArmed = false
	m.escStage = 0
	m.escLast = time.Time{}
	m.ctrlCStage = 0
	m.ctrlCLast = time.Time{}
	m.activeToolName = ""
	m.activeToolCallID = ""
	m.turnContext = m.focus.Active()
	ctxKey := ircContextToKey(m.turnContext)
	m.agent.InitContext(ctxKey)
	m.agent.SetContext(ctxKey)
	m.lastSavedMsgIdx = m.agent.SavedIdx(ctxKey)
	m.agent.SetScope(ircContextToMemoryScope(m.turnContext))
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.streaming = true

	ch := make(chan agent.Event, 100)
	m.eventCh = ch
	start(ctx, ch)

	return waitForAgentEvent(ch)
}

func (m *Model) cancelActiveTurn(message string, clearQueue bool) {
	if m.cancel != nil {
		m.cancel()
	}
	_ = m.transcript.FinishAgentMessage()
	m.streaming = false
	m.agentState = agent.StateIdle
	m.cancel = nil
	m.eventCh = nil
	m.activeToolName = ""
	m.activeToolCallID = ""
	m.escStage = 0
	m.escLast = time.Time{}
	if clearQueue {
		m.queued = nil
		m.queueClearArmed = false
	}
	m.addMessage(ChatMessage{
		Time:    time.Now(),
		Type:    MsgSystem,
		Content: message,
	})
	m.refreshViewport()
}

func (m *Model) handleCtrlCKey() (Model, tea.Cmd) {
	now := time.Now()
	if !m.ctrlCLast.IsZero() && now.Sub(m.ctrlCLast) > ctrlCGraduationWindow {
		m.ctrlCStage = 0
	}
	m.ctrlCLast = now
	m.ctrlCStage++

	if m.ctrlCStage >= 3 {
		if m.streaming {
			m.cancelActiveTurn("Interrupted.", true)
		} else if len(m.queued) > 0 {
			m.queued = nil
			m.queueClearArmed = false
		}
		return *m, tea.Quit
	}

	if m.ctrlCStage == 1 {
		if m.streaming {
			message := "Interrupted. Press Ctrl+C again to clear queued messages; press it a third time to quit."
			if len(m.queued) > 0 {
				message = fmt.Sprintf("Interrupted. %d queued message(s) remain. Press Ctrl+C again to clear them; press it a third time to quit.", len(m.queued))
			}
			m.cancelActiveTurn(message, false)
			return *m, nil
		}

		if len(m.queued) > 0 {
			m.addMessage(ChatMessage{
				Time:    now,
				Type:    MsgSystem,
				Content: fmt.Sprintf("%d queued message(s) remain. Press Ctrl+C again to clear them; press it a third time to quit.", len(m.queued)),
			})
		} else {
			m.addMessage(ChatMessage{
				Time:    now,
				Type:    MsgSystem,
				Content: "Press Ctrl+C twice more to quit.",
			})
		}
		m.refreshViewport()
		return *m, nil
	}

	if len(m.queued) > 0 {
		cleared := len(m.queued)
		m.queued = nil
		m.queueClearArmed = false
		m.addMessage(ChatMessage{
			Time:    now,
			Type:    MsgSystem,
			Content: fmt.Sprintf("Cleared %d queued message(s). Press Ctrl+C again to quit.", cleared),
		})
		m.refreshViewport()
		return *m, nil
	}

	m.addMessage(ChatMessage{
		Time:    now,
		Type:    MsgSystem,
		Content: "Press Ctrl+C again to quit.",
	})
	m.refreshViewport()
	return *m, nil
}

func (m *Model) handleEscKey() {
	now := time.Now()
	if !m.escLast.IsZero() && now.Sub(m.escLast) > escGraduationWindow {
		m.escStage = 0
		m.queueClearArmed = false
	}
	m.escLast = now

	// Close panel if open (does not count toward the cancel ladder).
	if m.toolPanel != nil && m.toolPanel.Visible {
		m.toolPanel.Visible = false
		m.sysMsg("Tool panel closed.")
		return
	}
	if m.mp3 != nil && m.mp3.visible {
		m.mp3.visible = false
		m.sysMsg("MP3 panel closed.")
		return
	}

	// Queue clearing: when armed after a previous cancel.
	if m.queueClearArmed && len(m.queued) > 0 {
		cleared := len(m.queued)
		m.queued = nil
		m.queueClearArmed = false
		m.escStage = 0
		m.sysMsg(fmt.Sprintf("Cleared %d queued message(s).", cleared))
		return
	}

	// Not streaming: reset and bail.
	if !m.streaming || m.cancel == nil {
		m.escStage = 0
		m.queueClearArmed = false
		return
	}

	m.escStage++

	switch m.escStage {
	case 1:
		// First meaningful Esc: cancel the active tool only, leaving the
		// turn alive so fantasy can process the cancelled tool result and
		// continue with remaining tool calls or the next LLM step.
		if m.activeToolName != "" {
			if err := m.agent.CancelTool(m.activeToolCallID); err != nil {
				m.sysMsg(fmt.Sprintf("Could not cancel %s: %v", m.activeToolName, err))
			} else {
				m.sysMsg(fmt.Sprintf("Cancelled %s.", m.activeToolName))
			}
			m.escStage = 0
			return
		}
		m.sysMsg("Press Esc again to cancel the turn.")

	case 2:
		m.cancelActiveTurnWithQueueArm("Interrupted by Esc.")

	default:
		m.escStage = 0
	}
}

func (m *Model) cancelActiveTurnWithQueueArm(message string) {
	m.cancelActiveTurn(message, false)
	if len(m.queued) > 0 {
		m.queueClearArmed = true
	}
}

func (m *Model) syncLastAssistantMessage() {
	content := m.agent.LastAssistantDisplayContent()
	if content == "" {
		return
	}
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Type != MsgAgent {
			continue
		}
		m.messages[i].Content = content
		return
	}
}

func (m *Model) addMessage(msg ChatMessage) {
	if m.suppressMessages {
		return
	}
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

// daemonCheckpointSubmittedMsg is dispatched by the goroutine launched from
// submitDaemonCheckpointCmd. It reports the outcome of a non-blocking daemon
// IPC submission so Update() can mutate background-activity state on the
// bubbletea event-loop goroutine (the only goroutine allowed to touch Model
// fields directly).
type daemonCheckpointSubmittedMsg struct {
	jobID   string // empty unless the daemon accepted the job
	context string // the active IRC context label at submission time
	err     error  // non-nil when IsLocked or Submit failed
	skipped bool   // true when no daemon was locked, so no submission occurred
}

// submitDaemonCheckpoint submits a session-checkpoint job to the daemon
// mailbox if the daemon is currently running. If no daemon is locked, the
// submission is silently skipped — the TUI already wrote an inline checkpoint
// above, so the daemon path is strictly additive.
//
// This synchronous helper exists for direct test invocation
// (internal/ui/daemon_ipc_test.go). Production code (the agentDoneMsg branch
// of Update) goes through submitDaemonCheckpointCmd so the I/O happens off
// the bubbletea event-loop goroutine.
func (m *Model) submitDaemonCheckpoint() {
	base := m.daemonBaseDir
	if base == "" {
		base = config.BaseDir()
	}
	paths := daemon.Layout(base)
	locked, err := daemon.IsLocked(paths.LockPath)
	if err != nil || !locked {
		return
	}

	sessionPath := ""
	if m.session != nil {
		sessionPath = m.session.Path
	}
	if sessionPath == "" {
		return
	}

	mailbox := daemon.New(base)
	job := daemon.Job{
		Kind:         daemonjobs.KindSessionCheckpoint,
		WorkDir:      m.config.WorkDir,
		SessionPath:  sessionPath,
		SubmittedAt:  time.Now().UTC(),
		RequestorPID: os.Getpid(),
	}
	id, err := mailbox.Submit(job)
	if err != nil {
		m.errMsg(fmt.Sprintf("daemon submit failed: %v", err))
		return
	}

	m.NotifyBackgroundActivity(BackgroundActivity{
		Time:    time.Now(),
		Context: m.focus.ActiveLabel(),
		Summary: fmt.Sprintf("session-checkpoint submitted to daemon (%s)", id),
	})
}

// submitDaemonCheckpointCmd returns a tea.Cmd that probes the daemon lock
// and (if a daemon is running) submits a session-checkpoint job from a
// goroutine. The result is reported back to Update via
// daemonCheckpointSubmittedMsg, so the actual NotifyBackgroundActivity
// state mutation happens on the event-loop goroutine where it's safe.
//
// Returns nil when daemon submission is disabled for this Model (the
// daemonBaseDir field is unset — i.e. tests, or when no session has been
// initialised). A nil tea.Cmd is a valid no-op for tea.Batch.
//
// CLAUDE.md non-blocking-Update rule: the only work performed synchronously
// here is reading three Model fields and capturing them into a closure. The
// flock probe and mailbox write happen inside the closure, which bubbletea
// runs on a worker goroutine.
func (m *Model) submitDaemonCheckpointCmd() tea.Cmd {
	if m.daemonBaseDir == "" {
		return nil
	}
	if m.session == nil || m.session.Path == "" {
		return nil
	}

	base := m.daemonBaseDir
	sessionPath := m.session.Path
	workDir := m.config.WorkDir
	contextLabel := m.focus.ActiveLabel()

	return func() tea.Msg {
		paths := daemon.Layout(base)
		locked, err := daemon.IsLocked(paths.LockPath)
		if err != nil {
			return daemonCheckpointSubmittedMsg{context: contextLabel, err: err}
		}
		if !locked {
			return daemonCheckpointSubmittedMsg{context: contextLabel, skipped: true}
		}

		mailbox := daemon.New(base)
		id, err := mailbox.Submit(daemon.Job{
			Kind:         daemonjobs.KindSessionCheckpoint,
			WorkDir:      workDir,
			SessionPath:  sessionPath,
			SubmittedAt:  time.Now().UTC(),
			RequestorPID: os.Getpid(),
		})
		if err != nil {
			return daemonCheckpointSubmittedMsg{context: contextLabel, err: err}
		}
		return daemonCheckpointSubmittedMsg{jobID: id, context: contextLabel}
	}
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
