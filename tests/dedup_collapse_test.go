package tests

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
)

// slowProvider blocks every call until release is closed, so tests can hold
// requests in flight together. It counts calls that reach the "API".
type slowProvider struct {
	calls   atomic.Int32
	started chan struct{} // receives once per call that starts
	release chan struct{}
}

func newSlowProvider() *slowProvider {
	return &slowProvider{started: make(chan struct{}, 100), release: make(chan struct{})}
}

func (p *slowProvider) Name() string { return "openai" }

func (p *slowProvider) Call(ctx context.Context, params providers.CallParams) (providers.CallResult, error) {
	p.calls.Add(1)
	p.started <- struct{}{}
	select {
	case <-p.release:
		return providers.CallResult{InputTokens: 100, OutputTokens: 50, Model: params.Model}, nil
	case <-ctx.Done():
		return providers.CallResult{}, ctx.Err()
	}
}

func (p *slowProvider) Stream(ctx context.Context, params providers.CallParams) (providers.Stream, error) {
	return nil, errors.New("not implemented")
}

func dedupParams(content string) providers.CallParams {
	return providers.CallParams{
		Model:    "gpt-4o-mini",
		Messages: []providers.Message{{Role: "user", Content: content}},
	}
}

// waitForCalls waits until n provider calls have started.
func waitForCalls(t *testing.T, p *slowProvider, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-p.started:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d provider calls started", i, n)
		}
	}
}

func TestDedupCollapsesConcurrentIdenticalRequests(t *testing.T) {
	p := newSlowProvider()
	cfg := &otellix.CacheConfig{TTL: time.Minute, Store: otellix.NewInMemoryCacheStore()}

	const callers = 10
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	results := make(chan providers.CallResult, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := otellix.Trace(context.Background(), p, dedupParams("collapse me"), otellix.WithRequestCache(cfg))
			errs <- err
			results <- res
		}()
	}

	waitForCalls(t, p, 1)
	time.Sleep(50 * time.Millisecond) // let the other callers join the in-flight request
	close(p.release)
	wg.Wait()
	close(errs)
	close(results)

	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	for res := range results {
		if res.OutputTokens != 50 {
			t.Errorf("expected every caller to get the shared result, got %+v", res)
		}
	}
	if got := p.calls.Load(); got != 1 {
		t.Errorf("expected 1 provider call for %d identical concurrent requests, got %d", callers, got)
	}
}

func TestDedupDoesNotCollapseDifferentRequests(t *testing.T) {
	p := newSlowProvider()
	cfg := &otellix.CacheConfig{TTL: time.Minute, Store: otellix.NewInMemoryCacheStore()}

	var wg sync.WaitGroup
	for _, content := range []string{"first question", "second question"} {
		wg.Add(1)
		go func(content string) {
			defer wg.Done()
			_, _ = otellix.Trace(context.Background(), p, dedupParams(content), otellix.WithRequestCache(cfg))
		}(content)
	}

	waitForCalls(t, p, 2)
	close(p.release)
	wg.Wait()

	if got := p.calls.Load(); got != 2 {
		t.Errorf("expected 2 provider calls for different requests, got %d", got)
	}
}

func TestDedupWaiterCanCancelWithoutStoppingLeader(t *testing.T) {
	p := newSlowProvider()
	cfg := &otellix.CacheConfig{TTL: time.Minute, Store: otellix.NewInMemoryCacheStore()}
	params := dedupParams("leader keeps going")

	leaderDone := make(chan error, 1)
	go func() {
		_, err := otellix.Trace(context.Background(), p, params, otellix.WithRequestCache(cfg))
		leaderDone <- err
	}()
	waitForCalls(t, p, 1)

	waiterCtx, cancel := context.WithCancel(context.Background())
	waiterDone := make(chan error, 1)
	go func() {
		_, err := otellix.Trace(waiterCtx, p, params, otellix.WithRequestCache(cfg))
		waiterDone <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-waiterDone:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected the cancelled waiter to get context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled waiter kept waiting for the leader")
	}

	close(p.release)
	if err := <-leaderDone; err != nil {
		t.Errorf("leader should be unaffected by a waiter cancelling, got %v", err)
	}
}

func TestDedupWaiterRetriesWhenLeaderIsCancelled(t *testing.T) {
	p := newSlowProvider()
	cfg := &otellix.CacheConfig{TTL: time.Minute, Store: otellix.NewInMemoryCacheStore()}
	params := dedupParams("leader gives up")

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderDone := make(chan error, 1)
	go func() {
		_, err := otellix.Trace(leaderCtx, p, params, otellix.WithRequestCache(cfg))
		leaderDone <- err
	}()
	waitForCalls(t, p, 1)

	waiterDone := make(chan error, 1)
	go func() {
		_, err := otellix.Trace(context.Background(), p, params, otellix.WithRequestCache(cfg))
		waiterDone <- err
	}()
	time.Sleep(50 * time.Millisecond)

	cancelLeader()
	if err := <-leaderDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected leader to be cancelled, got %v", err)
	}

	// The waiter's own request is still live, so it makes its own call.
	waitForCalls(t, p, 1)
	close(p.release)
	if err := <-waiterDone; err != nil {
		t.Errorf("waiter should not inherit the leader's cancellation, got %v", err)
	}
	if got := p.calls.Load(); got != 2 {
		t.Errorf("expected the waiter to make its own call (2 total), got %d", got)
	}
}

func TestDedupNilStoreUsesInMemoryDefault(t *testing.T) {
	mock := &providers.MockProvider{Result: providers.CallResult{InputTokens: 10, OutputTokens: 5}}
	cfg := &otellix.CacheConfig{TTL: time.Minute} // no Store

	for i := 0; i < 3; i++ {
		if _, err := otellix.Trace(context.Background(), mock, dedupParams("default store"),
			otellix.WithProvider("openai"), otellix.WithRequestCache(cfg)); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if mock.CallCount != 1 {
		t.Errorf("expected the default store to cache after the first call, got %d provider calls", mock.CallCount)
	}
}
