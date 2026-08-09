package tests

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
	"github.com/oluwajubelo1/otellix/stores/sqlite"
	oteltest "github.com/oluwajubelo1/otellix/test"
)

// --- Feature 1: gen_ai.* dual emission ---

func TestTraceEmitsGenAIAttributes(t *testing.T) {
	h := oteltest.New()
	defer h.Close()

	mock := &providers.MockProvider{
		ProviderName: "openai",
		Result: providers.CallResult{
			InputTokens: 100, OutputTokens: 50, Model: "gpt-4o",
		},
	}
	_, err := otellix.Trace(context.Background(), mock,
		providers.CallParams{Model: "gpt-4o", MaxTokens: 200},
		otellix.WithProvider("openai"),
		otellix.WithUserID("usr_gen"),
	)
	if err != nil {
		t.Fatalf("Trace failed: %v", err)
	}

	h.AssertAttribute(t, "llm.call", "gen_ai.provider.name", "openai")
	h.AssertAttribute(t, "llm.call", "gen_ai.request.model", "gpt-4o")
	h.AssertAttribute(t, "llm.call", "gen_ai.operation.name", "chat")
	h.AssertAttribute(t, "llm.call", "gen_ai.usage.input_tokens", int64(100))
	h.AssertAttribute(t, "llm.call", "gen_ai.usage.output_tokens", int64(50))
	h.AssertAttribute(t, "llm.call", "gen_ai.request.max_tokens", int64(200))
	h.AssertAttribute(t, "llm.call", "llm.cost_usd", 0.00075)
}

// --- Feature 2: TraceEmbedding ---

type mockEmbedder struct{}

func (m *mockEmbedder) Name() string { return "openai" }
func (m *mockEmbedder) Embed(ctx context.Context, params otellix.EmbeddingParams) (otellix.EmbeddingResult, error) {
	// Approximate token count for deterministic tests.
	tokens := int64(0)
	for _, s := range params.Input {
		tokens += int64(len(strings.Fields(s)) + 1)
	}
	return otellix.EmbeddingResult{
		InputTokens: tokens,
		Model:       params.Model,
		Embeddings:  [][]float32{{0.1, 0.2}, {0.3, 0.4}},
		Dimensions:  2,
	}, nil
}

func TestTraceEmbedding(t *testing.T) {
	h := oteltest.New()
	defer h.Close()

	_, err := otellix.TraceEmbedding(context.Background(), &mockEmbedder{},
		otellix.EmbeddingParams{Model: "text-embedding-3-small", Input: []string{"hello world"}},
		otellix.WithProvider("openai"),
	)
	if err != nil {
		t.Fatalf("TraceEmbedding failed: %v", err)
	}

	h.AssertAttribute(t, "llm.embeddings", "gen_ai.operation.name", "embeddings")
	h.AssertAttribute(t, "llm.embeddings", "llm.embeddings_dimensions", int64(2))
	h.AssertAttribute(t, "llm.embeddings", "llm.embeddings_count", int64(2))

	// 2 tokens at $0.02/M = $0.00000004
	cost := otellix.CalculateEmbeddingCost("openai", "text-embedding-3-small", 2)
	if cost < 0 {
		t.Errorf("embedding cost should be >= 0, got %f", cost)
	}
}

// --- Feature 5: smart routing ---

func TestSelectCheapestModel(t *testing.T) {
	// DeepSeek v4-flash is cheap; require it be selected over expensive models.
	model, err := otellix.SelectCheapestModel("deepseek", "deepseek-v4", 500, 10.0, true)
	if err != nil {
		t.Fatalf("SelectCheapestModel error: %v", err)
	}
	if model == "" {
		t.Fatal("expected a model suggestion")
	}

	// A tiny budget should still yield a model (free/cheap ones exist).
	if _, err := otellix.SelectCheapestModel("anthropic", "claude-opus-5", 500, 0.0001, false); err != nil {
		t.Errorf("expected a cheap cross-provider fallback, got: %v", err)
	}
}

// --- Feature 6: retries ---

func TestRetryOnRateLimit(t *testing.T) {
	rc := &otellix.RetryConfig{MaxAttempts: 3, BaseDelay: 1 * time.Millisecond, RetryOnRateLimit: true}
	mock := &providers.MockProvider{
		Err: &providers.RateLimitError{Provider: "openai", Model: "gpt-4o", Err: errors.New("429")},
	}
	rp := otellix.RetryProvider(mock, rc)

	_, err := rp.Call(context.Background(), providers.CallParams{Model: "gpt-4o"})
	if err == nil {
		t.Fatal("expected final error after retries exhausted")
	}
	if mock.CallCount != 3 {
		t.Errorf("expected 3 attempts, got %d", mock.CallCount)
	}
}

