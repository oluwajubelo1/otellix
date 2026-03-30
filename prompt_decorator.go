package otellix

import (
	"context"
	"github.com/oluwajubelo1/otellix/providers"
)

// BudgetStatus represents the current state of a user's budget.
type BudgetStatus struct {
	// Remaining is the USD amount left in the budget.
	Remaining float64
	// Usage is the USD amount spent so far in the current interval.
	Usage float64
	// IsExceeded is true if the budget limit has been reached.
	IsExceeded bool
	// Mode is the active fallback action (Block, Notify, Downgrade).
	Mode FallbackAction
}

// PromptDecorator is a function that can modify LLM call parameters
// based on the current budget status before the provider is called.
type PromptDecorator func(ctx context.Context, status BudgetStatus, params *providers.CallParams)
