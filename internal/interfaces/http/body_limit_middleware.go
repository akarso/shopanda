package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/akarso/shopanda/internal/platform/config"
)

// BodyLimitMiddleware applies route-aware MaxBytesReader limits.
// Media upload paths use mediaLimit; all other body-bearing methods use defaultLimit.
// The two caps are independent — media may be lower or higher than the default.
//
// Oversized bodies yield HTTP 413 with error code "payload_too_large" when the
// limit is hit before the handler has written a response.
//
// MaxBytesReader is bound to the unwrapped ResponseWriter so net/http can run
// requestTooLarge(); the synthetic 413 is written through a guarded wrapper so
// handlers cannot overwrite it.
func BodyLimitMiddleware(defaultLimit, mediaLimit int64) Middleware {
	if defaultLimit <= 0 {
		defaultLimit = config.DefaultHTTPMaxBodyBytes
	}
	if mediaLimit <= 0 {
		mediaLimit = config.DefaultHTTPMediaMaxBodyBytes
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !methodMayHaveBody(r.Method) || r.Body == nil {
				next.ServeHTTP(w, r)
				return
			}
			limit := defaultLimit
			if isMediaUploadPath(r.Method, r.URL.Path) {
				limit = mediaLimit
			}
			// Bind MaxBytesReader to the server writer (unwrap wrappers) so
			// net/http.requestTooLarge runs; write the JSON 413 via gw.
			gw := &guardedWriter{ResponseWriter: w}
			limited := http.MaxBytesReader(unwrapResponseWriter(w), r.Body, limit)
			r.Body = &bodyLimitReader{
				ReadCloser: limited,
				w:          gw,
			}
			next.ServeHTTP(gw, r)
		})
	}
}

func unwrapResponseWriter(w http.ResponseWriter) http.ResponseWriter {
	for {
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return w
		}
		next := u.Unwrap()
		if next == nil || next == w {
			return w
		}
		w = next
	}
}

func methodMayHaveBody(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isMediaUploadPath(method, path string) bool {
	if method != http.MethodPost {
		return false
	}
	return path == "/api/v1/admin/media" || path == "/api/v1/admin/media/upload"
}

type guardedWriter struct {
	http.ResponseWriter
	mu     sync.Mutex
	wrote  bool
	locked bool // discard further writes after body-too-large response
}

func (g *guardedWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }

func (g *guardedWriter) WriteHeader(code int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.wrote || g.locked {
		return
	}
	g.wrote = true
	g.ResponseWriter.WriteHeader(code)
}

func (g *guardedWriter) Write(b []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.locked {
		return len(b), nil
	}
	if !g.wrote {
		g.wrote = true
		g.ResponseWriter.WriteHeader(http.StatusOK)
	}
	return g.ResponseWriter.Write(b)
}

func (g *guardedWriter) Written() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.wrote || g.locked
}

func (g *guardedWriter) lock() {
	g.mu.Lock()
	g.locked = true
	g.mu.Unlock()
}

type bodyLimitReader struct {
	io.ReadCloser
	w *guardedWriter
}

func (b *bodyLimitReader) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == nil {
		return n, nil
	}
	var maxErr *http.MaxBytesError
	if !errors.As(err, &maxErr) {
		return n, err
	}
	if b.w != nil && !b.w.Written() {
		writeBodyTooLarge(b.w)
		b.w.lock()
	}
	return n, err
}

func writeBodyTooLarge(w http.ResponseWriter) {
	const code = "payload_too_large"
	const msg = "request body too large"
	body, err := json.Marshal(Response{
		Error: &ErrorBody{
			Code:    code,
			Message: msg,
		},
	})
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		_, _ = w.Write([]byte(`{"data":null,"error":{"code":"payload_too_large","message":"request body too large"}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Connection", "close")
	w.WriteHeader(http.StatusRequestEntityTooLarge)
	_, _ = w.Write(body)
}
