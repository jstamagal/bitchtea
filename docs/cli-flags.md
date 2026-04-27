# 🦍 BITCHTEA: CLI FLAGS

Control the launch from the terminal.

## 🚀 PRIMARY FLAGS

- **`-m, --model <name>`**: Set the model on startup (overrides default).
- **`-p, --profile <name>`**: Load a specific saved profile (e.g., `ollama`, `zai-openai`).
- **`-r, --resume [path]`**: Resume a session. If no path given, resumes the `latest`.
- **`-H, --headless`**: Run in "one-shot" mode without the TUI. Requires `--prompt`.

## 🤖 AUTOMATION FLAGS

- **`--prompt <text>`**: The message to send in headless mode.
- **`--auto-next-steps`**: Automatically trigger the "next steps" follow-up loop.
- **`--auto-next-idea`**: Automatically trigger the "improvement idea" follow-up loop.

## ℹ️ INFO FLAGS

- **`-h, --help`**: Show usage and flag descriptions.

## 🌍 ENVIRONMENT VARIABLES

- **`OPENAI_API_KEY`**: Key for OpenAI models.
- **`ANTHROPIC_API_KEY`**: Key for Anthropic models.
- **`OPENROUTER_API_KEY`**: Key for OpenRouter profile.
- **`ZAI_API_KEY`**: Key for Z.ai profiles.
- **`BITCHTEA_MODEL`**: Default model name.
- **`BITCHTEA_PROVIDER`**: Default provider (openai/anthropic).

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝
