package http

import (
	"encoding/json"
	"net/http"
)

// HealthResponse is the JSON body returned by the health endpoint.
type HealthResponse struct {
	Status string `json:"status"`
}

// HealthHandler returns an http.HandlerFunc for GET /healthz.
func HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(HealthResponse{Status: "ok"})
	}
}
