package otellix

import (
	"testing"

	"go.opentelemetry.io/otel/sdk/resource"
)

// TestServiceResource verifies the service.name resource helper sets a usable
// name instead of falling back to the SDK's unknown_service default.
func TestServiceResource(t *testing.T) {
	res := serviceResource(newSetupConfig(WithServiceName("my-api")))
	if got := attrValue(res, "service.name"); got != "my-api" {
		t.Errorf("service.name = %q, want %q", got, "my-api")
	}

	// Default should be a stable name, not the SDK's unknown_service:<binary>.
	res = serviceResource(newSetupConfig())
	if got := attrValue(res, "service.name"); got != defaultServiceName {
		t.Errorf("default service.name = %q, want %q", got, defaultServiceName)
	}
}

func attrValue(res *resource.Resource, key string) string {
	for _, kv := range res.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.AsString()
		}
	}
	return ""
}

// TestSetupOTLPWiring verifies SetupOTLP installs a batched OTLP provider and
// returns a shutdown func without panicking. The exporter connects lazily, so
// a non-routable endpoint is fine here — we only verify the wiring.
func TestSetupOTLPWiring(t *testing.T) {
	shutdown := SetupOTLP("127.0.0.1:1", WithServiceName("test-api"))
	if shutdown == nil {
		t.Fatal("SetupOTLP returned nil shutdown")
	}
	shutdown()
}
