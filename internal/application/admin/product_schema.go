package admin

import "github.com/akarso/shopanda/internal/domain/admin"

// RegisterProductSchemas registers the product form and grid with the admin registry.
func RegisterProductSchemas(r *admin.Registry) {
	r.RegisterForm("product.form", admin.Form{
		Fields: []admin.Field{
			{Name: "name", Type: "text", Label: "Product Name", Required: true},
			{Name: "slug", Type: "text", Label: "Slug", Required: true},
			{Name: "description", Type: "text", Label: "Description"},
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
