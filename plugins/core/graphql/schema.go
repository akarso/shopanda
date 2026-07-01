package graphql

import (
	"fmt"

	gql "github.com/graphql-go/graphql"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

// NewSchema builds the read-only catalog GraphQL schema.
func NewSchema(r *Resolver) (gql.Schema, error) {
	if r == nil {
		return gql.Schema{}, fmt.Errorf("graphql schema: resolver must not be nil")
	}

	productType := gql.NewObject(gql.ObjectConfig{
		Name: "Product",
		Fields: gql.Fields{
			"id":          &gql.Field{Type: gql.NewNonNull(gql.ID)},
			"name":        &gql.Field{Type: gql.NewNonNull(gql.String)},
			"slug":        &gql.Field{Type: gql.NewNonNull(gql.String)},
			"description": &gql.Field{Type: gql.String},
			"status":      &gql.Field{Type: gql.NewNonNull(gql.String)},
		},
	})

	categoryType := gql.NewObject(gql.ObjectConfig{
		Name: "Category",
		Fields: gql.Fields{
			"id":       &gql.Field{Type: gql.NewNonNull(gql.ID)},
			"name":     &gql.Field{Type: gql.NewNonNull(gql.String)},
			"slug":     &gql.Field{Type: gql.NewNonNull(gql.String)},
			"parentId": &gql.Field{Type: gql.ID},
			"position": &gql.Field{Type: gql.NewNonNull(gql.Int)},
		},
	})

	queryType := gql.NewObject(gql.ObjectConfig{
		Name: "Query",
		Fields: gql.Fields{
			"product": &gql.Field{
				Type: productType,
				Args: gql.FieldConfigArgument{
					"id": &gql.ArgumentConfig{Type: gql.NewNonNull(gql.ID)},
				},
				Resolve: func(p gql.ResolveParams) (interface{}, error) {
					id, err := stringArg(p.Args, "id")
					if err != nil {
						return nil, err
					}
					prod, err := r.productByID(p.Context, id)
					if err != nil || prod == nil {
						return nil, err
					}
					pg := productToGraph(*prod)
					return &pg, nil
				},
			},
			"productBySlug": &gql.Field{
				Type: productType,
				Args: gql.FieldConfigArgument{
					"slug": &gql.ArgumentConfig{Type: gql.NewNonNull(gql.String)},
				},
				Resolve: func(p gql.ResolveParams) (interface{}, error) {
					slug, err := stringArg(p.Args, "slug")
					if err != nil {
						return nil, err
					}
					prod, err := r.productBySlug(p.Context, slug)
					if err != nil || prod == nil {
						return nil, err
					}
					pg := productToGraph(*prod)
					return &pg, nil
				},
			},
			"products": &gql.Field{
				Type: gql.NewList(gql.NewNonNull(productType)),
				Args: gql.FieldConfigArgument{
					"offset": &gql.ArgumentConfig{Type: gql.Int},
					"limit":  &gql.ArgumentConfig{Type: gql.Int},
				},
				Resolve: func(p gql.ResolveParams) (interface{}, error) {
					offset, err := intArg(p.Args, "offset", 0)
					if err != nil {
						return nil, err
					}
					limit, err := intArg(p.Args, "limit", defaultListLimit)
					if err != nil {
						return nil, err
					}
					items, err := r.products(p.Context, offset, limit)
					if err != nil {
						return nil, err
					}
					return productsToGraph(items), nil
				},
			},
			"category": &gql.Field{
				Type: categoryType,
				Args: gql.FieldConfigArgument{
					"id": &gql.ArgumentConfig{Type: gql.NewNonNull(gql.ID)},
				},
				Resolve: func(p gql.ResolveParams) (interface{}, error) {
					id, err := stringArg(p.Args, "id")
					if err != nil {
						return nil, err
					}
					cat, err := r.categoryByID(p.Context, id)
					if err != nil || cat == nil {
						return nil, err
					}
					cg := categoryToGraph(*cat)
					return &cg, nil
				},
			},
			"categories": &gql.Field{
				Type: gql.NewList(gql.NewNonNull(categoryType)),
				Resolve: func(p gql.ResolveParams) (interface{}, error) {
					items, err := r.categories(p.Context)
					if err != nil {
						return nil, err
					}
					return categoriesToGraph(items), nil
				},
			},
			"categoryProducts": &gql.Field{
				Type: gql.NewList(gql.NewNonNull(productType)),
				Args: gql.FieldConfigArgument{
					"categoryId": &gql.ArgumentConfig{Type: gql.NewNonNull(gql.ID)},
					"offset":     &gql.ArgumentConfig{Type: gql.Int},
					"limit":      &gql.ArgumentConfig{Type: gql.Int},
				},
				Resolve: func(p gql.ResolveParams) (interface{}, error) {
					categoryID, err := stringArg(p.Args, "categoryId")
					if err != nil {
						return nil, err
					}
					offset, err := intArg(p.Args, "offset", 0)
					if err != nil {
						return nil, err
					}
					limit, err := intArg(p.Args, "limit", defaultListLimit)
					if err != nil {
						return nil, err
					}
					items, err := r.categoryProducts(p.Context, categoryID, offset, limit)
					if err != nil {
						return nil, err
					}
					return productsToGraph(items), nil
				},
			},
		},
	})

	return gql.NewSchema(gql.SchemaConfig{Query: queryType})
}

type productGraph struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

type categoryGraph struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Slug     string  `json:"slug"`
	ParentID *string `json:"parentId"`
	Position int     `json:"position"`
}

func productToGraph(p catalog.Product) productGraph {
	return productGraph{
		ID:          p.ID,
		Name:        p.Name,
		Slug:        p.Slug,
		Description: p.Description,
		Status:      string(p.Status),
	}
}

func productsToGraph(items []catalog.Product) []productGraph {
	out := make([]productGraph, len(items))
	for i, p := range items {
		out[i] = productToGraph(p)
	}
	return out
}

func categoryToGraph(c catalog.Category) categoryGraph {
	return categoryGraph{
		ID:       c.ID,
		Name:     c.Name,
		Slug:     c.Slug,
		ParentID: c.ParentID,
		Position: c.Position,
	}
}

func categoriesToGraph(items []catalog.Category) []categoryGraph {
	out := make([]categoryGraph, len(items))
	for i, c := range items {
		out[i] = categoryToGraph(c)
	}
	return out
}
