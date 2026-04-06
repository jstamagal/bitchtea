package llm

import "fmt"

// Pricing per 1M tokens (input, output) as of 2024
// Source: OpenAI/Anthropic pricing pages
var modelPricing = map[string]ModelPricing{
	// OpenAI GPT-4o series
	"gpt-4o":           {InputCostPerM: 5.00, OutputCostPerM: 15.00},
	"gpt-4o-mini":       {InputCostPerM: 0.15, OutputCostPerM: 0.60},
	"chatgpt-4o-latest": {InputCostPerM: 5.00, OutputCostPerM: 15.00},
	"gpt-4-turbo":       {InputCostPerM: 10.00, OutputCostPerM: 30.00},
	"gpt-4":             {InputCostPerM: 30.00, OutputCostPerM: 60.00},

	// OpenAI GPT-3.5
	"gpt-3.5-turbo": {InputCostPerM: 0.50, OutputCostPerM: 1.50},

	// Anthropic Claude 3.5
	"claude-sonnet-4-20250514": {InputCostPerM: 3.00, OutputCostPerM: 15.00},
	"claude-3-5-sonnet-latest": {InputCostPerM: 3.00, OutputCostPerM: 15.00},
	"claude-3-5-haiku-latest":  {InputCostPerM: 0.80, OutputCostPerM: 4.00},

	// Anthropic Claude 3
	"claude-3-opus-latest":   {InputCostPerM: 15.00, OutputCostPerM: 75.00},
	"claude-3-sonnet-latest": {InputCostPerM: 3.00, OutputCostPerM: 15.00},
	"claude-3-haiku-latest":  {InputCostPerM: 0.25, OutputCostPerM: 1.25},

	// Anthropic Claude 3.7
	"claude-3-7-sonnet-latest": {InputCostPerM: 3.00, OutputCostPerM: 15.00},
}

// ModelPricing holds cost information for a model
type ModelPricing struct {
	InputCostPerM   float64 // cost per 1M input tokens
	OutputCostPerM  float64 // cost per 1M output tokens
}

// GetPricing returns pricing for a model, or nil if unknown
func GetPricing(model string) *ModelPricing {
	if p, ok := modelPricing[model]; ok {
		return &p
	}
	// Try prefix matching (e.g., "claude-sonnet-4-20250514" matches "claude-sonnet-4")
	for name, p := range modelPricing {
		if len(model) >= len(name) && model[:len(name)] == name {
			return &p
		}
		if len(name) >= len(model) && name[:len(model)] == model {
			return &p
		}
	}
	return nil
}

// CalculateCost computes cost for given token counts
func CalculateCost(pricing *ModelPricing, inputTokens, outputTokens int) float64 {
	if pricing == nil {
		return 0
	}
	inputCost := (float64(inputTokens) / 1_000_000) * pricing.InputCostPerM
	outputCost := (float64(outputTokens) / 1_000_000) * pricing.OutputCostPerM
	return inputCost + outputCost
}

// FormatCost formats a cost in dollars with appropriate precision
func FormatCost(cost float64) string {
	if cost < 0.001 {
		return "<$0.001"
	}
	if cost < 0.01 {
		return fmt.Sprintf("$%.4f", cost)
	}
	if cost < 1.00 {
		return fmt.Sprintf("$%.3f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}

// CostTracker tracks token usage and calculates costs
type CostTracker struct {
	InputTokens   int // total input tokens used
	OutputTokens  int // total output tokens used
}

// NewCostTracker creates a new cost tracker
func NewCostTracker() *CostTracker {
	return &CostTracker{}
}

// AddUsage adds token usage from a response
func (c *CostTracker) AddUsage(inputTokens, outputTokens int) {
	c.InputTokens += inputTokens
	c.OutputTokens += outputTokens
}

// TotalTokens returns total tokens used
func (c *CostTracker) TotalTokens() int {
	return c.InputTokens + c.OutputTokens
}

// EstimateCost calculates estimated cost in USD for a model
func (c *CostTracker) EstimateCost(model string) float64 {
	pricing := GetPricing(model)
	if pricing == nil {
		return 0
	}
	inputCost := (float64(c.InputTokens) / 1_000_000) * pricing.InputCostPerM
	outputCost := (float64(c.OutputTokens) / 1_000_000) * pricing.OutputCostPerM
	return inputCost + outputCost
}
