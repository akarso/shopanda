package tracing_test

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/tracing"
)

func TestSetup_Disabled_ReturnsNoopShutdown(t *testing.T) {
	shutdown, err := tracing.Setup(context.Background(), config.TracingConfig{Enabled: false}, "test-service")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown func is nil, want a callable no-op")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("no-op shutdown returned error: %v", err)
	}
}

func TestSetup_EnabledWithoutEndpoint_ReturnsError(t *testing.T) {
	_, err := tracing.Setup(context.Background(), config.TracingConfig{Enabled: true}, "test-service")
	if err == nil {
		t.Fatal("expected error for enabled tracing with no endpoint")
	}
}

func TestSetup_Enabled_InstallsProviderAndShutdownSucceeds(t *testing.T) {
	// otlptracehttp.New does not eagerly dial — the exporter is only used
	// when a span is actually flushed, which the batcher does on its own
	// schedule (or on Shutdown's final flush). No collector needs to be
	// listening for Setup or Shutdown to succeed here; a real send failure
	// would be logged by the SDK's internal error handler, not returned.
	prevProvider := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(prevProvider) })

	shutdown, err := tracing.Setup(context.Background(), config.TracingConfig{
		Enabled:  true,
		Endpoint: "127.0.0.1:1", // nothing listening; exporter creation must still succeed
		Insecure: true,
	}, "test-service")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if otel.GetTracerProvider() == prevProvider {
		t.Error("expected Setup to install a new global tracer provider")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}

// TestSetup_SampleRatioZero_NeverSamples pins the fix for a clamp that used
// to live in Setup itself (ratio <= 0 was forced up to 1) and would have
// silently turned an explicit "export nothing" config into "export
// everything" even though config.normalizeTracing was already fixed to
// preserve 0 at the config layer — the bug was reintroduced one level
// down, in Setup's own defensive fallback. This exercises Setup end to
// end: install the provider, start a span, and check whether the SDK
// actually decided to sample it.
func TestSetup_SampleRatioZero_NeverSamples(t *testing.T) {
	prevProvider := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(prevProvider) })

	shutdown, err := tracing.Setup(context.Background(), config.TracingConfig{
		Enabled:     true,
		Endpoint:    "127.0.0.1:1",
		Insecure:    true,
		SampleRatio: 0,
	}, "test-service")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(ctx)
	})

	_, span := otel.Tracer("test").Start(context.Background(), "should-not-sample")
	defer span.End()
	if span.SpanContext().IsSampled() {
		t.Error("SampleRatio: 0 must never sample a span, got sampled")
	}
}

// TestSetup_SampleRatioOne_AlwaysSamples is the positive-case sibling of
// the test above — confirms the fix didn't just make 0 a no-op sampler by
// accident while breaking the normal "sample everything" default.
func TestSetup_SampleRatioOne_AlwaysSamples(t *testing.T) {
	prevProvider := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(prevProvider) })

	shutdown, err := tracing.Setup(context.Background(), config.TracingConfig{
		Enabled:     true,
		Endpoint:    "127.0.0.1:1",
		Insecure:    true,
		SampleRatio: 1,
	}, "test-service")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(ctx)
	})

	_, span := otel.Tracer("test").Start(context.Background(), "should-sample")
	defer span.End()
	if !span.SpanContext().IsSampled() {
		t.Error("SampleRatio: 1 must always sample a span, got not sampled")
	}
}
