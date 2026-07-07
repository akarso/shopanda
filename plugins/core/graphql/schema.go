package graphql

import (
	"fmt"

	gql "github.com/graphql-go/graphql"

	"github.com/akarso/shopanda/internal/domain/catalog"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
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

	extensionFieldType := gql.NewObject(gql.ObjectConfig{
		Name: "ExtensionField",
		Fields: gql.Fields{
			"code":        &gql.Field{Type: gql.NewNonNull(gql.String)},
			"label":       &gql.Field{Type: gql.NewNonNull(gql.String)},
			"description": &gql.Field{Type: gql.String},
			"type":        &gql.Field{Type: gql.NewNonNull(gql.String)},
			"scope":       &gql.Field{Type: gql.NewNonNull(gql.String)},
			"storageMode": &gql.Field{Type: gql.NewNonNull(gql.String)},
			"visibility":  &gql.Field{Type: gql.NewNonNull(gql.String)},
		},
	})

	jsonScalar := newJSONScalar()

	extensionValueType := gql.NewObject(gql.ObjectConfig{
		Name: "ExtensionValue",
		Fields: gql.Fields{
			"fieldCode": &gql.Field{Type: gql.NewNonNull(gql.String)},
			"type":      &gql.Field{Type: gql.NewNonNull(gql.String)},
			"value":     &gql.Field{Type: jsonScalar},
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
			"extensionFields": &gql.Field{
				Type: gql.NewList(gql.NewNonNull(extensionFieldType)),
				Args: gql.FieldConfigArgument{
					"scope":          &gql.ArgumentConfig{Type: gql.String},
					"includePrivate": &gql.ArgumentConfig{Type: gql.Boolean},
				},
				Resolve: func(p gql.ResolveParams) (interface{}, error) {
					scope, _ := p.Args["scope"].(string)
					includePrivate, err := boolArg(p.Args, "includePrivate", false)
					if err != nil {
						return nil, err
					}
					fields, err := r.extensionFields(p.Context, scope, includePrivate)
					if err != nil {
						return nil, err
					}
					return extensionFieldsToGraph(fields), nil
				},
			},
			"extensionValues": &gql.Field{
				Type: gql.NewList(gql.NewNonNull(extensionValueType)),
				Args: gql.FieldConfigArgument{
					"targetType":     &gql.ArgumentConfig{Type: gql.NewNonNull(gql.String)},
					"targetId":       &gql.ArgumentConfig{Type: gql.NewNonNull(gql.ID)},
					"includePrivate": &gql.ArgumentConfig{Type: gql.Boolean},
				},
				Resolve: func(p gql.ResolveParams) (interface{}, error) {
					targetType, err := stringArg(p.Args, "targetType")
					if err != nil {
						return nil, err
					}
					targetID, err := stringArg(p.Args, "targetId")
					if err != nil {
						return nil, err
					}
					includePrivate, err := boolArg(p.Args, "includePrivate", false)
					if err != nil {
						return nil, err
					}
					values, err := r.extensionValues(p.Context, targetType, targetID, includePrivate)
					if err != nil {
						return nil, err
					}
					return extensionValuesToGraph(values, r), nil
				},
			},
		},
	})

	extensionValueInputType := gql.NewInputObject(gql.InputObjectConfig{
		Name: "ExtensionValueInput",
		Fields: gql.InputObjectConfigFieldMap{
			"fieldCode": &gql.InputObjectFieldConfig{Type: gql.NewNonNull(gql.String)},
			"value":     &gql.InputObjectFieldConfig{Type: gql.NewNonNull(jsonScalar)},
		},
	})

	mutationType := gql.NewObject(gql.ObjectConfig{
		Name: "Mutation",
		Fields: gql.Fields{
			"upsertExtensionValues": &gql.Field{
				Type: gql.NewList(gql.NewNonNull(extensionValueType)),
				Args: gql.FieldConfigArgument{
					"targetType": &gql.ArgumentConfig{Type: gql.NewNonNull(gql.String)},
					"targetId":   &gql.ArgumentConfig{Type: gql.NewNonNull(gql.ID)},
					"values":     &gql.ArgumentConfig{Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(extensionValueInputType)))},
				},
				Resolve: func(p gql.ResolveParams) (interface{}, error) {
					targetType, err := stringArg(p.Args, "targetType")
					if err != nil {
						return nil, err
					}
					targetID, err := stringArg(p.Args, "targetId")
					if err != nil {
						return nil, err
					}
					rawValues, ok := p.Args["values"].([]interface{})
					if !ok || len(rawValues) == 0 {
						return nil, fmt.Errorf("values must not be empty")
					}
					inputs := make([]domainext.ValueInput, 0, len(rawValues))
					for _, raw := range rawValues {
						item, ok := raw.(map[string]interface{})
						if !ok {
							return nil, fmt.Errorf("values item must be an object")
						}
						fieldCode, _ := item["fieldCode"].(string)
						inputs = append(inputs, domainext.ValueInput{
							FieldCode: fieldCode,
							Value:     item["value"],
						})
					}
					values, err := r.upsertExtensionValues(p.Context, targetType, targetID, inputs)
					if err != nil {
						return nil, err
					}
					return extensionValuesToGraph(values, r), nil
				},
			},
			"deleteExtensionValue": &gql.Field{
				Type: gql.NewNonNull(gql.Boolean),
				Args: gql.FieldConfigArgument{
					"targetType": &gql.ArgumentConfig{Type: gql.NewNonNull(gql.String)},
					"targetId":   &gql.ArgumentConfig{Type: gql.NewNonNull(gql.ID)},
					"fieldCode":  &gql.ArgumentConfig{Type: gql.NewNonNull(gql.String)},
				},
				Resolve: func(p gql.ResolveParams) (interface{}, error) {
					targetType, err := stringArg(p.Args, "targetType")
					if err != nil {
						return nil, err
					}
					targetID, err := stringArg(p.Args, "targetId")
					if err != nil {
						return nil, err
					}
					fieldCode, err := stringArg(p.Args, "fieldCode")
					if err != nil {
						return nil, err
					}
					return r.deleteExtensionValue(p.Context, targetType, targetID, fieldCode)
				},
			},
		},
	})

	return gql.NewSchema(gql.SchemaConfig{Query: queryType, Mutation: mutationType})
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

