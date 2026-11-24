package extension

import (
	"context"
	"fmt"

	domainext "github.com/akarso/shopanda/internal/domain/extension"
)

// SoftFieldError reports a per-field APIValue failure skipped by
// PublicOrderItemAPIByVariant. Nil is fine (silent skip).
type SoftFieldError func(fieldCode string, err error)

// PublicOrderItemAPIByVariant loads public order_item extension values
// (includePrivate=false) keyed by variant id. Per-field APIValue failures are
// skipped (best-effort); onFieldErr is called with the field code when set.
// ListByOrderItems errors still return. Nil api or empty variantIDs yield (nil, nil).
func PublicOrderItemAPIByVariant(ctx context.Context, api ValueAPI, orderID string, variantIDs []string, onFieldErr SoftFieldError) (map[string][]APIValueItem, error) {
	if api == nil || len(variantIDs) == 0 {
		return nil, nil
	}
	storedByVariant, err := api.ListByOrderItems(ctx, orderID, variantIDs, false)
	if err != nil {
		return nil, fmt.Errorf("extension: list order item values: %w", err)
	}
	if len(storedByVariant) == 0 {
		return nil, nil
	}
	reg := api.Registry()
	out := make(map[string][]APIValueItem, len(storedByVariant))
	for variantID, stored := range storedByVariant {
		if len(stored) == 0 {
			continue
		}
		items := apiValuesForTargetSoft(reg, stored, onFieldErr)
		if len(items) == 0 {
			continue
		}
		out[variantID] = items
	}
	return out, nil
}

// apiValuesForTargetSoft is like APIValuesForTarget but skips unknown fields
// and per-field decode errors so one corrupt value cannot block email/SSR.
func apiValuesForTargetSoft(registry *Registry, values []domainext.Value, onFieldErr SoftFieldError) []APIValueItem {
	out := make([]APIValueItem, 0, len(values))
	for _, value := range values {
		if registry == nil {
			continue
		}
		field, ok := registry.Get(value.FieldCode)
		if !ok {
			continue
		}
		apiValue, err := domainext.APIValue(field, value.Payload)
		if err != nil {
			if onFieldErr != nil {
				onFieldErr(value.FieldCode, err)
			}
			continue
		}
		if apiValue == nil {
			continue
		}
		out = append(out, APIValueItem{
			FieldCode: value.FieldCode,
			Label:     field.Label,
			Type:      string(field.Type),
			Value:     apiValue,
		})
	}
	return out
}

// ExtensionMapFromAPIItems flattens API items to field_code → value for
// product email overrides ({{index .Extensions "code"}}). Duplicate codes
// keep the last value.
func ExtensionMapFromAPIItems(items []APIValueItem) map[string]interface{} {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(items))
	for _, item := range items {
		out[item.FieldCode] = item.Value
	}
	return out
}

// ExtensionDisplayRow is a shopper-facing label/value pair for default email
// and SSR lists (stable registration order; JSON composites omitted).
type ExtensionDisplayRow struct {
	FieldCode string
	Label     string
	Value     string
}

// ExtensionDisplayRows builds labeled rows in registry registration order for
// TargetOrderItem. Skips nil values and JSON objects/arrays (not safe for
// default html/template bodies). Bools use Yes/No.
func ExtensionDisplayRows(registry *Registry, items []APIValueItem) []ExtensionDisplayRow {
	if len(items) == 0 {
		return nil
	}
	byCode := make(map[string]APIValueItem, len(items))
	for _, item := range items {
		byCode[item.FieldCode] = item
	}
	var ordered []APIValueItem
	if registry != nil {
		for _, field := range PublicFieldsForScope(registry, domainext.TargetOrderItem) {
			if item, ok := byCode[field.Code]; ok {
				ordered = append(ordered, item)
				delete(byCode, field.Code)
			}
		}
	}
	for _, item := range items {
		if _, ok := byCode[item.FieldCode]; ok {
			ordered = append(ordered, item)
			delete(byCode, item.FieldCode)
		}
	}
	out := make([]ExtensionDisplayRow, 0, len(ordered))
	for _, item := range ordered {
		text, ok := FormatExtensionDisplayValue(item.Type, item.Value)
		if !ok {
			continue
		}
		label := item.Label
		if label == "" {
			label = item.FieldCode
		}
		out = append(out, ExtensionDisplayRow{
			FieldCode: item.FieldCode,
			Label:     label,
			Value:     text,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// FormatExtensionDisplayValue turns an API value into shopper-facing text.
// ok is false for nil and JSON composites (map/slice) that must not appear in
// default email/SSR bodies.
func FormatExtensionDisplayValue(fieldType string, value interface{}) (string, bool) {
	if value == nil {
		return "", false
	}
	switch value.(type) {
	case map[string]interface{}, []interface{}:
		return "", false
	}
	if fieldType == string(domainext.FieldTypeJSON) {
		switch value.(type) {
		case string, bool, int, int64, float64:
			// scalar JSON decode — still printable
		default:
			return "", false
		}
	}
	switch v := value.(type) {
	case bool:
		if v {
			return "Yes", true
		}
		return "No", true
	case string:
		if v == "" {
			return "", false
		}
		return v, true
	default:
		return fmt.Sprint(v), true
	}
}
