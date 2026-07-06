package http

import (
	"context"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
)

func orderItemExtensions(ctx context.Context, values *extensionapp.ValueService, orderID, variantID string) ([]cartItemExtensionResponse, error) {
	if values == nil {
		return nil, nil
	}
	target := domainext.OrderItemTarget(orderID, variantID)
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