type extensionFieldGraph struct {
	Code        string `json:"code"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Scope       string `json:"scope"`
	StorageMode string `json:"storageMode"`
	Visibility  string `json:"visibility"`
}

type extensionValueGraph struct {
	FieldCode string      `json:"fieldCode"`
	Type      string      `json:"type"`
	Value     interface{} `json:"value"`
}

func extensionFieldsToGraph(items []domainext.ExtensionField) []extensionFieldGraph {
	out := make([]extensionFieldGraph, 0, len(items))
	for _, item := range items {
		out = append(out, extensionFieldGraph{
			Code:        item.Code,
			Label:       item.Label,
			Description: item.Description,
			Type:        string(item.Type),
			Scope:       string(item.Scope),
			StorageMode: string(item.StorageMode),
			Visibility:  string(item.Visibility),
		})
	}
	return out
}

func extensionValuesToGraph(items []domainext.Value, r *Resolver) []extensionValueGraph {
	if r == nil || r.values == nil || r.values.Registry() == nil {
		return nil
	}
	out := make([]extensionValueGraph, 0, len(items))
	for _, item := range items {
		field, ok := r.values.Registry().Get(item.FieldCode)
		if !ok {
			continue
		}
		apiValue, err := domainext.APIValue(field, item.Payload)
		if err != nil {
			continue
		}
		out = append(out, extensionValueGraph{
			FieldCode: item.FieldCode,
			Type:      string(field.Type),
			Value:     apiValue,
		})
	}
	return out
}
