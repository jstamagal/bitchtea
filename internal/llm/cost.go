package llm

import (
	"fmt"
	"strings"
	"sync"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/catwalk/pkg/embedded"
)

// CostTracker accumulates token usage across a session and converts it to
// dollars using catwalk's embedded model pricing snapshot.
type CostTracker struct {
	mu                  sync.Mutex
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
}

func NewCostTracker() *CostTracker { return &CostTracker{} }

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
func (c *CostTracker) EstimateCost(model string) float64 {
	m := lookupModel(model)
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

// lookupModel returns the first catwalk Model whose ID matches modelID across
// all embedded providers. Match is exact first, then prefix-of-known and
// prefix-of-query as a coarse fallback (catwalk IDs and provider IDs do not
// always agree on suffixes).
func lookupModel(modelID string) *catwalk.Model {
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
