package http

import (
	"net/http"

	portsapp "github.com/akarso/shopanda/internal/application/ports"
)

// ExtensionPortAdminHandler serves read-only infrastructure port introspection.
type ExtensionPortAdminHandler struct {
	snapshot portsapp.Snapshot
}

// NewExtensionPortAdminHandler creates an ExtensionPortAdminHandler.
func NewExtensionPortAdminHandler(snapshot portsapp.Snapshot) *ExtensionPortAdminHandler {
	return &ExtensionPortAdminHandler{snapshot: snapshot}
}

type portCatalogResponse struct {
	Name           string                        `json:"name"`
	RegisterAPI    string                        `json:"register_api"`
	ConfigKey      string                        `json:"config_key,omitempty"`
	Status         portsapp.Status               `json:"status"`
	Source         string                        `json:"source,omitempty"`
	Driver         string                        `json:"driver,omitempty"`
	Implementation string                        `json:"implementation,omitempty"`
	Notes          string                        `json:"notes,omitempty"`
	Providers      []portsapp.ProviderDetail       `json:"providers,omitempty"`
}

// ListPorts handles GET /api/v1/admin/extensions/ports.
func (h *ExtensionPortAdminHandler) ListPorts() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out := make([]portCatalogResponse, 0, len(h.snapshot.Ports))
		for _, port := range h.snapshot.Ports {
			out = append(out, portCatalogResponse{
				Name:           port.Name,
				RegisterAPI:    port.RegisterAPI,
				ConfigKey:      port.ConfigKey,
				Status:         port.Status,
				Source:         port.Source,
				Driver:         port.Driver,
				Implementation: port.Implementation,
				Notes:          port.Notes,
				Providers:      port.Providers,
			})
		}
		JSON(w, http.StatusOK, map[string]interface{}{"ports": out})
	}
}
