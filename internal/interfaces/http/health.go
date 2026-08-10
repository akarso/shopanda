package http

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// HealthResponse is the JSON body returned by the health and readiness endpoints.
type HealthResponse struct {
	Status string `json:"status"`
}

// HealthHandler returns an http.HandlerFunc for GET /healthz (process liveness).
func HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(HealthResponse{Status: "ok"})
	}
}

// DBPinger is the subset of database/sql.DB used by readiness checks.
type DBPinger interface {
	PingContext(ctx context.Context) error
}

// DefaultReadyTimeout bounds DB ping wait for GET /readyz.
const DefaultReadyTimeout = 2 * time.Second

// ReadyHandler returns GET /readyz — 200 if the DB pings within DefaultReadyTimeout, else 503.
func ReadyHandler(db DBPinger) http.HandlerFunc {
	return ReadyHandlerWithTimeout(db, DefaultReadyTimeout)
}

// ReadyHandlerWithTimeout is like ReadyHandler with an explicit ping timeout.
func ReadyHandlerWithTimeout(db DBPinger, timeout time.Duration) http.HandlerFunc {
	if timeout <= 0 {
		timeout = DefaultReadyTimeout
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if db == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(HealthResponse{Status: "unavailable"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(HealthResponse{Status: "unavailable"})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(HealthResponse{Status: "ok"})
	}
}
