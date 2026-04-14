package http_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func testLog() logger.Logger { return logger.NewWithWriter(&bytes.Buffer{}, "info") }

func TestRateLimitMiddleware_Disabled(t *testing.T) {
	cfg := config.RateLimitConfig{Enabled: false}
	mw := shophttp.RateLimitMiddleware(cfg, testLog())
	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	req.RemoteAddr = "1.2.3.4:9999"
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("handler should be called when rate limiting is disabled")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestRateLimitMiddleware_DefaultLimit(t *testing.T) {
	cfg := config.RateLimitConfig{
		Enabled: true,
		Default: config.RateLimitRule{Rate: 1, Burst: 2},
	}
	mw := shophttp.RateLimitMiddleware(cfg, testLog())
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First two requests should pass (burst=2).
	for i := 0; i < 2; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i+1, rr.Code, http.StatusOK)
		}
	}

	// Third request should be rate limited.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusTooManyRequests)
	}
	if got := rr.Header().Get("Retry-After"); got != "1" {
		t.Errorf("Retry-After = %q, want %q", got, "1")
	}

	// Verify JSON error body.
	var resp shophttp.Response
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("error should not be nil")
	}
	if resp.Error.Code != "rate_limited" {
		t.Errorf("error.code = %q, want %q", resp.Error.Code, "rate_limited")
	}
}

func TestRateLimitMiddleware_PerRouteOverride(t *testing.T) {
	cfg := config.RateLimitConfig{
		Enabled: true,
		Default: config.RateLimitRule{Rate: 1, Burst: 100}, // generous default
		PerRoute: []config.RouteRateLimitRule{
			{PathPrefix: "/api/v1/auth", Rate: 1, Burst: 1}, // strict per-route
		},
	}
	mw := shophttp.RateLimitMiddleware(cfg, testLog())
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First auth request should pass.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = "10.0.0.2:5555"
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("first auth request: status = %d, want %d", rr.Code, http.StatusOK)
	}

	// Second auth request should be limited by per-route (burst=1).
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = "10.0.0.2:5555"
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("second auth request: status = %d, want %d", rr.Code, http.StatusTooManyRequests)
	}

	// Non-auth route should still work (default burst=100).
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	req.RemoteAddr = "10.0.0.2:5555"
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("product request: status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestRateLimitMiddleware_OverlappingPrefixes(t *testing.T) {
	cfg := config.RateLimitConfig{
		Enabled: true,
		Default: config.RateLimitRule{Rate: 1, Burst: 100},
		PerRoute: []config.RouteRateLimitRule{
			{PathPrefix: "/api", Rate: 1, Burst: 100},       // broad
			{PathPrefix: "/api/v1/auth", Rate: 1, Burst: 1}, // specific, strict
		},
	}
	mw := shophttp.RateLimitMiddleware(cfg, testLog())
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First auth request — should use the more specific /api/v1/auth (burst=1).
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = "10.0.0.3:1111"
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("first auth request: status = %d, want 200", rr.Code)
	}

	// Second auth request — should be limited by the specific prefix (burst=1).
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = "10.0.0.3:1111"
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("second auth request: status = %d, want 429 (longest prefix should win)", rr.Code)
	}

	// Broad /api prefix should still allow requests to other routes.
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	req.RemoteAddr = "10.0.0.3:1111"
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("products request: status = %d, want 200 (broad /api limiter)", rr.Code)
	}
}

func TestRateLimitMiddleware_ClientIP_XForwardedFor_TrustedProxy(t *testing.T) {
	cfg := config.RateLimitConfig{
		Enabled:        true,
		Default:        config.RateLimitRule{Rate: 1, Burst: 1},
		TrustedProxies: []string{"10.0.0.50"},
	}
	mw := shophttp.RateLimitMiddleware(cfg, testLog())
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request from IP-A via trusted proxy.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.50:8080"
	req.Header.Set("X-Forwarded-For", "192.168.1.1, 10.0.0.50")
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("first request: status = %d", rr.Code)
	}

	// Second from same forwarded IP — limited.
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.50:8080"
	req.Header.Set("X-Forwarded-For", "192.168.1.1, 10.0.0.50")
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("second request same IP: status = %d, want 429", rr.Code)
	}

	// Request from different forwarded IP — allowed.
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.50:8080"
	req.Header.Set("X-Forwarded-For", "192.168.1.2, 10.0.0.50")
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("different IP request: status = %d, want 200", rr.Code)
	}
}

func TestRateLimitMiddleware_ClientIP_XRealIP_TrustedProxy(t *testing.T) {
	cfg := config.RateLimitConfig{
		Enabled:        true,
		Default:        config.RateLimitRule{Rate: 1, Burst: 1},
		TrustedProxies: []string{"10.0.0.50"},
	}
	mw := shophttp.RateLimitMiddleware(cfg, testLog())
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.50:8080"
	req.Header.Set("X-Real-Ip", "10.0.0.99")
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("first request: status = %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.50:8080"
	req.Header.Set("X-Real-Ip", "10.0.0.99")
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("second request same IP: status = %d, want 429", rr.Code)
	}
}

func TestRateLimitMiddleware_ClientIP_UntrustedProxyIgnoresHeaders(t *testing.T) {
	cfg := config.RateLimitConfig{
		Enabled:        true,
		Default:        config.RateLimitRule{Rate: 1, Burst: 1},
		TrustedProxies: []string{"10.0.0.50"}, // only this IP is trusted
	}
	mw := shophttp.RateLimitMiddleware(cfg, testLog())
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Attacker sends X-Forwarded-For from untrusted peer — should be ignored.
	// Both requests come from different "forwarded" IPs but the same RemoteAddr.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:9999" // not trusted
	req.Header.Set("X-Forwarded-For", "fake-ip-1")
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("first request: status = %d", rr.Code)
	}

	// Second request — different XFF but same RemoteAddr → should be limited
	// because headers are ignored for untrusted peers.
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:9999"
	req.Header.Set("X-Forwarded-For", "fake-ip-2")
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("second request: status = %d, want 429 (untrusted proxy, same peer IP)", rr.Code)
	}
}
