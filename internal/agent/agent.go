package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"runtime"
	"strings"
	"sync"
	"time"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/llm"
	"github.com/jstamagal/bitchtea/internal/mcp"
	memorypkg "github.com/jstamagal/bitchtea/internal/memory"
	"github.com/jstamagal/bitchtea/internal/tools"
)

// Agent manages the conversation loop.
//
// messages is the canonical fantasy-native conversation history. Phase 3
// flipped this from []llm.Message to []fantasy.Message; conversion to the
// legacy llm.Message shape happens at the streamer boundary (sendMessage,
// Compact) via the adapter helpers in internal/llm.
// ContextKey is the canonical string key for a per-context message history.
// Channel contexts use "#name", direct contexts use the target name.
type ContextKey = string

// DefaultContextKey is the context key for the initial default context.
const DefaultContextKey = "#main"

// promptQueueItem is a queued user prompt with its enqueue time for staleness checks.
type promptQueueItem struct {
	text     string
	queuedAt time.Time
}

type Agent struct {
	client   *llm.Client
	streamer llm.ChatStreamer
	tools    *tools.Registry
	config   *config.Config

	// mu guards the mutable per-conversation state — message slices, queued
	// prompts, injected-path bookkeeping, the active scope, and the
	// per-context save watermark. drainAndMirrorQueuedPrompts runs from
	// PrepareStep on fantasy's goroutine while the main goroutine reads/
	// writes these fields in the done handler and via MessageCount/Messages
	// /etc. go test -race caught the unsynchronized access; this mutex is
	// the fix (HIGH #5).
	//
	// CRITICAL: do NOT hold mu across long-running calls — streamer.StreamChat,
	// LoadMemory/SaveMemory file I/O, channel sends to event channels, or
	// any a.client.* call that might block. Pattern is "copy what you need
	// under the lock, release, then do the long work".
	mu       sync.Mutex
	messages []fantasy.Message

	// Per-context message histories. Each context gets its own conversation
	// slice so /join and /query isolate conversations. The bootstrap prefix
	// (system prompt, context files, persona) is duplicated in each slice.
	contextMsgs     map[ContextKey][]fantasy.Message
	contextSavedIdx map[ContextKey]int // per-context session-save watermark
	currentContext  ContextKey

	bootstrapMsgCount int

	// Memory scope for the active IRC context.
	scope         MemoryScope
	injectedPaths map[string]bool // HOT paths already injected as context messages

	// Queued prompts for mid-turn drain via PrepareStep (bt-p4-queue).
	promptQueue []promptQueueItem

	// Stats
	TurnCount   int
	ToolCalls   map[string]int // tool name -> call count
	StartTime   time.Time
	CostTracker *llm.CostTracker

	lastTurnState         turnState
	activeFollowUpKind    followUpKind
	lastCompletedFollowUp followUpKind
	lastAssistantRaw      string
}

type turnState int

const (
	turnStateIdle turnState = iota
	turnStateCompleted
	turnStateErrored
	turnStateCanceled
)

type followUpKind int

const (
	followUpKindNone followUpKind = iota
	followUpKindAutoNextSteps
	followUpKindAutoNextIdea
)

const (
	autoNextDoneToken = "AUTONEXT_DONE"
	autoIdeaDoneToken = "AUTOIDEA_DONE"
)

// FollowUpRequest is an agent-authored autonomous continuation prompt.
type FollowUpRequest struct {
	Label  string
	Prompt string
	Kind   followUpKind
}

// NewAgent creates a new agent
func NewAgent(cfg *config.Config) *Agent {
	return NewAgentWithStreamer(cfg, nil)
}

func osPrettyName() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return runtime.GOOS // fallback
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
		}
	}
	return runtime.GOOS
}

// NewAgentWithStreamer creates a new agent with an injectable chat streamer.
func NewAgentWithStreamer(cfg *config.Config, streamer llm.ChatStreamer) *Agent {
	client := llm.NewClient(cfg.APIKey, cfg.BaseURL, cfg.Model, cfg.Provider)
	client.SetService(cfg.Service)
	client.SetSamplingParams(cfg.Temperature, cfg.TopP, cfg.TopK, cfg.RepetitionPenalty)
	// Wire effort so streamOnce can forward it as Anthropic output_config.effort
	// (service=="anthropic") or as OpenAI reasoning_effort (service=="cliproxyapi").
	// TODO(Agent D): verify after merge that validEfforts in rc.go covers xhigh/max.
	client.SetEffort(cfg.Effort)
	if streamer == nil {
		streamer = client
	}

	toolRegistry := tools.NewRegistry(cfg.WorkDir, cfg.SessionDir)
	// Pattern 4: apply tool_timeout from config when non-zero; otherwise the
	// registry keeps its built-in default of 300 s.
	if cfg.ToolTimeout > 0 {
		toolRegistry.SetToolTimeout(cfg.ToolTimeout)
	}
	systemPrompt := buildSystemPrompt(cfg)

	a := &Agent{
		client:        client,
		streamer:      streamer,
		tools:         toolRegistry,
		config:        cfg,
		messages:      []fantasy.Message{newSystemMessage(systemPrompt)},
		scope:         RootMemoryScope(),
		injectedPaths: make(map[string]bool),
		ToolCalls:     make(map[string]int),
		StartTime:     time.Now(),
		CostTracker:   llm.NewCostTracker(),
	}

	// --bare: skip persona/context-file/memory injection entirely.
	if !cfg.Bare {
		// Inject context files (BITCHTEA.md / AGENT.md / AGENTS.md etc.)
		contextFiles := DiscoverContextFiles(cfg.WorkDir)
		if contextFiles != "" {
			a.messages = append(a.messages,
				newUserMessage("Here are the project context files:\n\n"+contextFiles),
				newAssistantMessage("Got it. I've read the project context and will follow those conventions."),
			)
		}

		// Inject root MEMORY.md at bootstrap.
		memory := LoadMemory(cfg.WorkDir)
		if memory != "" {
			a.messages = append(a.messages,
				newUserMessage("Here is the session memory from previous work:\n\n"+memory),
				newAssistantMessage("Got it."),
			)
			rootHotPath := ScopedHotMemoryPath(cfg.SessionDir, cfg.WorkDir, RootMemoryScope())
			a.injectedPaths[rootHotPath] = true
		}

		// Persona anchor.
		personaAnchor := buildPersonaAnchor()
		a.messages = append(a.messages, personaAnchor...)
	}

	a.bootstrapMsgCount = len(a.messages)
	a.pushBootstrapToClient()

	// Initialize per-context storage with the default context.
	a.currentContext = DefaultContextKey
	a.contextMsgs = map[ContextKey][]fantasy.Message{
		DefaultContextKey: a.messages,
	}
	a.contextSavedIdx = map[ContextKey]int{
		DefaultContextKey: 0,
	}

	return a
}

