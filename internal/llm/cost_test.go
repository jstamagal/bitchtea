package llm

import (
	"math"
	"strings"
	"sync"
	"testing"

	"charm.land/catwalk/pkg/catwalk"

	"github.com/jstamagal/bitchtea/internal/catalog"
)

func TestCostTrackerAddTokenUsage(t *testing.T) {
	tracker := NewCostTracker()
	tracker.AddTokenUsage(TokenUsage{InputTokens: 123, OutputTokens: 45})
	tracker.AddTokenUsage(TokenUsage{InputTokens: 7, OutputTokens: 9, CacheReadTokens: 3})

	if tracker.InputTokens != 130 {
		t.Fatalf("expected 130 input tokens, got %d", tracker.InputTokens)
	}
	if tracker.OutputTokens != 54 {
		t.Fatalf("expected 54 output tokens, got %d", tracker.OutputTokens)
	}
	if tracker.CacheReadTokens != 3 {
		t.Fatalf("expected 3 cache-read tokens, got %d", tracker.CacheReadTokens)
	}
	if tracker.TotalTokens() != 184 {
		t.Fatalf("expected 184 total tokens, got %d", tracker.TotalTokens())
	}
}

func TestCostTrackerAddUsageNoCacheBleed(t *testing.T) {
	tracker := NewCostTracker()
	tracker.AddUsage(50, 25)
	if tracker.CacheCreationTokens != 0 || tracker.CacheReadTokens != 0 {
		t.Fatalf("AddUsage should not touch cache fields, got %+v", tracker)
	}
}

func TestEstimateCostUnknownModelIsZero(t *testing.T) {
	tracker := NewCostTracker()
	tracker.AddUsage(1000, 1000)
	if got := tracker.EstimateCost("definitely-not-a-real-model"); got != 0 {
		t.Fatalf("unknown model should cost zero, got %v", got)
	}
}

func TestEstimateCostKnownCatwalkModelIsNonZero(t *testing.T) {
	tracker := NewCostTracker()
	tracker.AddUsage(1_000_000, 1_000_000)

	// Pull any model id from the embedded catwalk catalog so the test does not
	// depend on a specific provider being present.
	cache := loadPricing()
	var sample string
	for id, m := range cache {
		if m.CostPer1MIn > 0 || m.CostPer1MOut > 0 {
			sample = id
			break
		}
	}
	if sample == "" {
		t.Skip("no priced models in embedded catwalk catalog")
	}

	cost := tracker.EstimateCost(sample)
	if cost <= 0 {
		t.Fatalf("expected non-zero cost for %s with priced tokens, got %v", sample, cost)
	}
}

func TestFormatCostBuckets(t *testing.T) {
	cases := []struct {
		cost float64
		want string
	}{
		{0.0001, "<$0.001"},
		{0.0042, "$0.0042"},
		{0.018, "$0.018"},
		{1.234, "$1.23"},
	}
	for _, c := range cases {
		got := FormatCost(c.cost)
		if got != c.want {
			t.Errorf("FormatCost(%v) = %q, want %q", c.cost, got, c.want)
		}
	}
}

func TestPer1MAndFallback(t *testing.T) {
	if got := per1M(1_000_000, 5); math.Abs(got-5) > 1e-9 {
		t.Fatalf("per1M(1M, 5) = %v, want 5", got)
	}
	if got := fallback(0, 7); got != 7 {
		t.Fatalf("fallback(0, 7) = %v, want 7", got)
	}
	if got := fallback(3, 7); got != 3 {
		t.Fatalf("fallback(3, 7) = %v, want 3", got)
	}
}

func TestLookupModelPrefixFallback(t *testing.T) {
	cache := loadPricing()
	var sample string
	for id := range cache {
		if strings.Contains(id, "-") {
			sample = id
			break
		}
	}
	if sample == "" {
		t.Skip("no hyphenated model ids in embedded catwalk catalog")
	}

	prefix := sample[:strings.LastIndex(sample, "-")]
	if prefix == "" {
		t.Skip("no usable prefix in sample id")
	}
	if got := lookupModel(prefix); got == nil {
		t.Fatalf("expected prefix lookup to find %q via %q, got nil", sample, prefix)
	}
}

// --- PriceSource / catalog wiring tests ----------------------------------

