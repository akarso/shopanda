package catalog

import (
	"fmt"
	"sync"
)

// AttributeRegistry holds registered attributes and attribute groups.
// It is thread-safe and returns deep copies on retrieval.
type AttributeRegistry struct {
	mu     sync.RWMutex
	attrs  map[string]Attribute
	groups map[string]AttributeGroup
}

// NewAttributeRegistry creates an empty attribute registry.
func NewAttributeRegistry() *AttributeRegistry {
	return &AttributeRegistry{
		attrs:  make(map[string]Attribute),
		groups: make(map[string]AttributeGroup),
	}
}

// RegisterAttribute registers an attribute by its code.
// If an attribute with the same code already exists it is replaced.
func (r *AttributeRegistry) RegisterAttribute(attr Attribute) {
	r.mu.Lock()
	r.attrs[attr.Code] = cloneAttribute(attr)
	r.mu.Unlock()
}

// Attribute returns the attribute registered under code and true,
// or a zero Attribute and false if not found.
func (r *AttributeRegistry) Attribute(code string) (Attribute, bool) {
	r.mu.RLock()
	a, ok := r.attrs[code]
	if !ok {
		r.mu.RUnlock()
		return Attribute{}, false
	}
	cp := cloneAttribute(a)
	r.mu.RUnlock()
	return cp, true
}

// Attributes returns a copy of all registered attributes.
func (r *AttributeRegistry) Attributes() []Attribute {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Attribute, 0, len(r.attrs))
	for _, a := range r.attrs {
		out = append(out, cloneAttribute(a))
	}
	return out
}

// RegisterGroup registers an attribute group by its code.
// Returns an error if any attribute code in the group is not registered.
func (r *AttributeRegistry) RegisterGroup(group AttributeGroup) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, code := range group.Attributes {
		if _, ok := r.attrs[code]; !ok {
			return fmt.Errorf("attribute %q not registered", code)
		}
	}
	r.groups[group.Code] = cloneGroup(group)
	return nil
}

// Group returns the group registered under code and true,
// or a zero AttributeGroup and false if not found.
func (r *AttributeRegistry) Group(code string) (AttributeGroup, bool) {
	r.mu.RLock()
	g, ok := r.groups[code]
	if !ok {
		r.mu.RUnlock()
		return AttributeGroup{}, false
	}
	cp := cloneGroup(g)
	r.mu.RUnlock()
	return cp, true
}

// Groups returns a copy of all registered groups.
func (r *AttributeRegistry) Groups() []AttributeGroup {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]AttributeGroup, 0, len(r.groups))
	for _, g := range r.groups {
		out = append(out, cloneGroup(g))
	}
	return out
}

// GroupAttributes returns the resolved attributes for a group.
// Returns an error if the group is not registered.
func (r *AttributeRegistry) GroupAttributes(groupCode string) ([]Attribute, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	g, ok := r.groups[groupCode]
	if !ok {
		return nil, fmt.Errorf("attribute group %q not registered", groupCode)
	}
	out := make([]Attribute, 0, len(g.Attributes))
	for _, code := range g.Attributes {
		if a, ok := r.attrs[code]; ok {
			out = append(out, cloneAttribute(a))
		}
	}
	return out, nil
}

// ValidateAttributes validates a map of attribute values against the attributes
// in the given group. It checks that required attributes are present and that
// values match their declared types.
func (r *AttributeRegistry) ValidateAttributes(groupCode string, values map[string]interface{}) []error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	g, ok := r.groups[groupCode]
	if !ok {
		return []error{fmt.Errorf("attribute group %q not registered", groupCode)}
	}
	// Build allowed set from group attributes.
	allowed := make(map[string]struct{}, len(g.Attributes))
	for _, code := range g.Attributes {
		allowed[code] = struct{}{}
	}

	var errs []error

	// Reject undeclared keys.
	for key := range values {
		if _, ok := allowed[key]; !ok {
			errs = append(errs, fmt.Errorf("attribute %q not declared in group %q", key, groupCode))
		}
	}

	// Validate declared attributes.
	for _, code := range g.Attributes {
		a, ok := r.attrs[code]
		if !ok {
			continue
		}
		if err := a.Validate(values[code]); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// --- deep-copy helpers ---

func cloneAttribute(a Attribute) Attribute {
	cp := a
	if a.Options != nil {
		cp.Options = make([]string, len(a.Options))
		copy(cp.Options, a.Options)
	}
	return cp
}

func cloneGroup(g AttributeGroup) AttributeGroup {
	cp := g
	if g.Attributes != nil {
		cp.Attributes = make([]string, len(g.Attributes))
		copy(cp.Attributes, g.Attributes)
	}
	return cp
}
