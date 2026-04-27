#!/bin/bash
# 🦍 SWARM OF THE CANOPY: DEEP TECHNICAL DOCUMENTATION 🦍
# Concurrency: 1x Pro, 2x Flash, 2x Lite

echo "🦍 RELEASING THE SWARM..."

# 1. THE ELDEST (PRO) - TOOLS DEEP DIVE [NON-BLOCKING]
acpx --approve-all --model gemini-3.1-pro gemini --no-wait \
"My eldest, peel back the skin of the tools in \`internal/tools/tools.go\`. 
For every tool (read, write, edit, bash, search_memory, terminal_*), I want the raw truth:
- What exactly fires in the executor?
- What does it send back to the LLM verbatim?
- If it uses \`exec.CommandContext\`, what are the timeout and signal behaviors?
- Show the internal state management for the terminal manager.
Cite the lines. Show the spinning gears. Give a technical explanation of what each function/command/etc does under the hood, if it shows output show exactly what output it shows. if it sends anything over the REPL, if it sends data to the LLM, i wanna know what all is going in and out. Append this technical deep-dive to docs/tools.md. 🦍💪🤝"

# 2. THE STRONG ONES (FLASH) - SESSIONS, MEMORY, COMMANDS [NON-BLOCKING]
# Flash 1: Sessions & Memory
acpx --approve-all --model gemini-3-flash-preview gemini --no-wait \
"Strong one, witness the record. Deep dive into \`internal/session/\` and \`internal/memory/\`.
- For sessions: Show how JSONL is appended, how \`parent_id\` creates the tree, and exactly what \`checkpoint.json\` stores during autonomous turns.
- For memory: Trace the scope discovery in \`agent/context.go\`. Show how it walks up the tree and how inheritance works in \`SearchInScope\`.
Explain the churn. Show the bytes moving. Give a technical explanation of what each function does under the hood, exactly what output it shows, what all is going in and out. Append this technical deep-dive to docs/sessions.md and docs/memory.md. 🦍💪"

# Flash 2: Commands
acpx --approve-all --model gemini-3-flash-preview gemini --no-wait \
"Strong one, handle the REPL. Deep dive into \`internal/ui/commands.go\`.
- For every slash command: What does it print to the viewport? What does it send to \`agent.SendMessage\`?
- Trace \`/compact\`: How does it trigger the LLM to summarize and where does that summary go?
- Trace \`/fork\`: Exactly how does it copy the file?
Show the REPL-to-Agent handshake. Give a technical explanation of what each command does under the hood, if it shows output show exactly what output it shows. if it sends anything over the REPL, if it sends data to the LLM, i wanna know what all is going in and out. Append this technical deep-dive to docs/commands.md. 🦍💪"

# 3. THE LITTLE ONES (LITE) - CLI FLAGS & SIGNALS [NON-BLOCKING / BLOCKING]
# Lite 1: CLI Flags [NON-BLOCKING]
acpx --approve-all --model gemini-3.1-flash-lite gemini --no-wait \
"Little one, look at \`main.go\` and \`internal/config/\`.
- Trace every CLI flag: What fields in the \`Config\` struct does it flip?
- How does \`applyStartupConfig\` merge RC files, profiles, and flags? 
- Show the priority of truth.
Keep it sharp. Give a technical explanation of what each flag does under the hood, if it shows output show exactly what output it shows. Append this technical deep-dive to docs/cli-flags.md. 🦍💪"

# Lite 2: Signals & Keys [BLOCKING - Final Sync]
acpx --approve-all --model gemini-3.1-flash-lite gemini \
"Little one, witness the control. Deep dive into \`internal/ui/model.go\` and \`REBUILD_NOTES.md\`.
- Trace \`Ctrl+C\`: How does it propagate through \`context.Cancel()\`?
- Trace the 3-stage \`Esc\` protocol: Show the timer logic and the transition from 'cancel tool' to 'cancel turn' to 'clear queue'.
Show the interrupts. Give a technical explanation of what each key/signal does under the hood, if it shows output show exactly what output it shows. Append this technical deep-dive to docs/signals-and-keys.md. 🦍💪"

echo "🦍 THE SWARM HAS RETURNED. THE CANOPY IS DEEP. 🦍"