// pickEmbeddedSample returns a (modelID, embeddedInputPrice) tuple for any
// priced model in the embedded snapshot. Used as the canonical "known
// embedded model" across the tests below.
func pickEmbeddedSample(t *testing.T) (string, float64) {
	t.Helper()
	for id, m := range loadPricing() {
		if m.CostPer1MIn > 0 {
			return id, m.CostPer1MIn
		}
	}
	t.Skip("no priced models in embedded catwalk catalog")
	return "", 0
}

// withDefaultPriceSource swaps the package default for the duration of a
// test and restores it on cleanup. Tests share package state so this must
// always run synchronously.
func withDefaultPriceSource(t *testing.T, src PriceSource) {
	t.Helper()
	defaultSourceMu.RLock()
	prev := defaultSource
	defaultSourceMu.RUnlock()
	SetDefaultPriceSource(src)
	t.Cleanup(func() { SetDefaultPriceSource(prev) })
}

// TestCatalogPriceSourceOfflineFallback covers the offline path: the
// catalog autoupdate cache is empty, so CatalogPriceSource MUST fall through
// to the embedded snapshot for every known model. This is the contract that
// keeps offline cost reporting unchanged from pre-Phase-5 behavior.
func TestCatalogPriceSourceOfflineFallback(t *testing.T) {
	sample, embeddedIn := pickEmbeddedSample(t)
	src := CatalogPriceSource(catalog.Envelope{}) // empty: no providers
	got := src.Lookup(sample, "")
	if got == nil {
		t.Fatalf("empty envelope should fall through to embedded for %q, got nil", sample)
	}
	if got.CostPer1MIn != embeddedIn {
		t.Fatalf("expected embedded price %v for %q, got %v", embeddedIn, sample, got.CostPer1MIn)
	}

	tracker := NewCostTracker()
	tracker.SetPriceSource(src)
	tracker.AddUsage(1_000_000, 0)
	cost := tracker.EstimateCost(sample)
	want := embeddedIn // 1M input tokens × $/1M
	if math.Abs(cost-want) > 1e-9 {
		t.Fatalf("offline cost = %v, want embedded %v", cost, want)
	}
}

// TestCatalogPriceSourceCatalogHit covers the online/refreshed path: a model
// present in the catalog with a different price MUST take precedence over
// embedded. This is what makes the autoupdate worth wiring at all.
func TestCatalogPriceSourceCatalogHit(t *testing.T) {
	const modelID = "synthetic-model-x"
	const catalogIn = 12.5
	env := catalog.Envelope{
		SchemaVersion: catalog.SchemaVersion,
		Source:        "test",
		Providers: []catwalk.Provider{
			{
				ID: catwalk.InferenceProviderOpenAI,
				Models: []catwalk.Model{
					{ID: modelID, Name: "Synthetic", CostPer1MIn: catalogIn, CostPer1MOut: 25},
				},
			},
		},
	}
	src := CatalogPriceSource(env)
	got := src.Lookup(modelID, "openai")
	if got == nil || got.CostPer1MIn != catalogIn {
		t.Fatalf("catalog price not picked up: got %+v", got)
	}

	tracker := NewCostTracker()
	tracker.SetPriceSource(src)
	tracker.AddUsage(1_000_000, 0)
	cost := tracker.EstimateCostFor(modelID, "openai")
	if math.Abs(cost-catalogIn) > 1e-9 {
		t.Fatalf("catalog-hit cost = %v, want %v", cost, catalogIn)
	}
}

// TestCatalogPriceSourceMissFallsBackToEmbedded covers the partial-cache
// path: the catalog has SOMETHING but not the requested model, and the
// embedded snapshot still does. We MUST return the embedded entry — losing
// pricing for a known model just because the cache shipped without it would
// silently zero out the cost UI.
func TestCatalogPriceSourceMissFallsBackToEmbedded(t *testing.T) {
	sample, embeddedIn := pickEmbeddedSample(t)
	env := catalog.Envelope{
		SchemaVersion: catalog.SchemaVersion,
		Providers: []catwalk.Provider{
			{
				ID: catwalk.InferenceProviderOpenAI,
				Models: []catwalk.Model{
					{ID: "some-other-model-not-in-embedded", CostPer1MIn: 99},
				},
			},
		},
	}
	src := CatalogPriceSource(env)
	got := src.Lookup(sample, "")
	if got == nil {
		t.Fatalf("expected embedded fallback for %q, got nil", sample)
	}
	if got.CostPer1MIn != embeddedIn {
		t.Fatalf("expected embedded price %v, got %v", embeddedIn, got.CostPer1MIn)
	}
}

