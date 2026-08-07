package integrationhttp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/akarso/shopanda/pkg/extapi"
)

const defaultIdempotencyTTL = 24 * time.Hour
const idempotencyInProgressTTL = 5 * time.Minute

var (
	// ErrIdempotencyConflict indicates the same key was used with a different payload.
	ErrIdempotencyConflict = errors.New("idempotency conflict")
	// ErrIdempotencyInProgress indicates another request with the same key is in flight.
	ErrIdempotencyInProgress = errors.New("idempotency in progress")
)

// IdempotencyRecord is a stored integration response for replay.
type IdempotencyRecord struct {
	RequestHash  string
	StatusCode   int
	ResponseBody []byte
	Completed    bool
	CreatedAt    time.Time
}

// IdempotencyStore persists inbound integration idempotency keys.
type IdempotencyStore interface {
	Begin(ctx context.Context, plugin, key, method, path, requestHash string, expiresAt time.Time) (*IdempotencyRecord, bool, error)
	Complete(ctx context.Context, plugin, key string, statusCode int, body []byte) error
}

// IdempotencyConfig configures idempotency middleware.
type IdempotencyConfig struct {
	Store        IdempotencyStore
	PluginSlug   string
	TTL          time.Duration
	Now          func() time.Time
	MaxBodyBytes int64
}

// IdempotentMethods reports whether HTTP method should participate in idempotency when a key is present.
func IdempotentMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// IdempotencyHandler deduplicates mutating requests that include Idempotency-Key.
func IdempotencyHandler(cfg IdempotencyConfig, next http.Handler) http.Handler {
	if next == nil {
		panic("integrationhttp: idempotency handler next must not be nil")
	}
	if cfg.Store == nil {
		return next
	}
	if cfg.TTL <= 0 {
		cfg.TTL = defaultIdempotencyTTL
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = defaultMaxBodyBytes
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IdempotentMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		key := strings.TrimSpace(r.Header.Get(extapi.IntegrationHeaderIdempotencyKey))
		if key == "" {
			next.ServeHTTP(w, r)
			return
		}
		body, err := readBody(r, cfg.MaxBodyBytes)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_payload", "unable to read request body", nil)
			return
		}
		hash := hashRequestBody(body)
		plugin := cfg.PluginSlug
		if plugin == "" {
			plugin = "default"
		}
		ctx := r.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		expiresAt := cfg.Now().Add(cfg.TTL)
		record, claimed, err := cfg.Store.Begin(ctx, plugin, key, r.Method, r.URL.Path, hash, expiresAt)
		if err != nil {
			if errors.Is(err, ErrIdempotencyConflict) {
				WriteError(w, http.StatusConflict, extapi.IntegrationErrorIdempotencyConflict, "idempotency key reused with different request", nil)
				return
			}
			if errors.Is(err, ErrIdempotencyInProgress) {
				WriteError(w, http.StatusConflict, extapi.IntegrationErrorIdempotencyInProgress, "idempotency key already in progress", nil)
				return
			}
			WriteError(w, http.StatusInternalServerError, "internal", "idempotency check failed", nil)
			return
		}
		if !claimed {
			if record == nil || !record.Completed {
				WriteError(w, http.StatusConflict, extapi.IntegrationErrorIdempotencyInProgress, "idempotency key already in progress", nil)
				return
			}
			for k, v := range replayHeaders(record.StatusCode) {
				w.Header().Set(k, v)
			}
			w.WriteHeader(record.StatusCode)
			if len(record.ResponseBody) > 0 {
				_, _ = w.Write(record.ResponseBody)
			}
			return
		}

		capture := &responseCapture{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(capture, r)
		status := capture.status
		if status == 0 {
			status = http.StatusOK
		}
		_ = cfg.Store.Complete(ctx, plugin, key, status, capture.body.Bytes())
	})
}

func replayHeaders(statusCode int) map[string]string {
	return map[string]string{
		"Content-Type":           "application/json; charset=utf-8",
		"X-Idempotency-Replayed": "true",
	}
}

func hashRequestBody(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

type responseCapture struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (w *responseCapture) WriteHeader(statusCode int) {
	w.status = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseCapture) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	_, _ = w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

// MemoryIdempotencyStore is an in-process idempotency store for tests.
type MemoryIdempotencyStore struct {
	mu    sync.Mutex
	items map[string]IdempotencyRecord
}

// NewMemoryIdempotencyStore creates an empty in-memory idempotency store.
func NewMemoryIdempotencyStore() *MemoryIdempotencyStore {
	return &MemoryIdempotencyStore{items: make(map[string]IdempotencyRecord)}
}

func (s *MemoryIdempotencyStore) Begin(_ context.Context, plugin, key, method, path, requestHash string, expiresAt time.Time) (*IdempotencyRecord, bool, error) {
	itemKey := idempotencyItemKey(plugin, key)
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.items[itemKey]; ok {
		if existing.Completed {
			if existing.RequestHash != requestHash {
				return nil, false, ErrIdempotencyConflict
			}
			copy := existing
			return &copy, false, nil
		}
		if now.Sub(existing.CreatedAt) < idempotencyInProgressTTL {
			return nil, false, ErrIdempotencyInProgress
		}
	}
	s.items[itemKey] = IdempotencyRecord{
		RequestHash: requestHash,
		CreatedAt:   now,
		Completed:   false,
	}
	return nil, true, nil
}

func (s *MemoryIdempotencyStore) Complete(_ context.Context, plugin, key string, statusCode int, body []byte) error {
	itemKey := idempotencyItemKey(plugin, key)
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.items[itemKey]
	item.Completed = true
	item.StatusCode = statusCode
	item.ResponseBody = append([]byte(nil), body...)
	s.items[itemKey] = item
	return nil
}

func idempotencyItemKey(plugin, key string) string {
	return plugin + "\x00" + key
}

// ReadBodyForTest exposes body reading for tests outside the package.
func ReadBodyForTest(r *http.Request, maxBytes int64) ([]byte, error) {
	return readBody(r, maxBytes)
}

// HashRequestBodyForTest exposes request hashing for tests.
func HashRequestBodyForTest(body []byte) string {
	return hashRequestBody(body)
}

// DrainBody reads and restores request body for tests.
func DrainBody(r *http.Request, maxBytes int64) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}
