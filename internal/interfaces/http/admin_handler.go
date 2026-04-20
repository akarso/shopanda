package http

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
)

//go:embed admin/dist/*
var adminFS embed.FS

// AdminHandler serves the embedded admin SPA files at /admin.
type AdminHandler struct {
	fileServer http.Handler
	index      []byte
}

// NewAdminHandler creates an AdminHandler that serves the embedded admin files.
func NewAdminHandler() (*AdminHandler, error) {
	sub, err := fs.Sub(adminFS, "admin/dist")
	if err != nil {
		return nil, err
	}

	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return nil, err
	}

	return &AdminHandler{
		fileServer: http.FileServer(http.FS(sub)),
		index:      index,
	}, nil
}

// ServeHTTP handles requests to /admin/*. Known static files are served
// directly; all other paths return index.html for client-side routing.
func (h *AdminHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Strip the /admin prefix so the file server sees relative paths.
	urlPath := r.URL.Path
	if urlPath == "/admin" {
		urlPath = "/"
	} else {
		urlPath = urlPath[len("/admin"):]
	}

	// Check whether the path maps to an actual embedded file.
	if urlPath != "/" {
		clean := path.Clean(urlPath[1:]) // remove leading slash
		sub, _ := fs.Sub(adminFS, "admin/dist")
		if f, err := sub.(fs.ReadFileFS).ReadFile(clean); err == nil && f != nil {
			// Serve the static file.
			r2 := r.Clone(r.Context())
			r2.URL.Path = urlPath
			h.fileServer.ServeHTTP(w, r2)
			return
		}
	}

	// SPA fallback — return index.html for client-side routing.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(h.index)
}
