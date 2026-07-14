package integrationhttp

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/akarso/shopanda/pkg/extapi"
)

const defaultMaxBodyBytes = 1 << 20 // 1 MiB

// WriteError writes the integration structured error envelope.
func WriteError(w http.ResponseWriter, status int, code, message string, details map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(extapi.IntegrationErrorResponse{
		Error:   code,
		Message: message,
		Details: details,
	})
}

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// DecodeJSON decodes a JSON request body with a size limit.
func DecodeJSON(r *http.Request, maxBytes int64, dst interface{}) error {
	if maxBytes <= 0 {
		maxBytes = defaultMaxBodyBytes
	}
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBytes))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