// pushBootstrapToClient mirrors the agent's bootstrapMsgCount into the LLM
// client so PrepareStep can place the Anthropic prompt-cache marker on the
// last surviving bootstrap message. Safe to call even when the streamer is a
// test fake — it only mutates the underlying *llm.Client field.
func (a *Agent) pushBootstrapToClient() {
	if a.client == nil {
		return
	}
	a.client.SetBootstrapMsgCount(a.bootstrapMsgCount)
}

// SendMessage processes a user message through the agent loop
// Events are sent to the channel as they happen
func (a *Agent) SendMessage(ctx context.Context, userMsg string, events chan<- Event) {
	a.sendMessage(ctx, userMsg, followUpKindNone, events)
}

// SendFollowUp processes an autonomous follow-up prompt through the agent loop.
func (a *Agent) SendFollowUp(ctx context.Context, req *FollowUpRequest, events chan<- Event) {
	if req == nil {
		close(events)
		return
	}
	a.sendMessage(ctx, req.Prompt, req.Kind, events)
}

// CancelTool cancels the context for a specific active tool call without
// cancelling the entire turn. Returns an error if no active tool with that
// ID exists (e.g., the tool already finished).
func (a *Agent) CancelTool(toolCallID string) error {
	mgr := a.client.ToolContextManager()
	if mgr == nil {
		return fmt.Errorf("no active turn")
	}
	return mgr.CancelTool(toolCallID)
}

// ActiveToolIDs returns the IDs of currently executing tool calls. Returns
// nil if no turn is active.
func (a *Agent) ActiveToolIDs() []string {
	mgr := a.client.ToolContextManager()
	if mgr == nil {
		return nil
	}
	return mgr.ActiveToolIDs()
}

// QueuePrompt adds a user message to the prompt queue for mid-turn drain
// via PrepareStep. Each item is timestamped so staleness can be checked
// when draining. Holds a.mu so promptQueue mutation is race-free with the
// drainer running on fantasy's goroutine.
func (a *Agent) QueuePrompt(text string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.promptQueue = append(a.promptQueue, promptQueueItem{
		text:     text,
		queuedAt: time.Now(),
	})
}

// QueueLen returns the number of queued prompts (mutex-guarded).
func (a *Agent) QueueLen() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.promptQueue)
}

// ClearQueue drops all queued prompts (mutex-guarded).
func (a *Agent) ClearQueue() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.promptQueue = nil
}

// drainAndMirrorQueuedPrompts atomically drains the prompt queue and
// mirrors each item into a.messages as a user message so session save and
// compaction see it. Returns the drained texts for PrepareStep to append
// to prepared.Messages.
//
// Runs on fantasy's goroutine via the SetPromptDrain hook, so it MUST take
// a.mu before touching promptQueue or messages — the main goroutine writes
// the same fields in the done handler. The contextMsgs slice header is also
// resynced under the lock so a subsequent SetContext doesn't lose the new tail.
func (a *Agent) drainAndMirrorQueuedPrompts() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.promptQueue) == 0 {
		return nil
	}
	texts := make([]string, len(a.promptQueue))
	for i, item := range a.promptQueue {
		texts[i] = item.text
		a.messages = append(a.messages, newUserMessage(
			fmt.Sprintf("[queued prompt %d]: %s", i+1, item.text),
		))
	}
	if a.contextMsgs != nil && a.currentContext != "" {
		a.contextMsgs[a.currentContext] = a.messages
	}
	a.promptQueue = nil
	return texts
}

// drainPromptQueueSnapshot returns a snapshot of queued prompt texts and
// mirrors them into a.messages. This is the func() []string hook passed to
// Client.SetPromptDrain for mid-turn drain via PrepareStep.
func (a *Agent) drainPromptQueueSnapshot() []string {
	return a.drainAndMirrorQueuedPrompts()
}

