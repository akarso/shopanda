package admin

import "github.com/akarso/shopanda/internal/domain/admin"

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

// RegisterProductSchemas registers the product form and grid with the admin registry.
func RegisterProductSchemas(r *admin.Registry) {
	r.RegisterForm("product.form", admin.Form{
		Fields: []admin.Field{
			{Name: "name", Type: "text", Label: "Product Name", Required: true, Meta: scopeMeta(scopeTranslatable)},
			{Name: "slug", Type: "text", Label: "Slug", Required: true, Meta: scopeMeta(scopeGlobal)},
			{Name: "description", Type: "text", Label: "Description", Meta: scopeMeta(scopeTranslatable)},
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
			{Name: "status", Label: "Status"},
			{Name: "created_at", Label: "Created"},
			{Name: "updated_at", Label: "Updated"},
		},
	})
}
