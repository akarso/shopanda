package storefront

import (
	"net/http"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	cmsApp "github.com/akarso/shopanda/internal/application/cms"
	"github.com/akarso/shopanda/internal/domain/cms"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// MenuHandler serves public menu read endpoints.
type MenuHandler struct {
	menus    cms.MenuRepository
	resolver *cmsApp.MenuResolver
}

// NewMenuHandler creates a MenuHandler.
func NewMenuHandler(menus cms.MenuRepository, resolver *cmsApp.MenuResolver) *MenuHandler {
	if menus == nil {
		panic("MenuHandler: menus repository must not be nil")
	}
	if resolver == nil {
		panic("MenuHandler: resolver must not be nil")
	}
	return &MenuHandler{menus: menus, resolver: resolver}
}

type publicMenuItemResponse struct {
	Label    string                   `json:"label"`
	URL      string                   `json:"url"`
	Children []publicMenuItemResponse `json:"children,omitempty"`
}

type publicMenuResponse struct {
	Code  string                   `json:"code"`
	Title string                   `json:"title"`
	Items []publicMenuItemResponse `json:"items"`
}

func toPublicMenuItems(items []cmsApp.ResolvedMenuItem) []publicMenuItemResponse {
	out := make([]publicMenuItemResponse, 0, len(items))
	for _, item := range items {
		out = append(out, toPublicMenuItem(item))
	}
	return out
}

func toPublicMenuItem(item cmsApp.ResolvedMenuItem) publicMenuItemResponse {
	resp := publicMenuItemResponse{
		Label: item.Label,
		URL:   item.URL,
	}
	if len(item.Children) > 0 {
		resp.Children = toPublicMenuItems(item.Children)
	}
	return resp
}

// GetByCode handles GET /api/v1/menus/{code}.
func (h *MenuHandler) GetByCode() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.PathValue("code")
		if code == "" {
			httpshared.JSONError(w, apperror.Validation("menu code is required"))
			return
		}
		if !cms.ValidMenuCode(code) {
			httpshared.JSONError(w, apperror.Validation("invalid menu code"))
			return
		}

		data, err := h.menus.FindByCode(r.Context(), code)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		if data == nil || !data.Menu.IsActive() {
			httpshared.JSONError(w, apperror.NotFound("menu not found"))
			return
		}

		tree, err := h.resolver.ResolveTree(r.Context(), data.Items)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}

		httpshared.JSON(w, http.StatusOK, publicMenuResponse{
			Code:  data.Menu.Code(),
			Title: data.Menu.Title(),
			Items: toPublicMenuItems(tree),
		})
	}
}
