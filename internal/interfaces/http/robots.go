package http

import (
	"fmt"
	"net/http"
)

// RobotsHandler serves GET /robots.txt.
type RobotsHandler struct {
	baseURL string
}

// NewRobotsHandler creates a RobotsHandler.
func NewRobotsHandler(baseURL string) *RobotsHandler {
	return &RobotsHandler{baseURL: baseURL}
}

// Serve handles GET /robots.txt.
func (h *RobotsHandler) Serve() http.HandlerFunc {
	body := fmt.Sprintf("User-agent: *\nAllow: /\n\nSitemap: %s/sitemap.xml\n", h.baseURL)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body))
	}
}
