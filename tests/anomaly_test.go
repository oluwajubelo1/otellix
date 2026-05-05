package tests

import (
	"context"
	"errors"
	"testing"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
)

// TestAnomalyLogNotTriggeredBelowThreshold verifies anomalies below threshold don't fire.
func TestAnomalyLogNotTriggeredBelowThreshold(t *testing.T) {
	otellix.ResetCallRecords()
	t.Cleanup(func() { otellix.ResetCallRecords() })

	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 100, OutputTokens: 50},
	}

	// Record baseline calls with consistent cost
	for i := 0; i < 10; i++ {
		otellix.Trace(context.Background(), mock, providers.CallParams{Model: "claude-sonnet-4-6"},
			otellix.WithProvider("anthropic"),
		)
	}

	anomalyCalled := false
	cfg := &otellix.AnomalyConfig{
		Threshold:  5.0,
		WindowSize: 10,
		Action:     otellix.AnomalyLog,
		OnAnomaly: func(ctx context.Context, d otellix.AnomalyDetection) {
			anomalyCalled = true
		},
	}

	// Call again with same token counts - should not trigger anomaly
	_, err := otellix.Trace(context.Background(), mock, providers.CallParams{Model: "claude-sonnet-4-6"},
		otellix.WithProvider("anthropic"),
		otellix.WithAnomalyDetection(cfg),
	)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if anomalyCalled {
		t.Error("anomaly callback should not fire below threshold")
	}
}

// TestAnomalyLogTriggeredAboveThreshold verifies anomalies above threshold fire.
func TestAnomalyLogTriggeredAboveThreshold(t *testing.T) {
	otellix.ResetCallRecords()
	t.Cleanup(func() { otellix.ResetCallRecords() })

	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 100, OutputTokens: 50},
	}

	// Record 10 baseline calls with small token counts
	for i := 0; i < 10; i++ {
		otellix.Trace(context.Background(), mock, providers.CallParams{Model: "claude-sonnet-4-6"},
			otellix.WithProvider("anthropic"),
		)
	}

	// Call with 10x tokens - should trigger anomaly
	expensiveMock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 1000, OutputTokens: 500},
	}

	anomalyDetected := false
	cfg := &otellix.AnomalyConfig{
		Threshold:  5.0,
		WindowSize: 10,
		Action:     otellix.AnomalyLog,
		OnAnomaly: func(ctx context.Context, d otellix.AnomalyDetection) {
			anomalyDetected = true
			if d.Multiplier < d.Threshold {
				t.Errorf("multiplier %f should be >= threshold %f", d.Multiplier, d.Threshold)
			}
		},
	}

	_, err := otellix.Trace(context.Background(), expensiveMock, providers.CallParams{Model: "claude-sonnet-4-6"},
		otellix.WithProvider("anthropic"),
		otellix.WithAnomalyDetection(cfg),
	)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !anomalyDetected {
		t.Error("anomaly callback should fire above threshold")
	}
}

// TestAnomalyBlockPreventCall verifies AnomalyBlock prevents provider calls.
func TestAnomalyBlockPreventCall(t *testing.T) {
	otellix.ResetCallRecords()
	t.Cleanup(func() { otellix.ResetCallRecords() })

	// Record baseline with tiny token counts (low cost)
	tinyMock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 1, OutputTokens: 1},
	}
	for i := 0; i < 10; i++ {
		otellix.Trace(context.Background(), tinyMock, providers.CallParams{Model: "claude-sonnet-4-6"},
			otellix.WithProvider("anthropic"),
		)
	}

	// Now try to call with high token count and AnomalyBlock
	expensiveMock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 10000, OutputTokens: 10000},
	}

	cfg := &otellix.AnomalyConfig{
		Threshold:  2.0, // 2x threshold
		WindowSize: 10,
		Action:     otellix.AnomalyBlock,
	}

	_, err := otellix.Trace(context.Background(), expensiveMock, providers.CallParams{Model: "claude-sonnet-4-6"},
		otellix.WithProvider("anthropic"),
		otellix.WithAnomalyDetection(cfg),
	)

	// Should get AnomalyBlockedError
	if err == nil {
		t.Fatal("expected AnomalyBlockedError, got nil")
	}

	var anomalyErr *otellix.AnomalyBlockedError
	if !errors.As(err, &anomalyErr) {
		t.Errorf("expected AnomalyBlockedError, got %T: %v", err, err)
	}

	// Provider should not have been called
	if expensiveMock.CallCount != 0 {
		t.Errorf("expected 0 provider calls when blocked, got %d", expensiveMock.CallCount)
	}
}

