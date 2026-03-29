package requestctx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithRequestID_And_RequestID(t *testing.T) {
	ctx := WithRequestID(t.Context(), "abc-123")
	if got := RequestID(ctx); got != "abc-123" {
		t.Errorf("RequestID() = %q, want %q", got, "abc-123")
	}
}

func TestRequestID_EmptyContext(t *testing.T) {
	if got := RequestID(t.Context()); got != "" {
		t.Errorf("RequestID() = %q, want empty", got)
	}
}

func TestMiddleware_GeneratesID(t *testing.T) {
	var capturedID string
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = RequestID(r.Context())
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedID == "" {
		t.Error("middleware should generate a request ID")
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("response should include X-Request-ID header")
	}
	if rec.Header().Get("X-Request-ID") != capturedID {
		t.Error("response header should match context ID")
	}
}

func TestMiddleware_UsesExistingHeader(t *testing.T) {
	var capturedID string
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = RequestID(r.Context())
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", "incoming-id-456")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedID != "incoming-id-456" {
		t.Errorf("RequestID() = %q, want %q", capturedID, "incoming-id-456")
	}
	if rec.Header().Get("X-Request-ID") != "incoming-id-456" {
		t.Error("response header should echo the incoming ID")
	}
}

func TestMiddleware_UniqueIDs(t *testing.T) {
	var ids []string
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids = append(ids, RequestID(r.Context()))
	}))

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	seen := make(map[string]bool)
	for _, id := range ids {
		if seen[id] {
			t.Errorf("duplicate request ID: %s", id)
		}
		seen[id] = true
	}
}
