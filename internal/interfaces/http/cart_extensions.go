package http

import (
	"context"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
)

type cartItemExtensionResponse struct {
	FieldCode string      `json:"field_code"`
	Label     string      `json:"label,omitempty"`
	Type      string      `json:"type"`
	Value     interface{} `json:"value"`
}

func cartItemExtensions(ctx context.Context, values *extensionapp.ValueService, cartID, variantID string) ([]cartItemExtensionResponse, error) {
	if values == nil {
		return nil, nil
	}
	target := domainext.CartItemTarget(cartID, variantID)
	stored, err := values.List(ctx, target, false)
	if err != nil {
		return nil, err
	}
	if len(stored) == 0 {
		return nil, nil
	}
	apiValues, err := extensionapp.APIValuesForTarget(values.Registry(), stored)
	if err != nil {
		return nil, err
	}
	out := make([]cartItemExtensionResponse, 0, len(apiValues))
	for _, item := range apiValues {
		out = append(out, cartItemExtensionResponse{
			FieldCode: item.FieldCode,
			Label:     item.Label,
			Type:      item.Type,
			Value:     item.Value,
		})
	}
	return out, nil
}

func extensionInputsFromRequest(items []cartItemExtensionInput) []domainext.ValueInput {
	if len(items) == 0 {
		return nil
	}
	out := make([]domainext.ValueInput, 0, len(items))
	for _, item := range items {
		out = append(out, domainext.ValueInput{
			FieldCode: item.FieldCode,
			Value:     item.Value,
		})
	}
	return out
}

type cartItemExtensionInput struct {
	FieldCode string      `json:"field_code"`
	Value     interface{} `json:"value"`
}