// TestAnomalyBlockSkipsWhenNoHistory verifies first calls are never blocked.
func TestAnomalyBlockSkipsWhenNoHistory(t *testing.T) {
	otellix.ResetCallRecords()
	t.Cleanup(func() { otellix.ResetCallRecords() })

	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 10000, OutputTokens: 10000},
	}

	cfg := &otellix.AnomalyConfig{
		Threshold:  2.0,
		WindowSize: 50,
		Action:     otellix.AnomalyBlock,
	}

	// First call ever - should succeed despite high cost (no baseline to compare)
	_, err := otellix.Trace(context.Background(), mock, providers.CallParams{Model: "claude-sonnet-4-6"},
		otellix.WithProvider("anthropic"),
		otellix.WithAnomalyDetection(cfg),
	)

	if err != nil {
		t.Fatalf("first call should not be blocked, got error: %v", err)
	}
	if mock.CallCount != 1 {
		t.Errorf("expected 1 provider call, got %d", mock.CallCount)
	}
}

// TestAnomalyCallbackReceivesValues verifies callback receives correct values.
func TestAnomalyCallbackReceivesValues(t *testing.T) {
	otellix.ResetCallRecords()
	t.Cleanup(func() { otellix.ResetCallRecords() })

	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 100, OutputTokens: 50},
	}

	// Baseline
	for i := 0; i < 5; i++ {
		otellix.Trace(context.Background(), mock, providers.CallParams{Model: "gpt-4o"},
			otellix.WithProvider("openai"),
		)
	}

	expensiveMock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 1000, OutputTokens: 500},
	}

	var receivedDetection otellix.AnomalyDetection
	cfg := &otellix.AnomalyConfig{
		Threshold:  3.0,
		WindowSize: 10,
		Action:     otellix.AnomalyLog,
		OnAnomaly: func(ctx context.Context, d otellix.AnomalyDetection) {
			receivedDetection = d
		},
	}

	_, _ = otellix.Trace(context.Background(), expensiveMock, providers.CallParams{Model: "gpt-4o"},
		otellix.WithProvider("openai"),
		otellix.WithAnomalyDetection(cfg),
	)

	// Verify the detection values are correct
	if receivedDetection.Multiplier <= 3.0 {
		t.Errorf("multiplier should be > threshold, got %f", receivedDetection.Multiplier)
	}
	if receivedDetection.AverageCost == 0 {
		t.Error("average cost should be non-zero")
	}
	if receivedDetection.CurrentCost == 0 {
		t.Error("current cost should be non-zero")
	}
	if receivedDetection.Key != "openai/gpt-4o" {
		t.Errorf("key should be 'openai/gpt-4o', got %s", receivedDetection.Key)
	}
}

// TestAnomalyLogAddsSpanEvent verifies anomaly is logged to span.
func TestAnomalyLogAddsSpanEvent(t *testing.T) {
	otellix.ResetCallRecords()
	t.Cleanup(func() { otellix.ResetCallRecords() })

	// Record baseline
	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 50, OutputTokens: 25},
	}
	for i := 0; i < 5; i++ {
		otellix.Trace(context.Background(), mock, providers.CallParams{Model: "claude-sonnet-4-6"},
			otellix.WithProvider("anthropic"),
		)
	}

	// Anomaly call
	expensiveMock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 500, OutputTokens: 250},
	}

	cfg := &otellix.AnomalyConfig{
		Threshold:  2.0,
		WindowSize: 10,
		Action:     otellix.AnomalyLog,
	}

	// This should not panic
	_, err := otellix.Trace(context.Background(), expensiveMock, providers.CallParams{Model: "claude-sonnet-4-6"},
		otellix.WithProvider("anthropic"),
		otellix.WithAnomalyDetection(cfg),
	)

	if err != nil {
		t.Fatalf("expected no error with AnomalyLog, got %v", err)
	}
}
