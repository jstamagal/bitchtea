# 🦍 THE BITCHTEA SCROLLS: DEVELOPMENT

How to feed and grow the ape.

## 🧪 TESTING PATTERNS

- **Headless Tests**: Use `runHeadlessLoop` with a `scriptedStreamer` to verify agent logic without the TUI (see `main_test.go`).
- **Package Tests**: Every internal package should have `*_test.go` verifying its core logic.
- **MAMA Rule**: Witness the failure before applying the fix.

## 🛠️ ADDING NEW TOOLS

1. **Define Schema**: Add a new `ToolDef` to `Registry.Definitions()` in `internal/tools/tools.go`.
2. **Implement Logic**: Create a private `execNewTool` method in `internal/tools/tools.go`.
3. **Route**: Add a case to the switch in `Registry.Execute()`.

## ⌨️ ADDING SLASH COMMANDS

1. **Handler**: Create a function with signature `func(Model, string, []string) (Model, tea.Cmd)` in `internal/ui/commands.go`.
2. **Register**: Add the command name and handler to the `slashCommandRegistry` at the top of the file.

## 🏗️ REQUIRED CHECKS

Before shipping code:
- **`go fmt ./...`**: Keep the metal clean.
- **`go test ./...`**: Don't break the jungle.
- **`go build`**: Ensure it compiles.

## 🦍 PERSONA COMPLIANCE

- All code comments must be in **Ape-speak**.
- The `Agent` must always anchor the persona via `buildPersonaAnchor` in `internal/agent/agent.go`.

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝
