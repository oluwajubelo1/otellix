package otellix

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/oluwajubelo1/otellix/providers"
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

// defaultCacheStoreMu guards the lazy creation of a config's default store,
// so concurrent first calls on the same config share one store.
var defaultCacheStoreMu sync.Mutex

// store returns the configured store, creating the in-memory default on first use.
func (c *CacheConfig) store() CacheStore {
	defaultCacheStoreMu.Lock()
	defer defaultCacheStoreMu.Unlock()
	if c.Store == nil {
		c.Store = NewInMemoryCacheStore()
	}
	return c.Store
}

// inflight collapses identical requests that are in flight at the same time.
var inflight singleflight.Group

// collapse runs call once for all concurrent callers with the same key and
// gives each of them its result. A caller whose own ctx ends stops waiting
// without affecting the others. If the shared call failed only because the
// caller that started it was cancelled, a caller whose own ctx is still live
// makes its own call instead of inheriting that cancellation.
func collapse(ctx context.Context, key string, call func() (providers.CallResult, error)) (providers.CallResult, error) {
	ch := inflight.DoChan(key, func() (any, error) { return call() })

	select {
	case <-ctx.Done():
		return providers.CallResult{}, ctx.Err()
	case res := <-ch:
		result, _ := res.Val.(providers.CallResult)
		if res.Err != nil && ctx.Err() == nil &&
			(errors.Is(res.Err, context.Canceled) || errors.Is(res.Err, context.DeadlineExceeded)) {
			return call()
		}
		return result, res.Err
	}
}

// cacheEntry holds a cached result with its expiry time.
type cacheEntry struct {
	result    providers.CallResult
	expiresAt time.Time
}

// InMemoryCacheStore is a thread-safe in-memory cache with TTL support.
// Trace also collapses identical requests that are in flight at the same
// time into one provider call, whichever CacheStore is used.
type InMemoryCacheStore struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
}

// NewInMemoryCacheStore creates a new in-memory cache store.
func NewInMemoryCacheStore() *InMemoryCacheStore {
	return &InMemoryCacheStore{
		entries: make(map[string]cacheEntry),
	}
}

// Get retrieves a cached result if it exists and is not expired.
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
