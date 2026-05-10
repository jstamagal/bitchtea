# Flowchart: Configuration and Profiles

```mermaid
flowchart TD
    subgraph "Config Build: defaults + env vars + rc file"
        A["DefaultConfig()<br/>config.go:84"] --> B["cfg: Config with sane defaults<br/>• APIKey from OPENAI_API_KEY env<br/>• BaseURL from OPENAI_BASE_URL env<br/>• Model: gpt-4o default<br/>• Provider: openai default<br/>• Service: openai default<br/>• ToolTimeout: 300<br/>• Banner: true<br/>• Effort: high"]
        
        B --> C["DetectProvider(cfg)<br/>config.go:121"]
        
        C --> D{ANTHROPIC_API_KEY<br/>set &amp;&amp; APIKey empty?}
        D -->|yes| E["cfg.APIKey = ANTHROPIC_API_KEY<br/>cfg.BaseURL = ANTHROPIC_BASE_URL<br/>cfg.Provider = 'anthropic'<br/>cfg.Service = 'anthropic'<br/>cfg.Model = 'claude-sonnet-4-20250514'<br/>config.go:122-129"]
        D -->|no| F{OPENROUTER_API_KEY<br/>set &amp;&amp; APIKey empty?}
        E --> L
        F -->|yes| G["cfg.APIKey = OPENROUTER_API_KEY<br/>cfg.Provider = builtinProfiles['openrouter'].Provider<br/>cfg.BaseURL = builtinProfiles['openrouter'].BaseURL<br/>cfg.Service = builtinProfiles['openrouter'].Service<br/>cfg.Profile = 'openrouter'<br/>cfg.Model = builtinProfiles['openrouter'].Model<br/>config.go:130-138"]
        G --> L
        F -->|no| H{ZAI_API_KEY<br/>set &amp;&amp; APIKey empty?}
        H -->|yes| I["cfg.APIKey = ZAI_API_KEY<br/>cfg.Provider = builtinProfiles['zai-openai'].Provider<br/>cfg.BaseURL = builtinProfiles['zai-openai'].BaseURL<br/>cfg.Service = builtinProfiles['zai-openai'].Service<br/>cfg.Profile = 'zai-openai'<br/>cfg.Model = builtinProfiles['zai-openai'].Model<br/>config.go:139-147"]
        I --> L
        H -->|no| J{OPENAI_API_KEY<br/>set &amp;&amp; APIKey empty?}
        J -->|yes| K["cfg.APIKey = OPENAI_API_KEY<br/>cfg.Provider = 'openai'<br/>cfg.Service = 'openai'<br/>config.go:148-151"]
        J -->|no| L["No override — keep defaults<br/>from DefaultConfig()<br/>config.go:152"]
        K --> L
        
        L --> M["ParseRC()<br/>rc.go:18"]
        
        M --> N["parseRCFile(RCPath())<br/>rc.go:24<br/>Path: ~/.bitchtea/bitchtearc"]
        N --> O["Read file line by line<br/>Skip blank lines &amp; comments (#)<br/>Return non-blank, non-comment lines"]
        O --> P["ApplyRCSetCommands(cfg, lines)<br/>rc.go:50"]
        
        P --> Q{Is line a 'set' command?<br/>case-insensitive}
        Q -->|yes| R["applySetToConfig(cfg, key, value)<br/>rc.go:193"]
        Q -->|no| S["Add to remaining[]<br/>for TUI execution"]
        R --> T{Key recognised?}
        T -->|yes| U["Update cfg field<br/>rc.go:196-305<br/>Side effects:<br/>• provider → Service='custom', Profile=''<br/>• model → Profile=''<br/>• apikey → Profile=''<br/>• baseurl → Service='custom', Profile=''<br/>• profile → ApplyProfile+ResolveProfile"]
        T -->|no| S
        U --> Q
        Q -->|no more lines| V["return remaining[]<br/>lines for TUI"]
    end

    subgraph "Profile Save/Load/Delete"
        V --> W{"Profile operation?"}
        
        W -->|SAVE| X["SaveProfile(p Profile)<br/>config.go:337"]
        X --> Y["os.MkdirAll(profiles/, 0755)<br/>config.go:339"]
        Y --> Z["json.MarshalIndent(p)<br/>config.go:343"]
        Z --> AA["os.WriteFile(profiles/{name}.json, 0600)<br/>config.go:348-351"]
        
        W -->|LOAD| AB["LoadProfile(name)<br/>config.go:356"]
        AB --> AC["os.ReadFile(profiles/{name}.json)<br/>config.go:358"]
        AC --> AD["json.Unmarshal → Profile<br/>config.go:364"]
        AD --> AE{Service field<br/>missing?}
        AE -->|yes| AF["deriveService(p)<br/>config.go:511"]
        AF --> AG["Fill Service from:<br/>1. builtinProfiles[name].Service<br/>2. host match<br/>3. 'custom'"]
        AG --> AH["return &Profile"]
        AE -->|no| AH
        
        W -->|LIST| AI["ListProfiles()<br/>config.go:394"]
        AI --> AJ["Merge builtinProfiles keys<br/>+ .json files in profiles/"]
        AJ --> AK["sort.Strings → []string"]
        
        W -->|DELETE| AL["DeleteProfile(name)<br/>config.go:418"]
        AL --> AM["os.Remove(profiles/{name}.json)<br/>config.go:420"]
        
        W -->|RESOLVE| AN["ResolveProfile(name)<br/>config.go:450"]
        AN --> AO{loadSavedProfile(name)<br/>exists?}
        AO -->|yes| AP["return saved profile"]
        AO -->|no, not ErrNotExist| AQ["return error"]
        AO -->|ErrNotExist| AR{builtinProfile(name)<br/>exists?}
        AR -->|yes| AS["Build Profile from<br/>builtinProfiles spec<br/>config.go:481-500"]
        AR -->|no| AT["return error: not exist<br/>config.go:461"]
        AS --> AU["Fill APIKey from<br/>spec.APIKeyEnv[]"]
        AU --> AP
    end

    subgraph "ApplyRCSetCommands flow"
        AN --> AV["For each 'set' line:<br/>set provider/model/apikey/baseurl/nick<br/>profile/service/sound/auto-next/auto-idea<br/>persona_file/top_k/top_p/temperature<br/>repetition_penalty/tool_verbosity<br/>banner/effort/tool_timeout"]
        AV --> AW{key == 'profile'?<br/>rc.go:227}
        AW -->|yes| AX["ResolveProfile(value)<br/>ApplyProfile(cfg, p)<br/>cfg.Profile = value"]
        AW -->|no| AY["Update cfg directly<br/>for other keys"]
    end

    subgraph "Builtin Profiles Registry"
        AZ["builtinProfiles map<br/>config.go:191<br/>Keys: cliproxyapi, ollama, openrouter,<br/>aihubmix, avian, copilot, cortecs,<br/>huggingface, ionet, nebius,<br/>synthetic, venice, vercel, xai,<br/>zai-openai, zai-anthropic"]
    end

    style AW fill:#f9f,stroke:#333
    style AX fill:#bbf,stroke:#333
```

