package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/ui"
)

func main() {
	cfg := config.DefaultConfig()
	config.DetectProvider(&cfg)

	// Parse CLI flags
	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--model", "-m":
			if i+1 < len(os.Args) {
				cfg.Model = os.Args[i+1]
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

	if cfg.APIKey == "" {
		fmt.Fprintln(os.Stderr, "bitchtea: no API key found")
		fmt.Fprintln(os.Stderr, "  Set OPENAI_API_KEY or ANTHROPIC_API_KEY environment variable")
		fmt.Fprintln(os.Stderr, "  Or are you too cool for authentication?")
		os.Exit(1)
	}

	m := ui.NewModel(&cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "bitchtea crashed: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`bitchtea — putting the BITCH back in your terminal

Usage: bitchtea [flags]

Flags:
  -m, --model <name>     Model to use (default: gpt-4o)
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
  /clear                 Clear chat
  /compact               Compact context
  /help                  Show help
  /quit                  Exit

Don't be a wimp. Just run it.`)
}
