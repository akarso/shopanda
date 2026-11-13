package extension

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/logger"
)

var (
	// ErrAlreadyRegistered indicates a duplicate field code in the registry.
	ErrAlreadyRegistered = errors.New("extension field already registered")
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
	owners map[string]string // code -> registering plugin; absent = unowned
	order  []string
}

// NewRegistry creates an empty extension field registry.
func NewRegistry() *Registry {
	return &Registry{
		fields: make(map[string]domainext.ExtensionField),
		owners: make(map[string]string),
	}
}

// Register validates and stores a field definition with no owner.
// Duplicate codes return a deterministic error.
func (r *Registry) Register(def domainext.FieldDef) error {
	return r.register(def, "")
}

// RegisterOwned registers a field and records owner (the registering plugin,
// whitespace-trimmed, case-sensitive). Owned plugin-only fields are writable only
// by a trusted write naming that owner.
func (r *Registry) RegisterOwned(def domainext.FieldDef, owner string) error {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return fmt.Errorf("extension: field owner must not be empty")
	}
	return r.register(def, owner)
}

func (r *Registry) register(def domainext.FieldDef, owner string) error {
	if r == nil {
		return fmt.Errorf("extension: registry must not be nil")
	}
	field, err := domainext.NewExtensionField(def)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.fields[field.Code]; exists {
		return ErrAlreadyRegistered
	}
	r.fields[field.Code] = field
	if owner != "" {
		r.owners[field.Code] = owner
	}
	r.order = append(r.order, field.Code)
	return nil
}

// AssignOwner sets owner on an existing field that has none. It reports whether
// it assigned (false: unknown code, or the field already has an owner).
func (r *Registry) AssignOwner(code, owner string) bool {
	owner = strings.TrimSpace(owner)
	if r == nil || owner == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.fields[code]; !ok {
		return false
	}
	if _, owned := r.owners[code]; owned {
		return false
	}
	r.owners[code] = owner
	return true
}

// UnownedPluginOnly lists plugin-only field codes with no owner, in registration
// order, from one consistent snapshot.
func (r *Registry) UnownedPluginOnly() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []string
	for _, code := range r.order {
		if r.fields[code].Access.PluginOnlyWrite {
			if _, owned := r.owners[code]; !owned {
				out = append(out, code)
			}
		}
	}
	return out
}

// Owner returns the plugin that registered code, or "" when unowned/unknown.
func (r *Registry) Owner(code string) string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.owners[code]
}

// LoadPersisted merges active definitions from repo into the registry.
// Plugin registrations already present win on code conflicts.
func (r *Registry) LoadPersisted(ctx context.Context, repo domainext.FieldRepository, log logger.Logger) error {
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
		if r.mergeField(field) {
			continue
		}
		if log != nil {
			log.Info("extension.registry.merge_skipped", map[string]interface{}{
				"field_code": field.Code,
				"reason":     "already registered",
			})
		}
	}
	return nil
}

func (r *Registry) mergeField(field domainext.ExtensionField) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.fields[field.Code]; exists {
		return false
	}
	r.fields[field.Code] = field
	r.order = append(r.order, field.Code)
	return true
}

// Replace updates an existing registered field definition.
func (r *Registry) Replace(field domainext.ExtensionField) error {
	if r == nil {
		return fmt.Errorf("extension: registry must not be nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.fields[field.Code]; !exists {
		return fmt.Errorf("extension field %q not registered", field.Code)
	}
	r.fields[field.Code] = field
	return nil
}

// Remove drops a field from the in-process registry.
func (r *Registry) Remove(code string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.fields[code]; !exists {
		return false
	}
	delete(r.fields, code)
	delete(r.owners, code)
	for i, c := range r.order {
		if c == code {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	return true
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
