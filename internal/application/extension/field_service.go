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
)

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
		return domainext.ExtensionField{}, fmt.Errorf("extension field code must not be empty")
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
	if err := s.ensureCreateAllowed(ctx, field.Code); err != nil {
		return domainext.ExtensionField{}, err
	}
	if err := s.repo.Save(ctx, field); err != nil {
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
		return domainext.ExtensionField{}, fmt.Errorf("extension field code must not be empty")
	}
	def.Code = code
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
	if err := s.repo.Save(ctx, field); err != nil {
		return domainext.ExtensionField{}, err
	}
	if err := s.registry.Replace(field); err != nil {
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
		return fmt.Errorf("extension field code must not be empty")
	}
	if err := s.repo.SoftDelete(ctx, code); err != nil {
		return err
	}
	s.registry.Remove(code)
	return nil
}

func (s *FieldService) ensureCreateAllowed(ctx context.Context, code string) error {
	if _, ok := s.registry.Get(code); ok {
		return ErrFieldAlreadyExists
	}
	_, err := s.repo.FindByCode(ctx, code)
	if err == nil {
		return ErrFieldAlreadyExists
	}
	if apperror.Is(err, apperror.CodeNotFound) {
		return nil
	}
	return err
}

func mapRegisterError(err error) error {
	if err != nil && strings.Contains(err.Error(), "already registered") {
		return ErrFieldAlreadyExists
	}
	return err
}