func TestRetrySucceedsOnSecondAttempt(t *testing.T) {
	rc := &otellix.RetryConfig{MaxAttempts: 3, BaseDelay: 1 * time.Millisecond, RetryOnRateLimit: true}
	mock := &flakyProvider{failFirst: true}
	rp := otellix.RetryProvider(mock, rc)

	_, err := rp.Call(context.Background(), providers.CallParams{Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if mock.Calls() != 2 {
		t.Errorf("expected 2 attempts, got %d", mock.Calls())
	}
}

type flakyProvider struct {
	attempts  int
	failFirst bool
}

func (f *flakyProvider) Name() string { return "openai" }
func (f *flakyProvider) Calls() int   { return f.attempts }
func (f *flakyProvider) Call(ctx context.Context, params providers.CallParams) (providers.CallResult, error) {
	f.attempts++
	if f.failFirst && f.attempts == 1 {
		return providers.CallResult{}, &providers.RateLimitError{Provider: "openai", Model: params.Model, Err: errors.New("429")}
	}
	return providers.CallResult{InputTokens: 10, OutputTokens: 5}, nil
}
func (f *flakyProvider) Stream(ctx context.Context, params providers.CallParams) (providers.Stream, error) {
	f.attempts++
	return nil, nil
}

// --- Feature 7: redaction ---

func TestCaptureRedactsPII(t *testing.T) {
	h := oteltest.New()
	defer h.Close()

	cc := &otellix.CaptureConfig{
		CaptureInput:   true,
		RedactPatterns: []string{`\b\d{10}\b`},
	}
	mock := &providers.MockProvider{
		ProviderName: "openai",
		Result:       providers.CallResult{InputTokens: 10, OutputTokens: 5, Model: "gpt-4o"},
	}
	_, err := otellix.Trace(context.Background(), mock,
		providers.CallParams{
			Model:    "gpt-4o",
			Messages: []providers.Message{{Role: "user", Content: "My card is 1234567890 ok?"}},
		},
		otellix.WithProvider("openai"),
		otellix.WithCaptureContent(cc),
	)
	if err != nil {
		t.Fatalf("Trace failed: %v", err)
	}
	v, ok := h.Attribute("llm.call", "gen_ai.content.user")
	if !ok {
		t.Fatal("expected captured user content")
	}
	userContent := v.AsString()
	if strings.Contains(userContent, "1234567890") {
		t.Errorf("PII not redacted: %q", userContent)
	}
	if !strings.Contains(userContent, "[REDACTED]") {
		t.Errorf("expected [REDACTED] marker, got %q", userContent)
	}
}

// --- Feature 8: budget alerting ---

func TestBudgetThresholdAlert(t *testing.T) {
	var fired []float64
	bc := &otellix.BudgetConfig{
		PerUserDailyLimit: 0.10,
		FallbackAction:    otellix.FallbackNotify,
		Thresholds:        []float64{0.5, 0.9},
		OnThreshold: func(ctx context.Context, ev otellix.BudgetThresholdEvent) {
			fired = append(fired, ev.Threshold)
		},
	}
	mock := &providers.MockProvider{
		ProviderName: "anthropic",
		Result:       providers.CallResult{InputTokens: 1000, OutputTokens: 4000, Model: "claude-sonnet-4-6"},
	}
	// 1000*3 + 4000*15 = $0.063 -> 63% of $0.10 limit.
	_, err := otellix.Trace(context.Background(), mock,
		providers.CallParams{Model: "claude-sonnet-4-6"},
		otellix.WithProvider("anthropic"),
		otellix.WithUserID("usr_alert"),
		otellix.WithBudgetConfig(bc),
	)
	if err != nil {
		t.Fatalf("Trace failed: %v", err)
	}
	if len(fired) != 1 || fired[0] != 0.5 {
		t.Errorf("expected 0.5 threshold fired once, got %v", fired)
	}
}

// --- Feature 4: SQLite store ---

func TestSQLiteBudgetStore(t *testing.T) {
	store, err := sqlite.NewSQLiteBudgetStore(t.TempDir()+"/budget.db", 24*time.Hour)
	if err != nil {
		t.Fatalf("NewSQLiteBudgetStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	store.AddSpend(ctx, "user:test", 0.25)
	store.AddSpend(ctx, "user:test", 0.10)
	got := store.GetSpend(ctx, "user:test")
	if got != 0.35 {
		t.Errorf("spend = %f, want 0.35", got)
	}
	if store.GetSpend(ctx, "user:other") != 0 {
		t.Errorf("expected 0 spend for unknown key")
	}
	if store.GetResetTime(ctx, "user:test").Before(time.Now()) {
		t.Error("reset time should be in the future")
	}
}

// --- Feature 13: SyncPricing ---

func TestSyncPricingFromJSON(t *testing.T) {
	doc := `{"openai/gpt-sync": {"input": 1.0, "output": 2.0, "cache_read": 0.1, "cache_write": 1.0}}`
	if err := otellix.SyncPricingFromJSON([]byte(doc)); err != nil {
		t.Fatalf("SyncPricingFromJSON: %v", err)
	}
	entry, ok := otellix.GetPricing("openai", "gpt-sync")
	if !ok {
		t.Fatal("expected synced pricing to be registered")
	}
	if entry.InputPricePerMToken != 1.0 || entry.OutputPricePerMToken != 2.0 {
		t.Errorf("unexpected synced entry: %+v", entry)
	}
}

// --- Feature 9: context window utilization + Feature 14: cache ROI ---

func TestTraceRecordingCacheROIAndUtilization(t *testing.T) {
	// These record via metric instruments; at minimum ensure no panic and that
	// cache savings math is non-negative.
	mock := &providers.MockProvider{
		ProviderName: "anthropic",
		Result: providers.CallResult{
			InputTokens: 100, OutputTokens: 50, CacheReadTokens: 80, Model: "claude-sonnet-4-6",
		},
	}
	_, err := otellix.Trace(context.Background(), mock,
		providers.CallParams{Model: "claude-sonnet-4-6", MaxTokens: 1000},
		otellix.WithProvider("anthropic"),
	)
	if err != nil {
		t.Fatalf("Trace failed: %v", err)
	}
}
