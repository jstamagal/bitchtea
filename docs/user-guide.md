# 🦍 USER GUIDE 🦍

How to use the tools of the canopy.

## 1. The Interface

- **Input Bar**: Bottom of the screen. Type messages or `/commands`.
- **Transcript**: Main window. Shows what you and the agent said.
- **Status Bar**: Shows your `nick`, the `model`, and the `context` (like #main).

## 2. Including Files (@file)

You can show the agent files by using the `@` symbol:
`Can you explain what @main.go does?`

The agent will read the file and include it in your prompt.

## 3. Tool Monitoring

The agent is autonomous. It can read, write, and run bash commands. 
- You will see "Tool Call" in the transcript when it works.
- If it takes a long time, use `/activity` to see what it is doing in the background.

## 4. IRC-Style Channels

You can split your work into channels:
- `/join #refactor`: Create a new context for refactoring.
- `/channels`: List all your open work contexts.
- `/part`: Leave the current channel.

APE STRONK TOGETHER. 🦍💪🤝
