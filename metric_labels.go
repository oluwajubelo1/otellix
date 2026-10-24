package otellix

import (
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricLabels chooses which attribution labels Otellix puts on its metrics.
//
// Every distinct label value creates a new time series in Prometheus and
// similar backends. user_id grows with your user count, so with many users
// it can overload the metrics backend. Turn it off and keep per-user detail
// in traces, where high cardinality is cheap.
//
// The provider, model and user_attributed labels are always present.
// user_attributed is "true" or "false", so calls with no user can still be
// counted with user_id turned off.
type MetricLabels struct {
	UserID    bool
	ProjectID bool
	FeatureID bool
}

var metricLabels atomic.Pointer[MetricLabels]

// SetMetricLabels sets the attribution labels on all metrics recorded from
// now on. By default all three are on.
//
// For example, to drop user_id:
//
//	otellix.SetMetricLabels(otellix.MetricLabels{ProjectID: true, FeatureID: true})
func SetMetricLabels(labels MetricLabels) {
	metricLabels.Store(&labels)
}

// currentMetricLabels returns the labels set by SetMetricLabels, or the default.
func currentMetricLabels() MetricLabels {
	if l := metricLabels.Load(); l != nil {
		return *l
	}
	return MetricLabels{UserID: true, ProjectID: true, FeatureID: true}
}

// metricAttributes returns the labels shared by all of a call's metrics.
func metricAttributes(cfg *Config) metric.MeasurementOption {
	labels := currentMetricLabels()
	attrs := []attribute.KeyValue{
		attribute.String("provider", cfg.Provider),
		attribute.String("model", cfg.Model),
		attribute.Bool("user_attributed", cfg.UserID != ""),
	}
	if labels.FeatureID {
		attrs = append(attrs, attribute.String("feature_id", cfg.FeatureID))
	}
	if labels.UserID {
		attrs = append(attrs, attribute.String("user_id", cfg.UserID))
	}
	if labels.ProjectID {
		attrs = append(attrs, attribute.String("project_id", cfg.ProjectID))
	}
	return metric.WithAttributes(attrs...)
}
