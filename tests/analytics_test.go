package tests

import (
	"context"
	"testing"
	"time"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
)

// TestGetBreakdownByUser verifies cost breakdown grouped by user.
func TestGetBreakdownByUser(t *testing.T) {
	otellix.ResetCallRecords()
	t.Cleanup(func() { otellix.ResetCallRecords() })

	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 1000, OutputTokens: 500},
	}

	// Call for user A
	_, _ = otellix.Trace(context.Background(), mock, providers.CallParams{Model: "claude-sonnet-4-6"},
		otellix.WithProvider("anthropic"),
		otellix.WithUserID("user-a"),
	)

	// Call for user B
	mock2 := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 2000, OutputTokens: 1000},
	}
	_, _ = otellix.Trace(context.Background(), mock2, providers.CallParams{Model: "gpt-4o"},
		otellix.WithProvider("openai"),
		otellix.WithUserID("user-b"),
	)

	entries := otellix.GetBreakdown(otellix.ByUser)
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 breakdown entries, got %d", len(entries))
	}

	// Verify user-a has expected cost
	for _, e := range entries {
		if e.Key == "user-a" {
			expectedCost := otellix.CalculateCost("anthropic", "claude-sonnet-4-6",
				providers.CallResult{InputTokens: 1000, OutputTokens: 500})
			if diff := e.TotalCostUSD - expectedCost; diff > 1e-12 || diff < -1e-12 {
				t.Errorf("user-a cost = %f, want %f", e.TotalCostUSD, expectedCost)
			}
			if e.CallCount != 1 {
				t.Errorf("user-a call count = %d, want 1", e.CallCount)
			}
		}
	}
}

// TestGetBreakdownByModel verifies cost breakdown grouped by model.
func TestGetBreakdownByModel(t *testing.T) {
	otellix.ResetCallRecords()
	t.Cleanup(func() { otellix.ResetCallRecords() })

	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 1000, OutputTokens: 500},
	}

	_, _ = otellix.Trace(context.Background(), mock, providers.CallParams{Model: "claude-sonnet-4-6"},
		otellix.WithProvider("anthropic"),
	)

	mock2 := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 2000, OutputTokens: 1000},
	}
	_, _ = otellix.Trace(context.Background(), mock2, providers.CallParams{Model: "gpt-4o"},
		otellix.WithProvider("openai"),
	)

	entries := otellix.GetBreakdown(otellix.ByModel)
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 breakdown entries, got %d", len(entries))
	}

	foundSonnet := false
	foundGPT := false
	for _, e := range entries {
		if e.Key == "claude-sonnet-4-6" {
			foundSonnet = true
		}
		if e.Key == "gpt-4o" {
			foundGPT = true
		}
	}
	if !foundSonnet {
		t.Error("expected to find claude-sonnet-4-6 in breakdown")
	}
	if !foundGPT {
		t.Error("expected to find gpt-4o in breakdown")
	}
}

// TestGetBreakdownByFeature verifies cost breakdown grouped by feature.
func TestGetBreakdownByFeature(t *testing.T) {
	otellix.ResetCallRecords()
	t.Cleanup(func() { otellix.ResetCallRecords() })

	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 100, OutputTokens: 50},
	}

	_, _ = otellix.Trace(context.Background(), mock, providers.CallParams{Model: "claude-sonnet-4-6"},
		otellix.WithProvider("anthropic"),
		otellix.WithFeatureID("search"),
	)

	_, _ = otellix.Trace(context.Background(), mock, providers.CallParams{Model: "claude-sonnet-4-6"},
		otellix.WithProvider("anthropic"),
		otellix.WithFeatureID("chat"),
	)

	entries := otellix.GetBreakdown(otellix.ByFeature)
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 breakdown entries, got %d", len(entries))
	}

	foundSearch := false
	foundChat := false
	for _, e := range entries {
		if e.Key == "search" {
			foundSearch = true
		}
		if e.Key == "chat" {
			foundChat = true
		}
	}
	if !foundSearch {
		t.Error("expected to find search feature in breakdown")
	}
	if !foundChat {
		t.Error("expected to find chat feature in breakdown")
	}
}

