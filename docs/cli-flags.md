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
| `--headless` | `-H` | (None) | Run the application once without the Terminal User Interface (TUI). Requires a prompt via `--prompt` or piped stdin. See [user-guide.md](user-guide.md) for output stream details, follow-up loop behavior, and exit codes. |
| `--prompt` | (None) | `<text>` | The prompt to send when running in `--headless` mode. When combined with piped stdin, both are concatenated with a newline. |
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

| Variable | Default | Precedence | Description |
| :--- | :--- | :--- | :--- |
| `OPENAI_API_KEY` | — | — | API key for OpenAI services. |
| `OPENAI_BASE_URL` | — | — | Custom base URL for OpenAI-compatible APIs. |
| `ANTHROPIC_API_KEY` | — | — | API key for Anthropic services. |
| `OPENROUTER_API_KEY` | — | — | API key for OpenRouter (used by the `openrouter` profile). |
| `ZAI_API_KEY` | — | — | API key for Z.ai services (used by the `zai-*` profiles). |
| `BITCHTEA_MODEL` | `gpt-4o` | 4th (env var) | Default model name. Overridden by `~/.bitchtea/bitchtearc` `set model ...`, by `--model` / `-m`, and by profile loading. Read once at startup. |
| `BITCHTEA_PROVIDER` | `openai` | 4th (env var) | Default provider name (`openai` or `anthropic`). Overridden by `~/.bitchtea/bitchtearc` `set provider ...`, by profile loading, and indirectly by `--model` (provider may be inferred). Read once at startup. |
| `BITCHTEA_CATWALK_URL` | (empty — off) | 4th (env var) | Base URL for the Catwalk model catalog. Background catalog refresh is disabled when unset. Ignored unless `BITCHTEA_CATWALK_AUTOUPDATE=true`. Read once at startup. |
| `BITCHTEA_CATWALK_AUTOUPDATE` | `false` | 4th (env var) | Enable background catalog refreshes. Accepts `1`, `true`, `yes`, `on` (case-insensitive). When enabled and `BITCHTEA_CATWALK_URL` is set, starts a bounded (5s timeout) background refresh of the model catalog at startup. Errors are silently swallowed — next `/models` read uses the previous cache. Read once at startup. |

**Precedence order (highest to lowest):**
1. CLI flags (`--model`, `--profile`)
2. Loaded profile (via `--profile`)
3. `~/.bitchtea/bitchtearc` `set` commands
4. Environment variables (`BITCHTEA_*`)
5. Hardcoded defaults

## Data Directories

Bitchtea stores its data in the following locations (platform dependent):

- **Config/RC:** `~/.bitchtea/bitchtearc`
- **Data Dir:** Defaults to `~/.bitchtea/`
  - **Sessions:** `~/.bitchtea/sessions/`
  - **Memory:** `~/.bitchtea/memory/`
  - **Catalog Cache:** `~/.bitchtea/catalog/providers.json`
  - **Daemon Logs:** `~/.bitchtea/daemon/daemon.log`
  - **Daemon Lock/PID:** `~/.bitchtea/daemon/daemon.lock`, `daemon.pid`
