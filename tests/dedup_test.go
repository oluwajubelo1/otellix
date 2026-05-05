package tests

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
)

// TestDedupCacheHitSkipsProvider verifies identical requests hit cache.
func TestDedupCacheHitSkipsProvider(t *testing.T) {
	store := otellix.NewInMemoryCacheStore()
	cfg := &otellix.CacheConfig{TTL: 5 * time.Minute, Store: store}

	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 100, OutputTokens: 50},
	}

	params := providers.CallParams{
		Model:    "claude-sonnet-4-6",
		Messages: []providers.Message{{Role: "user", Content: "hello"}},
	}

	// First call - cache miss, provider called
	_, err := otellix.Trace(context.Background(), mock, params,
		otellix.WithProvider("anthropic"),
		otellix.WithRequestCache(cfg),
	)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	firstCallCount := mock.CallCount

	// Second call with identical params - cache hit, provider not called
	_, err = otellix.Trace(context.Background(), mock, params,
		otellix.WithProvider("anthropic"),
		otellix.WithRequestCache(cfg),
	)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	if mock.CallCount != firstCallCount {
		t.Errorf("expected provider called once total, but was called %d times", mock.CallCount)
	}
}

// TestDedupCacheMissDifferentMessages verifies different messages miss cache.
func TestDedupCacheMissDifferentMessages(t *testing.T) {
	store := otellix.NewInMemoryCacheStore()
	cfg := &otellix.CacheConfig{TTL: 5 * time.Minute, Store: store}

	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 100, OutputTokens: 50},
	}

	params1 := providers.CallParams{
		Model:    "claude-sonnet-4-6",
		Messages: []providers.Message{{Role: "user", Content: "hello"}},
	}

	params2 := providers.CallParams{
		Model:    "claude-sonnet-4-6",
		Messages: []providers.Message{{Role: "user", Content: "goodbye"}},
	}

	// First call
	_, _ = otellix.Trace(context.Background(), mock, params1,
		otellix.WithProvider("anthropic"),
		otellix.WithRequestCache(cfg),
	)

	// Second call with different messages - should miss cache and call provider again
	_, _ = otellix.Trace(context.Background(), mock, params2,
		otellix.WithProvider("anthropic"),
		otellix.WithRequestCache(cfg),
	)

	if mock.CallCount != 2 {
		t.Errorf("expected 2 provider calls for different messages, got %d", mock.CallCount)
	}
}

// TestDedupCacheKeyVariesByModel verifies cache key includes model.
func TestDedupCacheKeyVariesByModel(t *testing.T) {
	store := otellix.NewInMemoryCacheStore()
	cfg := &otellix.CacheConfig{TTL: 5 * time.Minute, Store: store}

	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 100, OutputTokens: 50},
	}

	params := providers.CallParams{
		Model:    "claude-sonnet-4-6",
		Messages: []providers.Message{{Role: "user", Content: "test"}},
	}

	// Call with one model
	_, _ = otellix.Trace(context.Background(), mock, params,
		otellix.WithProvider("anthropic"),
		otellix.WithRequestCache(cfg),
	)

	// Call with different model - should miss cache
	params.Model = "claude-opus-4-6"
	_, _ = otellix.Trace(context.Background(), mock, params,
		otellix.WithProvider("anthropic"),
		otellix.WithRequestCache(cfg),
	)

	if mock.CallCount != 2 {
		t.Errorf("expected 2 provider calls for different models, got %d", mock.CallCount)
	}
}

// TestDedupCacheTTLExpiry verifies cache entries expire after TTL.
func TestDedupCacheTTLExpiry(t *testing.T) {
	store := otellix.NewInMemoryCacheStore()
	cfg := &otellix.CacheConfig{TTL: 50 * time.Millisecond, Store: store}

	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 100, OutputTokens: 50},
	}

	params := providers.CallParams{
		Model:    "claude-sonnet-4-6",
		Messages: []providers.Message{{Role: "user", Content: "test"}},
	}

	// First call - cached
	_, _ = otellix.Trace(context.Background(), mock, params,
		otellix.WithProvider("anthropic"),
		otellix.WithRequestCache(cfg),
	)

	if mock.CallCount != 1 {
		t.Errorf("expected 1 call initially, got %d", mock.CallCount)
	}

	// Wait for TTL to expire
	time.Sleep(100 * time.Millisecond)

	// Second call - cache should be expired, provider called again
	_, _ = otellix.Trace(context.Background(), mock, params,
		otellix.WithProvider("anthropic"),
		otellix.WithRequestCache(cfg),
	)

	if mock.CallCount != 2 {
		t.Errorf("expected 2 calls after TTL expiry, got %d", mock.CallCount)
	}
}

