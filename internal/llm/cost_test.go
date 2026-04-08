package llm

import (
	"math"
	"testing"
)

func TestCostTrackerAddTokenUsage(t *testing.T) {
	tracker := NewCostTracker()
	tracker.AddTokenUsage(TokenUsage{InputTokens: 123, OutputTokens: 45})
	tracker.AddTokenUsage(TokenUsage{InputTokens: 7, OutputTokens: 9})

	if tracker.InputTokens != 130 {
		t.Fatalf("expected 130 input tokens, got %d", tracker.InputTokens)
	}
	if tracker.OutputTokens != 54 {
		t.Fatalf("expected 54 output tokens, got %d", tracker.OutputTokens)
	}
	if tracker.TotalTokens() != 184 {
		t.Fatalf("expected 184 total tokens, got %d", tracker.TotalTokens())
	}
}

func TestCalculateCost(t *testing.T) {
	pricing := &ModelPricing{InputCostPerM: 5, OutputCostPerM: 15}
	cost := CalculateCost(pricing, 1000, 2000)

	want := 0.005 + 0.03
	if math.Abs(cost-want) > 1e-9 {
		t.Fatalf("expected %.3f, got %.3f", want, cost)
	}
}
