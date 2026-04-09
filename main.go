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
	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/session"
	"github.com/jstamagal/bitchtea/internal/ui"
)

type cliOptions struct {
	resumePath  string
	profileName string
	headless    bool
	prompt      string
}

func main() {
	cfg := config.DefaultConfig()
	config.DetectProvider(&cfg)

	opts, err := parseCLIArgs(os.Args[1:], &cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bitchtea: %v\n", err)
		os.Exit(1)
	}

	// Load profile if specified
	if opts.profileName != "" {
		p, err := config.ResolveProfile(opts.profileName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bitchtea: %v\n", err)
			os.Exit(1)
		}
		config.ApplyProfile(&cfg, p)
		fmt.Fprintf(os.Stderr, "Loaded profile: %s (provider=%s model=%s)\n", opts.profileName, p.Provider, p.Model)
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

	m := ui.NewModel(&cfg)
	if sess != nil {
		m.ResumeSession(sess)
	}

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

func parseCLIArgs(args []string, cfg *config.Config) (cliOptions, error) {
	var opts cliOptions

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--model", "-m":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("missing value for %s", args[i])
			}
			cfg.Model = args[i+1]
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
		ag.RestoreMessages(session.MessagesFromEntries(sess.Entries))
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	events := make(chan agent.Event, 100)
	go ag.SendMessage(ctx, prompt, events)

	textEndedWithNewline := true
	for ev := range events {
		switch ev.Type {
		case "text":
			if _, err := fmt.Fprint(os.Stdout, ev.Text); err != nil {
				return fmt.Errorf("write stdout: %w", err)
			}
			textEndedWithNewline = strings.HasSuffix(ev.Text, "\n")
		case "tool_start":
			fmt.Fprintf(os.Stderr, "[tool] %s args=%s\n", ev.ToolName, truncateForLog(ev.ToolArgs, 200))
		case "tool_result":
			if ev.ToolError != nil {
				fmt.Fprintf(os.Stderr, "[tool] %s error=%v result=%s\n", ev.ToolName, ev.ToolError, truncateForLog(ev.ToolResult, 200))
				continue
			}
			fmt.Fprintf(os.Stderr, "[tool] %s result=%s\n", ev.ToolName, truncateForLog(ev.ToolResult, 200))
		case "state":
			fmt.Fprintf(os.Stderr, "[status] %s\n", headlessStateLabel(ev.State))
		case "error":
			return ev.Error
		}
	}

	if !textEndedWithNewline {
		if _, err := fmt.Fprintln(os.Stdout); err != nil {
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

func printUsage() {
	fmt.Println(`bitchtea — putting the BITCH back in your terminal

Usage: bitchtea [flags]

Flags:
  -m, --model <name>     Model to use (default: gpt-4o)
  -p, --profile <name>   Load a saved or built-in profile (ollama, openrouter, zai-openai, zai-anthropic)
  -r, --resume [path]    Resume session (latest if no path given)
  -H, --headless         Run once without the TUI
  --prompt <text>        Prompt to send in headless mode
  --auto-next-steps      Auto-inject next-step prompts
  --auto-next-idea       Auto-generate improvement ideas
  -h, --help             Show this help

Environment:
  OPENAI_API_KEY         OpenAI API key
  OPENAI_BASE_URL        OpenAI-compatible base URL
  ANTHROPIC_API_KEY      Anthropic API key
  OPENROUTER_API_KEY     OpenRouter API key for the openrouter profile
  ZAI_API_KEY            Z.ai API key for the zai-* profiles
  BITCHTEA_MODEL         Default model
  BITCHTEA_PROVIDER      Provider name (openai, anthropic)

Commands (inside the TUI):
  /model <name>          Switch models
  /compact               Compact context
  /clear                 Clear chat display
  /diff                  Show git diff
  /status                Git status
  /undo                  Revert unstaged changes
  /commit [msg]          Git commit
  /tokens                Token usage estimate
  /sessions              List saved sessions
  /tree                  Show session tree
  /fork                  Fork session
  /auto-next             Toggle auto-next-steps
  /auto-idea             Toggle auto-next-idea
  /sound                 Toggle completion bell
  /mp3 [cmd]             Toggle MP3 panel and player
  /help                  Show help
  /quit                  Exit

Don't be a wimp. Just run it.`)
}