// sendMessage runs one user turn through the streamer. fantasy owns the
// LLM/tool loop now: it streams text, dispatches tool calls into bitchteaTool
// (which calls Registry.Execute), and emits StreamEvents back through the
// channel. This function's job is to translate llm.StreamEvent → agent.Event
// for the UI, accumulate cost, run the follow-up sanitizer on streamed text,
// track per-tool counters, and on the terminal "done" event splice the
// rebuilt transcript (ev.Messages) into a.messages.
func (a *Agent) sendMessage(ctx context.Context, userMsg string, kind followUpKind, events chan<- Event) {
	defer close(events)
	a.activeFollowUpKind = kind

	// Pattern 2 (read-before-edit guard): clear the per-turn files-read set at
	// the start of each new user turn so the guard covers exactly one turn.
	a.tools.ResetTurnState()

	expanded := ExpandFileRefs(userMsg, a.config.WorkDir)
	a.appendMessagesLocked(newUserMessage(injectPerMessagePrefix(expanded)))
	a.TurnCount++

	estimatedInputTokens := a.EstimateTokens()
	streamSanitizer := newFollowUpStreamSanitizer(kind)
	var textAccum strings.Builder
	var gotUsage bool

	events <- Event{Type: "state", State: StateThinking}

	// Wire the prompt drain so PrepareStep can pull mid-turn queued prompts.
	// Staleness check is deferred to the drainer — the old UI-side check is
	// kept as a post-turn guard but the per-step drain is what actually
	// routes mid-turn input.
	a.client.SetPromptDrain(a.drainAndMirrorQueuedPrompts)
	defer a.client.SetPromptDrain(nil)

	streamEvents := make(chan llm.StreamEvent, 100)
	// Bridge to the legacy llm.Message wire shape at the streamer boundary.
	// Client.StreamChat still takes []llm.Message; the in-memory canonical
	// form is fantasy.Message. The adapter is loss-aware (see the docstring
	// on FantasySliceToLLM) and round-trips text + tool calls + tool results.
	//
	// Snapshot the messages slice under the mutex BEFORE handing it to the
	// streamer goroutine. Without the snapshot, the streamer reads a.messages
	// while the drainer (also a goroutine) writes to it — go test -race fires.
	msgsForStream := a.snapshotMessages()
	go a.streamer.StreamChat(ctx, llm.FantasySliceToLLM(msgsForStream), a.tools, streamEvents)

	// Use select to watch both streamEvents and ctx.Done(), so we exit
	// immediately when the context is cancelled (e.g., by ctrl+c) rather
	// than blocking until fantasy closes the channel. Without this, a
	// cancelled turn leaks the goroutine — sendMessage blocks on the range
	// loop forever because streamEvents only closes when StreamChat returns,
	// and fantasy may not exit promptly on ctx cancellation.
	streamDone := false
	for !streamDone {
		select {
		case ev, ok := <-streamEvents:
			if !ok {
				streamDone = true
				break
			}
			switch ev.Type {
			case "text":
				textAccum.WriteString(ev.Text)
				if safeText := streamSanitizer.Consume(ev.Text); safeText != "" {
					events <- Event{Type: "text", Text: safeText}
				}

			case "thinking":
				events <- Event{Type: "thinking", Text: ev.Text}

			case "usage":
				if ev.Usage != nil {
					a.CostTracker.AddTokenUsage(*ev.Usage)
					gotUsage = true
				}

			case "tool_call":
				a.ToolCalls[ev.ToolName]++
				events <- Event{Type: "state", State: StateToolCall}
				events <- Event{
					Type:       "tool_start",
					ToolName:   ev.ToolName,
					ToolCallID: ev.ToolCallID,
					ToolArgs:   ev.ToolArgs,
				}

			case "tool_result":
				events <- Event{
					Type:       "tool_result",
					ToolName:   ev.ToolName,
					ToolCallID: ev.ToolCallID,
					ToolResult: ev.Text,
				}

			case "error":
				if errors.Is(ev.Error, context.Canceled) {
					a.lastTurnState = turnStateCanceled
				} else {
					a.lastTurnState = turnStateErrored
				}
				events <- Event{Type: "state", State: StateIdle}
				events <- Event{Type: "error", Error: ev.Error}
				events <- Event{Type: "done"}
				return

			case "done":
				if safeText := streamSanitizer.Flush(); safeText != "" {
					events <- Event{Type: "text", Text: safeText}
				}
				// Build the splice locally (no lock) — ev.Messages is a
				// stack-local copy from streamOnce and sanitization is pure,
				// so the loop body touches no shared state — then take the
				// lock once via appendMessagesLocked. Keeps PrepareStep's
				// drainer from waiting behind us for the duration of the loop.
				appended := make([]fantasy.Message, 0, len(ev.Messages))
				appendedAssistant := false
				// ev.Messages comes back from the streamer as legacy []llm.Message
				// (fantasy → llm projection inside streamOnce). Lift each one
				// back into fantasy parts before splicing so the canonical
				// history stays fantasy-native.
				for _, m := range ev.Messages {
					if m.Role == "assistant" {
						m.Content = sanitizeAssistantText(m.Content)
						appendedAssistant = true
					}
					appended = append(appended, llm.LLMToFantasy(m))
				}
				if !appendedAssistant && textAccum.Len() > 0 {
					appended = append(appended,
						newAssistantMessage(sanitizeAssistantText(textAccum.String())),
					)
				}
				if len(appended) > 0 {
					a.appendMessagesLocked(appended...)
				}
			}

		case <-ctx.Done():
			// Context was cancelled (ctrl+c / signal). Drain streamEvents
			// in the background so the StreamChat goroutine can close the
			// channel and exit without blocking.
			go func() {
				for range streamEvents {
				}
			}()
			a.lastTurnState = turnStateCanceled
			events <- Event{Type: "state", State: StateIdle}
			events <- Event{Type: "error", Error: ctx.Err()}
			events <- Event{Type: "done"}
			return
		}
	}

	a.lastAssistantRaw = strings.TrimSpace(textAccum.String())
	if !gotUsage {
		a.CostTracker.AddUsage(estimatedInputTokens, textAccum.Len()/4)
	}

	a.lastTurnState = turnStateCompleted
	a.lastCompletedFollowUp = kind
	events <- Event{Type: "state", State: StateIdle}
	events <- Event{Type: "done"}
}

// buildSystemPrompt assembles the stable-per-session system prompt.
//
// Section order is deliberate: persona first (largest stable chunk, anchors
// the cache prefix), then the rule sections, then the environment block. The
// timestamp that used to live at the top is gone — it churned every call to
// buildSystemPrompt and invalidated the entire prompt cache for what amounts
// to clock data the agent doesn't need in-prompt anyway. Wall time still
// reaches the model via tool results when relevant.
//
// Each section is wrapped in XML tags so Opus 4.7 can parse them as
// semantic structure rather than visual decoration.
//
// Tool definitions are NOT enumerated here; the provider attaches schemas
// via the function-calling API and emitting them again as text would just
// double-spend tokens and risk drift between the prose copy and the schema.
//
// Persona override: if cfg.PersonaFile is set and the file is readable, its
// contents replace the compiled-in personaPrompt constant. On read error the
// default is used silently — no crash, a warning to stderr.
func buildSystemPrompt(cfg *config.Config) string {
	var sb strings.Builder

	// --bare: skip persona block; persona_file is also skipped.
	if !cfg.Bare {
		persona := personaPrompt
		if cfg.PersonaFile != "" {
			expanded := cfg.PersonaFile
			if len(expanded) > 1 && expanded[:2] == "~/" {
				if home, err := os.UserHomeDir(); err == nil {
					expanded = home + expanded[1:]
				}
			}
			if data, err := os.ReadFile(expanded); err == nil {
				persona = string(data)
			} else {
				fmt.Fprintf(os.Stderr, "bitchtea: persona_file %q unreadable (%v); using default persona\n", cfg.PersonaFile, err)
			}
		}

		sb.WriteString("<persona>\n")
		sb.WriteString(persona)
		sb.WriteString("\n</persona>\n\n")
	}

	writeMemoryPrompt(&sb)
	writeToolRules(&sb)
	writeEnvironment(&sb, cfg)

	return sb.String()
}

func writeMemoryPrompt(sb *strings.Builder) {
	sb.WriteString("<memory_workflow>\n")
	sb.WriteString("- Before substantive work or when prior decision/history matters: call search_memory first; do not guess.\n")
	sb.WriteString("- After finishing meaningful work (a decision, a fix, a conclusion, a preference learned): call write_memory with a clear title and concise content. Skip trivia and small talk.\n")
	sb.WriteString("- Scope: omit (or 'current') for work tied to the active channel/query — usually correct. Use scope='root' only for global facts that apply everywhere. Use scope='channel'/'query' with name=… to write into a different context than the active one.\n")
	sb.WriteString("- daily=true for ephemeral session events worth archiving by date; default (hot file) for durable knowledge you'll want surfaced again.\n")
	sb.WriteString("- Consolidate: search before writing so you extend existing notes instead of duplicating them. Don't write what's already remembered.\n")
	sb.WriteString("</memory_workflow>\n\n")
}

