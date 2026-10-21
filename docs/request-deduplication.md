# Request Deduplication

LLM apps often send the same request more than once: a user double-clicks "Generate", a page re-renders, a retry fires, or many users ask the same FAQ-style question. Each of those is a full-price provider call.

Request deduplication makes identical requests cost one call:

1. **Caching:** once a request has completed, identical requests within the TTL get the stored result instantly, with no provider call.
2. **Collapsing:** if identical requests arrive *while the first is still in flight*, they wait for it and share its result instead of each calling the provider.

Both are switched on together with `otellix.WithRequestCache`.

---

## Quick start

```go
dedup := &otellix.CacheConfig{
    TTL: 10 * time.Minute, // how long a completed result is reused
}

result, err := otellix.Trace(ctx, provider, params,
    otellix.WithRequestCache(dedup),
)
```

Create the `CacheConfig` **once**, e.g. at startup, and reuse it for every call. With no `Store`, Otellix creates an in-memory store inside the config on first use, and every call that uses that config shares it. If you created a new `CacheConfig` per request, nothing would ever be reused.

---

## What counts as "identical"

Two requests are identical when all of these match:

| Included in the key | Not included |
| --- | --- |
| Provider name | `Temperature` |
| Model | User ID, project ID, feature ID |
| `SystemPrompt` | Any other `CallParams` field |
| `Messages` (role and content, in order) | Options such as `WithAttributes` |
| `MaxTokens` | |

The key is a SHA-256 hash of those fields, so prompts are never stored as cache keys in readable form.

Two consequences:

- **Temperature is ignored.** A request at temperature 0.9 can get a cached answer that was generated at 0.2. If you rely on varied outputs, such as "give me another idea", don't use deduplication for those calls, or make the request differ, e.g. by including a seed in the prompt.
- **Users share answers.** Two users who send exactly the same messages get the same answer. That's the point for FAQ-style traffic, but don't enable deduplication for calls whose answers must be personal unless the personal context is in the messages, which then makes the requests differ.

---

## Configuration

```go
type CacheConfig struct {
    TTL   time.Duration // how long a completed result is reused
    Store CacheStore    // where results are kept; nil = in-memory
}
```