// TestGetBreakdownSortedByCost verifies entries are sorted by cost descending.
func TestGetBreakdownSortedByCost(t *testing.T) {
	otellix.ResetCallRecords()
	t.Cleanup(func() { otellix.ResetCallRecords() })

	// Cheap model
	mock1 := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 10, OutputTokens: 5},
	}
	_, _ = otellix.Trace(context.Background(), mock1, providers.CallParams{Model: "gpt-4o-mini"},
		otellix.WithProvider("openai"),
		otellix.WithUserID("user-cheap"),
	)

	// Expensive model
	mock2 := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 1000, OutputTokens: 500},
	}
	_, _ = otellix.Trace(context.Background(), mock2, providers.CallParams{Model: "claude-opus-4-6"},
		otellix.WithProvider("anthropic"),
		otellix.WithUserID("user-expensive"),
	)

	entries := otellix.GetBreakdown(otellix.ByUser)
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(entries))
	}

	// First entry should be more expensive
	if entries[0].TotalCostUSD < entries[1].TotalCostUSD {
		t.Errorf("entries not sorted by cost desc: %f > %f", entries[0].TotalCostUSD, entries[1].TotalCostUSD)
	}
}

// TestGetForecastCurrentMonth verifies current month spend is non-zero.
func TestGetForecastCurrentMonth(t *testing.T) {
	otellix.ResetCallRecords()
	t.Cleanup(func() { otellix.ResetCallRecords() })

	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 1000, OutputTokens: 500},
	}

	_, _ = otellix.Trace(context.Background(), mock, providers.CallParams{Model: "claude-sonnet-4-6"},
		otellix.WithProvider("anthropic"),
	)

	report := otellix.GetForecast("")
	if report.CurrentMonthSpend <= 0 {
		t.Errorf("current month spend should be > 0, got %f", report.CurrentMonthSpend)
	}
	if report.DailyBurnRate <= 0 {
		t.Errorf("daily burn rate should be > 0, got %f", report.DailyBurnRate)
	}
}

// TestGetForecastEmpty verifies empty records return zero forecast.
func TestGetForecastEmpty(t *testing.T) {
	otellix.ResetCallRecords()
	t.Cleanup(func() { otellix.ResetCallRecords() })

	report := otellix.GetForecast("")
	if report.CurrentMonthSpend != 0 {
		t.Errorf("empty records should have zero spend, got %f", report.CurrentMonthSpend)
	}
	if report.ProjectedMonthSpend != 0 {
		t.Errorf("empty records should have zero projected spend, got %f", report.ProjectedMonthSpend)
	}
}

// TestGetForecastByUser verifies forecast filtered by user.
func TestGetForecastByUser(t *testing.T) {
	otellix.ResetCallRecords()
	t.Cleanup(func() { otellix.ResetCallRecords() })

	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 1000, OutputTokens: 500},
	}

	_, _ = otellix.Trace(context.Background(), mock, providers.CallParams{Model: "claude-sonnet-4-6"},
		otellix.WithProvider("anthropic"),
		otellix.WithUserID("user-a"),
	)

	_, _ = otellix.Trace(context.Background(), mock, providers.CallParams{Model: "claude-sonnet-4-6"},
		otellix.WithProvider("anthropic"),
		otellix.WithUserID("user-b"),
	)

	reportA := otellix.GetForecast("user:user-a")
	reportGlobal := otellix.GetForecast("")

	// Both individual reports should be approximately equal (same call)
	// Global should be roughly 2x individual
	if reportGlobal.CurrentMonthSpend <= reportA.CurrentMonthSpend {
		t.Errorf("global forecast should be > individual forecast")
	}
}

// TestGetForecastProjectionConsistency verifies projection math.
func TestGetForecastProjectionConsistency(t *testing.T) {
	otellix.ResetCallRecords()
	t.Cleanup(func() { otellix.ResetCallRecords() })

	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 1000, OutputTokens: 500},
	}

	// Make a call to populate records
	_, _ = otellix.Trace(context.Background(), mock, providers.CallParams{Model: "claude-sonnet-4-6"},
		otellix.WithProvider("anthropic"),
	)

	report := otellix.GetForecast("")

	// ProjectedMonthSpend should equal DailyBurnRate * the days in the
	// current month, with small tolerance for floating point.
	now := time.Now()
	daysInMonth := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	expectedProjected := report.DailyBurnRate * float64(daysInMonth)
	diff := report.ProjectedMonthSpend - expectedProjected
	if diff > 1e-6 || diff < -1e-6 {
		t.Errorf("projection inconsistent: %f != %f (burn %f * %d days)",
			report.ProjectedMonthSpend, expectedProjected, report.DailyBurnRate, daysInMonth)
	}
}

