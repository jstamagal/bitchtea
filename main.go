package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/session"
	"github.com/jstamagal/bitchtea/internal/ui"
)

func main() {
	cfg := config.DefaultConfig()
	config.DetectProvider(&cfg)

	var resumePath string
	var profileName string

	// Parse CLI flags
	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--model", "-m":
			if i+1 < len(os.Args) {
				cfg.Model = os.Args[i+1]
				i++
			}
		case "--resume", "-r":
			if i+1 < len(os.Args) && os.Args[i+1][0] != '-' {
				resumePath = os.Args[i+1]
				i++
			} else {
				resumePath = "latest"
			}
		case "--profile", "-p":
			if i+1 < len(os.Args) {
				profileName = os.Args[i+1]
				i++
			}
		case "--auto-next-steps":
			cfg.AutoNextSteps = true
		case "--auto-next-idea":
			cfg.AutoNextIdea = true
		case "--help", "-h":
			printUsage()
			os.Exit(0)
		}
	}

	// Load profile if specified
	if profileName != "" {
		p, err := config.LoadProfile(profileName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bitchtea: %v\n", err)
			os.Exit(1)
		}
		config.ApplyProfile(&cfg, p)
		fmt.Fprintf(os.Stderr, "Loaded profile: %s (provider=%s model=%s)\n", profileName, p.Provider, p.Model)
	}

	if cfg.APIKey == "" {
		fmt.Fprintln(os.Stderr, "bitchtea: no API key found")
		fmt.Fprintln(os.Stderr, "  Set OPENAI_API_KEY or ANTHROPIC_API_KEY environment variable")
		fmt.Fprintln(os.Stderr, "  Or are you too cool for authentication?")
		os.Exit(1)
	}

	// Handle resume
	var sess *session.Session
	if resumePath != "" {
		if resumePath == "latest" {
			resumePath = session.Latest(cfg.SessionDir)
			if resumePath == "" {
				fmt.Fprintln(os.Stderr, "bitchtea: no sessions to resume")
				os.Exit(1)
			}
		}
		var err error
		sess, err = session.Load(resumePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bitchtea: failed to load session: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Resuming session: %s (%d entries)\n", resumePath, len(sess.Entries))
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

func printUsage() {
	fmt.Println(`bitchtea — putting the BITCH back in your terminal

Usage: bitchtea [flags]

Flags:
  -m, --model <name>     Model to use (default: gpt-4o)
  -p, --profile <name>   Load a saved connection profile
  -r, --resume [path]    Resume session (latest if no path given)
  --auto-next-steps      Auto-inject next-step prompts
  --auto-next-idea       Auto-generate improvement ideas
  -h, --help             Show this help

Environment:
  OPENAI_API_KEY         OpenAI API key
  OPENAI_BASE_URL        OpenAI-compatible base URL
  ANTHROPIC_API_KEY      Anthropic API key
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
  /theme <name>          Switch color theme
  /sound                 Toggle completion bell
  /help                  Show help
  /quit                  Exit

Don't be a wimp. Just run it.`)
}
