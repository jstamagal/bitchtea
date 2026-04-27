# Configuration 🦍

`bitchtea` is highly configurable via environment variables, the `.bitchtearc` file, and named profiles.

## Environment Variables

- `OPENAI_API_KEY`: Key for OpenAI provider.
- `ANTHROPIC_API_KEY`: Key for Anthropic provider.
- `OPENROUTER_API_KEY`: Key for OpenRouter.
- `ZAI_API_KEY`: Key for ZAI (Zero AI) provider.
- `BITCHTEA_PROVIDER`: Default provider (`openai`, `anthropic`).
- `BITCHTEA_MODEL`: Default model ID.
- `OPENAI_BASE_URL`: Custom endpoint for OpenAI-compatible APIs.
- `ANTHROPIC_BASE_URL`: Custom endpoint for Anthropic.

## The .bitchtearc File

Located at `~/.bitchtea/bitchtearc`. This file is executed line-by-line on startup. Lines starting with `#` are comments.

### Directives
- `set provider <openai|anthropic>`: Set default provider.
- `set model <id>`: Set default model.
- `set apikey <key>`: Set API key (overrides env).
- `set baseurl <url>`: Set custom API base.
- `set nick <name>`: Set your display name.
- `set profile <name>`: Load a specific profile on startup.
- `set sound <on|off>`: Toggle notification bings.
- `set auto-next <on|off>`: Toggle automatic tool execution.
- `set auto-idea <on|off>`: Toggle automatic next-step suggestions.

## Profiles

Profiles are bundles of configuration (provider, model, baseURL, etc.).

### Built-in Profiles
- `ollama`: Configured for local Ollama instances (uses `http://localhost:11434/v1`).
- `openrouter`: Configured for OpenRouter.
- `zai-openai`: Zero AI OpenAI-compatible endpoint.
- `zai-anthropic`: Zero AI Anthropic-compatible endpoint.

### Commands
- `/profile save <name>`: Save current configuration as a profile.
- `/profile load <name>`: Load a saved profile.
- `/profile delete <name>`: Remove a profile.

---
APE STRONK TOGETHER. 🦍💪🤝
