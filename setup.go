package otellix

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// defaultServiceName is used when no service name is provided. Without it the
// OTel SDK falls back to "unknown_service:<binary>", which makes traces hard to
// identify in a collector.
const defaultServiceName = "otellix"

// SetupOption configures how Otellix's global exporters are set up.
type SetupOption func(*setupConfig)

// setupConfig holds exporter setup preferences.
type setupConfig struct {
	serviceName string
}

// WithServiceName sets the service.name resource attribute for all Otellix-set
// up exporters. This identifies your application in traces/metrics backends
// (Jaeger, Honeycomb, Grafana, etc.) instead of the SDK default
// "unknown_service:<binary>".
func WithServiceName(name string) SetupOption {
	return func(sc *setupConfig) { sc.serviceName = name }
}

// newSetupConfig applies setup options, defaulting the service name.
func newSetupConfig(opts ...SetupOption) *setupConfig {
	sc := &setupConfig{serviceName: defaultServiceName}
	for _, o := range opts {
		o(sc)
	}
	return sc
}

// serviceResource builds the OTel resource carrying service.name.
func serviceResource(sc *setupConfig) *resource.Resource {
	return resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(sc.serviceName),
	)
}

// SetupOTLP configures Otellix for production by sending spans to an OTLP
// endpoint (the protocol used by Jaeger, Tempo, Honeycomb, SigNoz, OTLP-native
// collectors, etc.). Unlike SetupDev, it does not write spans to stdout — use
// it in production instead of SetupDev.
//
// endpoint is a host[:port] (e.g. "otel-collector:4318"). An empty string uses
// the SDK default of localhost:4318. The exporter uses HTTP/OTLP by default.
//
// Usage:
//
//	shutdown := otellix.SetupOTLP("otel-collector:4318", otellix.WithServiceName("my-api"))
//	defer shutdown()
func SetupOTLP(endpoint string, opts ...SetupOption) func() {
	sc := newSetupConfig(opts...)

	exporterOpts := []otlptracehttp.Option{}
	if endpoint != "" {
		exporterOpts = append(exporterOpts, otlptracehttp.WithEndpoint(endpoint))
	}
	exporter, err := otlptracehttp.New(context.Background(), exporterOpts...)
	if err != nil {
		panic("otellix: failed to create OTLP exporter: " + err.Error())
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(serviceResource(sc)),
	)
	otel.SetTracerProvider(tp)

	return func() {
		_ = tp.Shutdown(context.Background())
	}
}
