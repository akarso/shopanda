package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/metrics"
)

func TestNewMetrics_Disabled_ReturnsNoopAndNilHandler(t *testing.T) {
	cfg := &config.Config{Metrics: config.MetricsConfig{Enabled: false}}
	rec, handler := newMetrics(cfg)
	if rec == nil {
		t.Fatal("expected a non-nil recorder even when disabled")
	}
	if handler != nil {
		t.Fatal("expected a nil handler when metrics are disabled")
	}
	rec.HTTPRequest("GET /x", "GET", "2xx", time.Millisecond)
}

func TestNewMetrics_Enabled_ReturnsWorkingHandler(t *testing.T) {
	cfg := &config.Config{Metrics: config.MetricsConfig{Enabled: true, Listen: "127.0.0.1:0"}}
	rec, handler := newMetrics(cfg)
	if handler == nil {
		t.Fatal("expected a non-nil /metrics handler when enabled")
	}
	rec.CheckoutResult(metrics.OutcomeSuccess)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "shopanda_checkout_result_total") {
		t.Fatalf("expected checkout metric in output, got:\n%s", w.Body.String())
	}
}

func TestStartMetricsServer_NilHandler_ReturnsNil(t *testing.T) {
	cfg := &config.Config{Metrics: config.MetricsConfig{Enabled: false}}
	srv, done, err := startMetricsServer(cfg, nil, logger.NewWithWriter(io.Discard, "error"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if srv != nil || done != nil {
		t.Fatal("expected nil server and done channel when handler is nil")
	}
}

func TestStartMetricsServer_BindFailure_ReturnsError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	cfg := &config.Config{Metrics: config.MetricsConfig{Enabled: true, Listen: ln.Addr().String()}}
	_, handler := newMetrics(cfg)
	srv, done, err := startMetricsServer(cfg, handler, logger.NewWithWriter(io.Discard, "error"))
	if err == nil {
		if srv != nil {
			srv.Close()
		}
		t.Fatal("expected bind error when port is already in use")
	}
	if done != nil {
		t.Fatal("expected nil done channel on bind failure")
	}
}

func TestStartMetricsServer_ServesAndStops(t *testing.T) {
	cfg := &config.Config{Metrics: config.MetricsConfig{Enabled: true, Listen: "127.0.0.1:0"}}
	_, handler := newMetrics(cfg)

	srv, done, err := startMetricsServer(cfg, handler, logger.NewWithWriter(io.Discard, "error"))
	if err != nil {
		t.Fatalf("startMetricsServer: %v", err)
	}
	if srv == nil || done == nil {
		t.Fatal("expected a non-nil server and done channel when handler is set")
	}
	time.Sleep(20 * time.Millisecond)
	srv.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected done channel to close after Close()")
	}
}
