package admin

import (
	"fmt"

	domainAdmin "github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
)

const productAttributeFieldScope = "global"

// AttributeToFormField maps a catalog attribute to an admin form field.
func AttributeToFormField(attr catalog.Attribute) (domainAdmin.Field, error) {
	return attributeToFormField(attr)
}

func attributeToFormField(attr catalog.Attribute) (domainAdmin.Field, error) {
	fieldType, err := attributeTypeToFieldType(attr.Type)
	if err != nil {
		return domainAdmin.Field{}, err
	}
	field := domainAdmin.Field{
		Name:     attr.Code,
		Type:     fieldType,
		Label:    attr.Label,
		Required: attr.Required,
		Meta:     map[string]interface{}{"scope": productAttributeFieldScope},
	}
	if attr.Type == catalog.AttributeTypeSelect {
		opts := make([]domainAdmin.Option, 0, len(attr.Options))
		for _, o := range attr.Options {
			opts = append(opts, domainAdmin.Option{Label: o, Value: o})
		}
		field.Options = opts
	}
	return field, nil
}

func attributeTypeToFieldType(attrType catalog.AttributeType) (string, error) {
	switch attrType {
	case catalog.AttributeTypeText:
		return "text", nil
	case catalog.AttributeTypeNumber:
		return "number", nil
	case catalog.AttributeTypeBoolean:
		return "checkbox", nil
	case catalog.AttributeTypeSelect:
		return "select", nil
	default:
		return "", fmt.Errorf("unsupported attribute type %q", attrType)
	}
}
