package requestctx

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"unicode"
)

type ctxKey string

const requestIDKey ctxKey = "request_id"

// MaxRequestIDLen caps inbound X-Request-ID before it is stored on context,
// echoed in responses, persisted into job payloads, or written to logs.
const MaxRequestIDLen = 128

// WithRequestID stores a request ID in the context.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID extracts the request ID from the context, or empty string.
func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// Middleware injects a request ID into each request's context.
// Uses X-Request-ID when present and safe (length + charset); otherwise
// generates a new one. Oversized or hostile client IDs must not reach job
// payloads or log fields.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := normalizeRequestID(r.Header.Get("X-Request-ID"))
		if id == "" {
			id = generateID()
		}
		ctx := WithRequestID(r.Context(), id)
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// normalizeRequestID accepts a client-supplied ID only when it is non-empty,
// within MaxRequestIDLen, and uses a safe printable charset (letters, digits,
// and common separators). Invalid values return "" so Middleware generates.
func normalizeRequestID(id string) string {
	if id == "" || len(id) > MaxRequestIDLen {
		return ""
	}
	for _, r := range id {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		switch r {
		case '-', '_', '.', ':', '/':
			continue
		default:
			return ""
		}
	}
	return id
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
