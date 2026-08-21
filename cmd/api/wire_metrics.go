package main

import (
	"fmt"
	"net"
	"net/http"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/metrics"
)

// newMetrics builds the metrics recorder and, when enabled, the /metrics
// scrape handler. The recorder is never nil — callers get metrics.Noop()
// when disabled, so instrumentation call sites never need a nil check. The
// handler is nil when disabled: no listener should be started.
func newMetrics(cfg *config.Config) (metrics.Recorder, http.Handler) {
	if !cfg.Metrics.Enabled {
		return metrics.Noop(), nil
	}
	rec, reg := metrics.NewPrometheusRecorder()
	return rec, metrics.Handler(reg)
}

// startMetricsServer binds cfg.Metrics.Listen synchronously (so bind failures
// such as "address already in use" surface at startup), then serves /metrics
// in a background goroutine. It has no auth of its own: operators who rebind
// Listen onto a non-loopback address are responsible for keeping it on a
// private scrape network. Returns (nil, nil, nil) when handler is nil.
func startMetricsServer(cfg *config.Config, handler http.Handler, log logger.Logger) (*http.Server, <-chan struct{}, error) {
	if handler == nil {
		return nil, nil, nil
	}
	ln, err := net.Listen("tcp", cfg.Metrics.Listen)
	if err != nil {
		return nil, nil, fmt.Errorf("metrics listen %s: %w", cfg.Metrics.Listen, err)
	}
	srv := &http.Server{Handler: handler}
	done := make(chan struct{})
	go func() {
		defer close(done)
		log.Info("metrics.server.start", map[string]interface{}{"addr": cfg.Metrics.Listen})
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Error("metrics.server.failed", err, map[string]interface{}{"addr": cfg.Metrics.Listen})
		}
	}()
	return srv, done, nil
}
