package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jstamagal/bitchtea/internal/agent"
	"github.com/jstamagal/bitchtea/internal/catalog"
	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/llm"
	"github.com/jstamagal/bitchtea/internal/session"
	"github.com/jstamagal/bitchtea/internal/ui"
)

type cliOptions struct {
	resumePath           string
	profileName          string
	headless             bool
	prompt               string
	bare                 bool
	createConfigDefaults bool
	forceOverwrite       bool
}

func main() {
	// Dispatch `bitchtea daemon ...` before any flag parsing — the daemon
	// CLI is intentionally orthogonal to the TUI startup path. This lets us
	// keep --resume / --profile / etc. simple and avoids accidental flag
	// collisions.
	if len(os.Args) >= 2 && os.Args[1] == "daemon" {
		os.Exit(runDaemon(os.Args[2:], os.Stdout, os.Stderr))
	}

	if err := config.MigrateDataPaths(); err != nil {
		fmt.Fprintf(os.Stderr, "bitchtea: data migration warning: %v\n", err)
	}

	cfg := config.DefaultConfig()
	config.DetectProvider(&cfg)

	// Kick the catwalk catalog refresh in the background. Off by default;
	// only runs when both BITCHTEA_CATWALK_AUTOUPDATE=true (or 1) and
	// BITCHTEA_CATWALK_URL are set. Bounded by a 5s context so a hung
	// catwalk endpoint can never block startup. Errors are swallowed —
	// next read just sees the previous cache (or the embedded floor).
	maybeStartCatalogRefresh()

	// Wire CostTracker to the catalog cache as the default price source.
	// catalog.Load returns the best envelope it can produce synchronously
	// (cache → embedded → empty); CatalogPriceSource transparently falls
	// back to the embedded snapshot on any per-model miss, so this is
	// always at-least-as-good as the prior embedded-only behavior.
	llm.SetDefaultPriceSource(llm.CatalogPriceSource(catalog.Load(catalog.LoadOptions{})))

	opts, rcCommands, err := applyStartupConfig(&cfg, os.Args[1:], config.ParseRC())
	if err != nil {
		fmt.Fprintf(os.Stderr, "bitchtea: %v\n", err)
		os.Exit(1)
	}

	if cfg.APIKey == "" && !config.ProfileAllowsEmptyAPIKey(cfg) {
		fmt.Fprintln(os.Stderr, "bitchtea: no API key found")
		fmt.Fprintln(os.Stderr, "  Set OPENAI_API_KEY, ANTHROPIC_API_KEY, OPENROUTER_API_KEY, or ZAI_API_KEY")
		fmt.Fprintln(os.Stderr, "  Or load the local ollama profile if you really mean no auth")
		fmt.Fprintln(os.Stderr, "  Or are you too cool for authentication?")
		os.Exit(1)
	}

	// Handle resume
	var sess *session.Session
	if opts.resumePath != "" {
		if opts.resumePath == "latest" {
			opts.resumePath = session.Latest(cfg.SessionDir)
			if opts.resumePath == "" {
				fmt.Fprintln(os.Stderr, "bitchtea: no sessions to resume")
				os.Exit(1)
			}
		}
		sess, err = session.Load(opts.resumePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bitchtea: failed to load session: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Resuming session: %s (%d entries)\n", opts.resumePath, len(sess.Entries))
	}

	if opts.headless {
		prompt, err := collectHeadlessPrompt(opts.prompt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bitchtea: %v\n", err)
			os.Exit(1)
		}
		if err := runHeadless(&cfg, sess, prompt); err != nil {
			fmt.Fprintf(os.Stderr, "bitchtea: %v\n", err)
			os.Exit(1)
		}
		return
	}

	m := buildStartupModel(&cfg, sess, rcCommands)

	// Set up signal channel for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Create the program
	p := tea.NewProgram(m, tea.WithAltScreen())

	// Forward signals to the program's event loop
	go func() {
		for sig := range sigCh {
			// Send the signal as a message to the model
			p.Send(ui.SignalMsg{Signal: sig})
		}
	}()

	if _, err := p.Run(); err != nil {
		// Check if it was just an interrupt (graceful shutdown)
		if err == tea.ErrInterrupted {
			fmt.Fprintln(os.Stderr, "\nLater, coward.")
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "bitchtea crashed: %v\n", err)
		os.Exit(1)
	}
}

func applyStartupConfig(cfg *config.Config, args []string, rcLines []string) (cliOptions, []string, error) {
	rcCommands := config.ApplyRCSetCommands(cfg, rcLines)

	opts, err := parseCLIArgs(args, cfg)
	if err != nil {
		return cliOptions{}, nil, err
	}

	if opts.profileName == "" {
		return opts, rcCommands, nil
	}

	p, err := config.ResolveProfile(opts.profileName)
	if err != nil {
		return cliOptions{}, nil, err
	}
	config.ApplyProfile(cfg, p)
	cfg.Profile = opts.profileName

	// Re-parse args so explicit flags override profile defaults using the same
	// manual-override rules as startup /set processing.
	if _, err := parseCLIArgs(args, cfg); err != nil {
		return cliOptions{}, nil, err
	}

	return opts, rcCommands, nil
}

func buildStartupModel(cfg *config.Config, sess *session.Session, rcCommands []string) ui.Model {
	m := ui.NewModel(cfg)
	// Enable daemon-mailbox submissions for production. Tests leave this
	// unset so they don't accidentally write to a developer's running
	// daemon. See ui.Model.SetDaemonBaseDir for details (bt-wire.6).
	m.SetDaemonBaseDir(config.BaseDir())
	if sess != nil {
		m.ResumeSession(sess)
	}
	for _, line := range rcCommands {
		m.ExecuteStartupCommand(line)
	}
	return m
}

func parseCLIArgs(args []string, cfg *config.Config) (cliOptions, error) {
	var opts cliOptions

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--model", "-m":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("missing value for %s", args[i])
			}
			cfg.Model = args[i+1]
			cfg.Profile = ""
			i++
		case "--resume", "-r":
			if i+1 < len(args) && len(args[i+1]) > 0 && args[i+1][0] != '-' {
				opts.resumePath = args[i+1]
				i++
			} else {
				opts.resumePath = "latest"
			}
		case "--profile", "-p":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("missing value for %s", args[i])
			}
			opts.profileName = args[i+1]
			i++
		case "--prompt":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("missing value for %s", args[i])
			}
			opts.prompt = args[i+1]
			i++
		case "--auto-next-steps":
			cfg.AutoNextSteps = true
		case "--auto-next-idea":
			cfg.AutoNextIdea = true
		case "--headless", "-H":
			opts.headless = true
		case "--bare":
			opts.bare = true
			cfg.Bare = true
		case "--create-config-defaults":
			opts.createConfigDefaults = true
		case "--force":
			opts.forceOverwrite = true
		case "--help", "-h":
			printUsage()
			os.Exit(0)
		default:
			return opts, fmt.Errorf("unknown flag: %s", args[i])
		}
	}

	return opts, nil
}