// TestGetOptimizationSuggestsCheeperModel verifies expensive models are flagged.
func TestGetOptimizationSuggestsCheeperModel(t *testing.T) {
	otellix.ResetCallRecords()
	t.Cleanup(func() { otellix.ResetCallRecords() })

	// Call expensive model (claude-opus-4-6)
	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 1000, OutputTokens: 500},
	}

	_, _ = otellix.Trace(context.Background(), mock, providers.CallParams{Model: "claude-opus-4-6"},
		otellix.WithProvider("anthropic"),
	)

	report := otellix.GetOptimizationSuggestions()

	if len(report.Suggestions) == 0 {
		t.Fatal("expected at least one optimization suggestion for expensive model")
	}

	// First suggestion should recommend a cheaper model
	suggestion := report.Suggestions[0]
	if suggestion.CurrentModel != "claude-opus-4-6" {
		t.Errorf("expected suggestion for claude-opus-4-6, got %s", suggestion.CurrentModel)
	}
	if suggestion.EstimatedSavingsPct <= 10 {
		t.Errorf("expected >10%% savings, got %f%%", suggestion.EstimatedSavingsPct)
	}
}

// TestGetOptimizationMinimumCallCount verifies suggestions work with few calls.
func TestGetOptimizationMinimumCallCount(t *testing.T) {
	otellix.ResetCallRecords()
	t.Cleanup(func() { otellix.ResetCallRecords() })

	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 100, OutputTokens: 50},
	}

	// Single call to expensive model
	_, _ = otellix.Trace(context.Background(), mock, providers.CallParams{Model: "claude-opus-4-6"},
		otellix.WithProvider("anthropic"),
	)

	report := otellix.GetOptimizationSuggestions()

	// Should still suggest alternatives even for one call
	if len(report.Suggestions) == 0 {
		t.Fatal("expected suggestion even for single call")
	}
}

// TestGetOptimizationEmptyRecords verifies empty report on no calls.
func TestGetOptimizationEmptyRecords(t *testing.T) {
	otellix.ResetCallRecords()
	t.Cleanup(func() { otellix.ResetCallRecords() })

	report := otellix.GetOptimizationSuggestions()

	if report.TotalSpend != 0 {
		t.Errorf("empty records should have zero total spend, got %f", report.TotalSpend)
	}
	if len(report.Suggestions) != 0 {
		t.Errorf("empty records should have no suggestions, got %d", len(report.Suggestions))
	}
}

// TestStreamsAreRecordedForAnalytics checks that streamed spend appears in
// GetBreakdown and GetForecast, as it does in budgets.
func TestStreamsAreRecordedForAnalytics(t *testing.T) {
	otellix.ResetCallRecords()
	t.Cleanup(func() { otellix.ResetCallRecords() })

	mock := &providers.MockProvider{ProviderName: "openai", Result: providers.CallResult{InputTokens: 100, OutputTokens: 50}}
	stream, err := otellix.TraceStream(context.Background(), mock, providers.CallParams{Model: "gpt-4o"},
		otellix.WithFeatureID("streamed-chat"))
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := stream.Recv(); err != nil {
			break
		}
	}

	breakdown := otellix.GetBreakdown(otellix.ByFeature)
	if len(breakdown) != 1 || breakdown[0].Key != "streamed-chat" || breakdown[0].CallCount != 1 {
		t.Fatalf("expected the stream in the breakdown, got %+v", breakdown)
	}
	if spend := otellix.GetForecast("").CurrentMonthSpend; spend <= 0 || spend != breakdown[0].TotalCostUSD {
		t.Errorf("expected the stream's $%.6f in the forecast, got $%.6f", breakdown[0].TotalCostUSD, spend)
	}
}
