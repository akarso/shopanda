package catalog

import "errors"

// AttributeGroup groups a set of attributes that apply together to products.
// A product can be assigned one or more groups, and each group defines which
// attributes are expected.
type AttributeGroup struct {
	Code       string   // unique identifier, e.g. "physical", "apparel"
	Label      string   // human-readable label
	Attributes []string // attribute codes belonging to this group
}

// NewAttributeGroup creates an AttributeGroup with the required fields.
func NewAttributeGroup(code, label string) (AttributeGroup, error) {
	if code == "" {
		return AttributeGroup{}, errors.New("attribute group code must not be empty")
	}
	if label == "" {
		return AttributeGroup{}, errors.New("attribute group label must not be empty")
	}
	return AttributeGroup{
		Code:       code,
		Label:      label,
		Attributes: make([]string, 0),
	}, nil
}

// HasAttribute reports whether the group contains the given attribute code.
func (g AttributeGroup) HasAttribute(code string) bool {
	for _, c := range g.Attributes {
		if c == code {
			return true
		}
	}
	return false
}

// AddAttribute appends an attribute code if not already present.
func (g *AttributeGroup) AddAttribute(code string) {
	if !g.HasAttribute(code) {
		g.Attributes = append(g.Attributes, code)
	}
}

// RemoveAttribute removes an attribute code from the group.
func (g *AttributeGroup) RemoveAttribute(code string) {
	for i, c := range g.Attributes {
		if c == code {
			g.Attributes = append(g.Attributes[:i], g.Attributes[i+1:]...)
			return
		}
	}
}
