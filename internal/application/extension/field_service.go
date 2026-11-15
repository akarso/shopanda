package extension

import (
	"context"
	"errors"
	"fmt"
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
)

// FieldValuePurger deletes stored values by field code (operator retire path).
type FieldValuePurger interface {
	DeleteByFieldCode(ctx context.Context, fieldCode string) (int64, error)
}

// RetireOptions controls FieldService.Retire.
type RetireOptions struct {
	// PurgeValues deletes every extension_values row for the code when Values is set.
	PurgeValues bool
	Values      FieldValuePurger
}

// RetireResult is the outcome of retiring a plugin-only definition.
type RetireResult struct {
	Code         string
	ValuesPurged int64
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
// the registry. Admin Delete continues to refuse plugin-only fields.
func (s *FieldService) Retire(ctx context.Context, code string, opts RetireOptions) (RetireResult, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return RetireResult{}, domainext.ValidationErr("extension field code must not be empty")
	}
	if opts.PurgeValues && opts.Values == nil {
		return RetireResult{}, domainext.ValidationErr("purge values requires a value store")
	}

	pluginOnly, err := s.isPluginOnly(ctx, code)
	if err != nil {
		return RetireResult{}, err
	}
	if !pluginOnly {
		return RetireResult{}, ErrFieldNotPluginOnly
	}

	if err := s.repo.Retire(ctx, code); err != nil {
		if apperror.Is(err, apperror.CodeNotFound) {
			return RetireResult{}, ErrFieldNotFound
		}
		if apperror.Is(err, apperror.CodeValidation) {
			return RetireResult{}, ErrFieldNotPluginOnly
		}
		return RetireResult{}, err
	}

	s.registry.Remove(code)
	result := RetireResult{Code: code}
	if opts.PurgeValues {
		n, err := opts.Values.DeleteByFieldCode(ctx, code)
		if err != nil {
			return result, fmt.Errorf("definition retired; value purge failed: %w", err)
		}
		result.ValuesPurged = n
	}
	return result, nil
}

// isPluginOnly reports whether the active persisted row is plugin-only.
// Fail closed: repo errors propagate; NotFound means the field is absent.
func (s *FieldService) isPluginOnly(ctx context.Context, code string) (bool, error) {
	stored, err := s.repo.FindByCode(ctx, code)
	if err != nil {
		if apperror.Is(err, apperror.CodeNotFound) {
			return false, ErrFieldNotFound
		}
		return false, err
	}
	return stored.Access.PluginOnlyWrite, nil
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