// TestCatalogPriceSourceUnknownEverywhere covers the both-miss path:
// behavior must match the pre-Phase-5 unknown-model contract → zero cost,
// no error.
func TestCatalogPriceSourceUnknownEverywhere(t *testing.T) {
	src := CatalogPriceSource(catalog.Envelope{})
	if got := src.Lookup("totally-unknown-zzz-model", ""); got != nil {
		t.Fatalf("unknown model in both sources should be nil, got %+v", got)
	}
	tracker := NewCostTracker()
	tracker.SetPriceSource(src)
	tracker.AddUsage(1_000_000, 1_000_000)
	if cost := tracker.EstimateCost("totally-unknown-zzz-model"); cost != 0 {
		t.Fatalf("unknown model should cost zero, got %v", cost)
	}
}

// TestCatalogPriceSourceServiceJoin covers the audit's "join on Service ↔
// InferenceProvider" rule: if the same model id appears under two providers
// with different prices, the lookup must pick the one matching the service.
func TestCatalogPriceSourceServiceJoin(t *testing.T) {
	const modelID = "shared-model-id"
	env := catalog.Envelope{
		SchemaVersion: catalog.SchemaVersion,
		Providers: []catwalk.Provider{
			{
				ID: catwalk.InferenceProviderOpenAI,
				Models: []catwalk.Model{
					{ID: modelID, CostPer1MIn: 1.0, CostPer1MOut: 2.0},
				},
			},
			{
				ID: catwalk.InferenceProviderOpenRouter,
				Models: []catwalk.Model{
					{ID: modelID, CostPer1MIn: 7.0, CostPer1MOut: 14.0},
				},
			},
		},
	}
	src := CatalogPriceSource(env)

	gotOAI := src.Lookup(modelID, "openai")
	if gotOAI == nil || gotOAI.CostPer1MIn != 1.0 {
		t.Fatalf("openai join: got %+v, want CostPer1MIn=1.0", gotOAI)
	}
	gotOR := src.Lookup(modelID, "openrouter")
	if gotOR == nil || gotOR.CostPer1MIn != 7.0 {
		t.Fatalf("openrouter join: got %+v, want CostPer1MIn=7.0", gotOR)
	}

	// Case-insensitive on the join key.
	gotMixed := src.Lookup(modelID, "OpenRouter")
	if gotMixed == nil || gotMixed.CostPer1MIn != 7.0 {
		t.Fatalf("case-insensitive join failed: got %+v", gotMixed)
	}

	// Empty service: scan everything in catalog order; first wins.
	gotAny := src.Lookup(modelID, "")
	if gotAny == nil || gotAny.CostPer1MIn != 1.0 {
		t.Fatalf("empty service should pick first provider in order, got %+v", gotAny)
	}
}

// TestSetDefaultPriceSource confirms that the package-level default is what
// CostTracker uses when no per-tracker source is set, and that nil resets
// to embedded.
func TestSetDefaultPriceSource(t *testing.T) {
	sample, embeddedIn := pickEmbeddedSample(t)

	const overrideIn = 42.0
	override := PriceSourceFunc(func(modelID, _ string) *catwalk.Model {
		if modelID == sample {
			return &catwalk.Model{ID: sample, CostPer1MIn: overrideIn}
		}
		return nil
	})
	withDefaultPriceSource(t, override)

	tracker := NewCostTracker()
	tracker.AddUsage(1_000_000, 0)
	if cost := tracker.EstimateCost(sample); math.Abs(cost-overrideIn) > 1e-9 {
		t.Fatalf("default override not honored: cost=%v, want %v", cost, overrideIn)
	}

	// Reset to embedded via nil and confirm we're back to embedded prices.
	SetDefaultPriceSource(nil)
	if cost := tracker.EstimateCost(sample); math.Abs(cost-embeddedIn) > 1e-9 {
		t.Fatalf("nil reset should restore embedded; cost=%v, want %v", cost, embeddedIn)
	}
}

// TestCostTrackerSetPriceSourceConcurrent is a smoke test for the mutex —
// SetPriceSource and EstimateCost must be safe to call from multiple
// goroutines without -race tripping.
func TestCostTrackerSetPriceSourceConcurrent(t *testing.T) {
	tracker := NewCostTracker()
	tracker.AddUsage(1000, 1000)
	src := CatalogPriceSource(catalog.Envelope{})

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); tracker.SetPriceSource(src) }()
		go func() { defer wg.Done(); _ = tracker.EstimateCost("anything") }()
	}
	wg.Wait()
}
