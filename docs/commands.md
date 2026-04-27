# 🦍 BITCHTEA: COMMAND REFERENCE

Slash commands control the TUI. Use them to bend the session to your will.

## 🏛️ CONTEXT & NAVIGATION

- **`/join <#channel>`**: Switch focus to a channel. Creates it if it doesn't exist.
- **`/part [#channel]`**: Leave the current or named context.
- **`/query <nick>`**: Open a direct persistent conversation with a persona/nick.
- **`/msg <nick> <text>`**: Send a one-shot message to a nick without changing focus.
- **`/channels`** (or **`/ch`**): List all open contexts and their active members.

## 🧠 MODEL & CONFIG

- **`/model <name>`**: Switch the LLM (e.g., `gpt-4o`, `claude-3-5-sonnet-latest`).
- **`/provider <name>`**: Switch provider (e.g., `openai`, `anthropic`).
- **`/profile [load|save|delete] <name>`**: Manage saved connection profiles.
- **`/set <key> <value>`**: Change settings (e.g., `nick`, `sound`, `auto-next`).

## 💾 SESSION & MEMORY

- **`/sessions`** (or **`/ls`**): List saved sessions in the session directory.
- **`/tree`**: Show the branch structure of the current session.
- **`/fork`**: Create a new session file from the current state.
- **`/compact`**: Summarize history to save tokens and durable knowledge.
- **`/memory`**: View the contents of `MEMORY.md` and scoped `HOT.md`.

## 🛠️ UTILITIES

- **`/copy [n]`**: Copy the last (or nth) assistant response to the clipboard.
- **`/tokens`**: Show estimated token usage and session cost.
- **`/debug [on|off]`**: Toggle verbose HTTP logging for API calls.
- **`/mp3 [cmd]`**: Control the built-in MP3 player (rescan, play, next, prev).
- **`/clear`**: Clear the scrollback buffer from the TUI.
- **`/help`** (or **`/h`**): Show the quick help menu.
- **`/quit`** (or **`/q`**): Exit bitchtea.

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝
