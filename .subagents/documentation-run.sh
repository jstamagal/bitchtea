#!/bin/bash
# 🦍 SWARM OF THE CANOPY: DOCUMENTATION RUN 🦍
# Concurrency: 1x Pro, 2x Flash, 2x Lite

# 1. THE ELDEST (PRO) - ARCHITECTURE & SOUL
acpx --approve-all --model gemini-3.1-pro gemini --no-wait \
"My eldest, the burden of truth lies with you. Map the very soul of bitchtea in /home/admin/bitchtea. 
Write these three sacred scrolls:
1. docs/architecture.md: Deep architectural treatment. Package map, dep graph rationale, runtime state machine.
2. docs/agent-loop.md: The meat. SendMessage loop, tool execution, follow-ups.
3. docs/README.md: The master index for all documentation.
Reference the source code at every step. Cite file:line. Be the Senior Architect. 🦍💪🤝"

# 2. THE STRONG ONES (FLASH) - GEARS & GREASE
# Flash 1: Tools & Dev
acpx --approve-all --model gemini-3-flash gemini --no-wait \
"Strong one, you handle the gears. Document the mechanics of /home/admin/bitchtea.
Write these three:
1. docs/tools.md: Every built-in tool in internal/tools/tools.go. Schema, dispatcher, executor.
2. docs/streaming.md: The StreamEvent contract and internal/llm shim details.
3. docs/development.md: Required checks, how to add commands/tools, testing patterns.
Be precise. Witness the code. 🦍💪"

# Flash 2: Persistence & Control
acpx --approve-all --model gemini-3-flash gemini --no-wait \
"Strong one, you handle the history. Document the bones of /home/admin/bitchtea.
Write these three:
1. docs/sessions.md: JSONL format, entry fields, resume/fork/tree logic.
2. docs/memory.md: IRC tiers, MemoryScope, discovery walking.
3. docs/signals-and-keys.md: Ctrl+C, Ctrl+Z, and the 3-stage Esc spec from REBUILD_NOTES.md.
Be accurate. Stay strong. 🦍💪"

# 3. THE LITTLE ONES (LITE) - VOICE & PATHS
# Lite 1: Onboarding
acpx --approve-all --model gemini-3.1-flash-lite gemini --no-wait \
"Little one, you are the voice for the new apes. Show them the path in /home/admin/bitchtea.
Write these three:
1. docs/getting-started.md: Build, install, first prompt.
2. docs/user-guide.md: Features, @file refs, tool monitoring.
3. docs/troubleshooting.md: API keys, debug hook, state reset.
Make it clear. Don't let them get lost. 🦍💪"

# Lite 2: Reference
acpx --approve-all --model gemini-3.1-flash-lite gemini \
"Little one, teach them the words. Document the reference for /home/admin/bitchtea.
Write these three:
1. docs/commands.md: Exhaustive catalog of REPL slash commands from internal/ui/commands.go.
2. docs/cli-flags.md: Every flag in main.go.
3. docs/glossary.md: Terms like fantasy, hyper, bootstrap context.
Keep it simple. Finish the work. 🦍💪"

echo "🦍 THE SWARM IS LOOSE. WATCH THE CANOPY GROW. 🦍"
