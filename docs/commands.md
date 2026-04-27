# 🦍 SLASH COMMANDS 🦍

These are the words you type in the input bar to control `bitchtea`.

## 🍌 Navigation & Context
- **/join <#channel>**: Switch focus to a channel (creates it if new).
- **/part [#channel]**: Leave the current (or named) context.
- **/channels (alias /ch)**: List all open contexts.
- **/query <nick>**: Open a private DM with a persona.
- **/msg <nick> <text>**: Send a quick message without switching focus.

## 🍌 Agent Control
- **/model <name>**: Change the LLM model (e.g., `gpt-4o`).
- **/compact**: Manually flush old history to save tokens.
- **/tokens**: Show estimated token usage and cost.
- **/auto-next**: Toggle automatic execution of tool chains.
- **/auto-idea**: Toggle automatic suggestion of next steps.

## 🍌 Session Management
- **/sessions (alias /ls)**: List saved session files.
- **/fork**: Branch the current session into a new file.
- **/tree**: See the history and branches of the session.

## 🍌 Configuration
- **/set [key [value]]**: View or change settings like `nick` or `sound`.
- **/profile [cmd]**: Save, load, or delete configuration profiles.
- **/apikey <key>**: Set your API key for the current session.
- **/provider <name>**: Switch transport (e.g., `openai`, `anthropic`).

## 🍌 UI & System
- **/clear**: Wipe the screen transcript.
- **/theme**: Rotate through UI color themes.
- **/sound**: Toggle notification bings.
- **/debug on|off**: Show raw API data for debugging.
- **/copy [n]**: Copy the last (or nth) assistant reply to clipboard.
- **/activity [clear]**: View or clear background tool tasks.
- **/mp3 [cmd]**: Control the built-in MP3 player.
- **/memory**: Show the contents of your `MEMORY.md`.
- **/help (alias /h)**: Show the help summary.
- **/quit (alias /q, /exit)**: Exit the canopy.

APE STRONK TOGETHER. 🦍💪🤝
