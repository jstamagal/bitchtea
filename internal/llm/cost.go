package llm

import (
	"fmt"
	"strings"
	"sync"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/catwalk/pkg/embedded"

	"github.com/jstamagal/bitchtea/internal/catalog"
)

// CostTracker accumulates token usage across a session and converts it to
// dollars using a PriceSource. The default source is the catwalk-embedded
// snapshot, which keeps the offline path identical to pre-Phase-5 behavior.
// main.go can swap in a CatalogPriceSource backed by the autoupdate cache
// (bt-p5-autoupdate); on any miss it falls through to the embedded source so
// known model prices never disappear because the cache is empty.
type CostTracker struct {
	mu                  sync.Mutex
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int

	// priceSource is the per-tracker override. nil → defaultPriceSource().
	priceSource PriceSource
}

func NewCostTracker() *CostTracker { return &CostTracker{} }

// SetPriceSource overrides the price source for this tracker. Pass nil to
// fall back to the package default. Safe to call at any time.
func (c *CostTracker) SetPriceSource(src PriceSource) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.priceSource = src
}

// AddUsage adds raw input/output token deltas. Cache fields are left untouched.
func (c *CostTracker) AddUsage(input, output int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.InputTokens += input
	c.OutputTokens += output
}

// AddTokenUsage accumulates a full TokenUsage report (preferred over AddUsage
// when the provider reports cache token counts).
func (c *CostTracker) AddTokenUsage(u TokenUsage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.InputTokens += u.InputTokens
	c.OutputTokens += u.OutputTokens
	c.CacheCreationTokens += u.CacheCreationTokens
	c.CacheReadTokens += u.CacheReadTokens
}

// TotalTokens returns the sum of input + output. Cache tokens are not added in
// — they are a subset of the input bucket already counted by providers.
func (c *CostTracker) TotalTokens() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.InputTokens + c.OutputTokens
}

// EstimateCost returns the dollar cost for the accumulated tokens using the
// pricing for model. Unknown models cost zero (no error).
//
// This is the legacy entry point: callers that don't know the upstream
// service get a service-agnostic lookup. Prefer EstimateCostFor when the
// service is available — it lets a CatalogPriceSource disambiguate models
// that appear in multiple providers.
func (c *CostTracker) EstimateCost(model string) float64 {
	return c.EstimateCostFor(model, "")
}

// EstimateCostFor is EstimateCost with the upstream service identity (the
// "join on Service ↔ InferenceProvider" rule from the Phase 5 audit). If
// service is empty, the source is free to do a service-agnostic match.
func (c *CostTracker) EstimateCostFor(model, service string) float64 {
	src := c.source()
	m := src.Lookup(model, service)
	if m == nil {
		return 0
	}
	c.mu.Lock()
	in, out := c.InputTokens, c.OutputTokens
	cacheCreate, cacheRead := c.CacheCreationTokens, c.CacheReadTokens
	c.mu.Unlock()

	regularInput := in - cacheCreate - cacheRead
	if regularInput < 0 {
		regularInput = in
	}
	cost := per1M(regularInput, m.CostPer1MIn) +
		per1M(out, m.CostPer1MOut) +
		per1M(cacheCreate, fallback(m.CostPer1MInCached, m.CostPer1MIn)) +
		per1M(cacheRead, fallback(m.CostPer1MInCached, m.CostPer1MIn))
	return cost
}

// source resolves the active PriceSource: per-tracker override > package
// default > embedded floor. Never returns nil.
func (c *CostTracker) source() PriceSource {
	c.mu.Lock()
	src := c.priceSource
	c.mu.Unlock()
	if src != nil {
		return src
	}
	return defaultPriceSource()
}

// FormatCost is a helper for UI lines: "<$0.001", "$0.0042", "$0.018", "$1.23".
func FormatCost(cost float64) string {
	switch {
	case cost < 0.001:
		return "<$0.001"
	case cost < 0.01:
		return fmt.Sprintf("$%.4f", cost)
	case cost < 1.00:
		return fmt.Sprintf("$%.3f", cost)
	default:
		return fmt.Sprintf("$%.2f", cost)
	}
}

// PriceSource is an abstraction over "where do model prices come from".
// Implementations must be safe for concurrent use and must never return an
// error — a miss is just a nil model (treated as unknown / zero cost).
type PriceSource interface {
	// Lookup returns pricing for modelID. service is the upstream service
	// identity (catwalk InferenceProvider, e.g. "anthropic", "openrouter")
	// and is used as a join key when the source has more than one provider
	// shipping the same model id. service may be empty.
	Lookup(modelID, service string) *catwalk.Model
}

// PriceSourceFunc adapts a function to the PriceSource interface.
type PriceSourceFunc func(modelID, service string) *catwalk.Model

// Lookup implements PriceSource.
func (f PriceSourceFunc) Lookup(modelID, service string) *catwalk.Model {
	return f(modelID, service)
}

