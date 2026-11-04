package tests

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
)

// TestTraceStreamCutsOffMidStreamWhenBudgetRunsOut checks the mid-stream
// cutoff: a stream that starts within budget is stopped on the token that
// would take spend over the limit, and the tokens generated are charged.
func TestTraceStreamCutsOffMidStreamWhenBudgetRunsOut(t *testing.T) {
	ctx := context.Background()
	const model = "gpt-4o"
	costOf := func(outputTokens int64) float64 {
		return otellix.CalculateCost("openai", model, providers.CallResult{OutputTokens: outputTokens})
	}

	// Room for exactly 2 output tokens: the 3rd goes over the limit. The
	// stream starts by holding an estimate of 1 output token, so the cutoff
	// comes from the tokens actually streamed rather than the estimate.
	store := otellix.NewInMemoryBudgetStore(24 * time.Hour)
	budget := &otellix.BudgetConfig{
		PerUserLimit:          (costOf(2) + costOf(3)) / 2,
		FallbackAction:        otellix.FallbackBlock,
		Store:                 store,
		EstimatedOutputTokens: 1,
	}

	// MockProvider streams the 5 tokens "mock", " ", "stream", " ", "response".
	mock := &providers.MockProvider{ProviderName: "openai"}
	stream, err := otellix.TraceStream(ctx, mock, providers.CallParams{Model: model},
		otellix.WithUserID("streamer"),
		otellix.WithBudgetConfig(budget),
	)
	if err != nil {
		t.Fatalf("stream should start within budget: %v", err)
	}
	defer func() { _ = stream.Close() }()

	var received []string
	var budgetErr *otellix.BudgetExceededError
	for {
		evt, err := stream.Recv()
		if errors.As(err, &budgetErr) {
			break
		}
		if errors.Is(err, io.EOF) {
			t.Fatalf("stream ran to completion (%q); expected a mid-stream budget cutoff", received)
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		received = append(received, evt.Token)
	}

	if len(received) != 2 {
		t.Errorf("expected 2 tokens before the cutoff, got %d: %q", len(received), received)
	}
	if budgetErr.UserID != "streamer" || budgetErr.Limit != budget.PerUserLimit {
		t.Errorf("unexpected budget error: %+v", budgetErr)
	}

	// The cutoff closes the stream and charges the 3 tokens generated.
	// Closing again (as the deferred Close does) must not charge twice.
	_ = stream.Close()
	if spend := store.GetSpend(ctx, "user:streamer"); spend < costOf(3)*0.999 || spend > costOf(3)*1.001 {
		t.Errorf("expected spend of 3 tokens ($%.8f), got $%.8f", costOf(3), spend)
	}
}

// TestTraceStreamNotifyDoesNotCutOff checks that FallbackNotify lets a
// stream over budget run to completion.
func TestTraceStreamNotifyDoesNotCutOff(t *testing.T) {
	ctx := context.Background()
	store := otellix.NewInMemoryBudgetStore(24 * time.Hour)
	store.AddSpend(ctx, "user:notified", 1.00)

	stream, err := otellix.TraceStream(ctx, &providers.MockProvider{ProviderName: "openai"},
		providers.CallParams{Model: "gpt-4o"},
		otellix.WithUserID("notified"),
		otellix.WithBudgetConfig(&otellix.BudgetConfig{
			PerUserLimit:   0.50,
			FallbackAction: otellix.FallbackNotify,
			Store:          store,
		}),
	)
	if err != nil {
		t.Fatalf("notify mode should not block the stream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	tokens := 0
	for {
		_, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("notify mode should not cut the stream off: %v", err)
		}
		tokens++
	}
	if tokens != 5 {
		t.Errorf("expected all 5 tokens, got %d", tokens)
	}
}

// countingStore counts the spend reads and reservations made on an
// in-memory store. With Redis, each is a network round trip.
type countingStore struct {
	*otellix.InMemoryBudgetStore
	mu       sync.Mutex
	gets     int
	reserves int
}

func (s *countingStore) GetSpend(ctx context.Context, key string) float64 {
	s.mu.Lock()
	s.gets++
	s.mu.Unlock()
	return s.InMemoryBudgetStore.GetSpend(ctx, key)
}

func (s *countingStore) Reserve(ctx context.Context, amount float64, limits []otellix.BudgetLimit) (string, bool, error) {
	s.mu.Lock()
	s.reserves++
	s.mu.Unlock()
	return s.InMemoryBudgetStore.Reserve(ctx, amount, limits)
}

// calls returns the store calls made since the last reset.
func (s *countingStore) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gets + s.reserves
}

func (s *countingStore) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gets, s.reserves = 0, 0
}

