package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
)

func TestTraceStreamReadsUserFromContext(t *testing.T) {
	store := otellix.NewInMemoryBudgetStore(24 * time.Hour)
	store.AddSpend(context.Background(), "user:ctx_stream_user", 1.00)

	cfg := &otellix.BudgetConfig{
		PerUserLimit:   0.50,
		FallbackAction: otellix.FallbackBlock,
		Store:          store,
	}

	ctx := otellix.ContextWithUser(context.Background(), "ctx_stream_user")
	mock := &providers.MockProvider{}
	_, err := otellix.TraceStream(ctx, mock, providers.CallParams{Model: "gpt-4o"},
		otellix.WithBudgetConfig(cfg),
	)

	var budgetErr *otellix.BudgetExceededError
	if !errors.As(err, &budgetErr) {
		t.Fatalf("expected BudgetExceededError for the context user, got %v", err)
	}
	if budgetErr.UserID != "ctx_stream_user" {
		t.Errorf("expected user ctx_stream_user, got %q", budgetErr.UserID)
	}
	if mock.CallCount != 0 {
		t.Errorf("expected provider not to be called, got %d calls", mock.CallCount)
	}
}

func TestUnattributedLimit(t *testing.T) {
	ctx := context.Background()

	t.Run("calls with no user share one budget", func(t *testing.T) {
		enforcer := otellix.NewBudgetEnforcer(&otellix.BudgetConfig{
			PerUserLimit:      10.00,
			UnattributedLimit: 1.00,
		})

		enforcer.Record(ctx, "", "", 0.60)
		enforcer.Record(ctx, "", "", 0.30)
		if allowed, _ := enforcer.Check(ctx, "", "", 0.20); allowed {
			t.Error("expected unattributed call over the shared limit to be blocked")
		}
		if allowed, _ := enforcer.Check(ctx, "known_user", "", 0.20); !allowed {
			t.Error("expected attributed user to be unaffected by the unattributed budget")
		}
	})

	t.Run("without it calls with no user skip user limits", func(t *testing.T) {
		enforcer := otellix.NewBudgetEnforcer(&otellix.BudgetConfig{PerUserLimit: 0.01})
		if allowed, _ := enforcer.Check(ctx, "", "", 5.00); !allowed {
			t.Error("expected unattributed call to be allowed when UnattributedLimit is unset")
		}
	})

	t.Run("budget error reports the shared budget", func(t *testing.T) {
		enforcer := otellix.NewBudgetEnforcer(&otellix.BudgetConfig{UnattributedLimit: 0.50})
		enforcer.Record(ctx, "", "", 0.75)

		budgetErr := enforcer.BudgetError("", "")
		if budgetErr.Limit != 0.50 || budgetErr.CurrentSpend != 0.75 {
			t.Errorf("expected spend 0.75 of limit 0.50, got %v of %v", budgetErr.CurrentSpend, budgetErr.Limit)
		}
		if budgetErr.ResetAt.IsZero() {
			t.Error("expected a reset time")
		}
	})
}

func TestBudgetErrorReportsTightestBudget(t *testing.T) {
	ctx := context.Background()
	enforcer := otellix.NewBudgetEnforcer(&otellix.BudgetConfig{
		PerUserLimit:    5.00,
		PerProjectLimit: 2.00,
	})
	enforcer.Record(ctx, "light_user", "busy_project", 1.90)

	budgetErr := enforcer.BudgetError("light_user", "busy_project")
	if budgetErr.Limit != 2.00 {
		t.Errorf("expected the project limit 2.00 (0.10 left) over the user limit 5.00 (3.10 left), got %v", budgetErr.Limit)
	}

	status := enforcer.Status(ctx, "light_user", "busy_project")
	if status.Remaining < 0.0999 || status.Remaining > 0.1001 {
		t.Errorf("expected 0.10 remaining, got %v", status.Remaining)
	}
}
