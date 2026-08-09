package otellix

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"github.com/oluwajubelo1/otellix/providers"
)

// RetryConfig controls automatic retry behaviour for provider calls.
// Retries apply to non-streaming calls and to the opening of streaming calls
// (a stream that starts successfully is never retried mid-stream).
type RetryConfig struct {
	// MaxAttempts is the total number of attempts, including the first. Default 3.
	MaxAttempts int

	// BaseDelay is the initial backoff delay. Doubles per attempt. Default 250ms.
	BaseDelay time.Duration

	// MaxDelay caps the exponential backoff delay. Default 8s.
	MaxDelay time.Duration

	// RetryOnRateLimit retries on *providers.RateLimitError and honours the
	// Retry-After header when present. Default true.
	RetryOnRateLimit bool

	// RetryOn5xx retries on generic transient provider errors. Default false.
	RetryOn5xx bool

	// RetryOnTimeout retries on *providers.TimeoutError. Default false.
	RetryOnTimeout bool

	// Jitter is the +/- fraction of randomized delay added to avoid thundering
	// herds. Default 0.2 (i.e. ±20%).
	Jitter float64
}

func (rc *RetryConfig) withDefaults() *RetryConfig {
	out := *rc
	if out.MaxAttempts <= 0 {
		out.MaxAttempts = 3
	}
	if out.BaseDelay <= 0 {
		out.BaseDelay = 250 * time.Millisecond
	}
	if out.MaxDelay <= 0 {
		out.MaxDelay = 8 * time.Second
	}
	if out.Jitter <= 0 {
		out.Jitter = 0.2
	}
	return &out
}

// retryProvider wraps a provider and retries retryable errors.
type retryProvider struct {
	inner providers.Provider
	cfg   *RetryConfig
}

// RetryProvider wraps any Provider with automatic retry handling.
func RetryProvider(inner providers.Provider, cfg *RetryConfig) providers.Provider {
	return &retryProvider{inner: inner, cfg: cfg.withDefaults()}
}

func (r *retryProvider) Name() string { return r.inner.Name() }

func (r *retryProvider) Call(ctx context.Context, params providers.CallParams) (providers.CallResult, error) {
	attempt := 0
	for {
		attempt++
		result, err := r.inner.Call(ctx, params)
		if err == nil {
			return result, nil
		}

		delay, retry := r.backoff(err, attempt)
		if !retry || attempt >= r.cfg.MaxAttempts {
			return result, err
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return result, ctx.Err()
		}
	}
}

func (r *retryProvider) Stream(ctx context.Context, params providers.CallParams) (providers.Stream, error) {
	attempt := 0
	for {
		attempt++
		stream, err := r.inner.Stream(ctx, params)
		if err == nil {
			return stream, nil
		}

		delay, retry := r.backoff(err, attempt)
		if !retry || attempt >= r.cfg.MaxAttempts {
			return nil, err
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// backoff computes the delay before retrying err on the given attempt number
// (1-based), and whether the error is retryable.
func (r *retryProvider) backoff(err error, attempt int) (time.Duration, bool) {
	var retryable bool
	var retryAfter time.Duration

	var rle *providers.RateLimitError
	if errors.As(err, &rle) {
		if r.cfg.RetryOnRateLimit {
			retryable = true
			if d, perr := time.ParseDuration(rle.RetryAfter); perr == nil && d > 0 {
				retryAfter = d
			}
		}
	}
	var toe *providers.TimeoutError
	if errors.As(err, &toe) && r.cfg.RetryOnTimeout {
		retryable = true
	}
	if r.cfg.RetryOn5xx {
		// Generic transient provider errors (5xx, overloaded, connection reset).
		var pe *providers.ProviderError
		if errors.As(err, &pe) {
			retryable = true
		}
	}
	if !retryable {
		return 0, false
	}

	// Exponential backoff: base * 2^(attempt-1), capped, plus jitter.
	delay := r.cfg.BaseDelay * time.Duration(1<<(attempt-1))
	if delay > r.cfg.MaxDelay {
		delay = r.cfg.MaxDelay
	}
	if retryAfter > delay {
		delay = retryAfter
	}
	if r.cfg.Jitter > 0 {
		frac := (rand.Float64()*2 - 1) * r.cfg.Jitter
		delay = delay + time.Duration(float64(delay)*frac)
		if delay <= 0 {
			delay = r.cfg.BaseDelay
		}
	}
	return delay, true
}
