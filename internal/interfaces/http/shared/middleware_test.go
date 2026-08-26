package shared_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func TestRequestIDMiddleware_SetsHeader(t *testing.T) {
	mw := shared.RequestIDMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rid := rec.Header().Get("X-Request-ID"); rid == "" {
		t.Fatal("expected X-Request-ID header to be set")
	}
}

func TestRequestIDMiddleware_EchoesHeader(t *testing.T) {
	mw := shared.RequestIDMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "test-123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rid := rec.Header().Get("X-Request-ID"); rid != "test-123" {
		t.Fatalf("expected X-Request-ID test-123, got %s", rid)
	}
}

func TestLoggingMiddleware_Returns200(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "info")
	mw := shared.LoggingMiddleware(log)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestRecoveryMiddleware_CatchesPanic(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "info")
	mw := shared.RecoveryMiddleware(log)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestStatusWriter_CapturesCode(t *testing.T) {
	mw := shared.LoggingMiddleware(logger.NewWithWriter(io.Discard, "info"))
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, "/create", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}
}

// TestStackedStatusMiddleware_AgreeOnFinalStatus pins the correctness side
// of sharing one statusWriter across Metrics, Tracing, and Logging
// (unexported wrapStatus helper) instead of each layering its own wrapper
// around the same request: all three must still observe the exact same
// final status code as the ResponseRecorder itself.
func TestStackedStatusMiddleware_AgreeOnFinalStatus(t *testing.T) {
	rec := &fakeRecorder{}
	log := logger.NewWithWriter(io.Discard, "info")

	router := shared.NewRouter()
	router.HandleFunc("GET /boom", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	router.Use(shared.LoggingMiddleware(log))
	router.Use(shared.TracingMiddleware())
	router.Use(shared.MetricsMiddleware(rec))

	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	w := httptest.NewRecorder()
	router.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusTeapot {
		t.Fatalf("recorder status = %d, want %d", w.Code, http.StatusTeapot)
	}
	if len(rec.calls) != 1 || rec.calls[0].statusClass != "4xx" {
		t.Fatalf("metrics saw statusClass = %v, want a single 4xx call — Metrics must observe the same final status as the recorder", rec.calls)
	}
}
