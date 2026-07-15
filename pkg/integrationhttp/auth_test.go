package integrationhttp_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/integrationhttp"
)

func TestSecureHandler_APIKeyOnly(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		integrationhttp.WriteJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	})
	handler := integrationhttp.SecureHandler(integrationhttp.AuthConfig{APIKey: "secret-key"}, next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/acme/order-status", nil)
	req.Header.Set(extapi.IntegrationHeaderAPIKey, "secret-key")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestSecureHandler_InvalidAPIKey(t *testing.T) {
	handler := integrationhttp.SecureHandler(integrationhttp.AuthConfig{APIKey: "secret-key"}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set(extapi.IntegrationHeaderAPIKey, "wrong")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), extapi.IntegrationErrorAuthInvalidKey) {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestSecureHandler_HMACValid(t *testing.T) {
	replay := integrationhttp.NewMemoryReplayStore()
	now := time.Unix(1_700_000_000, 0)
	body := []byte(`{"order_id":"100"}`)
	handler := integrationhttp.SecureHandler(integrationhttp.AuthConfig{
		APIKey:      "secret-key",
		HMACSecret:  "hmac-secret",
		ReplayStore: replay,
		PluginSlug:  "acme",
		Now:         func() time.Time { return now },
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		integrationhttp.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/acme/order-status", bytes.NewReader(body))
	integrationhttp.SignRequest(req, body, "secret-key", "hmac-secret", now.Unix(), "nonce-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestSecureHandler_HMACReplayRejected(t *testing.T) {
	replay := integrationhttp.NewMemoryReplayStore()
	now := time.Unix(1_700_000_000, 0)
	body := []byte(`{"order_id":"100"}`)
	cfg := integrationhttp.AuthConfig{
		APIKey:      "secret-key",
		HMACSecret:  "hmac-secret",
		ReplayStore: replay,
		PluginSlug:  "acme",
		Now:         func() time.Time { return now },
	}
	handler := integrationhttp.SecureHandler(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	makeReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/acme/order-status", bytes.NewReader(body))
		integrationhttp.SignRequest(req, body, "secret-key", "hmac-secret", now.Unix(), "nonce-dup")
		return req
	}

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, makeReq())
	if rec1.Code != http.StatusOK {
		t.Fatalf("first status = %d body = %s", rec1.Code, rec1.Body.String())
	}

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, makeReq())
	if rec2.Code != http.StatusUnauthorized || !strings.Contains(rec2.Body.String(), extapi.IntegrationErrorAuthReplayDetected) {
		t.Fatalf("second status = %d body = %s", rec2.Code, rec2.Body.String())
	}
}

func TestSecureHandler_ExpiredTimestamp(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	body := []byte(`{}`)
	handler := integrationhttp.SecureHandler(integrationhttp.AuthConfig{
		APIKey:     "secret-key",
		HMACSecret: "hmac-secret",
		Now:        func() time.Time { return now },
	}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/acme/x", bytes.NewReader(body))
	integrationhttp.SignRequest(req, body, "secret-key", "hmac-secret", now.Add(-10*time.Minute).Unix(), "nonce-old")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), extapi.IntegrationErrorAuthExpiredTimestamp) {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestMemoryReplayStore_Seen(t *testing.T) {
	store := integrationhttp.NewMemoryReplayStore()
	ctx := context.Background()
	exp := time.Now().Add(time.Minute)
	if seen, err := store.Seen(ctx, "acme", "n1", "sig1", time.Now(), exp); err != nil || seen {
		t.Fatalf("seen = %v err = %v", seen, err)
	}
	if err := store.Remember(ctx, "acme", "n1", "sig1", exp); err != nil {
		t.Fatalf("Remember: %v", err)
	}
	if seen, err := store.Seen(ctx, "acme", "n1", "sig1", time.Now(), exp); err != nil || !seen {
		t.Fatalf("seen after remember = %v err = %v", seen, err)
	}
}