| Field | Default | Notes |
| --- | --- | --- |
| `TTL` | none: set it | How long a completed result is served from the cache. Pick it by how stale an answer may be: minutes for live data, hours or days for static content. |
| `Store` | in-memory, created on first use | Any `CacheStore`. See [Custom stores](#custom-stores). |

### The in-memory store

`otellix.NewInMemoryCacheStore()` keeps results in the process:

- It's safe for concurrent use.
- Expired entries are removed whenever a new result is stored.
- Results are lost on restart and aren't shared between instances of your app.

Collapsing works the same with every store: it happens inside the process, before the store is consulted for a new call.

---

## How collapsing works

```text
t=0ms    request A  ──▶ cache miss ──▶ calls the provider ......... result ──▶ A
t=40ms   request B  ──▶ cache miss ──▶ A is in flight: wait ─────▶ result ──▶ B
t=90ms   request C  ──▶ cache miss ──▶ A is in flight: wait ─────▶ result ──▶ C
t=900ms  request D  ──▶ cache hit (A's result, within TTL) ───────▶ result ──▶ D

1 provider call, 4 answers.
```

The first request is the **leader**: it makes the provider call. Identical requests that arrive before it finishes are **waiters**: they get the leader's result.

### Cancellation and timeouts

Each request keeps control of its own context:

- **A waiter is cancelled** (e.g. its user closed the tab): it stops waiting and returns `context.Canceled` straight away. The leader and other waiters carry on.
- **The leader is cancelled:** the leader returns its cancellation error. Waiters whose own contexts are still live don't inherit it. Each makes its own provider call instead, so one impatient user can't fail everyone else's request.

### Errors

- A **failed** call is never cached. The next identical request after a failure tries the provider again.
- Waiters in flight at the time of a failure **do** receive the same error, e.g. a rate-limit error. A retry from them starts a fresh call.

### Only one process

Collapsing happens within a single process. Two instances of your app can each make the call. With a shared store, later requests on both instances are then served from the cache.

---

## Effect on budgets, traces and metrics

A request served from the cache, or by waiting on another request, makes **no provider call** and costs nothing. Accordingly:

- **Budgets:** only the call that actually reaches the provider (the first request, or the leader) is charged to its user and project. Cache hits and waiters aren't charged.
- **Attribution:** that charged call is attributed to the user and project of the request that made it. Requests served from the cache, or by waiting, aren't attributed to anyone.
- **Traces and metrics:** only the real call produces an `llm.call` span and metrics. Cache hits and waiters don't create spans or add to token, cost or latency metrics. This means your cost metrics reflect what you actually pay the provider.

> Results from the cache and from waiting are the same `CallResult` value shared between callers. Treat `RawResponse` as **read-only**: modifying it would change what other callers see.

---

## Custom stores

To share cached results across instances or keep them across restarts, implement `otellix.CacheStore`:

```go
type CacheStore interface {
    // Get returns the cached result and true, or false if missing or expired.
    Get(ctx context.Context, key string) (providers.CallResult, bool)

    // Set stores result under key for ttl.
    Set(ctx context.Context, key string, result providers.CallResult, ttl time.Duration)
}
```

A Redis-backed store:

```go
import (
    "context"
    "encoding/json"
    "time"

    "github.com/redis/go-redis/v9"

    "github.com/oluwajubelo1/otellix/providers"
)

type RedisCacheStore struct {
    rdb    *redis.Client
    prefix string
}

func (s *RedisCacheStore) Get(ctx context.Context, key string) (providers.CallResult, bool) {
    data, err := s.rdb.Get(ctx, s.prefix+key).Bytes()
    if err != nil {
        return providers.CallResult{}, false // missing, expired, or Redis unavailable
    }
    var result providers.CallResult
    if err := json.Unmarshal(data, &result); err != nil {
        return providers.CallResult{}, false
    }
    return result, true
}

func (s *RedisCacheStore) Set(ctx context.Context, key string, result providers.CallResult, ttl time.Duration) {
    data, err := json.Marshal(result)
    if err != nil {
        return
    }
    s.rdb.Set(ctx, s.prefix+key, data, ttl)
}
```

```go
dedup := &otellix.CacheConfig{
    TTL:   time.Hour,
    Store: &RedisCacheStore{rdb: rdb, prefix: "otellix:dedup:"},
}
```

Things to know when writing a store:

- **Treat any failure as a miss.** If `Get` returns false, the request simply goes to the provider. A store outage then costs money but never breaks requests.
- **`RawResponse` may not survive serialisation.** It holds the provider SDK's own response type. After a JSON round trip it comes back as generic maps, or not at all. Token counts and `Model` round-trip fine.
- **Store results, not errors.** Otellix only calls `Set` for successful calls.

---

## Streaming

Deduplication applies to `otellix.Trace` only. `otellix.TraceStream` always calls the provider: tokens are delivered as they're generated, so there's no finished result to share.

---

## Troubleshooting

**Identical requests still call the provider every time.**
- Check that the `CacheConfig` is created once and reused, not built per request.
- Check that the requests really are identical: the same model, system prompt, messages and `MaxTokens`. A timestamp or request ID inside the prompt makes every request unique.
- Check the TTL isn't too short.
- Check you're using `Trace`, not `TraceStream`.

**My app crashed with a nil pointer error in `Trace` when I enabled deduplication.**
That happened in versions before this fix when `CacheConfig.Store` was nil. Upgrade, or set `Store: otellix.NewInMemoryCacheStore()` explicitly.

**Users got an answer that was meant for someone else.**
The requests were identical, so they shared a result. Put the user-specific context in the messages, or don't enable deduplication for those calls.

**Cost metrics dropped after enabling deduplication.**
That's expected: metrics count only real provider calls, which is what you're billed for.
