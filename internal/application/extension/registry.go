package extension

import (
	"context"
	"fmt"
	"sync"

	domainext "github.com/akarso/shopanda/internal/domain/extension"
)

// ListFilter narrows registry list results.
type ListFilter struct {
	Scope          domainext.TargetType
	Visibility     domainext.Visibility
	IncludePrivate bool
}

// Registry holds in-process extension field definitions keyed by code.
type Registry struct {
	mu     sync.RWMutex
	fields map[string]domainext.ExtensionField
	order  []string
}

// NewRegistry creates an empty extension field registry.
func NewRegistry() *Registry {
	return &Registry{
		fields: make(map[string]domainext.ExtensionField),
	}
}

// Register validates and stores a field definition.
// Duplicate codes return a deterministic error.
func (r *Registry) Register(def domainext.FieldDef) error {
	if r == nil {
		return fmt.Errorf("extension: registry must not be nil")
	}
	field, err := domainext.NewExtensionField(def)
	if err != nil {
		return err
	}
	return r.registerField(field)
}

// LoadPersisted merges active definitions from repo into the registry.
// Plugin registrations already present win on code conflicts.
func (r *Registry) LoadPersisted(ctx context.Context, repo domainext.FieldRepository) error {
	if r == nil {
		return fmt.Errorf("extension: registry must not be nil")
	}
	if repo == nil {
		return fmt.Errorf("extension: field repository must not be nil")
	}
	fields, err := repo.ListActive(ctx, "")
	if err != nil {
		return fmt.Errorf("extension: load persisted fields: %w", err)
	}
	for _, field := range fields {
		if err := r.mergeField(field); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) registerField(field domainext.ExtensionField) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.fields[field.Code]; exists {
		return fmt.Errorf("extension field %q already registered", field.Code)
	}
	r.fields[field.Code] = field
	r.order = append(r.order, field.Code)
	return nil
}

func (r *Registry) mergeField(field domainext.ExtensionField) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.fields[field.Code]; exists {
		return nil
	}
	r.fields[field.Code] = field
	r.order = append(r.order, field.Code)
	return nil
}

// Get returns the field registered under code.
func (r *Registry) Get(code string) (domainext.ExtensionField, bool) {
	if r == nil {
		return domainext.ExtensionField{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	field, ok := r.fields[code]
	return field, ok
}

// List returns registered fields matching filter in registration order.
func (r *Registry) List(filter ListFilter) []domainext.ExtensionField {
	if r == nil {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.order) == 0 {
		return nil
	}

	out := make([]domainext.ExtensionField, 0, len(r.order))
	for _, code := range r.order {
		field := r.fields[code]
		if filter.Scope != "" && field.Scope != filter.Scope {
			continue
		}
		if filter.Visibility != "" && field.Visibility != filter.Visibility {
			continue
		}
		if !filter.IncludePrivate && field.Visibility == domainext.VisibilityPrivate {
			continue
		}
		out = append(out, field)
	}
	return out
}

// Codes returns registered field codes in registration order.
func (r *Registry) Codes() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.order) == 0 {
		return nil
	}
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// Len returns the number of registered fields.
func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.order)
}