// EmbeddedPriceSource returns the catwalk-bundled pricing snapshot. This is
// the default and matches pre-Phase-5 behavior exactly: lookups go through
// embedded.GetAll() with the same exact / prefix / reverse-prefix matching.
func EmbeddedPriceSource() PriceSource {
	return PriceSourceFunc(func(modelID, _ string) *catwalk.Model {
		return lookupEmbedded(modelID)
	})
}

// CatalogPriceSource returns a PriceSource backed by the catalog Envelope
// produced by catalog.Load. It joins on Service ↔ InferenceProvider per the
// Phase 5 audit: when service is set, only that provider's models are
// considered; otherwise it scans every provider in catalog order.
//
// On any miss (empty envelope, service mismatch, model not present, or
// malformed entry with both prices zero), it transparently falls back to
// the embedded source so a refreshed-but-incomplete catalog never strips
// pricing from a known model.
func CatalogPriceSource(env catalog.Envelope) PriceSource {
	return PriceSourceFunc(func(modelID, service string) *catwalk.Model {
		if m := lookupEnvelope(env, modelID, service); m != nil {
			return m
		}
		return lookupEmbedded(modelID)
	})
}

// lookupEnvelope walks an Envelope's providers respecting the service join
// key. Returns nil if no priced match is found — the caller decides what to
// do on a miss (CatalogPriceSource falls back to embedded).
func lookupEnvelope(env catalog.Envelope, modelID, service string) *catwalk.Model {
	if modelID == "" || len(env.Providers) == 0 {
		return nil
	}
	svc := strings.TrimSpace(strings.ToLower(service))
	for i := range env.Providers {
		p := &env.Providers[i]
		if svc != "" && !strings.EqualFold(strings.TrimSpace(string(p.ID)), svc) {
			continue
		}
		if m := matchModel(p.Models, modelID); m != nil {
			return m
		}
	}
	return nil
}

// matchModel applies the same exact-then-prefix matching that lookupEmbedded
// uses against a single provider's model slice.
func matchModel(models []catwalk.Model, modelID string) *catwalk.Model {
	for i := range models {
		if models[i].ID == modelID {
			return &models[i]
		}
	}
	for i := range models {
		id := models[i].ID
		if id == "" {
			continue
		}
		if strings.HasPrefix(modelID, id) || strings.HasPrefix(id, modelID) {
			return &models[i]
		}
	}
	return nil
}

// defaultPriceSource is the package-level fallback used when a CostTracker
// has not been wired to a specific source. main.go can swap this at startup
// via SetDefaultPriceSource so every tracker constructed afterwards picks
// it up.
var (
	defaultSourceMu sync.RWMutex
	defaultSource   PriceSource = EmbeddedPriceSource()
)

// SetDefaultPriceSource installs src as the package-wide default for any
// CostTracker that has not had SetPriceSource called on it. Pass nil to
// reset to the embedded source. Safe to call before or after trackers are
// constructed; lookups always re-read this value.
func SetDefaultPriceSource(src PriceSource) {
	defaultSourceMu.Lock()
	defer defaultSourceMu.Unlock()
	if src == nil {
		defaultSource = EmbeddedPriceSource()
		return
	}
	defaultSource = src
}

func defaultPriceSource() PriceSource {
	defaultSourceMu.RLock()
	defer defaultSourceMu.RUnlock()
	return defaultSource
}

// lookupEmbedded returns the first catwalk Model whose ID matches modelID
// across all embedded providers. Match is exact first, then prefix-of-known
// and prefix-of-query as a coarse fallback (catwalk IDs and provider IDs do
// not always agree on suffixes).
func lookupEmbedded(modelID string) *catwalk.Model {
	cache := loadPricing()
	if m, ok := cache[modelID]; ok {
		return m
	}
	for id, m := range cache {
		if strings.HasPrefix(modelID, id) || strings.HasPrefix(id, modelID) {
			return m
		}
	}
	return nil
}

// lookupModel preserves the pre-Phase-5 helper name for tests that still
// poke the embedded path directly.
func lookupModel(modelID string) *catwalk.Model { return lookupEmbedded(modelID) }

var (
	pricingOnce sync.Once
	pricingMap  map[string]*catwalk.Model
)

func loadPricing() map[string]*catwalk.Model {
	pricingOnce.Do(func() {
		pricingMap = make(map[string]*catwalk.Model)
		for _, p := range embedded.GetAll() {
			for i := range p.Models {
				m := &p.Models[i]
				pricingMap[m.ID] = m
			}
		}
	})
	return pricingMap
}

func per1M(tokens int, costPer1M float64) float64 {
	return float64(tokens) / 1_000_000 * costPer1M
}

func fallback(primary, secondary float64) float64 {
	if primary > 0 {
		return primary
	}
	return secondary
}
