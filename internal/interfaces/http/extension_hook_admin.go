package http

import (
	"net/http"

	hooksapp "github.com/akarso/shopanda/internal/application/hooks"
)

// ExtensionHookAdminHandler serves read-only hook catalog endpoints.
type ExtensionHookAdminHandler struct {
	hooks *hooksapp.Registry
}

// NewExtensionHookAdminHandler creates an ExtensionHookAdminHandler.
func NewExtensionHookAdminHandler(hooks *hooksapp.Registry) *ExtensionHookAdminHandler {
	if hooks == nil {
		panic("http: hook registry must not be nil")
	}
	return &ExtensionHookAdminHandler{hooks: hooks}
}

type hookCatalogResponse struct {
	Name     string                       `json:"name"`
	Handlers []hookCatalogHandlerResponse `json:"handlers"`
}

type hookCatalogHandlerResponse struct {
	Priority   int    `json:"priority"`
	Registrant string `json:"registrant"`
}

// ListHooks handles GET /api/v1/admin/extensions/hooks.
func (h *ExtensionHookAdminHandler) ListHooks() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		catalog := h.hooks.Catalog()
		out := make([]hookCatalogResponse, 0, len(catalog))
		for _, entry := range catalog {
			handlers := make([]hookCatalogHandlerResponse, 0, len(entry.Handlers))
			for _, handler := range entry.Handlers {
				handlers = append(handlers, hookCatalogHandlerResponse{
					Priority:   handler.Priority,
					Registrant: handler.Registrant,
				})
			}
			out = append(out, hookCatalogResponse{
				Name:     entry.Name,
				Handlers: handlers,
			})
		}
		JSON(w, http.StatusOK, map[string]interface{}{"hooks": out})
	}
}