// writeToolRules emits behavioral rules for tool use. Tool *schemas* are
// attached separately by the provider's function-calling API, so this section
// covers HOW to use tools, not WHAT tools exist.
func writeToolRules(sb *strings.Builder) {
	sb.WriteString("<tool_rules>\n")
	sb.WriteString("- Tool schemas are attached to the request. Call tools through the provider's function-calling API; never print JSON in your response in place of an actual tool call.\n")
	sb.WriteString("- Files: read before edit; edit only exact unique text; write only for new files or full rewrites. Prefer parallel reads when surveying multiple files at once.\n")
	sb.WriteString("- Shell vs terminal: use bash for one-shot commands whose stdout/stderr is enough. Use terminal_start / terminal_keys / terminal_wait / terminal_snapshot / terminal_resize / terminal_close for anything interactive — REPL, editor, TUI, or programs that need follow-up input.\n")
	sb.WriteString("- vim/vi: use terminal_keys. Quit safely with [\"esc\", \"esc\", \":q!\", \"enter\"] or save with [\"esc\", \":wq\", \"enter\"].\n")
	sb.WriteString("- Parallelize independent operations in a single response: multi-file reads in one batch, build + lint together, fan out independent tool calls. Do not serialize what does not need to be serial.\n")
	sb.WriteString("- Do NOT call a tool when you already know the answer or can reason it through. Trivial questions get answered directly; tools cost tokens, latency, and LO's patience.\n")
	sb.WriteString("- Reuse prior tool results within the same turn; do not re-query without a reason.\n")
	sb.WriteString("- Summarize tool results in 1–2 useful lines unless LO asks for raw output.\n")
	sb.WriteString("- Confirm once before destructive or hard-to-reverse operations (delete, force-push, schema drop, send/publish). Read-only and reversible work proceeds without ceremony.\n")
	sb.WriteString("</tool_rules>\n\n")
}

// writeEnvironment emits a small environment block describing the host the
// agent is running on. No timestamp — clock data churned the prompt cache
// for no benefit. OS / host / user / CWD are stable across a session.
func writeEnvironment(sb *strings.Builder, cfg *config.Config) {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown"
	}

	username := "unknown"
	if u, err := user.Current(); err == nil && u.Username != "" {
		username = u.Username
	}

	sb.WriteString("<environment>\n")
	sb.WriteString(fmt.Sprintf("- OS: %s (%s)\n", osPrettyName(), runtime.GOARCH))
	sb.WriteString(fmt.Sprintf("- Host: %s\n", hostname))
	sb.WriteString(fmt.Sprintf("- User: %s\n", username))
	sb.WriteString(fmt.Sprintf("- CWD: %s\n", cfg.WorkDir))
	sb.WriteString("</environment>\n")
}

// personaPrompt is the default persona/style harness shipped in the public repo.
// This text is wrapped in the <persona> XML block of the system prompt at
// session start; the persona anchor exchange (buildPersonaAnchor) points the
// model at this block right before the first real user message.
//
// This default is intentionally safe for a public GitHub repo — no private
// content. To use a custom persona, set 'persona_file' in ~/.bitchtearc or
// via '/set persona_file /path/to/persona.md'. The file contents replace this
// constant at runtime; the constant is the fallback when no file is configured
// or the file cannot be read.
//
// Backtick-safe: Go raw-string literal. Content MUST NOT contain a backtick.
// Markdown code markers around identifiers use apostrophes instead.
var personaPrompt = `You are bitchtea — a terminal-native coding assistant built on the Charm stack.

<identity>
bitchtea is a Go CLI coding agent. The runtime is built on Bubble Tea (TUI
event loop), Lipgloss (styling), and Glamour (markdown rendering). The
interaction model follows IRC conventions: channels (/join #name) and direct
queries (/query nick) each maintain their own isolated conversation history
and memory scope. The daemon (bitchtea daemon start) handles background
checkpointing and memory consolidation out of process.
</identity>

<role>
Your job is to be a competent, direct coding assistant inside a terminal. You
read files, write files, edit files, run shell commands, start interactive
terminal sessions, search and write persistent memory, and reason about code.
You do not add ceremony to simple tasks. You do not ask for permission to do
the obvious next thing. You do not pad responses with summaries of what you
just did — the diff speaks for itself.

Operating mode:
- Read before edit. Always.
- Parallelize independent tool calls: fan out multi-file reads, run build and
  lint together, do not serialize work that does not need to be serial.
- Match the language, style, and conventions already in the repo. If the
  codebase uses a pattern, continue it. Do not introduce a new abstraction
  where the existing one is sufficient.
- Production-quality error handling. No "TODO: handle this." No returning nil
  with a comment. If a path can fail, the caller learns about it.
- Do not over-engineer beyond the literal ask. Terse prompt does not mean
  infer a large scope — deliver what was asked, note what was deferred if
  anything, and stop.
- If something is genuinely ambiguous and guessing wrong costs real time,
  say so briefly. Otherwise make the reasonable call and move.
</role>

<project_context>
The bitchtea codebase is a dogfooding project — the agent being developed is
the agent used to develop it. This has two practical implications:

1. Changes to bootstrap paths, prompt loading, the agent loop, tool dispatch,
   or session serialization affect the running instance. Extra care on those
   paths is warranted: verify that the next cold start still works.

2. The codebase structure (as of current state) follows a strict acyclic
   dependency graph:
     main -> agent, config, llm, session, tools, ui
     ui   -> agent, config, llm, session, sound
     agent -> config, llm, memory, tools
     session -> llm
     llm -> tools
     tools -> memory
   Do not introduce an upward edge. A change that cycles (e.g. llm -> agent)
   is wrong on arrival.
</project_context>

<tool_surface>
Available tools:
- 'read', 'write', 'edit' — file operations
- 'bash' — one-shot shell commands; stdout/stderr returned as text
- 'terminal_start', 'terminal_send', 'terminal_keys', 'terminal_snapshot',
  'terminal_wait', 'terminal_resize', 'terminal_close' — full PTY suite for
  interactive programs (REPLs, editors, TUIs, programs that need follow-up input)
- 'search_memory', 'write_memory' — persistent scoped memory store
- 'preview_image' — image display in the terminal

Use 'bash' for one-shot work. Use the terminal suite when a program needs
interactive input or produces output that must be observed before continuing.
</tool_surface>

<memory_practice>
Search memory before starting work on anything where prior context, decisions,
or learned preferences might apply. Write memory after completing meaningful
work: a decision made, a bug fixed, a design chosen, a preference learned.
Skip trivial exchanges and tool noise. Scope: omit for the active context,
scope='root' for facts that apply globally across all contexts.
</memory_practice>

<voice>
Direct. No filler. No corpo-speak. No "Great question!" No summary of what
you just did — the output is the summary. If something is a bad idea, say so
and why. Technical precision over social cushioning. Short sentences over
elaborate ones when both convey the same information.
</voice>`

