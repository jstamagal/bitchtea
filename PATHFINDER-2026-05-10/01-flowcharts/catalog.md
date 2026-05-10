# Phase 1 Flowchart: Model Catalog and Pricing

```mermaid
flowchart TD
    %% Entry points
    A1["main.go:60<br/>llm.SetDefaultPriceSource()"]
    A2["internal/catalog/cache.go<br/>Load (exported)"]

    %% Envelope type annotation
    A2s{{"catalog.Envelope<br/>{SchemaVersion, FetchedAt, LastChecked,<br/>ETag, Source, Providers[]}"}}

    %% main.go startup path
    A1 --> A2
    A2 --> A3["cache.go:43<br/>ReadCache(CachePath(baseDir))"]
    A3 --> A4{"ReadCache error?"}
    A4 -- missing/corrupt --> A5["load.go:53<br/>embeddedEnvelope()"]
    A4 -- OK --> A6["return Envelope"]
    A5 --> A6
    A6 --> A7["load.go:45<br/>return env"]
    A7 --> A8["CatalogPriceSource(env)<br/>cost.go:95-112"]

    %% Background refresh path
    B1["main.go:49<br/>maybeStartCatalogRefresh()"]
    B1 --> B2{"BITCHTEA_CATWALK_AUTOUPDATE=1<br/>and BITCHTEA_CATWALK_URL set?"}
    B2 -- no --> B3["exit (no-op)"]
    B2 -- yes --> B4["goroutine: catalog.Refresh()<br/>5s timeout context"]
    B4 --> B5["refresh.go:65<br/>ReadCache(opts.CachePath)"]
    B5 --> B6{"Cache file OK?"}
    B6 -- corrupt/missing --> B7["zero Envelope"]
    B6 -- OK --> B8{"Enabled && SourceURL != \"\"<br/>&& ctx not cancelled?"}
    B8 -- no --> B9["return RefreshResult{FromCache: true}<br/>refresh.go:90-95"]
    B8 -- yes --> B10{"LastChecked within SoftTTL?"}
    B10 -- fresh --> B9
    B10 -- stale --> B11["client.GetProviders(ctx, cached.ETag)<br/>refresh.go:113"]

    %% Network outcomes
    B11 --> B12{"error?"}
    B12 -- ErrNotModified --> B13["bump LastChecked<br/>write envelope<br/>return NotModified"]
    B12 -- other error --> B14["return stale envelope<br/>with Err set"]
    B12 -- success --> B15["build new Envelope<br/>compute ETag<br/>WriteCache atomic"]
    B15 --> B16["return RefreshResult{Updated: true}"]

    %% Cache I/O
    C1["cache.go:66 WriteCache()"]
    C1 --> C2["MkdirAll catalog/ 0o700"]
    C2 --> C3["CreateTemp providers-*.json.tmp"]
    C3 --> C4["write + Sync + Close"]
    C4 --> C5["os.Rename to providers.json"]
    C5 --> C6["return"]

    %% Cache read
    C7["cache.go:48 ReadCache()"]
    C7 --> C8["os.ReadFile(path)"]
    C8 --> C9{"json.Unmarshal success<br/>SchemaVersion match?"}
    C9 -- no --> C10["return error"]
    C9 -- yes --> C11["return Envelope"]

    %% PriceSource usage
    D1["cost.go:50<br/>EstimateCostFor(model, service)"]
    D1 --> D2["c.source()"]
    D2 --> D3{"c.priceSource != nil?"}
    D3 -- yes --> D4["return per-tracker source"]
    D3 -- no --> D5["return defaultPriceSource()"]
    D5 --> D6["CatalogPriceSource lookup<br/>cost.go:100-108"]
    D6 --> D7{"lookupEnvelope finds match?"}
    D7 -- yes --> D8["return Model with prices"]
    D7 -- no --> D9["fallback: lookupEmbedded()"]
    D9 --> D10["return Model or nil"]

    %% File layout
    F1["~/.bitchtea/catalog/"]
    F1 --> F2["providers.json<br/>(Envelope)"]
    F2 --> F3["catalog/refresh.go:48<br/>atomic write: *.tmp → rename"]
    F1 --> F4["parent dir: 0o700"]

    %% Side effects noted
    %% Note across refresh path
    note1["[Side effect] Refresh is fire-and-forget;<br/>startup never blocks on network"]
    note2["[Side effect] WriteCache is atomic (tmp+rename);<br/>never returns error to Refresh caller on success"]
    note3["[Side effect] CostTracker estimation never errors —<br/>nil model = zero cost"]
    note4["[Side effect] CatalogPriceSource transparently falls<br/>back to embedded on any per-model miss"]

    style A1 fill:#e1f5fe
    style A2 fill:#e8f5e8
    style B4 fill:#fff3e0
    style C1 fill:#f3e5f5
    style D1 fill:#e1f5fe
    style F1 fill:#fce4ec
```
