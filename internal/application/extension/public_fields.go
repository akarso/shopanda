package extension

import (
	"fmt"

	domainext "github.com/akarso/shopanda/internal/domain/extension"
)

// PublicFieldsForScope returns public, storable fields for scope in registration order.
func PublicFieldsForScope(registry *Registry, scope domainext.TargetType) []domainext.ExtensionField {
	if registry == nil {
		return nil
	}
	fields := registry.List(ListFilter{Scope: scope, IncludePrivate: false})
	out := make([]domainext.ExtensionField, 0, len(fields))
	for _, field := range fields {
		if field.StorageMode == domainext.StorageComputed {
			continue
		}
		out = append(out, field)
	}
	return out
}

// APIValuesForTarget returns public API-shaped values for target.
func APIValuesForTarget(registry *Registry, values []domainext.Value) ([]APIValueItem, error) {
	out := make([]APIValueItem, 0, len(values))
	for _, value := range values {
		field, ok := registry.Get(value.FieldCode)
		if !ok {
			continue
		}
		apiValue, err := domainext.APIValue(field, value.Payload)
		if err != nil {
			return nil, fmt.Errorf("extension api value %q: %w", value.FieldCode, err)
		}
		out = append(out, APIValueItem{
			FieldCode: value.FieldCode,
			Label:     field.Label,
			Type:      string(field.Type),
			Value:     apiValue,
		})
	}
	return out, nil
}

// APIValueItem is a public extension value for API and storefront responses.
type APIValueItem struct {
	FieldCode string
	Label     string
	Type      string
	Value     interface{}
}
