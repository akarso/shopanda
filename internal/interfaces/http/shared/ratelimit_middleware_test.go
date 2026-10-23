package shared_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/ratelimit"
)

func testLog() logger.Logger { return logger.NewWithWriter(&bytes.Buffer{}, "info") }

func TestRateLimitMiddleware_Disabled(t *testing.T) {
	cfg := config.RateLimitConfig{Enabled: false}
	mw := shared.RateLimitMiddleware(cfg, testLog())
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
	mw := shared.RateLimitMiddleware(cfg, testLog())
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
	var resp shared.Response
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
	mw := shared.RateLimitMiddleware(cfg, testLog())
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
	mw := shared.RateLimitMiddleware(cfg, testLog())
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
	mw := shared.RateLimitMiddleware(cfg, testLog())
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
	mw := shared.RateLimitMiddleware(cfg, testLog())
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
	mw := shared.RateLimitMiddleware(cfg, testLog())
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

type denyAll struct{}

func (denyAll) Allow(context.Context, string) ratelimit.Decision {
	return ratelimit.Decision{RetryAfter: time.Second}
}

type denyFactory struct{}

func (denyFactory) New(ratelimit.Spec) ratelimit.Allow { return denyAll{} }

type cancelAll struct{}

func (cancelAll) Allow(context.Context, string) ratelimit.Decision {
	return ratelimit.Decision{Canceled: true, RetryAfter: time.Second}
}

type cancelFactory struct{}

func (cancelFactory) New(ratelimit.Spec) ratelimit.Allow { return cancelAll{} }

func TestRateLimitMiddleware_CanceledDoesNotReject(t *testing.T) {
	var buf bytes.Buffer
	log := logger.NewWithWriter(&buf, "info")
	cfg := config.RateLimitConfig{
		Enabled: true,
		Default: config.RateLimitRule{Rate: 1, Burst: 1},
		PerRoute: []config.RouteRateLimitRule{
			{PathPrefix: "/api/v1/auth", Rate: 1, Burst: 1},
		},
	}
	mw := shared.RateLimitMiddlewareWithFactory(cfg, log, cancelFactory{})
	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	for _, path := range []string{"/api/v1/products", "/api/v1/auth/login"} {
		buf.Reset()
		called = false
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "10.0.0.9:1"
		handler.ServeHTTP(rr, req)
		if called {
			t.Fatalf("%s: handler must not run for a cancelled decision", path)
		}
		if rr.Code != 499 {
			t.Fatalf("%s: status = %d, want 499 (client closed, not 429)", path, rr.Code)
		}
		if rr.Header().Get("Retry-After") != "" {
			t.Fatalf("%s: Retry-After = %q, want empty", path, rr.Header().Get("Retry-After"))
		}
		if bytes.Contains(buf.Bytes(), []byte("ratelimit.rejected")) {
			t.Fatalf("%s: logged ratelimit.rejected for a cancelled request", path)
		}
	}
}

func TestRateLimitMiddleware_CanceledRecords4xxNot2xx(t *testing.T) {
	rec := &fakeRecorder{}
	router := shared.NewRouter()
	router.HandleFunc("GET /api/v1/products", func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler must not run")
	})
	cfg := config.RateLimitConfig{
		Enabled: true,
		Default: config.RateLimitRule{Rate: 1, Burst: 1},
	}
	router.Use(shared.MetricsMiddleware(rec))
	router.Use(shared.RateLimitMiddlewareWithFactory(cfg, testLog(), cancelFactory{}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	req.RemoteAddr = "10.0.0.9:1"
	rr := httptest.NewRecorder()
	router.Handler().ServeHTTP(rr, req)

	if rr.Code != 499 {
		t.Fatalf("status = %d, want 499", rr.Code)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 recorded call, got %d", len(rec.calls))
	}
	if rec.calls[0].statusClass == "2xx" {
		t.Fatal("cancelled request must not be recorded as 2xx")
	}
	if rec.calls[0].statusClass != "4xx" {
		t.Fatalf("statusClass = %q, want 4xx", rec.calls[0].statusClass)
	}
}

func TestClientIP_XForwardedFor_MultipleHeaderLines(t *testing.T) {
	trusted := shared.ParseTrustedProxies([]string{"10.0.0.50"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.50:8080"
	// First line is client-supplied (HAProxy option forwardfor adds a second line).
	req.Header.Add("X-Forwarded-For", "203.0.113.1")
	req.Header.Add("X-Forwarded-For", "192.168.1.1, 10.0.0.50")
	if got := shared.ClientIP(req, trusted); got != "192.168.1.1" {
		t.Fatalf("ClientIP = %q, want 192.168.1.1 (joined lines, rightmost untrusted)", got)
	}
}

func TestClientIP_XForwardedFor_InvalidStopsWalk(t *testing.T) {
	trusted := shared.ParseTrustedProxies([]string{"10.0.0.50"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.50:8080"
	req.Header.Set("X-Forwarded-For", "203.0.113.1, not-an-ip, 10.0.0.50")
	if got := shared.ClientIP(req, trusted); got != "10.0.0.50" {
		t.Fatalf("ClientIP = %q, want peer (invalid hop stops the walk)", got)
	}
}

func TestClientIP_XForwardedFor_IgnoresSpoofedLeftmost(t *testing.T) {
	trusted := shared.ParseTrustedProxies([]string{"10.0.0.50"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.50:8080"
	req.Header.Set("X-Forwarded-For", "203.0.113.1, 192.168.1.1, 10.0.0.50")
	if got := shared.ClientIP(req, trusted); got != "192.168.1.1" {
		t.Fatalf("ClientIP = %q, want 192.168.1.1 (rightmost untrusted)", got)
	}
}

func TestClientIP_XForwardedFor_IPv4WithPort(t *testing.T) {
	trusted := shared.ParseTrustedProxies([]string{"10.0.0.50"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.50:8080"
	req.Header.Set("X-Forwarded-For", "192.168.1.1:5678, 10.0.0.50")
	if got := shared.ClientIP(req, trusted); got != "192.168.1.1" {
		t.Fatalf("ClientIP = %q, want 192.168.1.1 (strip :port)", got)
	}
}

func TestClientIP_XForwardedFor_IPv6WithPort(t *testing.T) {
	trusted := shared.ParseTrustedProxies([]string{"10.0.0.50"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.50:8080"
	req.Header.Set("X-Forwarded-For", "[2001:db8::1]:443, 10.0.0.50")
	if got := shared.ClientIP(req, trusted); got != "2001:db8::1" {
		t.Fatalf("ClientIP = %q, want 2001:db8::1 (strip [v6]:port)", got)
	}
}

func TestClientIP_XRealIPIgnoredWhenXFFPresent(t *testing.T) {
	trusted := shared.ParseTrustedProxies([]string{"10.0.0.50"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.50:8080"
	req.Header.Set("X-Forwarded-For", "not-an-ip, 10.0.0.50")
	req.Header.Set("X-Real-Ip", "203.0.113.9")
	if got := shared.ClientIP(req, trusted); got != "10.0.0.50" {
		t.Fatalf("ClientIP = %q, want peer (X-Real-Ip must not win when XFF is present)", got)
	}
}

func TestClientIP_XForwardedFor_InvalidFallsBackToPeer(t *testing.T) {
	trusted := shared.ParseTrustedProxies([]string{"10.0.0.50"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.50:8080"
	req.Header.Set("X-Forwarded-For", "not-an-ip, also-bad")
	if got := shared.ClientIP(req, trusted); got != "10.0.0.50" {
		t.Fatalf("ClientIP = %q, want peer 10.0.0.50", got)
	}
}

func TestRateLimitMiddleware_FactoryBackend(t *testing.T) {
	cfg := config.RateLimitConfig{
		Enabled: true,
		Default: config.RateLimitRule{Rate: 100, Burst: 100},
	}
	mw := shared.RateLimitMiddlewareWithFactory(cfg, testLog(), denyFactory{})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	req.RemoteAddr = "10.0.0.9:1"
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429 from injected factory", rr.Code)
	}
}

type errorModeAllow struct{ onError string }

func (e errorModeAllow) Allow(context.Context, string) ratelimit.Decision {
	return ratelimit.Decision{
		Allowed:      !ratelimit.FailClosed(e.onError),
		RetryAfter:   time.Second,
		BackendError: true,
	}
}

type errorModeFactory struct{}

func (errorModeFactory) New(spec ratelimit.Spec) ratelimit.Allow {
	return errorModeAllow{onError: spec.OnError}
}

func TestRateLimitMiddleware_PerRouteInheritsOnError(t *testing.T) {
	cfg := config.RateLimitConfig{
		Enabled: true,
		OnError: "closed",
		Default: config.RateLimitRule{Rate: 10, Burst: 10},
		PerRoute: []config.RouteRateLimitRule{
			{PathPrefix: "/api/v1/auth/login", Rate: 1, Burst: 2},
		},
	}
	mw := shared.RateLimitMiddlewareWithFactory(cfg, testLog(), errorModeFactory{})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = "10.0.0.8:1"
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("inherited on_error=closed: status = %d, want 429", rr.Code)
	}
}

func TestRateLimitMiddleware_MemoryRetryAfter(t *testing.T) {
	cfg := config.RateLimitConfig{
		Enabled: true,
		Default: config.RateLimitRule{Rate: 0.1, Burst: 1},
	}
	mw := shared.RateLimitMiddleware(cfg, testLog())
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.7:1"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("first: %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("second: %d", rr.Code)
	}
	if got := rr.Header().Get("Retry-After"); got != "10" {
		t.Fatalf("Retry-After = %q, want 10", got)
	}
}

func TestRateLimitMiddleware_LoginAndMFASeparateBudgets(t *testing.T) {
	cfg := config.RateLimitConfig{
		Enabled: true,
		Default: config.RateLimitRule{Rate: 10, Burst: 10},
		PerRoute: []config.RouteRateLimitRule{
			{PathPrefix: "/api/v1/auth/login", Rate: 1, Burst: 1},
			{PathPrefix: "/api/v1/auth/login/mfa", Rate: 1, Burst: 5},
		},
	}
	mw := shared.RateLimitMiddleware(cfg, testLog())
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	login.RemoteAddr = "10.0.0.6:1"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, login)
	if rr.Code != http.StatusOK {
		t.Fatalf("login 1: %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, login)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("login 2 should be 429, got %d", rr.Code)
	}

	mfa := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/mfa", nil)
	mfa.RemoteAddr = "10.0.0.6:1"
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, mfa)
	if rr.Code != http.StatusOK {
		t.Fatalf("mfa should use its own budget, got %d", rr.Code)
	}
}
