# Bitchtea Markdown Memory Architecture Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a pure markdown-based tiered memory system (HOT and WARM/COLD) driven by a pre-compaction memory flush and system prompt coercion.

**Architecture:** Bitchtea will maintain a HOT `memory.md` for active channels. Before the context window fills up, the agent loop will trigger a silent turn (Pre-Compaction Memory Flush) asking the LLM to summarize and append its context to a daily WARM memory file (`memory/YYYY-MM-DD-channel.md`). Agents are coerced to use `memory_search` and `memory_get` tools via dynamic system prompt injection.

**Tech Stack:** Go 1.24, Bubbletea, OpenAI/Anthropic APIs

---

### Task 1: Add Memory Tools (Search and Get)

**Files:**
- Modify: `internal/tools/tools.go`
- Modify: `internal/tools/tools_test.go`

- [ ] **Step 1: Write the failing tests for memory tools**

```go
// internal/tools/tools_test.go
package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemorySearch(t *testing.T) {
	tmp := t.TempDir()
	memDir := filepath.Join(tmp, "memory")
	os.MkdirAll(memDir, 0755)
	os.WriteFile(filepath.Join(memDir, "2026-04-09-cornhub.md"), []byte("The green button was approved by the design team yesterday."), 0644)
	
	registry := NewRegistry()
    // Need to test searching logic (assuming a Search method or tool execution wrapper)
	toolDef, ok := registry.Get("memory_search")
	if !ok {
		t.Fatalf("memory_search tool not found")
	}

	res, err := toolDef.Execute(tmp, `{"query":"green button"}`)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(res, "green button was approved") {
		t.Errorf("expected search results to contain snippet, got: %s", res)
	}
}

func TestMemoryGet(t *testing.T) {
	tmp := t.TempDir()
	memDir := filepath.Join(tmp, "memory")
	os.MkdirAll(memDir, 0755)
	os.WriteFile(filepath.Join(memDir, "test.md"), []byte("line1\nline2\nline3\n"), 0644)
	
	registry := NewRegistry()
	toolDef, ok := registry.Get("memory_get")
	if !ok {
		t.Fatalf("memory_get tool not found")
	}

	res, err := toolDef.Execute(tmp, `{"file":"memory/test.md"}`)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(res, "line2") {
		t.Errorf("expected to read file content, got: %s", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -v`
Expected: FAIL due to missing tools or methods in the registry.

- [ ] **Step 3: Write minimal implementation**

Modify `internal/tools/tools.go` to add `memory_search` (simple recursive substring or regex search in the `memory/` folder) and `memory_get` (read a file's contents) to the registry.

```go
// internal/tools/tools.go
// Inside init() or NewRegistry(), register memory_search and memory_get
// memory_search: searches markdown files in memory/ directory.
// memory_get: reads content from a specific file in the memory/ directory.
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tools/
git commit -m "feat: add memory_search and memory_get tools"
```

### Task 2: System Prompt Coercion

**Files:**
- Modify: `internal/agent/context.go`
- Modify: `internal/agent/context_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/agent/context_test.go
package agent

import (
	"strings"
	"testing"
)

func TestSystemPromptMemoryCoercion(t *testing.T) {
	prompt := BuildSystemPrompt("/tmp/bitchtea") // Or equivalent method generating the prompt
	
	expectedCoercion := "run memory_search on memory/"
	if !strings.Contains(prompt, expectedCoercion) {
		t.Errorf("expected system prompt to contain memory coercion instructions, got: %s", prompt)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestSystemPromptMemoryCoercion -v`
Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

Modify `internal/agent/context.go` to append the coercion block to the system prompt.

```go
// internal/agent/context.go
// Append to the system prompt generation:
// "## Memory Recall\nBefore answering anything about prior work, decisions, dates, people, preferences, or todos: run memory_search on memory/*.md; then use memory_get to pull only the needed lines.\nNo mental notes: If you want to remember something, WRITE IT TO A FILE."
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestSystemPromptMemoryCoercion -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/
git commit -m "feat: inject memory coercion into system prompt"
```

### Task 3: Pre-Compaction Memory Flush Mechanism

**Files:**
- Modify: `internal/agent/agent.go`
- Modify: `internal/agent/agent_loop_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/agent/agent_loop_test.go
// Add a test TestPreCompactionFlush that mocks a long conversation context
// and asserts that a silent flush message is injected into the stream and tools are executed silently.
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestPreCompactionFlush -v`
Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

```go
// internal/agent/agent.go
// In SendMessage or the main loop, right before checking if compaction is needed based on tokens:
// 1. Check if token count > threshold
// 2. If yes, inject a system/user message: "Pre-compaction memory flush. Store durable memories only in memory/YYYY-MM-DD-<channel>.md (create memory/ if needed). If memory/YYYY-MM-DD-<channel>.md already exists, APPEND new content only and do not overwrite. Treat workspace bootstrap files as read-only. Respond with NO_REPLY."
// 3. Process the LLM turn silently (do not emit StreamEvents to UI).
// 4. Run any memory tool calls made by the agent.
// 5. Proceed with actual Context Compaction.
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/
git commit -m "feat: implement pre-compaction memory flush (silent turn)"
```
