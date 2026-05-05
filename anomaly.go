package otellix

import (
	"context"
	"fmt"
)

// AnomalyAction determines what to do when an anomaly is detected.
type AnomalyAction int

const (
	// AnomalyLog records the anomaly in the span but does not block the call.
	AnomalyLog AnomalyAction = iota
	// AnomalyBlock prevents the call and returns an AnomalyBlockedError.
	AnomalyBlock
)

// AnomalyConfig configures cost anomaly detection for a call.
type AnomalyConfig struct {
	// Threshold is the multiplier above the rolling average to trigger anomaly.
	// E.g., 5.0 means a call is anomalous if it costs 5× the average.
	Threshold float64

	// WindowSize is the number of recent calls to compute the rolling average from.
	// If 0, defaults to 50.
	WindowSize int

	// Action determines whether to log or block anomalous calls.
	Action AnomalyAction

	// OnAnomaly is an optional callback fired when an anomaly is detected.
	OnAnomaly func(ctx context.Context, d AnomalyDetection)
}

// AnomalyDetection describes a detected cost anomaly.
type AnomalyDetection struct {
	// Key is "provider/model" (e.g., "anthropic/claude-sonnet-4-6").
	Key string
	// CurrentCost is the cost of the detected call.
	CurrentCost float64
	// AverageCost is the rolling average cost for this provider/model.
	AverageCost float64
	// Multiplier is CurrentCost / AverageCost.
	Multiplier float64
	// Threshold is the configured threshold for anomaly.
	Threshold float64
}

// AnomalyBlockedError is returned when Action == AnomalyBlock and an anomaly is detected.
type AnomalyBlockedError struct {
	Detection AnomalyDetection
}

func (e *AnomalyBlockedError) Error() string {
	return fmt.Sprintf("anomaly blocked: cost $%.6f is %.1fx average $%.6f (threshold: %.1fx) for %s",
		e.Detection.CurrentCost, e.Detection.Multiplier, e.Detection.AverageCost, e.Detection.Threshold, e.Detection.Key)
}

// checkAnomaly detects if a cost is anomalously high relative to recent history.
// Returns (detection, true) if anomalous, (_, false) otherwise.
// If there is insufficient history, returns (_, false) — never blocks on first calls.
func checkAnomaly(cfg *Config, costUSD float64) (AnomalyDetection, bool) {
	if cfg.AnomalyConfig == nil {
		return AnomalyDetection{}, false
	}

	windowSize := cfg.AnomalyConfig.WindowSize
	if windowSize <= 0 {
		windowSize = 50
	}

	// Snapshot the call records under RLock.
	trackerMu.RLock()
	n := len(callRecords)
	start := n - windowSize
	if start < 0 {
		start = 0
	}
	// Copy the relevant window before releasing the lock.
	window := make([]CallRecord, len(callRecords[start:n]))
	copy(window, callRecords[start:n])
	trackerMu.RUnlock()

	// Filter for same provider/model and compute rolling average.
	providerModel := cfg.Provider + "/" + cfg.Model
	var sum float64
	var count int
	for _, r := range window {
		if r.Provider+"/"+r.Model == providerModel {
			sum += r.CostUSD
			count++
		}
	}

	// No history for this model — skip anomaly detection.
	if count == 0 {
		return AnomalyDetection{}, false
	}

	avg := sum / float64(count)
	if avg <= 0 {
		return AnomalyDetection{}, false
	}

	multiplier := costUSD / avg
	threshold := cfg.AnomalyConfig.Threshold
	if threshold <= 0 {
		threshold = 5.0
	}

	if multiplier < threshold {
		return AnomalyDetection{}, false
	}

	// Anomaly detected.
	return AnomalyDetection{
		Key:         providerModel,
		CurrentCost: costUSD,
		AverageCost: avg,
		Multiplier:  multiplier,
		Threshold:   threshold,
	}, true
}
