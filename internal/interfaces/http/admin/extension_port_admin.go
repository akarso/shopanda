package admin

import (
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
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

// ListPorts handles GET /api/v1/admin/extensions/ports.
func (h *ExtensionPortAdminHandler) ListPorts() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"ports": h.snapshot.Ports})
	}
}
