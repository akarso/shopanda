package localfs

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

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
		if token == "" || strings.Contains(token, "/") || len(token) > MaxSignedTokenBytes {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		path, filename, err := s.ParseToken(token)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		f, err := s.Open(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || media.IsNotFound(err) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		defer f.Close()

		mod := time.Time{}
		if st, err := f.Stat(); err == nil {
			mod = st.ModTime()
		}
		w.Header().Set("Content-Disposition", media.ContentDispositionAttachment(filename))
		// ServeContent sets Content-Type from filename (or octet-stream) and streams.
		http.ServeContent(w, r, filename, mod, f)
	})
}
