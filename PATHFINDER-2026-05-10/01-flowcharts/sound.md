# Flowchart: Sound / Notification Effects

```mermaid
flowchart TD
    A["Beep<br/>sound.go:28"] --> B["writeBell(1)<br/>sound.go:47"]
    A2["Play(soundType)<br/>sound.go:12"] --> C{"soundType?"}
    C -->|"done"| D["writeBell(1)<br/>sound.go:47"]
    C -->|"success"| E["writeBell(1)<br/>sound.go:47"]
    C -->|"error"| F["writeBell(3)<br/>sound.go:47"]
    C -->|"bell"| G["writeBell(1)<br/>sound.go:47"]
    C -->|"default"| H["writeBell(1)<br/>sound.go:47"]
    B --> I["io.WriteString<br/>Output, '\a'"]
    D --> I
    E --> I
    F --> I
    G --> I
    H --> I

    J["Success()<br/>sound.go:33"] --> A2
    K["Error()<br/>sound.go:38"] --> A2
    L["Done()<br/>sound.go:43"] --> A2

    M["Output writer<br/>sound.go:9"] -.- I
    N["os.Stdout<br/>default"] -.-> M
    O["Test override<br/>io.Discard"] -.-> M
```

## Summary

`Beep()` is a direct single-bell emitter. `Play(soundType)` dispatches: `"error"` emits 3 bells, all others emit 1. Convenience wrappers `Success()`, `Error()`, `Done()` delegate to `Play`. Every path writes `\a` to the package-level `Output io.Writer` (default `os.Stdout`). Errors from `io.WriteString` are silently discarded. Fire-and-forget notification system with no returned errors or retry logic.

**External dependencies:** `io`, `os`
