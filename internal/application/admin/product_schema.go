package admin

import (
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
)

// Field scope values mirror the config field_scopes model so the admin UI can
// render the same scope banner and per-field badges for catalog editing.
const (
	scopeGlobal       = "global"
	scopeTranslatable = "translatable"
	scopeStore        = "store"
)

func scopeMeta(scope string) map[string]interface{} {
	return map[string]interface{}{"scope": scope}
}

func productTypeOptions() []admin.Option {
	types := catalog.AllTypes()
	opts := make([]admin.Option, 0, len(types))
	for _, t := range types {
		opts = append(opts, admin.Option{Label: ProductTypeLabel(t), Value: string(t)})
	}
	return opts
}

// ProductTypeLabel returns the admin UI label for a catalog product type.
func ProductTypeLabel(t catalog.Type) string {
	switch t {
	case catalog.TypeSimple:
		return "Simple"
	case catalog.TypeVirtual:
		return "Virtual"
	case catalog.TypeBundle:
		return "Bundle"
	case catalog.TypeGrouped:
		return "Grouped"
	case catalog.TypeConfigurable:
		return "Configurable"
	case catalog.TypeDownloadable:
		return "Downloadable"
	default:
		return string(t)
	}
}

// RegisterProductFormSection registers a product.form section after validating
// Types entries against catalog.Type.IsValid. Prefer this over Registry.RegisterFormSection
// for product type panels so typos like "bundles" fail at registration.
func RegisterProductFormSection(r *admin.Registry, section admin.FormSection) error {
	types := make([]string, len(section.Types))
	for i, raw := range section.Types {
		t := catalog.Type(strings.TrimSpace(raw))
		if !t.IsValid() {
			return fmt.Errorf("admin: form section %q types[%d]: %s", section.ID, i, catalog.InvalidTypeMessage(t))
		}
		types[i] = string(t)
	}
	section.Types = types
	return r.RegisterFormSection("product.form", section)
}

// RegisterProductSchemas registers the product form and grid with the admin registry.
//
// Type-specific form sections (bundle components, grouped members, linked-child
// eligibility, downloadable files) must register via RegisterProductFormSection
// (not raw Registry.RegisterFormSection) so Types are validated against
// catalog.Type. The admin SPA shows those sections when the type selector
// matches (PR-1053).
func RegisterProductSchemas(r *admin.Registry) {
	r.RegisterForm("product.form", admin.Form{
		Fields: []admin.Field{
			{Name: "name", Type: "text", Label: "Product Name", Required: true, Meta: scopeMeta(scopeTranslatable)},
			{Name: "slug", Type: "text", Label: "Slug", Required: true, Meta: scopeMeta(scopeGlobal)},
			{Name: "description", Type: "text", Label: "Description", Meta: scopeMeta(scopeTranslatable)},
			{
				Name:     "type",
				Type:     "select",
				Label:    "Type",
				Required: true,
				Options:  productTypeOptions(),
				Default:  string(catalog.TypeSimple),
				Meta:     scopeMeta(scopeGlobal),
			},
			{
				Name:  "status",
				Type:  "select",
				Label: "Status",
				Options: []admin.Option{
					{Label: "Draft", Value: "draft"},
					{Label: "Active", Value: "active"},
					{Label: "Archived", Value: "archived"},
				},
				Default: "draft",
				Meta:    scopeMeta(scopeGlobal),
			},
		},
	})

	r.RegisterGrid("product.grid", admin.Grid{
		Columns: []admin.Column{
			{Name: "id", Label: "ID"},
			{Name: "name", Label: "Name"},
			{Name: "slug", Label: "Slug"},
			{Name: "type", Label: "Type"},
			{Name: "status", Label: "Status"},
			{Name: "created_at", Label: "Created"},
			{Name: "updated_at", Label: "Updated"},
		},
	})
}
