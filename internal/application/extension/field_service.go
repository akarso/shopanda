package extension

import (
	"context"
	"errors"
	"strings"

	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

var (
	// ErrFieldAlreadyExists indicates a duplicate field code.
	ErrFieldAlreadyExists = errors.New("extension field already exists")
	// ErrFieldNotFound indicates the field is not registered.
	ErrFieldNotFound = errors.New("extension field not found")
	// ErrFieldPluginOnly indicates a PluginOnlyWrite field definition cannot be changed or removed via the admin API.
	ErrFieldPluginOnly = errors.New("plugin-only extension field cannot be modified or deleted")
	// ErrFieldNotPluginOnly indicates Retire was called on an ordinary (non-plugin-only) field.
	ErrFieldNotPluginOnly = errors.New("extension field is not plugin-only")
	// ErrPurgeWhileActive indicates --purge-values was requested while the definition is
	// still active. Soft-delete first (omit --purge-values), then purge. This keeps a
	// restored-during-purge definition active instead of soft-deleting it again.
	ErrPurgeWhileActive = errors.New("field is active; soft-delete before purging values")
)

// RetireOptions controls FieldService.Retire.
type RetireOptions struct {
	// PurgeValues deletes extension_values for a soft-deleted plugin-only row only.
	// An active row with PurgeValues returns ErrPurgeWhileActive (soft-delete first).
	PurgeValues bool
	// Store performs the soft-delete and optional purge. Required.
	Store PluginOnlyRetireStore
}

// RetireResult is the outcome of retiring a plugin-only definition.
type RetireResult struct {
	Code         string
	ValuesPurged int64
	// PurgeOnly is true when the definition was already soft-deleted and this call only purged values.
	PurgeOnly bool
}

// FieldService coordinates extension field definitions between persistence and the in-process registry.
type FieldService struct {
	registry *Registry
	repo     domainext.FieldRepository
}

// NewFieldService creates a FieldService.
func NewFieldService(registry *Registry, repo domainext.FieldRepository) *FieldService {
	if registry == nil {
		panic("extension: registry must not be nil")
	}
	if repo == nil {
		panic("extension: field repository must not be nil")
	}
	return &FieldService{registry: registry, repo: repo}
}

// List returns registered fields matching filter.
func (s *FieldService) List(filter ListFilter) []domainext.ExtensionField {
	return s.registry.List(filter)
}

// Get returns a field when visible to the caller.
func (s *FieldService) Get(code string, includePrivate bool) (domainext.ExtensionField, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return domainext.ExtensionField{}, domainext.ValidationErr("extension field code must not be empty")
	}
	field, ok := s.registry.Get(code)
	if !ok {
		return domainext.ExtensionField{}, ErrFieldNotFound
	}
	if field.Visibility == domainext.VisibilityPrivate && !includePrivate {
		return domainext.ExtensionField{}, ErrFieldNotFound
	}
	return field, nil
}

// Create validates, persists, and registers a new field definition.
func (s *FieldService) Create(ctx context.Context, def domainext.FieldDef) (domainext.ExtensionField, error) {
	field, err := domainext.NewExtensionField(def)
	if err != nil {
		return domainext.ExtensionField{}, err
	}
	if _, ok := s.registry.Get(field.Code); ok {
		return domainext.ExtensionField{}, ErrFieldAlreadyExists
	}
	if err := s.repo.Create(ctx, field); err != nil {
		if apperror.Is(err, apperror.CodeConflict) {
			return domainext.ExtensionField{}, ErrFieldAlreadyExists
		}
		return domainext.ExtensionField{}, err
	}
	if err := s.registry.Register(field.ToFieldDef()); err != nil {
		return domainext.ExtensionField{}, mapRegisterError(err)
	}
	return field, nil
}

// Update validates, persists, and replaces a field definition.
func (s *FieldService) Update(ctx context.Context, code string, def domainext.FieldDef) (domainext.ExtensionField, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return domainext.ExtensionField{}, domainext.ValidationErr("extension field code must not be empty")
	}
	def.Code = code
	if err := s.guardPluginOnly(ctx, code); err != nil {
		return domainext.ExtensionField{}, err
	}
	field, err := domainext.NewExtensionField(def)
	if err != nil {
		return domainext.ExtensionField{}, err
	}
	if _, ok := s.registry.Get(code); !ok {
		if _, err := s.repo.FindByCode(ctx, code); err != nil {
			if apperror.Is(err, apperror.CodeNotFound) {
				return domainext.ExtensionField{}, ErrFieldNotFound
			}
			return domainext.ExtensionField{}, err
		}
	}
	// Refuse before persisting so the store and the registry cannot diverge.
	if err := s.registry.ValidateReplace(field); err != nil {
		if errors.Is(err, ErrPluginOnlyImmutable) {
			return domainext.ExtensionField{}, ErrFieldPluginOnly
		}
		if !errors.Is(err, ErrNotRegistered) {
			return domainext.ExtensionField{}, err
		}
	}
	if err := s.repo.Save(ctx, field); err != nil {
		if apperror.Is(err, apperror.CodePluginOnlyFieldImmutable) {
			return domainext.ExtensionField{}, ErrFieldPluginOnly
		}
		return domainext.ExtensionField{}, err
	}
	if err := s.registry.Replace(field); err != nil {
		if !errors.Is(err, ErrNotRegistered) {
			return domainext.ExtensionField{}, err
		}
		if err := s.registry.Register(field.ToFieldDef()); err != nil {
			return domainext.ExtensionField{}, mapRegisterError(err)
		}
	}
	return field, nil
}

