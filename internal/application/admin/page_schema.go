package admin

import "github.com/akarso/shopanda/internal/domain/admin"

// RegisterPageSchemas registers the page form and grid with the admin registry.
func RegisterPageSchemas(r *admin.Registry) {
	r.RegisterForm("page.form", admin.Form{
		Fields: []admin.Field{
			{Name: "title", Type: "text", Label: "Title", Required: true},
			{Name: "slug", Type: "text", Label: "Slug", Required: true},
			{Name: "content", Type: "text", Label: "Content"},
			{
				Name:    "is_active",
				Type:    "checkbox",
				Label:   "Active",
				Default: true,
			},
		},
	})

	r.RegisterGrid("page.grid", admin.Grid{
		Columns: []admin.Column{
			{Name: "id", Label: "ID"},
			{Name: "title", Label: "Title"},
			{Name: "slug", Label: "Slug"},
			{Name: "is_active", Label: "Active"},
			{Name: "created_at", Label: "Created"},
			{Name: "updated_at", Label: "Updated"},
		},
	})
}
