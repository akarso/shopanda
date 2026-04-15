package http

import (
	"net/http"
)

// DocsHandler serves the OpenAPI spec and a Swagger UI page.
type DocsHandler struct {
	specBytes []byte
}

// NewDocsHandler creates a DocsHandler. specBytes is the raw openapi.yaml content.
func NewDocsHandler(specBytes []byte) *DocsHandler {
	return &DocsHandler{specBytes: specBytes}
}

// Spec serves GET /docs/openapi.yaml — the raw OpenAPI spec.
func (h *DocsHandler) Spec() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.WriteHeader(http.StatusOK)
		w.Write(h.specBytes)
	}
}

// UI serves GET /docs — an HTML page that loads Swagger UI from a CDN
// and points it at the local openapi.yaml endpoint.
func (h *DocsHandler) UI() http.HandlerFunc {
	page := []byte(swaggerHTML)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(page)
	}
}

const swaggerHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Shopanda API Documentation</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
  <style>
    html { box-sizing: border-box; overflow-y: scroll; }
    *, *::before, *::after { box-sizing: inherit; }
    body { margin: 0; background: #fafafa; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    SwaggerUIBundle({
      url: "/docs/openapi.yaml",
      dom_id: "#swagger-ui",
      deepLinking: true,
      presets: [
        SwaggerUIBundle.presets.apis,
        SwaggerUIBundle.SwaggerUIStandalonePreset
      ],
      layout: "BaseLayout"
    });
  </script>
</body>
</html>`