// tokenStream streams events, each adding tokensPerEvent output tokens, and
// then ends with err (io.EOF if nil).
type tokenStream struct {
	events, sent   int
	tokensPerEvent int64
	err            error
}

func (s *tokenStream) Recv() (providers.StreamEvent, error) {
	if s.sent == s.events {
		if s.err != nil {
			return providers.StreamEvent{}, s.err
		}
		return providers.StreamEvent{}, io.EOF
	}
	s.sent++
	return providers.StreamEvent{Token: "x", OutputTokens: int64(s.sent) * s.tokensPerEvent}, nil
}

func (s *tokenStream) Close() error { return nil }

// tokenStreamProvider is an "openai" provider whose streams are tokenStreams.
type tokenStreamProvider struct {
	providers.MockProvider
	events         int
	tokensPerEvent int64
	err            error
}

func (p *tokenStreamProvider) Name() string { return "openai" }

func (p *tokenStreamProvider) Stream(ctx context.Context, params providers.CallParams) (providers.Stream, error) {
	return &tokenStream{events: p.events, tokensPerEvent: p.tokensPerEvent, err: p.err}, nil
}

// TestTraceStreamStoreCalls checks that a stream within its estimate makes
// no store calls while it runs, and that a long stream makes few: each time
// it runs past what it holds, it reserves as much again.
func TestTraceStreamStoreCalls(t *testing.T) {
	store := &countingStore{InMemoryBudgetStore: otellix.NewInMemoryBudgetStore(24 * time.Hour)}
	budget := &otellix.BudgetConfig{PerUserLimit: 10, FallbackAction: otellix.FallbackBlock, Store: store, EstimatedOutputTokens: 1}

	run := func(events int) int {
		t.Helper()
		p := &tokenStreamProvider{events: events, tokensPerEvent: 1}
		stream, err := otellix.TraceStream(context.Background(), p, providers.CallParams{Model: "gpt-4o"},
			otellix.WithUserID("reader"), otellix.WithBudgetConfig(budget))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = stream.Close() }()
		store.reset() // ignore the reservation made before the stream started
		for i := range events {
			if _, err := stream.Recv(); err != nil {
				t.Fatalf("event %d: %v", i+1, err)
			}
		}
		// Count before the stream ends: closing it may read spend to print
		// the budget, when a dev printer is set up.
		return store.calls()
	}

	// The stream starts holding 1 output token, and streams 1.
	if calls := run(1); calls != 0 {
		t.Errorf("a stream within its estimate should make no store calls while it runs, made %d", calls)
	}
	// 1,000 tokens from a 1-token hold: holds of 1, 2, 4, ... tokens, so
	// about 10 reservations, not 1,000.
	if calls := run(1000); calls > 12 {
		t.Errorf("a 1,000-token stream should make about 10 store calls, made %d", calls)
	}
}

