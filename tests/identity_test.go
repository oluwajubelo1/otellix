package tests

import (
	"context"
	"errors"
	"testing"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
)

// TestBudgetsRequireVerifiedIdentity checks that a budget never applies a
// limit to an ID a caller could have chosen, and refuses the call instead.
func TestBudgetsRequireVerifiedIdentity(t *testing.T) {
	unverifiedUser := otellix.ContextWithUnverifiedUser(context.Background(), "forged_user")
	unverifiedProject := otellix.ContextWithUnverifiedProject(context.Background(), "forged_project")

	for _, tc := range []struct {
		name        string
		ctx         context.Context
		opts        []otellix.Option
		budget      otellix.BudgetConfig
		wantRefused string // Kind of UnverifiedIdentityError, or "" if allowed
	}{
		{"per-user limit on an unverified user", unverifiedUser, nil,
			otellix.BudgetConfig{PerUserLimit: 1}, "user"},
		{"unattributed limit, which any user ID would escape", unverifiedUser, nil,
			otellix.BudgetConfig{UnattributedLimit: 1}, "user"},
		{"only a project limit: the unverified user doesn't matter", unverifiedUser, []otellix.Option{otellix.WithProjectID("acme")},
			otellix.BudgetConfig{PerProjectLimit: 1}, ""},
		{"per-project limit on an unverified project", unverifiedProject, nil,
			otellix.BudgetConfig{PerProjectLimit: 1}, "project"},
		{"only a user limit: the unverified project doesn't matter", unverifiedProject, []otellix.Option{otellix.WithUserID("ada")},
			otellix.BudgetConfig{PerUserLimit: 1}, ""},
		{"a user set by your code takes precedence", otellix.ContextWithUser(unverifiedUser, "ada"), nil,
			otellix.BudgetConfig{PerUserLimit: 1}, ""},
		{"WithUserID takes precedence", unverifiedUser, []otellix.Option{otellix.WithUserID("ada")},
			otellix.BudgetConfig{PerUserLimit: 1}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock := &providers.MockProvider{ProviderName: "openai"}
			budget := tc.budget
			opts := append([]otellix.Option{otellix.WithBudgetConfig(&budget)}, tc.opts...)
			_, err := otellix.Trace(tc.ctx, mock, providers.CallParams{Model: "gpt-4o"}, opts...)

			var unverified *otellix.UnverifiedIdentityError
			switch {
			case tc.wantRefused == "" && err != nil:
				t.Fatalf("expected the call to be allowed, got %v", err)
			case tc.wantRefused != "" && (!errors.As(err, &unverified) || unverified.Kind != tc.wantRefused):
				t.Fatalf("expected an UnverifiedIdentityError for the %s, got %v", tc.wantRefused, err)
			case tc.wantRefused != "" && mock.CallCount != 0:
				t.Errorf("a refused call should not reach the provider")
			}
		})
	}
}

func TestTraceStreamRequiresVerifiedIdentity(t *testing.T) {
	ctx := otellix.ContextWithUnverifiedUser(context.Background(), "forged_user")
	mock := &providers.MockProvider{ProviderName: "openai"}

	_, err := otellix.TraceStream(ctx, mock, providers.CallParams{Model: "gpt-4o"},
		otellix.WithBudgetConfig(&otellix.BudgetConfig{PerUserLimit: 1}))

	var unverified *otellix.UnverifiedIdentityError
	if !errors.As(err, &unverified) || mock.CallCount != 0 {
		t.Fatalf("expected the stream to be refused before it started, got err=%v calls=%d", err, mock.CallCount)
	}
}

// TestUnverifiedIdentityIsStillAttributed checks that without a budget, an
// unverified ID still attributes the call, and the span says it's unverified.
func TestUnverifiedIdentityIsStillAttributed(t *testing.T) {
	spans := recordSpans(t)
	ctx := otellix.ContextWithUnverifiedUser(context.Background(), "jwt_user")
	mock := &providers.MockProvider{ProviderName: "openai"}

	if _, err := otellix.Trace(ctx, mock, providers.CallParams{Model: "gpt-4o"}); err != nil {
		t.Fatal(err)
	}
	if got := spanAttr(t, spans, "llm.user_id"); got != "jwt_user" {
		t.Errorf("expected the call attributed to jwt_user, got %q", got)
	}
	if got := spanAttr(t, spans, "llm.user_verified"); got != "false" {
		t.Errorf("expected llm.user_verified=false, got %q", got)
	}
}
