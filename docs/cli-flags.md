# bitchtea CLI Flags Reference

This document provides a comprehensive reference for all CLI flags and environment variables supported by `bitchtea`.

## Usage

```bash
bitchtea [flags]
bitchtea daemon <subcommand>
```

## Application Flags

| Flag | Shorthand | Argument | Description |
| :--- | :--- | :--- | :--- |
| `--model` | `-m` | `<name>` | Specify the LLM model to use (e.g., `gpt-4o`, `claude-3-5-sonnet-20240620`). Defaults to `gpt-4o`. |
| `--profile` | `-p` | `<name>` | Load a saved or built-in profile. Built-in profiles include: `ollama`, `openrouter`, `zai-openai`, `zai-anthropic`. |
| `--resume` | `-r` | `[path]` | Resume a previous session. If `path` is omitted or set to `latest`, it resumes the most recent session from the session directory. |
| `--headless` | `-H` | (None) | Run the application once without the Terminal User Interface (TUI). Requires a prompt via `--prompt` or piped stdin. |
| `--prompt` | (None) | `<text>` | The prompt to send when running in `--headless` mode. |
| `--auto-next-steps` | (None) | (None) | Automatically inject "next step" prompts into the conversation after an agent turn. |
| `--auto-next-idea` | (None) | (None) | Automatically generate and display improvement ideas after an agent turn. |
| `--help` | `-h` | (None) | Display the help message and exit. |

## Daemon Subcommands

The `daemon` command provides orthogonal functionality for background tasks.

| Subcommand | Description |
| :--- | :--- |
| `start` | Runs the daemon in the foreground. Logging is sent to stdout and the log file. |
| `status` | Checks if the daemon is running and reports its PID if available. |
| `stop` | Sends a `SIGTERM` signal to the running daemon. |

## Environment Variables

| Variable | Description |
| :--- | :--- |
| `OPENAI_API_KEY` | API key for OpenAI services. |
| `OPENAI_BASE_URL` | Custom base URL for OpenAI-compatible APIs. |
| `ANTHROPIC_API_KEY` | API key for Anthropic services. |
| `OPENROUTER_API_KEY` | API key for OpenRouter (used by the `openrouter` profile). |
| `ZAI_API_KEY` | API key for Z.ai services (used by the `zai-*` profiles). |
| `BITCHTEA_MODEL` | Set the default model. |
| `BITCHTEA_PROVIDER` | Set the default provider (e.g., `openai`, `anthropic`). |
| `BITCHTEA_CATWALK_URL` | Base URL for the Catwalk model catalog. If unset, background updates are disabled. |
| `BITCHTEA_CATWALK_AUTOUPDATE` | Enable background catalog refreshes (accepts `1`, `true`, `yes`, `on`). Defaults to `false`. |

## Data Directories

Bitchtea stores its data in the following locations (platform dependent):

- **Config/RC:** `~/.bitchtearc`
- **Data Dir:** Defaults to `~/.bitchtea/`
  - **Sessions:** `~/.bitchtea/sessions/`
  - **Memory:** `~/.bitchtea/memory/`
  - **Catalog Cache:** `~/.bitchtea/catalog/providers.json`
  - **Daemon Logs:** `~/.bitchtea/daemon/daemon.log`
  - **Daemon Lock/PID:** `~/.bitchtea/daemon/daemon.lock`, `daemon.pid`