// TestConcurrentStreamsCannotOvershootLimit is the stream version of the
// concurrent-calls race: streams that run past their estimate used to spend
// beyond what they held without other calls seeing it, so streams running
// at the same time could together spend several times the limit.
func TestConcurrentStreamsCannotOvershootLimit(t *testing.T) {
	ctx := context.Background()
	store := otellix.NewInMemoryBudgetStore(24 * time.Hour)
	const limit = 0.05
	budget := &otellix.BudgetConfig{PerUserLimit: limit, FallbackAction: otellix.FallbackBlock, Store: store}

	// Each stream would produce 5,000 output tokens of gpt-4o, $0.05, in
	// events of 100 tokens, $0.001.
	p := &tokenStreamProvider{events: 50, tokensPerEvent: 100}
	eventCost := otellix.CalculateCost("openai", "gpt-4o", providers.CallResult{OutputTokens: 100})

	var streams []*otellix.Stream
	for range 5 {
		s, err := otellix.TraceStream(ctx, p, providers.CallParams{Model: "gpt-4o"},
			otellix.WithUserID("streamer"), otellix.WithBudgetConfig(budget))
		if err != nil {
			t.Fatal(err)
		}
		streams = append(streams, s)
	}

	// Read the streams in turn, so they all run at once.
	live := len(streams)
	done := make([]bool, len(streams))
	for live > 0 {
		for i, s := range streams {
			if done[i] {
				continue
			}
			if _, err := s.Recv(); err != nil {
				done[i] = true
				live--
			}
		}
	}

	// Each stream can overshoot by at most the one event that crossed the limit.
	if spend := store.GetSpend(ctx, "user:streamer"); spend > limit+5*eventCost+1e-9 {
		t.Errorf("5 concurrent streams spent $%.4f against a $%.4f limit", spend, limit)
	}
}

// TestTraceStreamChargedWithoutClose checks that a stream read to the end
// is charged even if the caller never calls Close.
func TestTraceStreamChargedWithoutClose(t *testing.T) {
	store := otellix.NewInMemoryBudgetStore(24 * time.Hour)
	budget := &otellix.BudgetConfig{PerUserLimit: 10, Store: store}
	p := &tokenStreamProvider{events: 5, tokensPerEvent: 1000}

	stream, err := otellix.TraceStream(context.Background(), p, providers.CallParams{Model: "gpt-4o"},
		otellix.WithUserID("no-close"), otellix.WithBudgetConfig(budget))
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := stream.Recv(); err != nil {
			break
		}
	}
	// No Close.

	want := otellix.CalculateCost("openai", "gpt-4o", providers.CallResult{OutputTokens: 5000})
	if spend := store.GetSpend(context.Background(), "user:no-close"); spend < want*0.999 || spend > want*1.001 {
		t.Errorf("expected the stream's $%.4f to be charged at io.EOF, got $%.4f", want, spend)
	}
}

// TestTraceStreamErrorIsRecorded checks that a stream that fails part way
// is charged for what it streamed, and its span marked as an error.
func TestTraceStreamErrorIsRecorded(t *testing.T) {
	spans := recordSpans(t)
	store := otellix.NewInMemoryBudgetStore(24 * time.Hour)
	budget := &otellix.BudgetConfig{PerUserLimit: 10, Store: store}
	p := &tokenStreamProvider{events: 3, tokensPerEvent: 100, err: errors.New("connection reset")}

	stream, err := otellix.TraceStream(context.Background(), p, providers.CallParams{Model: "gpt-4o"},
		otellix.WithUserID("broken"), otellix.WithBudgetConfig(budget))
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := stream.Recv(); err != nil {
			if err.Error() != "connection reset" {
				t.Fatalf("expected the provider's error, got %v", err)
			}
			break
		}
	}

	want := otellix.CalculateCost("openai", "gpt-4o", providers.CallResult{OutputTokens: 300})
	if spend := store.GetSpend(context.Background(), "user:broken"); spend < want*0.999 || spend > want*1.001 {
		t.Errorf("expected the $%.6f streamed before the error to be charged, got $%.6f", want, spend)
	}
	if got := spanAttr(t, spans, "llm.error"); got != "true" {
		t.Errorf("expected llm.error=true on the span, got %q", got)
	}
}
