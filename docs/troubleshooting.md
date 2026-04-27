# 🦍 TROUBLESHOOTING 🦍

If the vines tangle, use this guide.

## 1. "No API key found"

This means `bitchtea` can't find your keys.
- **Fix**: Run `export OPENAI_API_KEY="your-key"` before starting.
- **Fix**: Or use the `/apikey your-key` command inside the app.

## 2. Debugging

If the agent is acting strange, see what it sees:
- Run `/debug on` to see raw API data.
- Look at the terminal output if you ran it in a way that captures logs.

## 3. Resetting State

If a session is broken or memory is corrupted:
- **Sessions**: Delete files in `~/.bitchtea/sessions/`.
- **Memory**: Delete the `~/.bitchtea/memory/` directory.
- **Full Reset**: Delete the entire `~/.bitchtea/` directory.

APE STRONK TOGETHER. 🦍💪🤝
