# 🦍 BITCHTEA: USER GUIDE

The Green Dark is powerful. Here is how you navigate it.

## 🌟 CORE FEATURES

- **Autonomous Follow-ups**: Enable `/auto-next` or `/auto-idea` to let the agent work while you watch.
- **Persistent Terminals**: The agent can open REPLs, editors, and background processes using `terminal_start`.
- **IRC Contexts**: Use `/join #channel` to organize different tasks within the same session.

## 📎 @FILE REFERENCES

You can include any file in your prompt by prefixing it with `@`.
- `>> check this @main.go for bugs`
- `>> read @docs/architecture.md and summarize`

Bitchtea will automatically expand these references into the full content of the file before sending the prompt.

## 🛠️ MONITORING TOOLS

When the agent runs a tool, you will see a status update in the bottom bar and a message in the transcript:
- **Thinking**: The model is drafting its plan.
- **Calling [tool]**: The agent is reaching out to the system.
- **[tool] result**: The output of the command.

Use **Ctrl+T** to toggle the Tool Panel for a detailed view of in-flight operations.

## 🧭 SLASH COMMANDS

Type `/help` inside the TUI to see the full list of commands.
- `/model <name>`: Switch the brain.
- `/compact`: Shrink the history but keep the knowledge.
- `/fork`: Create a new timeline.

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝
