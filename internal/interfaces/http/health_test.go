package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/store"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/logger"
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
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("Pragma"); got != "no-cache" {
		t.Fatalf("Pragma = %q, want no-cache", got)
	}

	var resp shophttp.HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("expected status ok, got %s", resp.Status)
	}
}

func TestHealthHandler_HEAD(t *testing.T) {
	handler := shophttp.MountProbes(shophttp.HealthHandler(), shophttp.ReadyHandler(stubPinger{}), http.NotFoundHandler())
	req := httptest.NewRequest(http.MethodHead, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD body should be empty, got %q", rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
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
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	var resp shophttp.HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("status = %q", resp.Status)
	}
}

func TestReadyHandler_NilInterface(t *testing.T) {
	var db shophttp.DBPinger
	handler := shophttp.ReadyHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
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

func TestMountProbes_BypassesNext(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		// Simulate StoreMiddleware hung on DB — must never run for probes.
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusTeapot)
	})

	handler := shophttp.MountProbes(
		shophttp.HealthHandler(),
		shophttp.ReadyHandler(stubPinger{}),
		next,
	)

	for _, path := range []string{"/healthz", "/readyz"} {
		nextCalled = false
		start := time.Now()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if nextCalled {
			t.Fatalf("%s invoked next (store stack)", path)
		}
		if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
			t.Fatalf("%s blocked on next: %v", path, elapsed)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", path, rec.Code)
		}
	}

	// Non-probe paths still reach next.
	nextCalled = false
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	rec := httptest.NewRecorder()
	// Use a fast next for the forward check.
	fast := shophttp.MountProbes(
		shophttp.HealthHandler(),
		shophttp.ReadyHandler(stubPinger{}),
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusNoContent)
		}),
	)
	fast.ServeHTTP(rec, req)
	if !nextCalled || rec.Code != http.StatusNoContent {
		t.Fatalf("non-probe: nextCalled=%v code=%d", nextCalled, rec.Code)
	}
}

func TestMountProbes_WithStoreMiddlewareStack(t *testing.T) {
	// Reproduce the CR failure mode: StoreMiddleware ahead of ReadyHandler
	// would hang; MountProbes must short-circuit before that stack runs.
	hungRepo := &hangingStoreRepo{block: make(chan struct{})}
	defer close(hungRepo.block)

	inner := shophttp.NewRouter()
	inner.Use(shophttp.StoreMiddleware(hungRepo, logger.NewWithWriter(io.Discard, "error")))
	inner.HandleFunc("GET /readyz", shophttp.ReadyHandler(stubPinger{}))
	inner.HandleFunc("GET /healthz", shophttp.HealthHandler())

	root := shophttp.MountProbes(
		shophttp.HealthHandler(),
		shophttp.ReadyHandler(stubPinger{}),
		inner.Handler(),
	)

	start := time.Now()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("probe blocked on StoreMiddleware: %v", elapsed)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// hangingStoreRepo blocks FindByDomain/FindDefault until block is closed.
type hangingStoreRepo struct {
	block chan struct{}
}

var _ store.StoreRepository = (*hangingStoreRepo)(nil)

func (h *hangingStoreRepo) FindByID(context.Context, string) (*store.Store, error) {
	return nil, nil
}
func (h *hangingStoreRepo) FindByCode(context.Context, string) (*store.Store, error) {
	return nil, nil
}
func (h *hangingStoreRepo) FindByDomain(ctx context.Context, _ string) (*store.Store, error) {
	select {
	case <-h.block:
		return nil, errors.New("unblocked")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (h *hangingStoreRepo) FindDefault(ctx context.Context) (*store.Store, error) {
	return h.FindByDomain(ctx, "")
}
func (h *hangingStoreRepo) FindAll(context.Context) ([]store.Store, error) { return nil, nil }
func (h *hangingStoreRepo) Create(context.Context, *store.Store) error     { return nil }
func (h *hangingStoreRepo) Update(context.Context, *store.Store) error     { return nil }

func TestReadyProbeLimitMiddleware(t *testing.T) {
	pingCount := 0
	ready := shophttp.ReadyProbeLimitMiddleware(nil, 1, 1, logger.NewWithWriter(io.Discard, "error"))(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			pingCount++
			w.WriteHeader(http.StatusOK)
		}),
	)
	root := shophttp.MountProbes(shophttp.HealthHandler(), ready, http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || pingCount != 1 {
		t.Fatalf("first: code=%d pings=%d", rec.Code, pingCount)
	}

	rec = httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second: expected 429, got %d", rec.Code)
	}
	if pingCount != 1 {
		t.Fatalf("rate-limited request still pinged DB: pings=%d", pingCount)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("429 Cache-Control = %q", got)
	}
}

func TestMountProbes_HEADReadyz(t *testing.T) {
	root := shophttp.MountProbes(
		shophttp.HealthHandler(),
		shophttp.ReadyHandler(stubPinger{}),
		http.NotFoundHandler(),
	)
	req := httptest.NewRequest(http.MethodHead, "/readyz", nil)
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD body should be empty, got %q", rec.Body.String())
	}
}
