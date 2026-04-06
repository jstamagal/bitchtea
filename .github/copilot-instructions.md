# Copilot Instructions

Read AGENTS.md for full project context, module map, and build commands.

This project uses **bd (beads)** for issue tracking.

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

After completing work: `go build ./... && go test ./... && go vet ./...` then commit and push.
