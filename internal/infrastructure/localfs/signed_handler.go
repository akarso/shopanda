package localfs

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/akarso/shopanda/internal/domain/media"
)

// Handler serves GET /api/v1/media/local-signed/{token}.
func (s *Storage) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		token := r.PathValue("token")
		if token == "" {
			token = strings.TrimPrefix(r.URL.Path, SignedPathPrefix)
			token = strings.Trim(token, "/")
		}
		if token == "" || strings.Contains(token, "/") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		path, filename, err := s.ParseToken(token)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		data, err := s.Read(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || media.IsNotFound(err) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		if filename != "" {
			w.Header().Set("Content-Disposition", media.ContentDispositionAttachment(filename))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})
}
