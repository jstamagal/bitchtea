package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jstamagal/bitchtea/internal/agent"
	"github.com/jstamagal/bitchtea/internal/config"
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
	streamBuffer strings.Builder // accumulates current agent response

	// Input history
	history    []string
	historyIdx int

	// Queued messages (steering - typed while agent is working)
	queued []string

	// Stats
	toolStats map[string]int
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

	return Model{
		config:     cfg,
		agent:      ag,
		agentState: agent.StateIdle,
		input:      ti,
		spinner:    sp,
		messages:   []ChatMessage{},
		history:    []string{},
		historyIdx: -1,
		toolStats:  make(map[string]int),
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
		m.addMessage(ChatMessage{Time: time.Now(), Type: MsgRaw, Content: SplashArt})
		m.addMessage(ChatMessage{Time: time.Now(), Type: MsgRaw, Content: SplashTagline})
		m.addMessage(ChatMessage{Time: time.Now(), Type: MsgRaw, Content: fmt.Sprintf(ConnectMsg, m.config.Provider, m.config.Model, m.config.WorkDir)})
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
		// Process queued messages
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
	topLeft := TopBarStyle.Render(fmt.Sprintf(" bitchtea — %s ", m.config.Model))
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

	statusLeft := BottomBarStyle.Render(fmt.Sprintf(" [%s] %s ", m.config.AgentNick, stateStr))

	// Tool stats
	var statsItems []string
	for name, count := range m.toolStats {
		statsItems = append(statsItems, fmt.Sprintf("%s(%d)", name, count))
	}
	statsStr := ""
	if len(statsItems) > 0 {
		statsStr = strings.Join(statsItems, " ")
	}
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
			Content: "Commands: /model <name>, /compact, /clear, /quit, /help\n" +
				"  Just type to talk to the agent. It has read, write, edit, bash tools.\n" +
				"  Type while agent is working to queue messages (steering).\n" +
				"  Ctrl+C to interrupt. Ctrl+C again to quit.",
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
				Content: fmt.Sprintf("Model switched to: %s", newModel),
			})
		}
		m.refreshViewport()
		return m, nil

	case "/clear":
		m.messages = []ChatMessage{}
		m.refreshViewport()
		return m, nil

	case "/compact":
		m.addMessage(ChatMessage{
			Time:    time.Now(),
			Type:    MsgSystem,
			Content: "Context compaction not yet implemented. Deal with it.",
		})
		m.refreshViewport()
		return m, nil

	default:
		m.addMessage(ChatMessage{
			Time:    time.Now(),
			Type:    MsgError,
			Content: fmt.Sprintf("Unknown command: %s. Try /help, genius.", cmd),
		})
		m.refreshViewport()
		return m, nil
	}
}
