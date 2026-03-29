package http_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func TestRequestIDMiddleware_SetsHeader(t *testing.T) {
	mw := shophttp.RequestIDMiddleware()
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
	mw := shophttp.RequestIDMiddleware()
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
	mw := shophttp.LoggingMiddleware(log)
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
	mw := shophttp.RecoveryMiddleware(log)
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
	mw := shophttp.LoggingMiddleware(logger.NewWithWriter(io.Discard, "info"))
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
