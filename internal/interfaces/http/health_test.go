package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

func TestHealthHandler(t *testing.T) {
	handler := shophttp.HealthHandler()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %s", ct)
	}

	var resp shophttp.HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("expected status ok, got %s", resp.Status)
	}
}

type stubPinger struct {
	err error
	fn  func(ctx context.Context) error
}

func (s stubPinger) PingContext(ctx context.Context) error {
	if s.fn != nil {
		return s.fn(ctx)
	}
	return s.err
}

func TestReadyHandler_OK(t *testing.T) {
	handler := shophttp.ReadyHandler(stubPinger{})
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp shophttp.HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("status = %q", resp.Status)
	}
}

func TestReadyHandler_PingError(t *testing.T) {
	handler := shophttp.ReadyHandler(stubPinger{err: errors.New("db down")})
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	var resp shophttp.HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "unavailable" {
		t.Fatalf("status = %q", resp.Status)
	}
}

func TestReadyHandler_Timeout(t *testing.T) {
	handler := shophttp.ReadyHandlerWithTimeout(stubPinger{
		fn: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}, 50*time.Millisecond)

	start := time.Now()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("handler hung: elapsed %v", elapsed)
	}
}
