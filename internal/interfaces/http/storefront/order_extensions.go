package storefront

import (
	"context"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
)

func orderItemExtensionsMap(ctx context.Context, values *extensionapp.ValueService, orderID string, variantIDs []string) (map[string][]cartItemExtensionResponse, error) {
	if values == nil || len(variantIDs) == 0 {
		return nil, nil
	}

	storedByVariant, err := values.ListByOrderItems(ctx, orderID, variantIDs, false)
	if err != nil {
		return nil, err
	}
	if len(storedByVariant) == 0 {
		return nil, nil
	}

	out := make(map[string][]cartItemExtensionResponse, len(storedByVariant))
	for variantID, stored := range storedByVariant {
		if len(stored) == 0 {
			continue
		}
		apiValues, err := extensionapp.APIValuesForTarget(values.Registry(), stored)
		if err != nil {
			return nil, err
		}
		items := make([]cartItemExtensionResponse, 0, len(apiValues))
		for _, item := range apiValues {
			items = append(items, cartItemExtensionResponse{
				FieldCode: item.FieldCode,
				Label:     item.Label,
				Type:      item.Type,
				Value:     item.Value,
			})
		}
		out[variantID] = items
	}
	return out, nil
}

func orderItemExtensionsFromMap(byVariant map[string][]cartItemExtensionResponse, variantID string) []cartItemExtensionResponse {
	if len(byVariant) == 0 {
		return nil
	}
	return byVariant[variantID]
}
