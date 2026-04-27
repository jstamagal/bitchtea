package llm

import (
	"math"
	"strings"
	"testing"
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
