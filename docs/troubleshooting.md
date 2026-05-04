# BITCHTEA: TROUBLESHOOTING

Common failure modes, their symptoms, and how to fix them.

---

## 1. Missing or Invalid API Key

**Symptom:** The TUI boots but every prompt returns an error. The status bar shows an error hint like `auth failed -- check /set apikey`.

**Cause:** No API key is set in the environment, or the key set does not match the active provider.

**Fix:**

```bash
# Set the key for your provider
export ANTHROPIC_API_KEY="sk-ant-..."
export OPENAI_API_KEY="sk-..."
```

Or set it at runtime inside the TUI:

```
/set apikey sk-ant-...
```

The error hint system (`ErrorHint` in `internal/llm/errors.go`) maps HTTP 401 to the auth hint and is displayed automatically.

---

## 2. Wrong Provider Detected

**Symptom:** The model name or provider shown in the top bar does not match what you expect. The TUI connects but gets 404s or garbled responses.

**Cause:** Both `ANTHROPIC_API_KEY` and `OPENAI_API_KEY` are set. `DetectProvider` in `internal/config` checks `ANTHROPIC_API_KEY` first, then `OPENAI_API_KEY`. If you want OpenAI and both are set, force the provider:

```bash
export BITCHTEA_PROVIDER=openai
```

**Fix:**

- Set `BITCHTEA_PROVIDER` environment variable to `openai` or `anthropic`.
- Or use `/set provider openai` inside the TUI.
- Or use `--profile` to pick a built-in profile (`ollama`, `openrouter`, etc.).

---

## 3. Daemon Won't Start

**Symptom:** Running `bitchtea daemon` returns an error or silently exits.

**Common causes and fixes:**

| Symptom | Cause | Fix |
|---|---|---|
| `address already in use` or `lock` error | Another daemon instance is already running | Check with `pgrep -a bitchtea` and kill the stale process, then remove the lockfile: `rm -f ~/.bitchtea/daemon.lock` |
| `permission denied` when writing to data directory | Data directory not writable | Check ownership: `ls -la ~/.bitchtea/` |
| Daemon starts but no checkpoint jobs run | Daemon jobs registry not loaded | Check `internal/daemon/jobs/` for registered jobs. Run `bitchtea daemon --debug` |

---

## 4. MCP Server Crash

**Symptom:** Tools from an MCP server stop working mid-session. The UI shows tool errors with no result, or the agent reports tool failures.

**Cause:** The external MCP server process crashed or was killed. Bitchtea connects to MCP servers as subprocesses.

**Fix:**

1. Look for crashed processes: `pgrep -a mcp`
2. Restart the MCP server manually.
3. Restart the bitchtea session with `/restart` or a fresh invocation.
4. Check the MCP server config in your profile or `.bitchtearc` for the correct command and arguments.

---

## 5. Terminal Tool Hangs

**Symptom:** A `terminal_send` or `terminal_start` call does not complete. The agent appears stuck waiting for tool output.

**Cause:** The tool is waiting for input or the process inside the terminal is blocked.

**Fix using the escape ladder:**

1. Press **Esc** once to cancel the currently running tool (the agent turn continues).
2. Press **Esc** a second time to cancel the entire LLM turn.
3. If the terminal PTY is truly stuck, use `/restart` to reset the agent state.

If a terminal was started but never closed, it will be cleaned up when the session ends. To manually kill orphaned PTY processes:

```bash
pkill -f "bitchtea.*terminal"
```

---

## 6. Session Corruption or Load Failure

**Symptom:** `--resume` fails, or the session file cannot be loaded. Error messages reference JSON parsing failures.

**Cause:** The session JSONL file (`~/.bitchtea/sessions/*.jsonl`) has a corrupted or truncated line. Sessions are append-only; a crash during write can produce a partial line.

**Fix:**

1. Locate the session file: `ls -lt ~/.bitchtea/sessions/ | head -5`
2. Open the file and look for the last line. If it is truncated, remove it.
3. Run `bitchtea --resume <path>` again.

The JSONL format is line-oriented — each line is independently parseable. Removing one corrupted line loses only that single turn. The session loader (`session.Load`) skips unparseable v0 lines gracefully.

---

## 7. Memory Not Recalled

**Symptom:** The agent cannot find information that was previously stored. `search_memory` returns empty results or the agent does not reference known facts.

**Cause:** Memory scope mismatch. The tool searched in `RootScope` but the data was stored under `ChannelScope`, or vice versa. The agent's current memory scope is determined by its active IRC context (`#channel` vs root vs `/query`).

**Fix:**

- Switch to the correct context: `/join #chan` or `/query nick` where the memory was written.
- Use the `/memory` command to see what is stored at the workspace level.
- Run `/compact` to consolidate recent conversation into durable memory.
- The `write_memory` tool stores to the current scope; ensure the agent is in the right context before writing.

---

## 8. Model Picker Empty

**Symptom:** `/models` shows "no models available" or an empty list.

**Cause:** The model catalog (`~/.bitchtea/catalog/providers.json`) is missing, stale, or the provider API returned no models.

**Fix:**

1. Check if the catalog file exists: `ls -la ~/.bitchtea/catalog/`
2. Delete the cached catalog and restart: `rm -rf ~/.bitchtea/catalog/`
3. The catalog refresh happens asynchronously at startup if `BITCHTEA_CATWALK_AUTOUPDATE` is set.
4. If offline, the embedded snapshot is used — ensure your build is up to date: `git pull && go build -o bitchtea .`

---

## 9. Build Failures

**Symptom:** `go build` fails with version errors or missing dependencies.

**Common causes and fixes:**

| Error | Cause | Fix |
|---|---|---|
| `go: go.mod requires go 1.26.x` | Wrong Go version installed | Run `go version`. Install Go 1.26+ from https://go.dev/dl/. |
| `undefined: ...` | Stale module cache | Run `go mod tidy` then `go build ./...` |
| `missing go.sum entry` | Changed dependency | Run `go mod tidy` |
| `cannot find module` | Proxy or network issue | Try `GONOSUMCHECK=* GONOSUMDB=* GOPROXY=direct go build ./...` |

The project targets Go 1.26 (see `go.mod`). Older toolchains will not work.

---

## General Debugging Tips

- Run `bitchtea --headless --prompt "test"` to isolate TUI issues from LLM issues.
- Check `~/.bitchtea/` for error logs and session data.
- Use the `/set` command to inspect current configuration values.
- File a bug on the issue tracker with the output of `bitchtea --help` and your provider/OS.
