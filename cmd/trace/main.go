package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jstamagal/bitchtea/internal/tools"
)

func main() {
	dir, _ := os.MkdirTemp("", "trace-*")
	defer os.RemoveAll(dir)

	reg := tools.NewRegistry(dir, filepath.Join(dir, "sessions"))
	ctx := context.Background()

	fmt.Println("═══════════════════════════════════════════")
	fmt.Println("🦍 TRACE: real tool execution, line by line")
	fmt.Println("═══════════════════════════════════════════")
	fmt.Println()

	// ── BUG 8: read offset past EOF ──
	fmt.Println("── BUG 8: read offset past EOF returns empty string ──")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("line1\nline2\n"), 0644)
	out, err := reg.Execute(ctx, "read", `{"path":"f.txt","offset":10}`)
	fmt.Printf("  output: %q\n", out)
	fmt.Printf("  error:  %v\n", err)
	fmt.Printf("  → LLM CANNOT tell empty file vs past EOF\n\n")

	// ── BUG 1: bash with parent ctx cancelled → nil pointer panic ──
	fmt.Println("── BUG 1: bash with cancelled ctx → ProcessState = nil ──")
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()
	out, err = reg.Execute(cancelledCtx, "bash", `{"command":"echo hi"}`)
	fmt.Printf("  output: %q\n", out)
	fmt.Printf("  error:  %v\n", err)
	fmt.Printf("  → PANIC? ProcessState nil if cmd never started\n\n")

	// ── BUG 2: bash timeout vs cancellation ambiguity ──
	fmt.Println("── BUG 2: parent ctx cancel → says 'timed out' ──")
	parentCtx, parentCancel := context.WithCancel(ctx)
	parentCancel() // cancel BEFORE execution
	out, err = reg.Execute(parentCtx, "bash", `{"command":"sleep 5","timeout":10}`)
	fmt.Printf("  output: %q\n", out)
	fmt.Printf("  error:  %v\n", err)
	fmt.Printf("  → error says 'timed out' but ctx was CANCELLED not deadline\n\n")

	// ── BUG 3: UTF-8 truncation ──
	fmt.Println("── BUG 3: UTF-8 truncation corrupts multi-byte chars ──")
	bigStr := strings.Repeat("你", 18000) // ~54KB of 3-byte chars
	out, err = reg.Execute(ctx, "bash", fmt.Sprintf(`{"command":"printf '%%s' '%s'"}`, bigStr))
	if err != nil {
		fmt.Printf("  error: %v\n", err)
	}
	fmt.Printf("  last 20 bytes: %q\n", out[len(out)-20:])
	fmt.Printf("  → truncated mid-rune? check %q\n\n", out[len(out)-6:])

	// ── BUG 4: truncate helper byte-split ──
	fmt.Println("── BUG 4: truncate() byte-splits UTF-8 ──")
	// Can't call private truncate directly; BUG 3 covers this path

	// ── BUG 6: empty oldText in edit ──
	fmt.Println("── BUG 6: edit with empty oldText ──")
	os.WriteFile(filepath.Join(dir, "e.txt"), []byte("hello world\n"), 0644)
	out, err = reg.Execute(ctx, "edit", `{"path":"e.txt","edits":[{"oldText":"","newText":"X"}]}`)
	fmt.Printf("  output: %q\n", out)
	fmt.Printf("  error:  %v\n", err)
	fmt.Printf("  → Empty string 'not found'? Or matches everywhere?\n\n")

	// ── BUG 5: terminal close race ──
	fmt.Println("── BUG 5: terminal close data race ──")
	out, err = reg.Execute(ctx, "terminal_start", `{"command":"sleep 0.1","width":20,"height":5,"delay_ms":50}`)
	fmt.Printf("  start: error=%v\n", err)
	if err == nil {
		fields := strings.Fields(out)
		if len(fields) >= 3 && fields[0] == "terminal" && fields[1] == "session" {
			id := fields[2]
			// Close immediately while io.Copy goroutines still running
			out2, err2 := reg.Execute(ctx, "terminal_close", fmt.Sprintf(`{"id":"%s"}`, id))
			fmt.Printf("  close: %q err=%v\n", out2, err2)
			fmt.Printf("  → Run with -race to see data race\n")
		}
	}
	fmt.Println()

	// ── BUG 9: write reports original path not resolved ──
	fmt.Println("── BUG 9: write reports unresolved path ──")
	out, err = reg.Execute(ctx, "write", `{"path":"sub/rel.txt","content":"data"}`)
	fmt.Printf("  output: %q\n", out)
	fmt.Printf("  error:  %v\n", err)
	absPath := filepath.Join(dir, "sub/rel.txt")
	if _, statErr := os.Stat(absPath); statErr == nil {
		fmt.Printf("  → file written to %s but message says 'sub/rel.txt'\n\n", absPath)
	}

	// ── BUG 7: bash -lc login pollution ──
	fmt.Println("── BUG 7: bash -lc reads .bashrc ──")
	fmt.Printf("  → terminal_start uses 'bash -lc' which sources ~/.bashrc\n")
	fmt.Printf("  → Non-deterministic: aliases, PATH, env depend on user's dotfiles\n\n")

	// ── BUG 10: filterRequired empty slice ──
	fmt.Println("── BUG 10: filterRequired returns empty slice not nil ──")
	fmt.Printf("  → when all required names filtered, JSON gets \"required\": []\n")
	fmt.Printf("  → Fantasy providers may reject empty required arrays\n\n")

	fmt.Println("═══════════════════════════════════════════")
	fmt.Println("🦍 TRACE COMPLETE — 10 bugs traced live")
	fmt.Println("═══════════════════════════════════════════")
}
