package catalog

import "errors"

// AttributeType represents the data type of an attribute value.
type AttributeType string

const (
	AttributeTypeText    AttributeType = "text"
	AttributeTypeNumber  AttributeType = "number"
	AttributeTypeBoolean AttributeType = "boolean"
	AttributeTypeSelect  AttributeType = "select"
)

// IsValid returns true if t is a recognised attribute type.
func (t AttributeType) IsValid() bool {
	switch t {
	case AttributeTypeText, AttributeTypeNumber, AttributeTypeBoolean, AttributeTypeSelect:
		return true
	}
	return false
}

// Attribute describes a named, typed attribute that can be assigned to products
// and variants via attribute groups.
type Attribute struct {
	Code     string        // unique identifier, e.g. "color", "weight"
	Label    string        // human-readable label
	Type     AttributeType // value type
	Required bool          // whether a value must be provided
	Options  []string      // allowed values when Type == AttributeTypeSelect
}

// NewAttribute creates an Attribute with the required fields.
func NewAttribute(code, label string, attrType AttributeType) (Attribute, error) {
	if code == "" {
		return Attribute{}, errors.New("attribute code must not be empty")
	}
	if label == "" {
		return Attribute{}, errors.New("attribute label must not be empty")
	}
	if !attrType.IsValid() {
		return Attribute{}, errors.New("attribute type is invalid")
	}
	return Attribute{
		Code:  code,
		Label: label,
		Type:  attrType,
	}, nil
}

// Validate checks whether value is acceptable for this attribute.
func (a Attribute) Validate(value interface{}) error {
	if value == nil {
		if a.Required {
			return errors.New("attribute " + a.Code + " is required")
		}
		return nil
	}

	switch a.Type {
	case AttributeTypeText:
		if _, ok := value.(string); !ok {
			return errors.New("attribute " + a.Code + " must be a string")
		}
	case AttributeTypeNumber:
		switch value.(type) {
		case int, int64, float64:
			// ok
		default:
			return errors.New("attribute " + a.Code + " must be a number")
		}
	case AttributeTypeBoolean:
		if _, ok := value.(bool); !ok {
			return errors.New("attribute " + a.Code + " must be a boolean")
		}
	case AttributeTypeSelect:
		s, ok := value.(string)
		if !ok {
			return errors.New("attribute " + a.Code + " must be a string")
		}
		if len(a.Options) > 0 {
			found := false
			for _, opt := range a.Options {
				if opt == s {
					found = true
					break
				}
			}
			if !found {
				return errors.New("attribute " + a.Code + " value not in allowed options")
			}
		}
	}
	return nil
}
