package http_test

import (
	"bytes"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func TestSecurityHeadersMiddleware_ExactValues(t *testing.T) {
	mw := shophttp.SecurityHeadersMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Content-Type-Options"); got != shophttp.HeaderContentTypeOptions {
		t.Fatalf("X-Content-Type-Options = %q, want %q", got, shophttp.HeaderContentTypeOptions)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != shophttp.HeaderFrameOptions {
		t.Fatalf("X-Frame-Options = %q, want %q", got, shophttp.HeaderFrameOptions)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != shophttp.HeaderReferrerPolicy {
		t.Fatalf("Referrer-Policy = %q, want %q", got, shophttp.HeaderReferrerPolicy)
	}
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("HSTS on plain HTTP = %q, want absent", got)
	}
}

func TestSecurityHeadersMiddleware_HSTSOnTLS(t *testing.T) {
	mw := shophttp.SecurityHeadersMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "https://shop.test/", nil)
	req.TLS = &tls.ConnectionState{}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got != shophttp.HeaderHSTS {
		t.Fatalf("HSTS = %q, want %q", got, shophttp.HeaderHSTS)
	}
}

func TestSecurityHeadersMiddleware_HSTSOnTrustedForwardedProto(t *testing.T) {
	mw := shophttp.SecurityHeadersMiddleware("10.0.0.0/8")
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://shop.test/", nil)
	req.RemoteAddr = "10.1.2.3:443"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got != shophttp.HeaderHSTS {
		t.Fatalf("HSTS via trusted proto = %q, want %q", got, shophttp.HeaderHSTS)
	}
}

func TestSecurityHeadersMiddleware_HSTSAbsentUntrustedForwardedProto(t *testing.T) {
	mw := shophttp.SecurityHeadersMiddleware("10.0.0.0/8")
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://shop.test/", nil)
	req.RemoteAddr = "203.0.113.9:443"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("HSTS from untrusted peer = %q, want absent", got)
	}
}

func TestBodyLimitMiddleware_OversizedJSON413(t *testing.T) {
	const limit = 64
	mw := shophttp.BodyLimitMiddleware(limit, 10<<20)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	body := bytes.Repeat([]byte("a"), limit+1)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"payload_too_large"`) {
		t.Fatalf("body = %s, want payload_too_large code", rec.Body.String())
	}
}

func TestBodyLimitMiddleware_MediaPathAllowsAboveDefault(t *testing.T) {
	const defaultLimit = 64
	const mediaLimit = 256
	mw := shophttp.BodyLimitMiddleware(defaultLimit, mediaLimit)

	var read int
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("ReadAll: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		read = len(b)
		w.WriteHeader(http.StatusOK)
	}))

	payload := bytes.Repeat([]byte("m"), defaultLimit+50) // > default, < media
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/media/upload", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("media upload status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if read != len(payload) {
		t.Fatalf("read = %d, want %d", read, len(payload))
	}
}

func TestBodyLimitMiddleware_MediaCapBelowDefaultStaysIndependent(t *testing.T) {
	// Media cap lower than JSON default must not be raised.
	mw := shophttp.BodyLimitMiddleware(512, 64)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	body := bytes.Repeat([]byte("x"), 65)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/media/upload", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (media cap 64 independent of default 512)", rec.Code)
	}
}

func TestBodyLimitMiddleware_MediaPathStillCaps(t *testing.T) {
	mw := shophttp.BodyLimitMiddleware(64, 128)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	body := bytes.Repeat([]byte("x"), 129)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/media", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestBodyLimitMiddleware_LoggedAs413(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "info")
	// Mirror production order: Logging wraps BodyLimit.
	chain := shophttp.LoggingMiddleware(log)(shophttp.BodyLimitMiddleware(32, 10<<20)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusUnprocessableEntity)
		}),
	))

	body := bytes.Repeat([]byte("z"), 40)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("client status = %d, want 413", rec.Code)
	}
}
