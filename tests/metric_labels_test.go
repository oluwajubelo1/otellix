package tests

import (
	"context"
	"sync"
	"testing"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// recordSpans records the spans ended during the test.
func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	return recorder
}

// spanAttr returns the value of key on the last span recorded.
func spanAttr(t *testing.T, recorder *tracetest.SpanRecorder, key string) string {
	t.Helper()
	ended := recorder.Ended()
	if len(ended) == 0 {
		t.Fatal("no spans recorded")
	}
	for _, kv := range ended[len(ended)-1].Attributes() {
		if string(kv.Key) == key {
			return kv.Value.Emit()
		}
	}
	return ""
}

var (
	metricReaderOnce sync.Once
	metricReader     *sdkmetric.ManualReader
)

// readMetrics returns Otellix's metrics, installing a reader as the global
// meter provider on first use.
func readMetrics(t *testing.T) metricdata.ResourceMetrics {
	t.Helper()
	metricReaderOnce.Do(func() {
		metricReader = sdkmetric.NewManualReader()
		otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader)))
	})
	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	return rm
}

// costLabelSets returns the label sets recorded on the cost metric for model.
func costLabelSets(t *testing.T, model string) []attribute.Set {
	t.Helper()
	var sets []attribute.Set
	for _, sm := range readMetrics(t).ScopeMetrics {
		for _, m := range sm.Metrics {
			sum, ok := m.Data.(metricdata.Sum[float64])
			if m.Name != "otellix.llm.cost.usd" || !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				if v, _ := dp.Attributes.Value("model"); v.AsString() == model {
					sets = append(sets, dp.Attributes)
				}
			}
		}
	}
	return sets
}

func TestMetricLabels(t *testing.T) {
	readMetrics(t) // install the reader before any call is recorded
	t.Cleanup(func() {
		otellix.SetMetricLabels(otellix.MetricLabels{UserID: true, ProjectID: true, FeatureID: true})
	})

	call := func(model string) {
		mock := &providers.MockProvider{ProviderName: "openai", Result: providers.CallResult{OutputTokens: 10}}
		if _, err := otellix.Trace(context.Background(), mock, providers.CallParams{Model: model},
			otellix.WithUserID("usr_1"), otellix.WithProjectID("acme"), otellix.WithFeatureID("chat")); err != nil {
			t.Fatal(err)
		}
	}

	// Default: every attribution label, plus user_attributed.
	call("gpt-4o-mini")
	sets := costLabelSets(t, "gpt-4o-mini")
	if len(sets) != 1 {
		t.Fatalf("expected 1 cost series for gpt-4o-mini, got %d", len(sets))
	}
	for _, key := range []attribute.Key{"user_id", "project_id", "feature_id", "user_attributed"} {
		if !sets[0].HasValue(key) {
			t.Errorf("default labels should include %s, got %v", key, sets[0].ToSlice())
		}
	}

	// With user_id turned off, it is dropped and user_attributed remains.
	otellix.SetMetricLabels(otellix.MetricLabels{ProjectID: true, FeatureID: true})
	call("gpt-4.1-mini")
	sets = costLabelSets(t, "gpt-4.1-mini")
	if len(sets) != 1 {
		t.Fatalf("expected 1 cost series for gpt-4.1-mini, got %d", len(sets))
	}
	if sets[0].HasValue("user_id") {
		t.Errorf("user_id should be dropped, got %v", sets[0].ToSlice())
	}
	if v, _ := sets[0].Value("user_attributed"); !v.AsBool() {
		t.Errorf("expected user_attributed=true, got %v", sets[0].ToSlice())
	}
}