## Summary

**Primary Happy Path — Config Build:**

1. **`DefaultConfig()`** (config.go:84) returns a Config seeded with env vars (`OPENAI_API_KEY`, `OPENAI_BASE_URL`) and hardcoded defaults (`gpt-4o`, `openai` as provider/service, `ToolTimeout=300`, `Banner=true`, `Effort=high`).

2. **`DetectProvider()`** (config.go:121) checks for provider-specific API keys in order: `ANTHROPIC_API_KEY` (highest priority) → `OPENROUTER_API_KEY` → `ZAI_API_KEY` → `OPENAI_API_KEY`. Each override sets the appropriate `Provider`, `Service`, `BaseURL`, `Model`, and `Profile` fields. Setting `ANTHROPIC_API_KEY` switches the model to `claude-sonnet-4-20250514`.

3. **`ParseRC()`** (rc.go:18) reads `~/.bitchtea/bitchtearc`, returning non-blank, non-comment lines.

4. **`ApplyRCSetCommands()`** (rc.go:50) processes each line: `set` commands update `cfg` via `applySetToConfig()`; non-set lines are returned for TUI execution. Notable side effects: setting `provider`, `model`, `apikey`, or `baseurl` clears `cfg.Profile`; `baseurl` additionally sets `Service='custom'`.

**Profile Operations:**
- **`SaveProfile`** writes JSON to `~/.bitchtea/profiles/{name}.json` with `0600` permissions.
- **`LoadProfile`** reads from disk, derives `Service` lazily via `deriveService()` if missing.
- **`ResolveProfile`** first tries a saved profile, then falls back to `builtinProfiles` registry (built-in provider presets).
- **`ListProfiles`** merges built-in keys and saved `.json` files.
- **`DeleteProfile`** calls `os.Remove()`.

**ApplyRCSetCommands Flow:**
When `set profile <name>` is encountered, it calls `ResolveProfile()` to load and `ApplyProfile()` to merge fields into `cfg`.

**External Dependencies:**
- Filesystem: `~/.bitchtea/` base dir, `~/.bitchtea/profiles/`, `~/.bitchtea/bitchtearc`
- Environment: `OPENAI_API_KEY`, `OPENAI_BASE_URL`, `ANTHROPIC_API_KEY`, `ANTHROPIC_BASE_URL`, `OPENROUTER_API_KEY`, `ZAI_API_KEY`, `CLIPROXYAPI_KEY`, and other provider-specific env vars
- OS: `os.UserHomeDir()`, `os.Getwd()`, `user.Current()` for path resolution
