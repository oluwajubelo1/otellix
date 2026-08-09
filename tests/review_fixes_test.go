package tests

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// slowProvider blocks until the context is cancelled, then returns the error.
type slowProvider struct {
	mu      sync.Mutex
	started chan struct{}
}

func (s *slowProvider) Name() string { return "openai" }

func (s *slowProvider) Call(ctx context.Context, params providers.CallParams) (providers.CallResult, error) {
	s.mu.Lock()
	if s.started != nil {
		close(s.started)
	}
	s.mu.Unlock()
	<-ctx.Done()
	return providers.CallResult{}, ctx.Err()
}

func (s *slowProvider) Stream(ctx context.Context, params providers.CallParams) (providers.Stream, error) {
	return nil, errors.New("not implemented")
}

// TestWithTimeout verifies WithTimeout bounds the provider call and the span is
// still exported (with an error status), so timeouts are visible in traces.
func TestWithTimeout(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(prev)
	}()

	slow := &slowProvider{started: make(chan struct{})}
	parent := otellix.ContextWithUser(context.Background(), "usr_timeout")

	_, err := otellix.Trace(parent, slow,
		providers.CallParams{Model: "gpt-4o"},
		otellix.WithProvider("openai"),
		otellix.WithTimeout(50*time.Millisecond),
	)
	if err == nil {
		t.Fatal("expected context deadline exceeded error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span to be exported on timeout, got %d", len(spans))
	}
	if spans[0].Status.Code != codes.Error {
		t.Errorf("expected error span status, got %v", spans[0].Status.Code)
	}
}
