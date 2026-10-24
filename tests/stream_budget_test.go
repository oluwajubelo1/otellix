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

// countingStore counts GetSpend calls on an in-memory store.
type countingStore struct {
	*otellix.InMemoryBudgetStore
	mu   sync.Mutex
	gets int
}

func (s *countingStore) GetSpend(ctx context.Context, key string) float64 {
	s.mu.Lock()
	s.gets++
	s.mu.Unlock()
	return s.InMemoryBudgetStore.GetSpend(ctx, key)
}

// TestTraceStreamDoesNotReadStoreEveryToken checks that a stream well
// within budget reads spend once, not on every token. With Redis, every
// read is a network round trip.
func TestTraceStreamDoesNotReadStoreEveryToken(t *testing.T) {
	store := &countingStore{InMemoryBudgetStore: otellix.NewInMemoryBudgetStore(24 * time.Hour)}
	budget := &otellix.BudgetConfig{PerUserLimit: 10, FallbackAction: otellix.FallbackBlock, Store: store}

	// MockProvider streams 5 tokens.
	mock := &providers.MockProvider{ProviderName: "openai"}
	stream, err := otellix.TraceStream(context.Background(), mock, providers.CallParams{Model: "gpt-4o"},
		otellix.WithUserID("reader"), otellix.WithBudgetConfig(budget))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()

	store.mu.Lock()
	store.gets = 0 // ignore reads made before the stream started
	store.mu.Unlock()
	tokens := 0
	for {
		if _, err := stream.Recv(); err != nil {
			break
		}
		tokens++
	}

	if tokens != 5 {
		t.Fatalf("expected 5 tokens, got %d", tokens)
	}
	if store.gets != 1 {
		t.Errorf("expected 1 spend read for the whole stream, got %d", store.gets)
	}
}
