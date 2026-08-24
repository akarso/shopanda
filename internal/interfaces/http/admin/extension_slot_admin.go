package admin

import (
	"net/http"
	"sort"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	slotsapp "github.com/akarso/shopanda/internal/application/slots"
)

// ExtensionSlotAdminHandler serves read-only slot catalog endpoints.
type ExtensionSlotAdminHandler struct {
	slots *slotsapp.Registry
}

// NewExtensionSlotAdminHandler creates an ExtensionSlotAdminHandler.
func NewExtensionSlotAdminHandler(slots *slotsapp.Registry) *ExtensionSlotAdminHandler {
	if slots == nil {
		panic("http: slot registry must not be nil")
	}
	return &ExtensionSlotAdminHandler{slots: slots}
}

type slotCatalogResponse struct {
	Name        string                       `json:"name"`
	Group       string                       `json:"group,omitempty"`
	Description string                       `json:"description,omitempty"`
	Handlers    []slotCatalogHandlerResponse `json:"handlers"`
}

type slotCatalogHandlerResponse struct {
	Placement  string `json:"placement"`
	Priority   int    `json:"priority"`
	Registrant string `json:"registrant"`
}

// ListSlots handles GET /api/v1/admin/extensions/slots.
func (h *ExtensionSlotAdminHandler) ListSlots() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		catalogMeta := standardSlotCatalogMeta()
		registrations := registrationIndex(h.slots.Catalog())

		out := make([]slotCatalogResponse, 0, len(catalogMeta)+len(registrations))
		seen := make(map[string]struct{}, len(catalogMeta))
		for _, item := range catalogMeta {
			seen[item.Name] = struct{}{}
			handlers := registrations[item.Name]
			if handlers == nil {
				handlers = []slotCatalogHandlerResponse{}
			}
			out = append(out, slotCatalogResponse{
				Name:        item.Name,
				Group:       item.Group,
				Description: item.Description,
				Handlers:    handlers,
			})
		}

		extra := make([]string, 0)
		for name := range registrations {
			if _, ok := seen[name]; ok {
				continue
			}
			extra = append(extra, name)
		}
		sort.Strings(extra)
		for _, name := range extra {
			out = append(out, slotCatalogResponse{
				Name:     name,
				Handlers: registrations[name],
			})
		}

		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"slots": out})
	}
}

func standardSlotCatalogMeta() []slotsapp.StandardAnchor {
	return slotsapp.StandardAnchors()
}

func registrationIndex(catalog []slotsapp.CatalogEntry) map[string][]slotCatalogHandlerResponse {
	out := make(map[string][]slotCatalogHandlerResponse)
	for _, entry := range catalog {
		handlers := make([]slotCatalogHandlerResponse, 0, len(entry.Handlers))
		for _, handler := range entry.Handlers {
			handlers = append(handlers, slotCatalogHandlerResponse{
				Placement:  string(handler.Placement),
				Priority:   handler.Priority,
				Registrant: handler.Registrant,
			})
		}
		out[entry.Name] = handlers
	}
	return out
}
