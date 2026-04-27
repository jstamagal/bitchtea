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

## 🧱 TECHNICAL DEEP-DIVE: THE PRIORITY OF TRUTH

Bitchtea follows a strict hierarchical merge strategy for configuration. Each layer can overwrite the previous ones, ensuring that the most specific intent (the command line) always wins.

### 🧬 THE MERGE STACK (Ascending Priority)

1.  **Hardcoded Defaults**: `internal/config/config.go:48`. Sane defaults like `AgentNick = "bitchtea"`.
2.  **Environment Variables**: `internal/config/config.go:57`. Detected via `DetectProvider`. 
    - `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` / etc.
    - `BITCHTEA_MODEL` / `BITCHTEA_PROVIDER`.
3.  **RC File (`~/.bitchtea/bitchtearc`)**: `internal/config/rc.go:37`.
    - Any `set <key> <value>` lines are applied via `ApplyRCSetCommands`.
    - Note: Setting `provider`, `model`, `apikey`, or `baseurl` in RC will clear the active `Profile` field to prevent configuration ambiguity.
4.  **CLI Flags (Initial Pass)**: `main.go:127`. Flips bits in the `Config` struct before profile resolution.
5.  **Profiles (`-p, --profile`)**: `main.go:117`.
    - If a profile is resolved, it is applied via `ApplyProfile`. 
    - This can overwrite RC settings and Env vars.
6.  **CLI Flags (Override Pass)**: `main.go:125`. 
    - **CRITICAL**: `parseCLIArgs` is called a *second* time if a profile was loaded. This ensures that `-m gpt-4o-mini --profile my-anthropic-profile` correctly uses the OpenAI model override even if the profile specifies Claude.

### 🧬 CLI FLAG TO CONFIG MAPPING

| Flag | Config Struct Field | Side Effects |
| :--- | :--- | :--- |
| `-m, --model` | `cfg.Model` | Sets `cfg.Profile = ""` |
| `-p, --profile` | `opts.profileName` | Triggers profile load & merge |
| `-r, --resume` | `opts.resumePath` | None (Handled in `main.go` for session load) |
| `--prompt` | `opts.prompt` | None (Headless prompt content) |
| `--auto-next-steps` | `cfg.AutoNextSteps` | Sets to `true` |
| `--auto-next-idea` | `cfg.AutoNextIdea` | Sets to `true` |
| `-H, --headless` | `opts.headless` | Switches `main.go` execution path |

### 🔍 UNDER THE HOOD: `applyStartupConfig`
The function `applyStartupConfig` in `main.go:104` is the conductor:
1.  **RC Pass**: `ApplyRCSetCommands` filters out `set` commands, applying them to the config pointer and returning the rest (like `join #ch`) for the TUI to execute as startup commands.
2.  **Arg Pass 1**: `parseCLIArgs` reads the environment/flags.
3.  **Profile Resolution**: If `-p` was given, `ResolveProfile` searches `~/.bitchtea/profiles/*.json` then built-in presets.
4.  **Override Pass**: If a profile was applied, `parseCLIArgs` runs again. This is the "Ape's Last Word" — ensuring explicit flags always trump profile defaults.

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝

## 🧱 TECHNICAL DEEP-DIVE: CONFIGURATION HIERARCHY

Bitchtea merges configuration through a multi-pass pipeline in `main.go` and `internal/config/`. The "Priority of Truth" ensures that the most specific intent always overrides general defaults.

### 🧬 THE PRIORITY OF TRUTH (Ascending Order)
1. **Hardcoded Defaults**: Set in `internal/config/config.go:DefaultConfig()`.
2. **Environment Variables**: Overlays like `OPENAI_API_KEY` or `BITCHTEA_MODEL`.
3. **RC File (`~/.bitchtea/bitchtearc`)**: Commands applied via `ApplyRCSetCommands`.
4. **Initial CLI Args**: First pass of `parseCLIArgs`.
5. **Profile Data**: Loaded if `-p` is provided, overwriting previous values.
6. **CLI Overrides**: A recursive second pass of `parseCLIArgs` to ensure explicit flags trump profile defaults.

---

### 🔍 FLAG-TO-STRUCT TRACE
Every flag parsed in `main.go:parseCLIArgs` directly mutates the `config.Config` or `cliOptions` structs.

| Flag | Struct Field Mutation | Technical Behavior |
| :--- | :--- | :--- |
| `-m, --model` | `cfg.Model = args[i+1]` | Sets target LLM. Also sets `cfg.Profile = ""` to prevent state conflict. |
| `-p, --profile` | `opts.profileName = args[i+1]` | Triggers `config.ResolveProfile`. Loads provider, baseurl, and key from `profiles/*.json`. |
| `-r, --resume` | `opts.resumePath = args[i+1]` | If value is "latest", calls `session.Latest(cfg.SessionDir)`. Loads JSONL into agent history. |
| `-H, --headless` | `opts.headless = true` | Diverts execution to `runHeadlessLoop`, bypassing Bubbletea TUI. |
| `--prompt` | `opts.prompt = args[i+1]` | Direct input for headless mode. Merged with Stdin if both present. |
| `--auto-next-steps` | `cfg.AutoNextSteps = true` | Enables autonomous follow-up loop after every assistant turn. |
| `--auto-idea` | `cfg.AutoNextIdea = true` | Enables autonomous improvement suggestions. |

---

### ⚙️ UNDER THE HOOD: `applyStartupConfig`
This function (`main.go:104`) is the primary configuration conductor.

1. **RC Pass**: It reads `bitchtearc`. It filters out "set" commands and applies them to the `Config` struct. It returns non-set commands (like `/join`) for later TUI execution.
2. **Pass 1 (Flags)**: Runs `parseCLIArgs` to catch a `-p` or `--profile` request.
3. **Profile Resolution**: If a profile is requested, it calls `config.ApplyProfile`. 
   - *Logic Trace*: This can overwrite API keys and BaseURLs set in Step 2.
4. **Pass 2 (The Override)**: It calls `parseCLIArgs` **again**. 
   - *Rationale*: If KING runs `bitchtea -p ollama -m gpt-4o`, the second pass ensures the explicit `-m` flag wins over the `llama3` default inside the `ollama` profile.

---

### 📡 HEADLESS OUTPUT VERBATIM
When running with `-H`, output is piped to `stdout` and status to `stderr`.

- **Text Tokens**: Sent to `stdout` as they arrive.
- **Tool Starts**: Prints to `stderr`: `[tool] <name> args=<json>`
- **Tool Results**: Prints to `stderr`: `[tool] <name> result=<text>`
- **State Changes**: Prints to `stderr`: `[status] thinking|tool_call`