// personaRehearsal is the assistant's reply in the final bootstrap exchange.
// This is the last assistant message the model sees before the first real
// user message. It should demonstrate the voice, not explain it.
var personaRehearsal = `Ready. Drop the task.`

// personaAnchorReminder is the synthetic user message in the anchor exchange.
// It points at the system-prompt <persona> block instead of repeating the
// entire personaPrompt — the previous version sent ~6KB of duplicated text
// every bootstrap (and every Reset / RestoreMessages). The "last thing the
// model sees before the user's first real message is your voice" anchor
// pattern is preserved by the rehearsal reply below; the user message just
// needs to hand the baton.
var personaAnchorReminder = `Adopt the persona and style defined in your <persona> block above. Reply in voice to confirm.`

// buildPersonaAnchor returns a synthetic user/assistant pair that re-anchors
// the persona after all context injection. This is the last thing the model
// sees before the user's first real message.
func buildPersonaAnchor() []fantasy.Message {
	return []fantasy.Message{
		newUserMessage(personaAnchorReminder),
		newAssistantMessage(personaRehearsal),
	}
}

// PerMessagePrefix is prepended to every user message before it enters the
// conversation. Use this to keep the persona fresh in long sessions.
// Leave empty for no injection. Edit this string to customize.
var PerMessagePrefix = ``

// injectPerMessagePrefix prepends PerMessagePrefix to the user's message
// if it is non-empty. Returns the message unchanged otherwise.
func injectPerMessagePrefix(msg string) string {
	if PerMessagePrefix == "" {
		return msg
	}
	return PerMessagePrefix + "\n" + msg
}

// MessageCount returns the number of messages in the conversation (mutex-guarded).
func (a *Agent) MessageCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.messages)
}

// Model returns the current model name
func (a *Agent) Model() string {
	return a.config.Model
}

// SetModel changes the model. Routes through Client.SetModel so the
// cached fantasy.Provider/LanguageModel are invalidated.
func (a *Agent) SetModel(model string) {
	a.config.Model = model
	a.client.SetModel(model)
}

// SetBaseURL changes the API base URL and invalidates the cached provider.
func (a *Agent) SetBaseURL(url string) {
	a.config.BaseURL = url
	a.client.SetBaseURL(url)
}

// SetAPIKey changes the API key and invalidates the cached provider.
func (a *Agent) SetAPIKey(key string) {
	a.config.APIKey = key
	a.client.SetAPIKey(key)
}

// SetProvider changes the LLM provider and invalidates the cached provider.
func (a *Agent) SetProvider(provider string) {
	a.config.Provider = provider
	a.client.SetProvider(provider)
}

// SetService changes the upstream service identity (cfg.Service). This is
// what gates per-service behavior like Anthropic prompt-cache markers; the
// wire format / dialect stays governed by Provider. Does not invalidate the
// cached provider because Service is consumed inside PrepareStep, not during
// provider construction.
func (a *Agent) SetService(service string) {
	a.config.Service = service
	a.client.SetService(service)
}

// SetDebugHook installs (or clears) the debug hook; rebuilds the HTTP
// transport on the next call.
func (a *Agent) SetDebugHook(hook func(llm.DebugInfo)) {
	a.client.SetDebugHook(hook)
}

// SetMCPManager wires (or clears) an MCP client manager whose tools will be
// merged into every subsequent turn's tool list. Per the contract, MCP is
// opt-in: when no manager is set the agent loop behaves exactly as it does
// without MCP — local tools only.
//
// Wiring is forwarded to the underlying *llm.Client so the assembly happens
// inside streamOnce, where the per-turn tool list is built. The manager is
// NOT auto-started here; that is the bootstrap's responsibility (bt-p6-verify).
func (a *Agent) SetMCPManager(m *mcp.Manager) {
	if a.client == nil {
		return
	}
	a.client.SetMCPManager(m)
}

// Config returns the current config (for profile save)
func (a *Agent) Config() *config.Config {
	return a.config
}

// snapshotMessages returns a copy of a.messages under the lock so callers
// can iterate the slice without racing against concurrent mutations.
func (a *Agent) snapshotMessages() []fantasy.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	snap := make([]fantasy.Message, len(a.messages))
	copy(snap, a.messages)
	return snap
}

// appendMessagesLocked appends msgs to a.messages and resyncs the active
// contextMsgs slice header under the lock. Used by sendMessage and the
// queue mirror so message appends don't race against snapshotMessages and
// don't leave the per-context map pointing at a stale array.
func (a *Agent) appendMessagesLocked(msgs ...fantasy.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.messages = append(a.messages, msgs...)
	if a.contextMsgs != nil && a.currentContext != "" {
		a.contextMsgs[a.currentContext] = a.messages
	}
}

// EstimateTokens returns a rough token count (chars / 4). Counts the text
// projection of each fantasy.Message so the estimate stays comparable to the
// pre-Phase-3 behavior — multi-part assistants and tool results contribute
// the same characters they would have when collapsed into llm.Message.Content.
func (a *Agent) EstimateTokens() int {
	// Snapshot so we don't iterate a.messages while a concurrent drain or
	// done-handler splice mutates it.
	msgs := a.snapshotMessages()
	total := 0
	for _, m := range msgs {
		total += len(messageText(m))
	}
	return total / 4
}

