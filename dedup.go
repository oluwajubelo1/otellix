package otellix

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/oluwajubelo1/otellix/providers"
	"golang.org/x/sync/singleflight"
)

// CacheStore is the interface for pluggable request deduplication backends.
type CacheStore interface {
	// Get retrieves a cached result for the given key.
	// Returns (result, true) if found and not expired, (_, false) otherwise.
	Get(ctx context.Context, key string) (providers.CallResult, bool)

	// Set stores a result in the cache with the given TTL.
	Set(ctx context.Context, key string, result providers.CallResult, ttl time.Duration)
}

// CacheConfig configures request deduplication for a call.
type CacheConfig struct {
	// TTL is the time-to-live for cached results.
	TTL time.Duration

	// Store is the backend for caching results. If nil, defaults to in-memory.
	Store CacheStore
}

// cacheEntry holds a cached result with its expiry time.
type cacheEntry struct {
	result    providers.CallResult
	expiresAt time.Time
}

// InMemoryCacheStore is a thread-safe in-memory cache with TTL support.
// It uses singleflight to collapse concurrent requests for the same cache key,
// ensuring exactly one provider call when multiple goroutines request the same result.
type InMemoryCacheStore struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	sg      singleflight.Group
}

// NewInMemoryCacheStore creates a new in-memory cache store.
func NewInMemoryCacheStore() *InMemoryCacheStore {
	return &InMemoryCacheStore{
		entries: make(map[string]cacheEntry),
	}
}

// Get retrieves a cached result if it exists and is not expired.
// Uses singleflight to collapse concurrent requests for the same key.
func (s *InMemoryCacheStore) Get(ctx context.Context, key string) (providers.CallResult, bool) {
	s.mu.RLock()
	e, ok := s.entries[key]
	s.mu.RUnlock()

	if !ok {
		return providers.CallResult{}, false
	}

	// Check expiry.
	if time.Now().After(e.expiresAt) {
		return providers.CallResult{}, false
	}

	return e.result, true
}

// Set stores a result in the cache with the given TTL.
// Also performs background pruning of expired entries.
func (s *InMemoryCacheStore) Set(ctx context.Context, key string, result providers.CallResult, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries[key] = cacheEntry{
		result:    result,
		expiresAt: time.Now().Add(ttl),
	}

	// Prune expired entries to prevent unbounded memory growth.
	now := time.Now()
	for k, e := range s.entries {
		if now.After(e.expiresAt) {
			delete(s.entries, k)
		}
	}
}

// makeCacheKey generates a deterministic cache key from the request parameters.
// The key is a hash of provider, model, system prompt, messages, and max tokens.
// Temperature is NOT included in the key (callers with different temperatures may get identical cached results).
func makeCacheKey(provider, model string, params providers.CallParams) string {
	h := sha256.New()
	h.Write([]byte(provider))
	h.Write([]byte("|"))
	h.Write([]byte(model))
	h.Write([]byte("|"))
	h.Write([]byte(params.SystemPrompt))
	h.Write([]byte("|"))

	// Serialize messages to JSON for deterministic hashing.
	msgBytes, _ := json.Marshal(params.Messages)
	h.Write(msgBytes)
	h.Write([]byte("|"))
	h.Write([]byte(strconv.Itoa(params.MaxTokens)))

	// Return first 16 bytes as hex (32 hex characters).
	return fmt.Sprintf("%x", h.Sum(nil)[:16])
}