// Delete soft-deletes a persisted field and removes it from the registry.
func (s *FieldService) Delete(ctx context.Context, code string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return domainext.ValidationErr("extension field code must not be empty")
	}
	if err := s.guardPluginOnly(ctx, code); err != nil {
		return err
	}
	if err := s.repo.SoftDelete(ctx, code); err != nil {
		if apperror.Is(err, apperror.CodePluginOnlyFieldImmutable) {
			return ErrFieldPluginOnly
		}
		return err
	}
	s.registry.Remove(code)
	return nil
}

// Retire soft-deletes a plugin-only definition (operator CLI) and removes it from
// the registry after the store commits. Admin Delete continues to refuse plugin-only fields.
// When PurgeValues is set and the active row is already gone, a soft-deleted
// plugin-only row may still be purged (retry after a failed earlier purge).
func (s *FieldService) Retire(ctx context.Context, code string, opts RetireOptions) (RetireResult, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return RetireResult{}, domainext.ValidationErr("extension field code must not be empty")
	}
	if opts.Store == nil {
		return RetireResult{}, domainext.ValidationErr("retire store is required")
	}

	stored, err := s.repo.FindByCode(ctx, code)
	if err != nil && !apperror.Is(err, apperror.CodeNotFound) {
		return RetireResult{}, err
	}

	result := RetireResult{Code: code}
	switch {
	case err == nil && !stored.Access.PluginOnlyWrite:
		return RetireResult{}, ErrFieldNotPluginOnly
	case err == nil && opts.PurgeValues:
		// Leave the active definition alone. A restore mid-purge must not be
		// undone by a --purge-values re-run that soft-deletes again.
		return RetireResult{}, ErrPurgeWhileActive
	case err == nil:
		if _, retireErr := opts.Store.RetireActive(ctx, code, false); retireErr != nil {
			return RetireResult{}, mapRetireStoreError(retireErr)
		}
	case opts.PurgeValues:
		n, purgeErr := opts.Store.PurgeIfSoftDeletedPluginOnly(ctx, code)
		result.ValuesPurged = n
		result.PurgeOnly = true
		if purgeErr != nil {
			if errors.Is(purgeErr, domainext.ErrPurgeIncomplete) {
				// Soft-deleted row still present; drop any stale in-memory entry.
				s.registry.Remove(code)
				return result, purgeErr
			}
			if errors.Is(purgeErr, domainext.ErrPurgeDefinitionRestored) || errors.Is(purgeErr, domainext.ErrPurgeDefinitionMissing) {
				// DB row is active again or gone — do not drop a restored in-memory def.
				return result, purgeErr
			}
			mapped := mapRetireStoreError(purgeErr)
			if errors.Is(mapped, ErrFieldNotFound) {
				// Race: Save restored between FindByCode(not found) and assert.
				if active, findErr := s.repo.FindByCode(ctx, code); findErr == nil {
					if active.Access.PluginOnlyWrite {
						return RetireResult{}, ErrPurgeWhileActive
					}
					return RetireResult{}, ErrFieldNotPluginOnly
				}
				// Still missing (e.g. soft-deleted non-plugin-only mid-batch): keep ValuesPurged.
				return result, mapped
			}
			return RetireResult{}, mapped
		}
	default:
		return RetireResult{}, ErrFieldNotFound
	}

	s.registry.Remove(code)
	return result, nil
}

func mapRetireStoreError(err error) error {
	if apperror.Is(err, apperror.CodeNotFound) {
		return ErrFieldNotFound
	}
	if apperror.Is(err, apperror.CodeValidation) {
		return ErrFieldNotPluginOnly
	}
	return err
}

// guardPluginOnly stops the definition API from lifting the write guard
// (flip flag off, write, flip back) or deleting the field.
func (s *FieldService) guardPluginOnly(ctx context.Context, code string) error {
	if existing, ok := s.registry.Get(code); ok && existing.Access.PluginOnlyWrite {
		return ErrFieldPluginOnly
	}
	// The persisted row wins when this instance's registry has drifted.
	// Fail closed: only NotFound means "no stored plugin-only row".
	stored, err := s.repo.FindByCode(ctx, code)
	if err != nil {
		if apperror.Is(err, apperror.CodeNotFound) {
			return nil
		}
		return err
	}
	if stored.Access.PluginOnlyWrite {
		return ErrFieldPluginOnly
	}
	return nil
}

func mapRegisterError(err error) error {
	if errors.Is(err, ErrAlreadyRegistered) {
		return ErrFieldAlreadyExists
	}
	return err
}
