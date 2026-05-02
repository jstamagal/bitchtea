# 🦍 BITCHTEA: TROUBLESHOOTING

If the jungle gets quiet, something is wrong.

## 🔑 API KEY ISSUES

If you see `no API key found`:
1. Check your environment variables (`echo $OPENAI_API_KEY`).
2. Ensure you are using the correct provider with `/set provider <openai|anthropic>`.
3. Use `/set apikey <key>` to set it manually for the active session.

## 🔍 DEBUGGING THE METAL

If the agent is acting weird or failing silently, use the debug hook:
- Type `/debug on`.
- Bitchtea will now log every raw HTTP request and response header into the transcript.
- Check for `401 Unauthorized` (key issue) or `429 Too Many Requests` (rate limits).

## 🧹 STATE RESET

If a session is corrupted or the TUI is hanging:
1. **Kill the process**: `Ctrl+C` (twice) or `killall bitchtea`.
2. **Clear checkpoints**: Delete `~/.config/bitchtea/sessions/.bitchtea_checkpoint.json`.
3. **Start Fresh**: Run without resuming: `./bitchtea`.

## 🆘 APE STUCK

If the agent says `🦍😱💀 APE STUCK. KING HELP.`:
- This means 3 consecutive failures occurred.
- Usually, the model is trying to use a tool incorrectly or a file path is wrong.
- Give it a direct instruction: `>> stop trying to read that file, it does not exist. try ls instead.`

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝
