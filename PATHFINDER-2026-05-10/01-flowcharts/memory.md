# Flowchart: Scoped Memory Management

```mermaid
flowchart TD
    %% Entry points
    Load([memory.go:57 Load])
    Save([memory.go:67 Save])

    %% Scope constructors
    RootScope([memory.go:44 RootScope])
    ChannelScope([memory.go:48 ChannelScope])
    QueryScope([memory.go:52 QueryScope])

    %% Scope helpers
    HotPath([memory.go:74 HotPath])
    LoadScoped([memory.go:82 LoadScoped])
    SaveScoped([memory.go:92 SaveScoped])
    AppendHot([memory.go:103 AppendHot])
    DailyPath([memory.go:141 DailyPath])
    DailyPathForScope([memory.go:146 DailyPathForScope])
    AppendDaily([memory.go:160 AppendDaily])
    AppendDailyForScope([memory.go:166 AppendDailyForScope])

    %% Search flow
    Search([memory.go:209 Search])
    SearchInScope([memory.go:215 SearchInScope])
    RenderSearchResults([memory.go:273 RenderSearchResults])
    candidatePaths([memory.go:309 candidatePaths])
    queryTerms([memory.go:337 queryTerms])
    containsAllTerms([memory.go:345 containsAllTerms])
    extractSnippet([memory.go:446 extractSnippet])
    nearestMarkdownHeading([memory.go:425 nearestMarkdownHeading])
    formatSource([memory.go:357 formatSource])

    %% Scope helpers
    scopeName([memory.go:290 scopeName])
    memoryBaseDir([memory.go:305 memoryBaseDir])
    lineage([memory.go:369 Scope.lineage])
    relativePath([memory.go:393 Scope.relativePath])
    sanitizeSegment([memory.go:415 sanitizeSegment])

    %% Load flow
    Load --> load_path["path = filepath.Join(workDir, 'MEMORY.md')"]
    load_path --> load_read["data, err = os.ReadFile(path)"]
    load_read --> load_err{err != nil?}
    load_err -->|yes| load_empty["return ''"]
    load_err -->|no| load_return["return string(data)"]

    %% Save flow
    Save --> save_path["path = filepath.Join(workDir, 'MEMORY.md')"]
    save_path --> save_write["os.WriteFile(path, []byte(content), 0644)"]
    save_write --> save_return["return nil"]

    %% Scope determination
    RootScope --> scope_root["return Scope{Kind: ScopeRoot}"]
    ChannelScope --> scope_channel["return Scope{Kind: ScopeChannel, Name: name, Parent: parent}"]
    QueryScope --> scope_query["return Scope{Kind: ScopeQuery, Name: name, Parent: parent}"]

    %% HotPath determination
    HotPath --> hp_kind{scope.Kind == "" or ScopeRoot?}
    hp_kind -->|yes| hp_legacy["return filepath.Join(workDir, 'MEMORY.md')"]
    hp_kind -->|no| hp_scoped["return memoryBaseDir(sessionDir, workDir)/contexts/scope.relativePath()/HOT.md"]

    memoryBaseDir --> mb_formula["filepath.Join(filepath.Dir(sessionDir), 'memory', scopeName(workDir))"]
    scopeName --> sn_clean["filepath.Clean(workDir) → lowercase → sanitize → fnv32a hash"]
    relativePath --> rp_build["for ancestor in lineage (bottom-up): dir=channels|queries, segments+=dir/sanitizedName"]
    rp_build --> rp_result["filepath.Join(segments...)"]

    %% LoadScoped
    LoadScoped --> ls_path["path = HotPath(sessionDir, workDir, scope)"]
    ls_path --> ls_read["data, err = os.ReadFile(path)"]
    ls_read --> ls_err{err != nil?}
    ls_err -->|yes| ls_empty["return ''"]
    ls_err -->|no| ls_return["return string(data)"]

    %% SaveScoped
    SaveScoped --> ss_path["path = HotPath(sessionDir, workDir, scope)"]
    ss_path --> ss_mkdir["os.MkdirAll(filepath.Dir(path), 0755)"]
    ss_mkdir --> ss_write["os.WriteFile(path, []byte(content), 0644)"]
    ss_write --> ss_return["return nil"]

    %% AppendHot
    AppendHot --> ah_trim{content = strings.TrimSpace(content) == ""?}
    ah_trim -->|yes| ah_noop["return nil"]
    ah_trim -->|no| ah_path["path = HotPath(sessionDir, workDir, scope)"]
    ah_path --> ah_mkdir["os.MkdirAll(filepath.Dir(path), 0755)"]
    ah_mkdir --> ah_open["f = os.OpenFile(path, O_CREATE|O_APPEND|O_WRONLY, 0644)"]
    ah_open --> ah_flock["syscall.Flock(f.Fd(), LOCK_EX)"]
    ah_flock --> ah_heading["heading = title ? 'title (RFC3339)' : 'RFC3339'"]
    ah_heading --> ah_entry["entry = '## heading\n\ncontent\n\n'"]
    ah_entry --> ah_write["f.WriteString(entry)"]
    ah_write --> ah_close["f.Close() + flock(LOCK_UN)"]

    %% Daily file structure
    DailyPath --> dp_root["DailyPathForScope(sessionDir, workDir, RootScope(), when)"]
    DailyPathForScope --> dpf_kind{scope.Kind == "" or ScopeRoot?}
    dpf_kind -->|yes| dpf_legacy["return memoryBaseDir/YYYY-MM-DD.md"]
    dpf_kind -->|no| dpf_scoped["return memoryBaseDir/contexts/scope.relativePath()/daily/YYYY-MM-DD.md"]

    %% AppendDailyForScope
    AppendDailyForScope --> ad_trim{content = strings.TrimSpace(content) == ""?}
    ad_trim -->|yes| ad_noop["return nil"]
    ad_trim -->|no| ad_path["path = DailyPathForScope(sessionDir, workDir, scope, when)"]
    ad_path --> ad_mkdir["os.MkdirAll(filepath.Dir(path), 0755)"]
    ad_mkdir --> ad_open["f = os.OpenFile(path, O_CREATE|O_APPEND|O_WRONLY, 0644)"]
    ad_open --> ad_flock["syscall.Flock(f.Fd(), LOCK_EX)"]
    ad_flock --> ad_label["label = string(source) (default: 'compaction')"]
    ad_label --> ad_entry["entry = '## RFC3339 label flush\n\ncontent\n\n'"]
    ad_entry --> ad_write["f.WriteString(entry)"]
    ad_write --> ad_close["f.Close() + flock(LOCK_UN)"]

    %% Search flow
    Search --> si_call["SearchInScope(sessionDir, workDir, RootScope(), query, limit)"]
    SearchInScope --> si_trim{query = strings.TrimSpace(query)}
    si_trim --> si_empty{query == ""?}
    si_empty -->|yes| si_err["return error: 'query is required'"]
    si_empty -->|no| si_limit{limit <= 0?}
    si_limit -->|yes| si_def["limit = 5"]
    si_limit -->|no| si_terms["terms = queryTerms(query)"]
    si_def --> si_terms
    si_terms --> si_cands["candidates = candidatePaths(sessionDir, workDir, scope)"]
    si_cands --> si_loop["for each path in candidates (inherited scopes + daily files)"]
    si_loop --> si_read["data, err = os.ReadFile(path)"]
    si_read --> si_notexist{os.IsNotExist(err)?}
    si_notexist -->|yes| si_continue["continue"]
    si_notexist -->|no| si_check["containsAllTerms(content, terms)?"]
    si_check -->|no| si_loop
    si_check -->|yes| si_match["matchIdx = Index(lowerContent, query)"]
    si_match --> si_result{len(results) >= limit?}
    si_result -->|no| si_append["append SearchResult{Source, Heading, Snippet}"]
    si_append --> si_loop
    si_result -->|yes| si_done["return results"]

    candidatePaths --> cp_lineage["for ancestor in scope.lineage()"]
    cp_lineage --> cp_hot["candidates += HotPath(ancestor)"]
    cp_hot --> cp_daily["dailyDir = Dir(DailyPathForScope(ancestor, now))"]
    cp_daily --> cp_read["entries = os.ReadDir(dailyDir)"]
    cp_read --> cp_filter["filter .md files, sort reverse (newest first)"]
    cp_filter --> cp_append["candidates += dailyPaths"]
    cp_append --> cp_done["return candidates"]

    RenderSearchResults --> rr_empty{len(results) == 0?}
    rr_empty -->|yes| rr_none["return 'No memory matches found for query %q'"]
    rr_empty -->|no| rr_build["for i, result in results: format '#N. Source: ...\nHeading: ...\nSnippet'"]
    rr_build --> rr_return["return string"]

    %% External dependencies
    subgraph External["External Dependencies"]
        os["os (ReadFile, WriteFile, MkdirAll, OpenFile, ReadDir)"]
        filepath["filepath (Join, Clean, Base, Dir, Rel, Ext)"]
        strings["strings (TrimSpace, Fields, ToLower, Contains, Index, HasPrefix, Split, Builder)"]
        time["time (Time, RFC3339, Format)"]
        hash_fnv["hash/fnv (New32a)"]
        regexp["regexp (MustCompile, ReplaceAllString)"]
        sort["sort (Sort, Rev, StringSlice)"]
        syscall["syscall (Flock)"]
    end
```

## Summary

**Load and Save flows (lines 57–70)** are straightforward: they read/write `MEMORY.md` directly in `workDir` with no scope overhead.

**Scope constructors:**
- `RootScope()` → `ScopeRoot` (legacy top-level `MEMORY.md`)
- `ChannelScope(name, parent)` → `ScopeChannel` (per-channel context under `.../memory/<scope>/contexts/channels/<name>/`)
- `QueryScope(name, parent)` → `ScopeQuery` (per-query context under `.../memory/<scope>/contexts/queries/<name>/`)

**Daily append file structure** stores durable memory as `YYYY-MM-DD.md` files. For root scope: `memory/<scopeName>/YYYY-MM-DD.md`. For scoped contexts: `memory/<scopeName>/contexts/<path>/daily/YYYY-MM-DD.md`. All writes use `syscall.Flock` for concurrency safety.

**`search_memory` tool** consumes `Search` → `SearchInScope` → `candidatePaths`, which traverses the scope lineage (self + ancestors to root) collecting each `HOT.md` plus all daily `.md` files sorted newest-first. Results are rendered via `RenderSearchResults`.

**External Dependencies:** `os`, `filepath`, `strings`, `time`, `hash/fnv`, `regexp`, `sort`, `syscall` (for file locking).
