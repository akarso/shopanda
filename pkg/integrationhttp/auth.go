package integrationhttp

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/akarso/shopanda/pkg/extapi"
)

const defaultFreshnessWindow = 5 * time.Minute

// AuthConfig configures integration inbound auth middleware.
type AuthConfig struct {
	APIKey          string
	HMACSecret      string
	FreshnessWindow time.Duration
	ReplayStore     ReplayStore
	MaxBodyBytes    int64
	PluginSlug      string
	Now             func() time.Time
}

// SecureHandler wraps next with API-key auth and optional HMAC + replay protection.
func SecureHandler(cfg AuthConfig, next http.Handler) http.Handler {
	if next == nil {
		panic("integrationhttp: secure handler next must not be nil")
	}
	if cfg.ReplayStore == nil {
		cfg.ReplayStore = NewMemoryReplayStore()
	}
	if cfg.FreshnessWindow <= 0 {
		cfg.FreshnessWindow = defaultFreshnessWindow
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = defaultMaxBodyBytes
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := authenticateRequest(r, cfg); err != nil {
			WriteError(w, err.status, err.code, err.message, err.details)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type authError struct {
	status  int
	code    string
	message string
	details map[string]interface{}
}

func authenticateRequest(r *http.Request, cfg AuthConfig) *authError {
	key := extractAPIKey(r)
	if key == "" {
		return &authError{http.StatusUnauthorized, extapi.IntegrationErrorAuthMissingKey, "integration API key required", nil}
	}
	if cfg.APIKey == "" || subtle.ConstantTimeCompare([]byte(key), []byte(cfg.APIKey)) != 1 {
		return &authError{http.StatusUnauthorized, extapi.IntegrationErrorAuthInvalidKey, "invalid integration API key", nil}
	}
	if strings.TrimSpace(cfg.HMACSecret) == "" {
		return nil
	}
	return verifyHMACRequest(r, cfg)
}

func verifyHMACRequest(r *http.Request, cfg AuthConfig) *authError {
	sig := strings.TrimSpace(r.Header.Get(extapi.IntegrationHeaderSignature))
	if sig == "" {
		return &authError{http.StatusUnauthorized, extapi.IntegrationErrorAuthMissingSignature, "integration signature required", nil}
	}
	tsRaw := strings.TrimSpace(r.Header.Get(extapi.IntegrationHeaderTimestamp))
	if tsRaw == "" {
		return &authError{http.StatusUnauthorized, extapi.IntegrationErrorAuthExpiredTimestamp, "integration timestamp required", nil}
	}
	ts, err := strconv.ParseInt(tsRaw, 10, 64)
	if err != nil {
		return &authError{http.StatusUnauthorized, extapi.IntegrationErrorAuthExpiredTimestamp, "integration timestamp invalid", nil}
	}
	nonce := strings.TrimSpace(r.Header.Get(extapi.IntegrationHeaderNonce))
	if nonce == "" {
		return &authError{http.StatusUnauthorized, extapi.IntegrationErrorAuthMissingSignature, "integration nonce required", nil}
	}

	now := cfg.Now()
	reqTime := time.Unix(ts, 0)
	if reqTime.After(now.Add(cfg.FreshnessWindow)) || reqTime.Before(now.Add(-cfg.FreshnessWindow)) {
		return &authError{http.StatusUnauthorized, extapi.IntegrationErrorAuthExpiredTimestamp, "integration timestamp outside freshness window", nil}
	}

	body, err := readBody(r, cfg.MaxBodyBytes)
	if err != nil {
		return &authError{http.StatusBadRequest, "invalid_payload", "unable to read request body", nil}
	}

	expected := ComputeHMACSignature(cfg.HMACSecret, CanonicalPayload(r.Method, r.URL.Path, ts, nonce, body))
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(sig)), []byte(expected)) != 1 {
		return &authError{http.StatusUnauthorized, extapi.IntegrationErrorAuthInvalidSignature, "invalid integration signature", nil}
	}

	namespace := cfg.PluginSlug
	if namespace == "" {
		namespace = "default"
	}
	expiresAt := now.Add(cfg.FreshnessWindow)
	ctx := r.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	seen, err := cfg.ReplayStore.Seen(ctx, namespace, nonce, sig, now, expiresAt)
	if err != nil {
		return &authError{http.StatusInternalServerError, "internal", "replay check failed", nil}
	}
	if seen {
		return &authError{http.StatusUnauthorized, extapi.IntegrationErrorAuthReplayDetected, "integration request replay detected", nil}
	}
	if err := cfg.ReplayStore.Remember(ctx, namespace, nonce, sig, expiresAt); err != nil {
		return &authError{http.StatusInternalServerError, "internal", "replay store failed", nil}
	}
	return nil
}

func extractAPIKey(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get(extapi.IntegrationHeaderAPIKey)); v != "" {
		return v
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

func readBody(r *http.Request, maxBytes int64) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("body too large")
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// CanonicalPayload builds the HMAC signing payload for inbound integration requests.
func CanonicalPayload(method, path string, timestamp int64, nonce string, body []byte) string {
	sum := sha256.Sum256(body)
	return strings.Join([]string{
		strings.ToUpper(strings.TrimSpace(method)),
		path,
		strconv.FormatInt(timestamp, 10),
		nonce,
		hex.EncodeToString(sum[:]),
	}, "\n")
}

// ComputeHMACSignature returns the lowercase hex HMAC-SHA256 signature.
func ComputeHMACSignature(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// SignRequest sets integration auth headers for tests and ERP clients.
func SignRequest(r *http.Request, body []byte, apiKey, hmacSecret string, ts int64, nonce string) {
	if apiKey != "" {
		r.Header.Set(extapi.IntegrationHeaderAPIKey, apiKey)
	}
	if strings.TrimSpace(hmacSecret) == "" {
		return
	}
	r.Header.Set(extapi.IntegrationHeaderTimestamp, strconv.FormatInt(ts, 10))
	r.Header.Set(extapi.IntegrationHeaderNonce, nonce)
	payload := CanonicalPayload(r.Method, r.URL.Path, ts, nonce, body)
	r.Header.Set(extapi.IntegrationHeaderSignature, ComputeHMACSignature(hmacSecret, payload))
}
