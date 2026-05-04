# CONTRIBUTING

## Quality Gates

Every code change must pass all four checks before merging:

```bash
go build ./...
go test ./...
go test -race ./...
go vet ./...
```

Run `go build -o bitchtea .` to build the main binary. See [CLAUDE.md](CLAUDE.md) for additional build and test guidance.

## Issue Tracking

This project uses **bd (beads)** for issue tracking. The workflow is:

```
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd update <id> --status=in_progress  # Start work
... do the work ...
bd close <id>         # Complete work
```

After closing an issue, push both the dolt database and the git repository:

```bash
bd dolt push
git push
```

## Dependency Graph Rules

The package dependency graph must remain strictly acyclic:

```
main    -> agent, config, llm, session, tools, ui
ui      -> agent, config, llm, session, sound
agent   -> config, llm, memory, tools, agent/event
session -> llm
llm     -> tools
tools   -> memory
```

**No upward edges.** For example, `llm` must not import `agent`, and `tools` must not import `llm`. A change that introduces a cycle will be rejected. See [docs/architecture.md](docs/architecture.md) for the full dependency diagram.

## Adding a Tool

1. Extend `Definitions()` in `internal/tools/tools.go` with the schema exposed to the LLM.
2. Extend `Execute()` with the new dispatcher case.
3. Implement the executor with path handling and useful error messages.
4. Add tests in `internal/tools/tools_test.go`.

See [docs/tools.md](docs/tools.md) for the tool system architecture.

## Adding a Slash Command

1. Update parsing and state changes in `handleCommand` (`internal/ui/model.go`).
2. Add tests in `internal/ui/`.
3. Update `README.md` if the command is user-facing.
4. Update `printUsage()` in `main.go` if it should show in `--help`.

Do not block inside command handlers -- return `tea.Cmd`s. See [docs/commands.md](docs/commands.md).

## Testing Philosophy

Prefer asserting **behavior and state** over merely checking that code does not panic. Tests named `Test*DoesNotPanic` are considered superficial. A good test validates:

- Correct output values
- Expected error messages
- Side-effect correctness (file writes, state transitions, tool results)

See [docs/testing.md](docs/testing.md) for the detailed testing guide.

## Commit Policy

- Use descriptive commit messages that explain **why** a change was made, not just what changed.
- Include `Co-Authored-By` attribution for agent-assisted work.
- Keep commits focused on a single logical change.
- Do not use `--no-verify` to skip hooks.
- Do not amend published commits.

## Session Completion Protocol

When ending a work session:

1. File issues for any remaining or follow-up work.
2. Run quality gates (build, test, vet).
3. Update issue status -- close finished work.
4. Push to remote:
   ```bash
   git pull --rebase
   bd dolt push
   git push
   git status   # must show "up to date with origin"
   ```
5. Verify all changes are committed and pushed.

## Development Environment

See [docs/development-guide.md](docs/development-guide.md) for setup instructions, recommended tools, and debugging workflows.
