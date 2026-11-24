package storefront

import (
	"context"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/domain/order"
)

func orderItemExtensionsMap(ctx context.Context, values extensionapp.ValueAPI, orderID string, variantIDs []string, onFieldErr extensionapp.SoftFieldError) (map[string][]cartItemExtensionResponse, error) {
	byVariant, err := extensionapp.PublicOrderItemAPIByVariant(ctx, values, orderID, variantIDs, onFieldErr)
	if err != nil {
		return nil, err
	}
	if len(byVariant) == 0 {
		return nil, nil
	}
	out := make(map[string][]cartItemExtensionResponse, len(byVariant))
	for variantID, apiValues := range byVariant {
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

// storefrontOrderLineItems builds account/confirm line rows with VariantID and
// public order_item extensions (same ExtensionDisplayRows + enrich order as email).
func (h *StorefrontHandler) storefrontOrderLineItems(ctx context.Context, o *order.Order) ([]StorefrontAccountOrderItem, error) {
	if o == nil {
		return nil, nil
	}
	orderItems := o.Items()
	items := make([]StorefrontAccountOrderItem, 0, len(orderItems))
	variantIDs := make([]string, len(orderItems))
	for i, item := range orderItems {
		variantIDs[i] = item.VariantID
		lineTotal, err := item.LineTotal()
		if err != nil {
			return nil, err
		}
		items = append(items, StorefrontAccountOrderItem{
			Name:          item.Name,
			SKU:           item.SKU,
			VariantID:     item.VariantID,
			Quantity:      item.Quantity,
			UnitPriceText: formatStorefrontMoney(item.UnitPrice.Amount(), item.UnitPrice.Currency()),
			LineTotalText: formatStorefrontMoney(lineTotal.Amount(), lineTotal.Currency()),
		})
	}
	if h == nil || len(variantIDs) == 0 {
		return items, nil
	}
	var byVariant map[string][]extensionapp.APIValueItem
	var reg *extensionapp.Registry
	if h.extensions != nil {
		var onFieldErr extensionapp.SoftFieldError
		if h.log != nil {
			onFieldErr = func(code string, fieldErr error) {
				h.log.Error("storefront.order_item_extension_field_failed", fieldErr, map[string]interface{}{
					"order_id":   o.ID,
					"field_code": code,
				})
			}
		}
		var err error
		byVariant, err = extensionapp.PublicOrderItemAPIByVariant(ctx, h.extensions, o.ID, variantIDs, onFieldErr)
		if err != nil {
			if h.log != nil {
				h.log.Error("storefront.order_line_extensions_failed", err, map[string]interface{}{
					"order_id": o.ID,
				})
			}
			byVariant = nil
		} else {
			reg = h.extensions.Registry()
		}
	}
	for i := range items {
		apiItems := byVariant[items[i].VariantID]
		extMap := extensionapp.ExtensionMapFromAPIItems(apiItems)
		rows := extensionapp.ExtensionDisplayRows(reg, apiItems)
		hookExt := extensionapp.CloneExtensionMap(extMap)
		rows = extensionapp.EnrichOrderLineDisplayRows(ctx, h.orderLineHooks, o.ID, items[i].VariantID, hookExt, rows,
			func(registrant string, hookErr error) {
				if h.log != nil {
					h.log.Error("storefront.order_line_display_failed", hookErr, map[string]interface{}{
						"order_id":   o.ID,
						"variant_id": items[i].VariantID,
						"registrant": registrant,
					})
				}
			},
			func(fieldCode, reason string) {
				if h.log != nil {
					h.log.Info("storefront.order_line_display_row_skipped", map[string]interface{}{
						"order_id":   o.ID,
						"variant_id": items[i].VariantID,
						"field_code": fieldCode,
						"reason":     reason,
					})
				}
			},
		)
		items[i].Extensions = storefrontDisplaysFromRows(rows)
	}
	return items, nil
}

func storefrontDisplaysFromRows(rows []extensionapp.ExtensionDisplayRow) []StorefrontOrderLineExtensionDisplay {
	if len(rows) == 0 {
		return nil
	}
	out := make([]StorefrontOrderLineExtensionDisplay, len(rows))
	for i, row := range rows {
		out[i] = StorefrontOrderLineExtensionDisplay{
			FieldCode: row.FieldCode,
			Label:     row.Label,
			Kind:      row.Kind,
			Value:     row.Value,
			Alt:       row.Alt,
		}
	}
	return out
}