func collectHeadlessPrompt(flagPrompt string) (string, error) {
	var stdinPrompt string

	if info, err := os.Stdin.Stat(); err == nil && info.Mode()&os.ModeCharDevice == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		stdinPrompt = string(data)
	}

	flagPrompt = strings.TrimSpace(flagPrompt)
	stdinPrompt = strings.TrimSpace(stdinPrompt)

	switch {
	case flagPrompt != "" && stdinPrompt != "":
		return flagPrompt + "\n\n" + stdinPrompt, nil
	case flagPrompt != "":
		return flagPrompt, nil
	case stdinPrompt != "":
		return stdinPrompt, nil
	default:
		return "", fmt.Errorf("headless mode requires --prompt or piped stdin")
	}
}

func runHeadless(cfg *config.Config, sess *session.Session, prompt string) error {
	ag := agent.NewAgent(cfg)
	if sess != nil {
		ag.RestoreMessages(session.FantasyFromEntries(sess.Entries))
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	return runHeadlessLoop(ctx, ag, prompt, os.Stdout, os.Stderr)
}

func runHeadlessLoop(ctx context.Context, ag *agent.Agent, prompt string, stdout, stderr io.Writer) error {
	currentPrompt := prompt
	var followUp *agent.FollowUpRequest
	textEndedWithNewline := true

	for {
		events := make(chan agent.Event, 100)
		if followUp == nil {
			go ag.SendMessage(ctx, currentPrompt, events)
		} else {
			if _, err := fmt.Fprintf(stderr, "[auto] %s\n", followUp.Label); err != nil {
				return fmt.Errorf("write stderr: %w", err)
			}
			go ag.SendFollowUp(ctx, followUp, events)
		}

		for ev := range events {
			switch ev.Type {
			case "text":
				if _, err := fmt.Fprint(stdout, ev.Text); err != nil {
					return fmt.Errorf("write stdout: %w", err)
				}
				textEndedWithNewline = strings.HasSuffix(ev.Text, "\n")
			case "tool_start":
				if _, err := fmt.Fprintf(stderr, "[tool] %s args=%s\n", ev.ToolName, truncateForLog(ev.ToolArgs, 200)); err != nil {
					return fmt.Errorf("write stderr: %w", err)
				}
			case "tool_result":
				if ev.ToolError != nil {
					if _, err := fmt.Fprintf(stderr, "[tool] %s error=%v result=%s\n", ev.ToolName, ev.ToolError, truncateForLog(ev.ToolResult, 200)); err != nil {
						return fmt.Errorf("write stderr: %w", err)
					}
					continue
				}
				if _, err := fmt.Fprintf(stderr, "[tool] %s result=%s\n", ev.ToolName, truncateForLog(ev.ToolResult, 200)); err != nil {
					return fmt.Errorf("write stderr: %w", err)
				}
			case "state":
				if _, err := fmt.Fprintf(stderr, "[status] %s\n", headlessStateLabel(ev.State)); err != nil {
					return fmt.Errorf("write stderr: %w", err)
				}
			case "error":
				return ev.Error
			}
		}

		followUp = ag.MaybeQueueFollowUp()
		if followUp == nil {
			break
		}
		if !textEndedWithNewline {
			if _, err := fmt.Fprintln(stdout); err != nil {
				return fmt.Errorf("write stdout: %w", err)
			}
			textEndedWithNewline = true
		}
	}

	if !textEndedWithNewline {
		if _, err := fmt.Fprintln(stdout); err != nil {
			return fmt.Errorf("write trailing newline: %w", err)
		}
	}

	return nil
}

func headlessStateLabel(state agent.State) string {
	switch state {
	case agent.StateThinking:
		return "thinking"
	case agent.StateToolCall:
		return "tool_call"
	default:
		return "idle"
	}
}

func truncateForLog(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// runCreateConfigDefaults writes a fully-populated ~/.bitchtea/bitchtearc with one
// "# set <key> <value>" line per recognised SET key, using current config
// values as defaults. Lines are commented out so the file is a reference
// template — the user uncomments and edits to activate.
func runCreateConfigDefaults(cfg *config.Config, force bool) error {
	rcPath := config.RCPath()
	if _, err := os.Stat(rcPath); err == nil && !force {
		fmt.Printf("~/.bitchtea/bitchtearc already exists. Overwrite? [y/N]: ")
		var answer string
		fmt.Scanln(&answer)
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	keys := config.SetKeys()
	var sb strings.Builder
	sb.WriteString("# bitchtea runtime config — generated by --create-config-defaults\n")
	sb.WriteString("# Uncomment and edit any line to override the default.\n")
	sb.WriteString("#\n")
	for _, key := range keys {
		val, _ := config.GetSetting(cfg, key)
		sb.WriteString(fmt.Sprintf("# set %s %s\n", key, val))
	}

	if err := os.MkdirAll(config.BaseDir(), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(rcPath, []byte(sb.String()), 0600); err != nil {
		return fmt.Errorf("write %s: %w", rcPath, err)
	}

	fmt.Printf("Wrote %s (%d keys)\n", rcPath, len(keys))
	fmt.Println("Each line is commented out — uncomment and edit to activate.")
	return nil
}

// maybeStartCatalogRefresh fires a single background goroutine that tries to
// pull a fresh catwalk catalog into ~/.bitchtea/catalog/providers.json. It
// is gated on env vars, time-bound, and entirely non-fatal: startup never
// waits on the result, and no error is ever surfaced to the user. Next
// startup picks up whatever the goroutine wrote.
func maybeStartCatalogRefresh() {
	enabled := envBool("BITCHTEA_CATWALK_AUTOUPDATE")
	url := strings.TrimSpace(os.Getenv("BITCHTEA_CATWALK_URL"))
	if !enabled || url == "" {
		return
	}
	cachePath := catalog.CachePath(config.BaseDir())
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), catalog.DefaultRefreshTimeout)
		defer cancel()
		_ = catalog.Refresh(ctx, catalog.RefreshOptions{
			CachePath: cachePath,
			Enabled:   true,
			SourceURL: url,
		})
	}()
}

// envBool reads a boolean env var with the same loose acceptance most
// BITCHTEA_* flags use: "1", "true", "yes", "on" (case-insensitive) → true.
func envBool(name string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func printUsage() {
	fmt.Println(`bitchtea — putting the BITCH back in your terminal

Usage: bitchtea [flags]
       bitchtea daemon <start|status|stop>

Flags:
  -m, --model <name>     Model to use (default: gpt-4o)
  -p, --profile <name>   Load a saved or built-in profile (ollama, openrouter, zai-openai, zai-anthropic)
  -r, --resume [path]    Resume session (latest if no path given)
  -H, --headless         Run once without the TUI
  --prompt <text>        Prompt to send in headless mode
  --auto-next-steps      Auto-inject next-step prompts
  --auto-next-idea       Auto-generate improvement ideas
  --bare                 Skip splash banner / startup chrome
  -h, --help             Show this help

Environment:
  OPENAI_API_KEY         OpenAI API key
  OPENAI_BASE_URL        OpenAI-compatible base URL
  ANTHROPIC_API_KEY      Anthropic API key
  OPENROUTER_API_KEY     OpenRouter API key for the openrouter profile
  ZAI_API_KEY            Z.ai API key for the zai-* profiles
  BITCHTEA_MODEL         Default model
  BITCHTEA_PROVIDER      Provider name (openai, anthropic)
  BITCHTEA_CATWALK_URL   Catwalk catalog base URL (no default; off when unset)
  BITCHTEA_CATWALK_AUTOUPDATE
                         Enable background catalog refresh (default: false)

In-TUI commands (slash commands):
  Run bitchtea, then type /help inside the TUI for the full reference of
  /set, /profile, /models, /join, /msg, /memory, /mp3, /quit, and friends.

Don't be a wimp. Just run it.`)
}
