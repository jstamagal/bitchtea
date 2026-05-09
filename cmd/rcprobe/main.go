// rcprobe is a developer scratchpad — like cmd/trace — for diagnosing
// bitchtearc parsing. Run `go run ./cmd/rcprobe` to see which lines of
// ~/.bitchtea/bitchtearc were applied vs silently rejected, and what each
// config field looked like before vs after. Useful when an rc change
// "doesn't seem to take" — the probe shows whether the parser saw the line
// at all. Not shipped in the main binary.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/jstamagal/bitchtea/internal/config"
)

func main() {
	rcPath := os.ExpandEnv("$HOME/.bitchtea/bitchtearc")
	f, err := os.Open(rcPath)
	if err != nil {
		fmt.Println("open:", err)
		return
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		lines = append(lines, l)
	}

	cfg := config.DefaultConfig()
	before := snap(&cfg)
	remaining := config.ApplyRCSetCommands(&cfg, lines)
	after := snap(&cfg)

	fmt.Println("=== input lines (non-comment, non-blank) ===")
	for i, l := range lines {
		fmt.Printf("  %2d: %s\n", i+1, l)
	}

	fmt.Println("\n=== lines REJECTED (returned in 'remaining', i.e. NOT applied) ===")
	if len(remaining) == 0 {
		fmt.Println("  (none — all lines applied)")
	} else {
		for i, l := range remaining {
			fmt.Printf("  %2d: %s\n", i+1, l)
		}
	}

	fmt.Println("\n=== config fields: BEFORE -> AFTER ===")
	for _, k := range []string{"Provider", "Model", "APIKey", "BaseURL", "UserNick", "Service", "Profile", "PersonaFile", "NotificationSound", "AutoNextSteps", "AutoNextIdea", "ToolVerbosity", "Banner", "Effort", "ToolTimeout"} {
		b := before[k]
		a := after[k]
		marker := "  "
		if b != a {
			marker = "✓ "
		}
		fmt.Printf("%s%-22s  %-40q -> %q\n", marker, k, b, a)
	}
}

func snap(c *config.Config) map[string]string {
	return map[string]string{
		"Provider":          c.Provider,
		"Model":             c.Model,
		"APIKey":            c.APIKey,
		"BaseURL":           c.BaseURL,
		"UserNick":          c.UserNick,
		"Service":           c.Service,
		"Profile":           c.Profile,
		"PersonaFile":       c.PersonaFile,
		"NotificationSound": fmt.Sprintf("%v", c.NotificationSound),
		"AutoNextSteps":     fmt.Sprintf("%v", c.AutoNextSteps),
		"AutoNextIdea":      fmt.Sprintf("%v", c.AutoNextIdea),
		"ToolVerbosity":     c.ToolVerbosity,
		"Banner":            fmt.Sprintf("%v", c.Banner),
		"Effort":            c.Effort,
		"ToolTimeout":       fmt.Sprintf("%d", c.ToolTimeout),
	}
}
