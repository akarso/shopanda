// Package tracing wires an optional OpenTelemetry trace pipeline. Nothing
// in this package needs to be threaded through call sites: instrumentation
// elsewhere in the codebase calls otel.Tracer(...) directly, which is
// always safe (OpenTelemetry's documented no-op behavior) unless Setup has
// installed a real SDK provider — cheap even then, since building a span's
// start options is unavoidable Go argument evaluation regardless of
// whether the tracer behind it is real, but never expensive: nothing is
// batched, serialized, or sent over the network unless Setup ran. This
// mirrors how the metrics package uses metrics.Noop() as the always-safe
// default, except here the no-op is OTel's own, not one this codebase has
// to implement.
package tracing

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	semconv "go.opentelemetry.io/otel/semconv/v1.38.0"

	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/akarso/shopanda/internal/platform/config"
)

// Shutdown flushes buffered spans and closes the exporter. Always safe to
// call, including when tracing was never enabled (a no-op in that case).
// Bound the context with a timeout — this talks to a network exporter.
type Shutdown func(ctx context.Context) error

// noopShutdown is returned whenever there is nothing to flush or close.
func noopShutdown(context.Context) error { return nil }

// Setup configures the global OTel tracer provider from cfg and installs it
// via otel.SetTracerProvider, so every otel.Tracer(name) call anywhere in
// the process starts producing real spans. serviceName identifies this
// process in exported spans (e.g. "shopanda-api", "shopanda-worker") so a
// collector can distinguish serve from worker traces.
//
// When cfg.Enabled is false, Setup does nothing and returns a no-op
// shutdown — the global tracer provider is left at OTel's own no-op
// default, so instrumentation elsewhere costs nothing.
func Setup(ctx context.Context, cfg config.TracingConfig, serviceName string) (Shutdown, error) {
	if !cfg.Enabled {
		return noopShutdown, nil
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		// config.validateTracing already rejects this at Load time; this
		// is a defensive check for callers that build a TracingConfig by
		// hand (e.g. tests) rather than through config.Load.
		return noopShutdown, fmt.Errorf("tracing: enabled requires a non-empty endpoint")
	}

	// Built before the exporter, and deliberately so: nothing below this
	// point is fallible once the exporter exists, so there is no step that
	// could fail and leak the exporter's background goroutines/connection
	// without anyone calling its Shutdown.
	res, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceName(serviceName),
	))
	if err != nil {
		return noopShutdown, fmt.Errorf("tracing: build resource: %w", err)
	}

	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(cfg.Endpoint)}
	if cfg.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	if len(cfg.Headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(cfg.Headers))
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return noopShutdown, fmt.Errorf("tracing: create OTLP exporter: %w", err)
	}

	// No clamping here: sdktrace.TraceIDRatioBased already treats fraction
	// >= 1 as "always sample" and fraction <= 0 as "never sample" (see its
	// own doc comment), so 0 — a deliberate "record spans, export none"
	// signal (config.DefaultTracingSampleRatio) — passes through correctly
	// on its own. A prior version of this line re-clamped ratio <= 0 up to
	// 1, silently turning an explicit SampleRatio: 0 into "sample
	// everything" — exactly the bug config.normalizeTracing was fixed to
	// avoid at the config layer, reintroduced here by a redundant
	// defensive fallback that used the wrong comparison.
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
	)
	otel.SetTracerProvider(provider)

	return func(shutdownCtx context.Context) error {
		return provider.Shutdown(shutdownCtx)
	}, nil
}
