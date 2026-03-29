// Package exporters provides OTel exporters for Otellix telemetry data.
//
// prometheus.go provides a Prometheus metrics exporter that exposes all Otellix
// metrics via an HTTP endpoint for scraping.
package exporters

import (
	"fmt"
	"net/http"

	promexporter "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// PrometheusExporter sets up a Prometheus metrics endpoint.
type PrometheusExporter struct {
	provider *metric.MeterProvider
	handler  http.Handler
}

// NewPrometheusExporter creates a new Prometheus exporter and registers it as
// the global OTel MeterProvider. Metrics are available via the HTTP handler.
func NewPrometheusExporter() (*PrometheusExporter, error) {
	exporter, err := promexporter.New()
	if err != nil {
		return nil, fmt.Errorf("otellix/prometheus: failed to create exporter: %w", err)
	}

	provider := metric.NewMeterProvider(
		metric.WithReader(exporter),
	)

	return &PrometheusExporter{
		provider: provider,
		handler:  promhttp.Handler(),
	}, nil
}

// MeterProvider returns the OTel MeterProvider. Set this as the global provider
// to have Otellix metrics exported to Prometheus:
//
//	exp, _ := exporters.NewPrometheusExporter()
//	otel.SetMeterProvider(exp.MeterProvider())
func (e *PrometheusExporter) MeterProvider() *metric.MeterProvider {
	return e.provider
}

// Handler returns the HTTP handler for the /metrics endpoint.
// Mount this on your HTTP server:
//
//	http.Handle("/metrics", exp.Handler())
func (e *PrometheusExporter) Handler() http.Handler {
	return e.handler
}

// ListenAndServe starts an HTTP server on the given address serving the
// Prometheus metrics endpoint. This is a convenience method for simple setups.
//
//	go exp.ListenAndServe(":9090")
func (e *PrometheusExporter) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", e.handler)
	return http.ListenAndServe(addr, mux)
}