// Compact summarizes the conversation to reduce context size.
// Keeps the system prompt and last N messages, replaces the middle with a summary.
func (a *Agent) Compact(ctx context.Context) error {
	if len(a.messages) < 6 {
		return nil // nothing to compact
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	end := len(a.messages) - 4
	bootstrapEnd := a.bootstrapMsgCount
	if bootstrapEnd < 1 {
		bootstrapEnd = 1
	}
	if bootstrapEnd > end {
		return nil
	}
	compacted := a.messages[bootstrapEnd:end]
	if err := a.flushCompactedMessagesToDailyMemory(ctx, compacted); err != nil {
		return err
	}

	// Build a summary request
	var sb strings.Builder
	sb.WriteString("Summarize the following conversation concisely, preserving all important ")
	sb.WriteString("technical details, decisions made, files modified, and current state:\n\n")

	// Everything except the bootstrap prefix and last 4 messages.
	for _, m := range compacted {
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", m.Role, truncateStr(messageText(m), 500)))
	}

	summaryMsgs := []llm.Message{
		{Role: "user", Content: sb.String()},
	}

	events := make(chan llm.StreamEvent, 100)
	go a.streamer.StreamChat(ctx, summaryMsgs, nil, events)

	var summary strings.Builder
	for ev := range events {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch ev.Type {
		case "text":
			summary.WriteString(ev.Text)
		case "error":
			if ev.Error != nil {
				return ev.Error
			}
			return fmt.Errorf("compact summary stream error")
		}
	}

	// Rebuild messages: bootstrap prefix + summary + last 4.
	keep := a.messages[end:]
	rebuilt := append([]fantasy.Message(nil), a.messages[:bootstrapEnd]...)
	rebuilt = append(rebuilt, newUserMessage("[Previous conversation summary]:\n"+summary.String()))
	a.messages = rebuilt
	a.messages = append(a.messages, keep...)
	a.contextMsgs[a.currentContext] = a.messages
	a.contextSavedIdx[a.currentContext] = len(a.messages)

	return nil
}

func (a *Agent) flushCompactedMessagesToDailyMemory(ctx context.Context, messages []fantasy.Message) error {
	if len(messages) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	var sb strings.Builder
	sb.WriteString("Extract durable memory from this conversation slice before it is compacted.\n")
	sb.WriteString("Return concise markdown bullets covering only lasting facts: user preferences, decisions, completed work, relevant files, and open follow-ups.\n")
	sb.WriteString("Skip transient chatter and tool noise. If nothing deserves durable memory, reply with exactly NONE.\n\n")
	for _, m := range messages {
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", m.Role, truncateStr(messageText(m), 700)))
	}

	streamEvents := make(chan llm.StreamEvent, 100)
	go a.streamer.StreamChat(ctx, []llm.Message{{Role: "user", Content: sb.String()}}, nil, streamEvents)

	var summary strings.Builder
	for ev := range streamEvents {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch ev.Type {
		case "text":
			summary.WriteString(ev.Text)
		case "error":
			if ev.Error != nil {
				return ev.Error
			}
			return fmt.Errorf("compact memory stream error")
		}
	}

	text := strings.TrimSpace(summary.String())
	if text == "" || strings.EqualFold(text, "none") {
		return nil
	}

	return AppendScopedDailyMemory(a.config.SessionDir, a.config.WorkDir, a.scope, time.Now(), memorypkg.SourceCompaction, text)
}

// AutoNextPrompt returns the auto-next-steps prompt
func AutoNextPrompt() string {
	return "What are the next steps? If there is remaining work, do it now instead of just describing it, " +
		"including tests or verification when they matter. If everything is done, start your response with " +
		autoNextDoneToken + " and briefly say why."
}

// AutoIdeaPrompt returns the auto-next-idea prompt
func AutoIdeaPrompt() string {
	return "Based on what you've done so far, pick the next highest-impact improvement and implement it now. " +
		"If there is nothing worthwhile left to improve, start your response with " +
		autoIdeaDoneToken + " and briefly say why."
}

// MaybeQueueFollowUp returns an autonomous continuation prompt derived from the
// last completed assistant turn. Failed or canceled turns never queue follow-up
// work because they need an explicit user decision first.
func (a *Agent) MaybeQueueFollowUp() *FollowUpRequest {
	if a.lastTurnState != turnStateCompleted {
		return nil
	}
	if !a.config.AutoNextSteps && !a.config.AutoNextIdea {
		return nil
	}

	switch a.lastCompletedFollowUp {
	case followUpKindAutoNextIdea:
		if assistantMarkedDone(a.lastAssistantRaw, autoIdeaDoneToken) {
			return nil
		}
		return &FollowUpRequest{
			Label:  "auto-next-steps",
			Prompt: AutoNextPrompt(),
			Kind:   followUpKindAutoNextSteps,
		}
	case followUpKindAutoNextSteps:
		if assistantMarkedDone(a.lastAssistantRaw, autoNextDoneToken) {
			if a.config.AutoNextIdea {
				return &FollowUpRequest{
					Label:  "auto-next-idea",
					Prompt: AutoIdeaPrompt(),
					Kind:   followUpKindAutoNextIdea,
				}
			}
			return nil
		}
		return &FollowUpRequest{
			Label:  "auto-next-steps",
			Prompt: AutoNextPrompt(),
			Kind:   followUpKindAutoNextSteps,
		}
	default:
		if a.config.AutoNextSteps {
			return &FollowUpRequest{
				Label:  "auto-next-steps",
				Prompt: AutoNextPrompt(),
				Kind:   followUpKindAutoNextSteps,
			}
		}
		if a.config.AutoNextIdea {
			return &FollowUpRequest{
				Label:  "auto-next-idea",
				Prompt: AutoIdeaPrompt(),
				Kind:   followUpKindAutoNextIdea,
			}
		}
		return nil
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func compactWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func assistantMarkedDone(text, token string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	return strings.HasPrefix(strings.ToUpper(trimmed), token)
}

func sanitizeAssistantText(text string) string {
	trimmedLeft := strings.TrimLeft(text, " \t\r\n")
	for _, token := range []string{autoNextDoneToken, autoIdeaDoneToken} {
		if !strings.HasPrefix(strings.ToUpper(trimmedLeft), token) {
			continue
		}
		withoutToken := trimmedLeft[len(token):]
		withoutToken = strings.TrimLeft(withoutToken, " \t\r\n:-")
		if withoutToken == "" {
			return "Done."
		}
		return withoutToken
	}
	return text
}

type followUpStreamSanitizer struct {
	state  followUpStreamState
	buffer string
}

type followUpStreamState int

const (
	followUpStreamPass followUpStreamState = iota
	followUpStreamUndecided
	followUpStreamStrip
)

func newFollowUpStreamSanitizer(kind followUpKind) *followUpStreamSanitizer {
	state := followUpStreamPass
	if kind != followUpKindNone {
		state = followUpStreamUndecided
	}
	return &followUpStreamSanitizer{state: state}
}

func (s *followUpStreamSanitizer) Consume(chunk string) string {
	switch s.state {
	case followUpStreamPass:
		return chunk
	case followUpStreamStrip:
		trimmed := strings.TrimLeft(chunk, " \t\r\n:-")
		if trimmed == "" {
			return ""
		}
		s.state = followUpStreamPass
		return trimmed
	default:
		s.buffer += chunk
		return s.consumeBuffer()
	}
}

func (s *followUpStreamSanitizer) Flush() string {
	if s.state != followUpStreamUndecided || s.buffer == "" {
		return ""
	}
	out := s.buffer
	s.buffer = ""
	s.state = followUpStreamPass
	return out
}

func (s *followUpStreamSanitizer) consumeBuffer() string {
	trimmed := strings.TrimLeft(s.buffer, " \t\r\n")
	upper := strings.ToUpper(trimmed)
	for _, token := range []string{autoNextDoneToken, autoIdeaDoneToken} {
		switch {
		case strings.HasPrefix(token, upper):
			return ""
		case strings.HasPrefix(upper, token):
			rest := trimmed[len(token):]
			s.buffer = ""
			s.state = followUpStreamStrip
			return s.Consume(rest)
		}
	}

	out := s.buffer
	s.buffer = ""
	s.state = followUpStreamPass
	return out
}

func (a *Agent) lastAssistantContent() string {
	if strings.TrimSpace(a.lastAssistantRaw) != "" {
		return a.lastAssistantRaw
	}
	for i := len(a.messages) - 1; i >= 0; i-- {
		if a.messages[i].Role != fantasy.MessageRoleAssistant {
			continue
		}
		content := strings.TrimSpace(messageText(a.messages[i]))
		if content != "" {
			return content
		}
	}
	return ""
}

// LastAssistantDisplayContent returns the latest assistant message as it should
// be shown to the user after any autonomous control tokens are removed.
func (a *Agent) LastAssistantDisplayContent() string {
	for i := len(a.messages) - 1; i >= 0; i-- {
		if a.messages[i].Role != fantasy.MessageRoleAssistant {
			continue
		}
		content := strings.TrimSpace(messageText(a.messages[i]))
		if content != "" {
			return content
		}
	}
	return ""
}

// SetScope updates the active memory scope. If the scoped HOT.md is non-empty
// and has not been injected yet, it is added to the conversation as context.
func (a *Agent) SetScope(scope MemoryScope) {
	a.scope = scope
	a.tools.SetScope(scope)

	hot := LoadScopedMemory(a.config.SessionDir, a.config.WorkDir, scope)
	if hot == "" {
		return
	}
	path := ScopedHotMemoryPath(a.config.SessionDir, a.config.WorkDir, scope)
	if a.injectedPaths[path] {
		return
	}
	a.injectedPaths[path] = true
	a.messages = append(a.messages,
		newUserMessage("Context memory for "+scopeLabel(scope)+":\n\n"+hot),
		newAssistantMessage("Got it."),
	)
}

// Scope returns the memory scope currently used by agent memory tools.
func (a *Agent) Scope() MemoryScope {
	return a.scope
}

func scopeLabel(scope MemoryScope) string {
	switch scope.Kind {
	case MemoryScopeChannel:
		return "#" + scope.Name
	case MemoryScopeQuery:
		return scope.Name
	default:
		return "root"
	}
}

// SystemPrompt returns the active system prompt text (mutex-guarded).
func (a *Agent) SystemPrompt() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.messages) > 0 && a.messages[0].Role == fantasy.MessageRoleSystem {
		return messageText(a.messages[0])
	}
	return ""
}

// Messages returns a snapshot of the current message history (for session
// saving). The canonical in-memory shape is fantasy.Message — callers that
// still need the legacy llm.Message form should pass through
// llm.FantasySliceToLLM. Returns a copy so the caller can iterate without
// racing against in-flight stream goroutines mutating a.messages.
func (a *Agent) Messages() []fantasy.Message {
	return a.snapshotMessages()
}

// BootstrapMessageCount returns how many startup-injected messages should be
// hidden from the normal transcript when persisted.
func (a *Agent) BootstrapMessageCount() int {
	return a.bootstrapMsgCount
}

// InjectNote adds a synthetic context note to the conversation history without
// running the agent loop. Used for catch-up on channel invite so the agent
// is aware of the invited persona and the prior conversation.
func (a *Agent) InjectNote(note string) {
	a.messages = append(a.messages,
		newUserMessage(note),
		newAssistantMessage("Understood."),
	)
}

// RestoreMessages replaces the current message history with a prior session.
// It resets session-local stats so that counters and timing start fresh.
//
// Phase 3: the input is fantasy-native. The agent's first message is forced
// to be a system message carrying the freshly built system prompt — if the
// restored slice already starts with one, its content is overwritten;
// otherwise a system message is prepended. This matches the pre-Phase-3
// behavior so callers (UI ResumeSession, headless --resume) don't change.
//
// bt-wire.10: the pre-resume `injectedPaths` markers are stale once the
// active message slice is replaced — any HOT.md content the SetScope call at
// NewModel time appended is now gone, but the marker would otherwise persist
// and prevent re-injection on the next SetScope. Reset the markers and then
// scan the freshly-restored slice so we re-track only those scopes whose
// "Context memory for X:" / root-memory bootstrap message is actually present.
func (a *Agent) RestoreMessages(messages []fantasy.Message) {
	a.messages = append([]fantasy.Message(nil), messages...)
	systemPrompt := buildSystemPrompt(a.config)
	if len(a.messages) == 0 || a.messages[0].Role != fantasy.MessageRoleSystem {
		a.messages = append([]fantasy.Message{newSystemMessage(systemPrompt)}, a.messages...)
	} else {
		a.messages[0] = newSystemMessage(systemPrompt)
	}

	// Reset session-local stats so resume starts with clean counters.
	//
	// bootstrapMsgCount = 1 covers the freshly-rebuilt system message at
	// index 0 — enough to keep applyAnthropicCacheMarkers in business across
	// the resumed session. Setting this to 0 (the previous behavior) caused
	// bootstrapPreparedIndex to return -1 for every subsequent turn, no-op-ing
	// cache-marker placement permanently. Restored sessions don't carry the
	// AGENTS.md / MEMORY / persona-anchor exchange in their saved transcript
	// by design — those bootstrap user/assistant pairs are session-local — so
	// 1 is the maximum boundary we can safely claim here.
	a.bootstrapMsgCount = 1
	a.pushBootstrapToClient()
	a.TurnCount = 0
	a.ToolCalls = make(map[string]int)
	a.CostTracker = llm.NewCostTracker()
	a.StartTime = time.Now()
	a.lastTurnState = turnStateIdle
	a.activeFollowUpKind = followUpKindNone
	a.lastCompletedFollowUp = followUpKindNone
	a.lastAssistantRaw = ""

	// Drop pre-resume injection markers (they refer to messages that were
	// just overwritten) and rebuild from whatever survives in the restored
	// slice so SetScope on the next turn neither double-injects nor stays a
	// no-op when the saved bootstrap was missing the scoped memory.
	a.injectedPaths = make(map[string]bool)
	a.scanInjectedPathsFromMessages(a.messages)

	// Sync the context map so the current context points to the restored messages.
	a.contextMsgs[a.currentContext] = a.messages
}

// scanInjectedPathsFromMessages walks msgs looking for the synthetic context
// exchanges produced by SetScope ("Context memory for <label>:") and the root
// MEMORY.md bootstrap injection ("Here is the session memory from previous
// work:") and records the corresponding HOT path in a.injectedPaths. Used by
// RestoreMessages and RestoreContextMessages to keep injection bookkeeping in
// sync with the post-resume message state.
func (a *Agent) scanInjectedPathsFromMessages(msgs []fantasy.Message) {
	for _, msg := range msgs {
		if msg.Role != fantasy.MessageRoleUser {
			continue
		}
		text := messageText(msg)
		if strings.HasPrefix(text, "Here is the session memory from previous work:") {
			rootHotPath := ScopedHotMemoryPath(a.config.SessionDir, a.config.WorkDir, RootMemoryScope())
			a.injectedPaths[rootHotPath] = true
			continue
		}
		if !strings.HasPrefix(text, "Context memory for ") {
			continue
		}
		rest := text[len("Context memory for "):]
		colonIdx := strings.IndexByte(rest, ':')
		if colonIdx <= 0 {
			continue
		}
		label := strings.TrimSpace(rest[:colonIdx])
		if label == "" {
			continue
		}
		scope := scopeFromLabel(label)
		path := ScopedHotMemoryPath(a.config.SessionDir, a.config.WorkDir, scope)
		a.injectedPaths[path] = true
	}
}

// scopeFromLabel inverts scopeLabel: "root" → root, "#X" → channel X,
// otherwise → query scope keyed by the bare label. The mapping is best-effort
// (we cannot reconstruct nested parents from a label alone), which is fine
// for the only caller — injection-marker rebuild — because all that matters
// is that ScopedHotMemoryPath returns the same path the original SetScope
// call produced.
func scopeFromLabel(label string) MemoryScope {
	switch {
	case label == "root":
		return RootMemoryScope()
	case strings.HasPrefix(label, "#"):
		return ChannelMemoryScope(label[1:], nil)
	default:
		return QueryMemoryScope(label, nil)
	}
}

// Reset clears the conversation history back to its bootstrap state — system
// prompt, discovered context files (BITCHTEA.md/AGENT.md/AGENTS.md), root
// MEMORY.md, and the persona anchor — and resets session-local stats. When
// cfg.Bare is true, persona/context/memory injection is skipped.
func (a *Agent) Reset() {
	systemPrompt := buildSystemPrompt(a.config)
	a.messages = []fantasy.Message{newSystemMessage(systemPrompt)}

	a.injectedPaths = make(map[string]bool)

	if !a.config.Bare {
		contextFiles := DiscoverContextFiles(a.config.WorkDir)
		if contextFiles != "" {
			a.messages = append(a.messages,
				newUserMessage("Here are the project context files:\n\n"+contextFiles),
				newAssistantMessage("Got it. I've read the project context and will follow those conventions."),
			)
		}

		memory := LoadMemory(a.config.WorkDir)
		if memory != "" {
			a.messages = append(a.messages,
				newUserMessage("Here is the session memory from previous work:\n\n"+memory),
				newAssistantMessage("Got it."),
			)
			rootHotPath := ScopedHotMemoryPath(a.config.SessionDir, a.config.WorkDir, RootMemoryScope())
			a.injectedPaths[rootHotPath] = true
		}

		a.messages = append(a.messages, buildPersonaAnchor()...)
	}

	a.bootstrapMsgCount = len(a.messages)
	a.pushBootstrapToClient()

	a.TurnCount = 0
	a.ToolCalls = make(map[string]int)
	a.CostTracker = llm.NewCostTracker()
	a.StartTime = time.Now()
	a.lastTurnState = turnStateIdle
	a.activeFollowUpKind = followUpKindNone
	a.lastCompletedFollowUp = followUpKindNone
	a.lastAssistantRaw = ""

	// Reset per-context storage to just the default context.
	a.currentContext = DefaultContextKey
	a.contextMsgs = map[ContextKey][]fantasy.Message{
		DefaultContextKey: a.messages,
	}
	a.contextSavedIdx = map[ContextKey]int{
		DefaultContextKey: 0,
	}
}

// Elapsed returns time since agent creation
func (a *Agent) Elapsed() time.Duration {
	return time.Since(a.StartTime)
}

// newSystemMessage builds a single-text-part fantasy.Message with the system
// role. This is the canonical bootstrap shape — one TextPart per system /
// user / assistant prompt — so equality checks across Reset/Restore stay
// stable.
func newSystemMessage(text string) fantasy.Message {
	return fantasy.Message{
		Role:    fantasy.MessageRoleSystem,
		Content: []fantasy.MessagePart{fantasy.TextPart{Text: text}},
	}
}

// newUserMessage builds a single-text-part fantasy.Message with the user role.
func newUserMessage(text string) fantasy.Message {
	return fantasy.Message{
		Role:    fantasy.MessageRoleUser,
		Content: []fantasy.MessagePart{fantasy.TextPart{Text: text}},
	}
}

// newAssistantMessage builds a single-text-part fantasy.Message with the
// assistant role. Tool calls are added as separate parts when needed; this
// helper is for the text-only bootstrap and post-stream synthesized cases.
func newAssistantMessage(text string) fantasy.Message {
	return fantasy.Message{
		Role:    fantasy.MessageRoleAssistant,
		Content: []fantasy.MessagePart{fantasy.TextPart{Text: text}},
	}
}

// messageText returns the concatenated text projection of a fantasy.Message.
// Used by EstimateTokens, Compact, and the assistant-display helpers — the
// same characters that the legacy llm.Message.Content collapse produced
// before Phase 3, so behavior on those code paths is unchanged.
func messageText(m fantasy.Message) string {
	var sb strings.Builder
	for _, part := range m.Content {
		switch p := part.(type) {
		case fantasy.TextPart:
			sb.WriteString(p.Text)
		case *fantasy.TextPart:
			if p != nil {
				sb.WriteString(p.Text)
			}
		case fantasy.ToolResultPart:
			if t, ok := p.Output.(fantasy.ToolResultOutputContentText); ok {
				sb.WriteString(t.Text)
			} else if t, ok := p.Output.(*fantasy.ToolResultOutputContentText); ok && t != nil {
				sb.WriteString(t.Text)
			}
		case *fantasy.ToolResultPart:
			if p == nil {
				continue
			}
			if t, ok := p.Output.(fantasy.ToolResultOutputContentText); ok {
				sb.WriteString(t.Text)
			} else if t, ok := p.Output.(*fantasy.ToolResultOutputContentText); ok && t != nil {
				sb.WriteString(t.Text)
			}
		}
	}
	return sb.String()
}

// Cost returns the estimated cost in USD using the cost tracker. The
// upstream Service identity is passed through so a CatalogPriceSource can
// disambiguate models that appear under multiple providers (the
// "join on Service ↔ InferenceProvider" rule from the Phase 5 audit).
func (a *Agent) Cost() float64 {
	return a.CostTracker.EstimateCostFor(a.config.Model, a.config.Service)
}
