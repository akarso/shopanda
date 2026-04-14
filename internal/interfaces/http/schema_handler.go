package http

import (
	"net/http"

	"github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
)

// SchemaHandler serves admin schema endpoints.
type SchemaHandler struct {
	registry *admin.Registry
}

// NewSchemaHandler creates a SchemaHandler.
func NewSchemaHandler(registry *admin.Registry) *SchemaHandler {
	return &SchemaHandler{registry: registry}
}

// --- JSON-safe response DTOs ---

type optionDTO struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type fieldDTO struct {
	Name     string                 `json:"name"`
	Type     string                 `json:"type"`
	Label    string                 `json:"label"`
	Required bool                   `json:"required,omitempty"`
	Default  interface{}            `json:"default,omitempty"`
	Options  []optionDTO            `json:"options,omitempty"`
	Meta     map[string]interface{} `json:"meta,omitempty"`
}

type formDTO struct {
	Name   string     `json:"name"`
	Fields []fieldDTO `json:"fields"`
}

type columnDTO struct {
	Name  string                 `json:"name"`
	Label string                 `json:"label"`
	Meta  map[string]interface{} `json:"meta,omitempty"`
}

type actionDTO struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

type gridDTO struct {
	Name    string      `json:"name"`
	Columns []columnDTO `json:"columns"`
	Actions []actionDTO `json:"actions,omitempty"`
}

// GetForm handles GET /api/v1/admin/forms/{name}.
func (h *SchemaHandler) GetForm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			JSONError(w, apperror.Validation("form name is required"))
			return
		}

		form, ok := h.registry.Form(name)
		if !ok {
			JSONError(w, apperror.NotFound("form not found"))
			return
		}

		if perm, hasPerm := h.registry.FormPermission(name); hasPerm {
			id := auth.IdentityFrom(r.Context())
			if id.IsGuest() {
				JSONError(w, apperror.Unauthorized("authentication required"))
				return
			}
			if !rbac.HasPermission(id.Role, perm) {
				JSONError(w, apperror.Forbidden("insufficient permissions"))
				return
			}
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"form": toFormDTO(form),
		})
	}
}

// GetGrid handles GET /api/v1/admin/grids/{name}.
func (h *SchemaHandler) GetGrid() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			JSONError(w, apperror.Validation("grid name is required"))
			return
		}

		grid, ok := h.registry.Grid(name)
		if !ok {
			JSONError(w, apperror.NotFound("grid not found"))
			return
		}

		if perm, hasPerm := h.registry.GridPermission(name); hasPerm {
			id := auth.IdentityFrom(r.Context())
			if id.IsGuest() {
				JSONError(w, apperror.Unauthorized("authentication required"))
				return
			}
			if !rbac.HasPermission(id.Role, perm) {
				JSONError(w, apperror.Forbidden("insufficient permissions"))
				return
			}
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"grid": toGridDTO(grid),
		})
	}
}

func toFormDTO(f admin.Form) formDTO {
	fields := make([]fieldDTO, len(f.Fields))
	for i, fld := range f.Fields {
		var opts []optionDTO
		if len(fld.Options) > 0 {
			opts = make([]optionDTO, len(fld.Options))
			for j, o := range fld.Options {
				opts[j] = optionDTO{Label: o.Label, Value: o.Value}
			}
		}
		fields[i] = fieldDTO{
			Name:     fld.Name,
			Type:     fld.Type,
			Label:    fld.Label,
			Required: fld.Required,
			Default:  fld.Default,
			Options:  opts,
			Meta:     fld.Meta,
		}
	}
	return formDTO{Name: f.Name, Fields: fields}
}

func toGridDTO(g admin.Grid) gridDTO {
	cols := make([]columnDTO, len(g.Columns))
	for i, c := range g.Columns {
		cols[i] = columnDTO{Name: c.Name, Label: c.Label, Meta: c.Meta}
	}
	var acts []actionDTO
	if len(g.Actions) > 0 {
		acts = make([]actionDTO, len(g.Actions))
		for i, a := range g.Actions {
			acts[i] = actionDTO{Name: a.Name, Label: a.Label}
		}
	}
	return gridDTO{Name: g.Name, Columns: cols, Actions: acts}
}
