# 🦍 DEVELOPMENT GUIDE 🦍

This scroll is for the apes building the tree. 

## 1. Required Checks

Before you commit or close a task, the "Canopy Four" must pass:
```bash
go build ./...
go test ./...
go test -race ./...
go vet ./...
```

## 2. Adding a Slash Command

1.  **Logic**: Implement the logic in `internal/ui/model.go` under `handleCommand`.
2.  **Registration**: Wire it into the `switch` block.
3.  **UI**: Ensure it returns a `tea.Cmd` if it performs side effects.
4.  **Testing**: Add a test case in `internal/ui/commands_test.go`.

## 3. Adding a Tool

1.  **Definition**: Add the JSON schema to `Definitions()` in `internal/tools/tools.go`.
2.  **Dispatch**: Add a `case` to the `Execute()` method.
3.  **Implementation**: Write the `execYourTool` function. Handle paths with `resolvePath`.
4.  **Testing**: Add a test in `internal/tools/tools_test.go`.

## 4. Testing Patterns

- **Fake Streamers**: Do not call real APIs in tests. Use the `fakeStreamer` pattern in `internal/agent/agent_loop_test.go`.
- **Hermetic Filesystem**: Use `t.TempDir()` for all tests that read/write files.
- **Non-Blocking**: Ensure `Update()` in the UI never blocks. Use goroutines and channels to feed messages back to the model.

APE STRONK TOGETHER. 🦍💪🤝