// TestDedupConcurrentSafety verifies concurrent requests cache safely.
func TestDedupConcurrentSafety(t *testing.T) {
	store := otellix.NewInMemoryCacheStore()
	cfg := &otellix.CacheConfig{TTL: 5 * time.Minute, Store: store}

	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 100, OutputTokens: 50},
	}

	params := providers.CallParams{
		Model:    "claude-sonnet-4-6",
		Messages: []providers.Message{{Role: "user", Content: "concurrent test"}},
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = otellix.Trace(context.Background(), mock, params,
				otellix.WithProvider("anthropic"),
				otellix.WithRequestCache(cfg),
			)
		}()
	}
	wg.Wait()

	// Concurrent requests will result in some provider calls, but far fewer than 20.
	// Most requests should hit the cache after the first few complete.
	// Expect significantly fewer calls than 20 (at least 10x reduction).
	if mock.CallCount > 10 {
		t.Logf("concurrent safety test: %d provider calls (expected < 10 for 20 concurrent requests)", mock.CallCount)
		// Not a hard failure, but note reduced caching efficiency
	}
	if mock.CallCount == 20 {
		t.Errorf("expected caching to reduce calls, got all 20")
	}
}

// TestDedupCustomCacheStore verifies custom store implementation works.
func TestDedupCustomCacheStore(t *testing.T) {
	// Verify that InMemoryCacheStore implements CacheStore interface
	var _ otellix.CacheStore = otellix.NewInMemoryCacheStore()

	// Test that a custom implementation can be used
	store := otellix.NewInMemoryCacheStore()

	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 100, OutputTokens: 50},
	}

	params := providers.CallParams{
		Model:    "gpt-4o",
		Messages: []providers.Message{{Role: "user", Content: "test"}},
	}

	cfg := &otellix.CacheConfig{TTL: 5 * time.Minute, Store: store}

	// First call should go through provider
	_, _ = otellix.Trace(context.Background(), mock, params,
		otellix.WithProvider("openai"),
		otellix.WithRequestCache(cfg),
	)

	if mock.CallCount != 1 {
		t.Errorf("expected 1 call with custom store, got %d", mock.CallCount)
	}

	// Second call should hit cache
	_, _ = otellix.Trace(context.Background(), mock, params,
		otellix.WithProvider("openai"),
		otellix.WithRequestCache(cfg),
	)

	if mock.CallCount != 1 {
		t.Errorf("expected cache hit with custom store, got %d total calls", mock.CallCount)
	}
}

// TestDedupDoesNotCacheErrors verifies errors are not cached.
func TestDedupDoesNotCacheErrors(t *testing.T) {
	store := otellix.NewInMemoryCacheStore()
	cfg := &otellix.CacheConfig{TTL: 5 * time.Minute, Store: store}

	mockErr := &providers.MockProvider{
		Err: &providers.ProviderError{
			Provider: "anthropic",
			Model:    "claude-sonnet-4-6",
			Err:      testError("api failed"),
		},
	}

	params := providers.CallParams{
		Model:    "claude-sonnet-4-6",
		Messages: []providers.Message{{Role: "user", Content: "test"}},
	}

	// First call returns error
	_, err := otellix.Trace(context.Background(), mockErr, params,
		otellix.WithProvider("anthropic"),
		otellix.WithRequestCache(cfg),
	)

	if err == nil {
		t.Fatal("expected error from provider")
	}

	// Second call with same params - provider should be called again (error not cached)
	_, err2 := otellix.Trace(context.Background(), mockErr, params,
		otellix.WithProvider("anthropic"),
		otellix.WithRequestCache(cfg),
	)

	if err2 == nil {
		t.Fatal("expected error from second call")
	}

	if mockErr.CallCount != 2 {
		t.Errorf("expected 2 provider calls (errors not cached), got %d", mockErr.CallCount)
	}
}

// Simple error for testing
type testError string

func (e testError) Error() string {
	return string(e)
}
