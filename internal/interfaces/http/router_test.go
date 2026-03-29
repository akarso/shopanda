package http_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

func TestRouter_HandleFunc(t *testing.T) {
	r := shophttp.NewRouter()
	r.HandleFunc("GET /test", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if body != "ok" {
		t.Fatalf("expected ok, got %s", body)
	}
}

func TestRouter_MiddlewareOrder(t *testing.T) {
	r := shophttp.NewRouter()

	var order []string
	mw := func(name string) shophttp.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name+"-before")
				next.ServeHTTP(w, r)
				order = append(order, name+"-after")
			})
		}
	}

	r.Use(mw("first"))
	r.Use(mw("second"))
	r.HandleFunc("GET /mw", func(w http.ResponseWriter, _ *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/mw", nil)
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, req)

	expected := []string{"first-before", "second-before", "handler", "second-after", "first-after"}
	if len(order) != len(expected) {
		t.Fatalf("expected %d entries, got %d: %v", len(expected), len(order), order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Fatalf("index %d: expected %s, got %s", i, v, order[i])
		}
	}
}

func TestRouter_NotFound(t *testing.T) {
	r := shophttp.NewRouter()
	r.HandleFunc("GET /exists", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
