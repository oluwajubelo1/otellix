package tests

import (
	"context"
	"strings"
	"testing"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
)

func TestPromptDecorator(t *testing.T) {
	// Setup budget with $0.05 limit and $0.04 already spent.
	bc := &otellix.BudgetConfig{
		PerUserDailyLimit: 0.05,
		FallbackAction:    otellix.FallbackNotify,
	}
	enforcer := otellix.NewBudgetEnforcer(bc)
	enforcer.Record(context.Background(), "user_decorator", "", 0.04)

	// Define a decorator that injects the remaining budget.
	decorator := func(ctx context.Context, status otellix.BudgetStatus, params *providers.CallParams) {
		if status.Remaining < 0.02 {
			params.SystemPrompt += " [LOW BUDGET]"
		}
	}

	p := &providers.MockProvider{
		Result: providers.CallResult{RawResponse: "Hello!"},
	}

	params := providers.CallParams{
		SystemPrompt: "Be nice.",
	}

	_, err := otellix.Trace(context.Background(), p, params,
		otellix.WithBudgetConfig(bc),
		otellix.WithPromptDecorator(decorator),
		otellix.WithUserID("user_decorator"),
	)

	if err != nil {
		t.Fatalf("Trace failed: %v", err)
	}

	// Verify that the decorator modified the params passed to provider.
	if !strings.Contains(p.LastParams.SystemPrompt, "[LOW BUDGET]") {
		t.Errorf("expected system prompt to contain [LOW BUDGET], got: %s", p.LastParams.SystemPrompt)
	}
}
