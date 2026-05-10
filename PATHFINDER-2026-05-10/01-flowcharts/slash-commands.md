# Flowchart: TUI Commands (Slash Commands)

```mermaid
flowchart TD
    subgraph Entry["Entry: User Input in TUI"]
        A[/user types text/] --> B[input field<br/>internal/ui/model.go:102]
    end

    subgraph Submit["Submit: Enter Key"]
        B --> C[Update called<br/>internal/ui/model.go:380]
        C --> D[Extract trimmed input<br/>internal/ui/model.go:562]
        D --> E{input == ""?}
        E -->|yes| Z[ignore]
        E -->|no| F{starts with "/"?<br/>internal/ui/model.go:574}
    end

    subgraph Dispatch["Command Dispatch"]
        F -->|yes| G[handleCommand called<br/>internal/ui/model.go:1518]
        G --> H[parts = strings.Fields(input)<br/>internal/ui/model.go:1519]
        H --> I[lookupSlashCommand(parts[0])<br/>internal/ui/commands.go:75-77]
        I --> J{handler found?}
        J -->|no| K[m.errMsg "Unknown command"<br/>internal/ui/model.go:1526]
        J -->|yes| L[m.handler(m, input, parts)<br/>internal/ui/model.go:1530]
    end

    subgraph Registration["Registration: init time"]
        M[slashCommandRegistry = registerSlashCommands(...)<br/>internal/ui/commands.go:34]
        M --> N[registerSlashCommands(...)<br/>internal/ui/commands.go:65-72]
        N --> O[registry map built]
        O --> P["Spec: /set → handleSetCommand"]
        O --> Q["Spec: /sessions → handleSessionsCommand"]
        O --> R["Spec: /compact → handleCompactCommand"]
        O --> S["Spec: /query → handleQueryCommand"]
    end

    subgraph SetHandler["/set handler"]
        L --> T[handleSetCommand<br/>internal/ui/commands.go:129]
        T --> U{parts count?}
        U -->|1| V[/set alone → list all keys<br/>config.SetKeys()]
        U -->|2| W{bare /set key<br/>model|profile|debug?}
        W -->|model| X[handleModelsCommand → picker]
        W -->|profile| Y[handleProfileCommand → picker]
        W -->|debug| Z1[handleDebugCommand → toggle]
        W -->|other| Z2[show current value]
        U -->|3| AA[/set key value]
        AA --> AB{key == provider|model|baseurl|apikey?}
        AB -->|yes| AC[handleProviderCommand etc.]
        AB -->|no| AD[config.ApplySet()]
        AD --> AE{key == profile?}
        AE -->|yes| AF[m.agent.SetProvider/Model/BaseURL/APIKey]
        AE -->|no| AG[side-effect: none]
        AF --> AH[m.sysMsg "Value of KEY set to VALUE"]
    end

    subgraph SessionsHandler["/sessions handler"]
        L --> S1[handleSessionsCommand<br/>internal/ui/commands.go:620]
        S1 --> S2[session.List(m.config.SessionDir)]
        S2 --> S3{pagination: pageSize=20}
        S3 --> S4[m.sysMsg paginated list]
    end

    subgraph CompactHandler["/compact handler"]
        L --> C1[handleCompactCommand<br/>internal/ui/commands.go:284]
        C1 --> C2{agent streaming?}
        C2 -->|yes| C3[m.sysMsg "Can't compact while agent is working"]
        C2 -->|no| C4[m.agent.Compact(context.Background())]
        C4 --> C5[m.agent.EstimateTokens → formatTokens]
        C5 --> C6[m.sysMsg "Compacted: before → after tokens"]
    end

    subgraph QueryHandler["/query handler"]
        L --> Q1[handleQueryCommand<br/>internal/ui/commands.go:926]
        Q1 --> Q2{m.focus.SetFocus(Direct(parts[1]))}
        Q2 --> Q3[m.syncAgentContextIfIdle(ctx)]
        Q3 --> Q4[m.focus.Save(m.config.SessionDir)]
        Q4 --> Q5[m.sysMsg "Query open: name"]
    end

    K --> K1[addMessage MsgError]
    K1 --> K2[refreshViewport]
    V --> V1[m.sysMsg settings listing]
    X --> X1[openModelPicker → applyModelSelection]
    X1 --> X2[m.agent.SetModel + clearLoadedProfile]
    Y --> Y1[handleProfileCommand → applyProfileToModel]
    Y1 --> Y2[m.agent.SetService/Provider/Model/BaseURL/APIKey]
    Z2 --> Z3[m.sysMsg "Value of KEY is VALUE"]
    AG --> AH

    Z3 --> AH
    S4 --> AH
    C3 --> AH
    C6 --> AH
    Q5 --> AH
    AH --> End[return updated Model, tea.Cmd<br/>to bubbletea loop]

    subgraph Exit["Exit: tea.Model returned to framework"]
        End --> Out[Update loop continues<br/>internal/ui/model.go:380]
    end

    style Z fill:#fff3cd
    style C3 fill:#fff3cd
    style K fill:#f8d7da
```

## Summary

**Command registration**: `registerSlashCommands()` at `commands.go:34` builds map of ~27 commands at init.

**Input parsing**: `Update()` → trim → empty check → `HasPrefix "/"`.

**Dispatch**: `handleCommand` (model.go:1518) splits input, looks up `parts[0]` in registry, dispatches to handler function `(Model, rawInput, parts[])`.

**Representative handlers:**
- `/set` (commands.go:129): bare → list keys; key+no-value → show/picker; key+value → `config.ApplySet` + agent re-config for profile
- `/sessions` (commands.go:620): `session.List()` with pagination (pageSize=20)
- `/compact` (commands.go:284): guard streaming, call `m.agent.Compact()`, report token diff
- `/query` (commands.go:926): `SetFocus(Direct)`, sync agent context, persist focus

**Side effects:** config mutation, agent re-configuration, session persistence, viewport refresh.

**External dependencies:** bubbletea, internal/agent, internal/config, internal/session, internal/catalog, internal/llm
