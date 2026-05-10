# Flowchart: Terminal TUI (User Interface)

```mermaid
flowchart TD
    %% ENTRY POINTS
    EP1["main.go:273<br/>runHeadless"]
    EP2["main.go:118<br/>TUI startup"]

    %% STARTUP PATH
    A1["main.go:118<br/>m := buildStartupModel(&cfg, ...)"]
    A2["main.go:174<br/>buildStartupModel"]
    A3["ui.NewModel(cfg)<br/>model.go:181"]
    A4["tea.NewProgram(m, ...)<br/>main.go:125"]
    A5["p.Run()<br/>main.go:135"]
    A6["m.Init()<br/>model.go:363"]
    A7["tea.Batch cmd batch<br/>spinner tick, mp3 tick, EnterAltScreen, showSplash"]
    A8["splashMsg handled<br/>model.go:423 - 504"]

    %% INPUT HANDLING PATH
    B1["tea.KeyMsg dispatched<br/>model.go:506"]
    B2["Picker overlay?<br/>model.go:510"]
    B3["Picker active<br/>handlePickerKey"]
    B4["Text in textarea?<br/>Enter key pressed"]
    B5["msgInput executed<br/>model.go:345"]
    B6["text line captured<br/>model.go:346"]
    B7["starts with '/' ?<br/>model.go:352"]
    B8["m.handleCommand(line)<br/>model.go:357"]
    B9["m.sendToAgent(input)<br/>model.go:579"]

    %% SLASH COMMAND DISPATCH
    C1["handleCommand()<br/>model.go:1518"]
    C2["strings.Fields(input)<br/>split command"]
    C3["lookupSlashCommand(parts[0])<br/>commands.go:75"]
    C4{"Found in registry?"}
    C5["errMsg Unknown command<br/>model.go:1526"]
    C6["handler(m, input, parts)<br/>dispatch to handler func"]
    
    %% HANDLER EXAMPLES (subset)
    H1["handleQuitCommand<br/>commands.go:115"]
    H2["handleHelpCommand<br/>commands.go:119"]
    H3["handleSetCommand<br/>commands.go:129"]
    H4["handleJoinCommand<br/>commands.go:875"]
    H5["handleRestartCommand<br/>commands.go:259"]
    
    H1 --> CX["returns m, tea.Quit"]
    H2 --> CX2["m.addMessage + m.refreshViewport"]
    H3 --> CX3["config.ApplySet + m.agent.* setters"]
    H4 --> CX4["m.focus.SetFocus + focus.Save"]
    H5 --> CX5["m.agent.Reset + new session.New"]

    %% AGENT EVENT PATH
    D1["startAgentTurn()<br/>model.go:1009"]
    D2["ctx, cancel := context.WithCancel()<br/>m.cancel = cancel"]
    D3["m.streaming = true<br/>model.go:1028"]
    D4["go m.agent.SendMessage(ctx, input, ch)<br/>model.go:999"]
    D5["waitForAgentEvent(ch)<br/>returns tea.Cmd"]
    D6["Cmd reads ch → returns agentEventMsg<br/>model.go:960-966"]
    D7["Update case: agentEventMsg<br/>model.go:668"]
    D8["handleAgentEvent(ev)<br/>model.go:831"]
    D9{"ev.Type switch"}
    
    D10["text: m.streamBuffer.Write<br/>m.updateStreamingMessage<br/>model.go:833"]
    D11["thinking: m.addMessage MsgThink<br/>model.go:856"]
    D12["tool_start: m.addMessage MsgTool<br/>toolPanel.StartTool<br/>model.go:872"]
    D13["tool_result: add MsgTool/MsgError<br/>toolPanel.FinishTool<br/>model.go:890"]
    D14["state: m.agentState = ev.State<br/>model.go:916"]
    
    D9 --> D10
    D9 --> D11
    D9 --> D12
    D9 --> D13
    D9 --> D14
    
    D10 -.-&gt; DV["m.refreshViewport()<br/>viewport content updated"]
    D11 -.-&gt; DV
    D12 -.-&gt; DV
    D13 -.-&gt; DV
    D14 -.-&gt; DV

    %% AGENT DONE PATH
    E1["agentDoneMsg received<br/>model.go:679"]
    E2["m.streaming = false<br/>m.agentState = StateIdle"]
    E3["m.syncLastAssistantMessage()"]
    E4["NotificationSound?<br/>sound.Play"]
    E5["toolPanel update<br/>tokens, elapsed"]
    E6["session.Append entries<br/>model.go:716-722"]
    E7["m.focus.Save<br/>model.go:728"]
    E8["session.SaveCheckpoint<br/>model.go:732"]
    E9{"Queued messages?"}
    E10["m.sendToAgent(combined)<br/>process queue"]
    E11["followUp := MaybeQueueFollowUp()<br/>model.go:772"]
    E12["sendFollowUpToAgent(followUp)"]

    %% EXTERNAL DEPENDENCIES BOX
    EXT["External Dependencies:<br/>• charmbracelet/bubbletea (tea)<br/>• github.com/jstamagal/bitchtea/internal/agent<br/>• github.com/jstamagal/bitchtea/internal/config<br/>• github.com/jstamagal/bitchtea/internal/session<br/>• github.com/jstamagal/bitchtea/internal/catalog<br/>• github.com/jstamagal/bitchtea/internal/sound<br/>• Bubble Tea viewport, spinner, textarea primitives"]

    %% HAPPY PATH EDGES
    EP1 --> |"Headless mode"| A1
    EP2 --> A1
    A1 --> A2
    A2 --> A3
    A3 --> A4
    A4 --> A5
    A5 --> A6
    A6 --> A7
    A7 --> A8
    
    A8 --> B1
    B1 --> B2
    B2 -->|no| B4
    B2 -->|yes| B3
    B3 --> B1
    B4 -->|Enter| B5
    B5 --> B6
    B6 --> B7
    B7 -->|no| B9
    B7 -->|yes| B8
    B8 --> C1
    B9 --> D1
    
    C1 --> C2
    C2 --> C3
    C3 --> C4
    C4 -->|yes| C6
    C4 -->|no| C5
    C5 --> CX5
    C6 --> H1
    C6 --> H2
    C6 --> H3
    C6 --> H4
    C6 --> H5
    
    D1 --> D2
    D2 --> D3
    D3 --> D4
    D4 --> D5
    D5 --> D6
    D6 --> D7
    D7 --> D8
    D8 --> D9
    
    D8 --> E1
    E1 --> E2
    E2 --> E3
    E3 --> E4
    E4 --> E5
    E5 --> E6
    E6 --> E7
    E7 --> E8
    E8 --> E9
    E9 -->|yes| E10
    E9 -->|no| E11
    E11 -->|followup| E12
    E11 -->|none| ENDEXIT
    
    ENDEXIT["Agent cycle complete<br/>viewport displays final state"]

    %% SIDE EFFECTS ANNOTATIONS
    DB["<b>DB Write:</b><br/>session.Append<br/>focus.Save<br/>SaveCheckpoint"]
    IO["<b>File I/O:</b><br/>transcript log<br/>session persistence"]
    NET["<b>HTTP:</b><br/>agent.SendMessage<br/>LLM API call"]
    PROC["<b>Process spawn:</b><br/>go agent goroutine<br/>go signal forwarder"]

    D4 -.-&gt; NET
    E6 -.-&gt; DB
    E6 -.-&gt; IO
    E7 -.-&gt; DB
    E8 -.-&gt; IO
    A4 -.-&gt; PROC

    %% LEGEND
    subgraph legend["Legend"]
        L1["── Happy path"]
        L2["-.- Side effect"]
        L3["--> Decision"]
    end
```

## Summary

**Startup** (`main.go:118` → `NewModel` → `tea.Program` → `Init`): `NewModel` wires agent, focus manager, session, transcript logger. `Init` returns splash screen command batch.

**Input routing**: `tea.KeyMsg` → `Update` → textarea Enter → `msgInput` → `/` prefix test → `handleCommand` or `sendToAgent`.

**Slash command dispatch**: `handleCommand` at `model.go:1518` splits input, looks up `parts[0]` in registry (~27 commands), dispatches to handler.

**Agent event loop**: `startAgentTurn` → goroutine `agent.SendMessage` → `waitForAgentEvent` → `handleAgentEvent`. Text streaming writes in-place to last `MsgAgent`. Tool events add `MsgTool` entries. On done: stop streaming, persist session, drain queue, send follow-up.

**Side effects:** session append, focus save, checkpoint save, transcript logging, LLM API calls.

**External dependencies:** bubbletea, internal/agent, internal/config, internal/session, internal/catalog, internal/sound
