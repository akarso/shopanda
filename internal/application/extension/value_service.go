package extension

import (
	"context"
	"strings"
	"time"

	domainext "github.com/akarso/shopanda/internal/domain/extension"
)

// ValueService manages extension field values.
type ValueService struct {
	registry *Registry
	repo     domainext.ValueRepository
}

// Registry returns the field registry used by the service.
func (s *ValueService) Registry() *Registry {
	return s.registry
}

// NewValueService creates a ValueService.
func NewValueService(registry *Registry, repo domainext.ValueRepository) *ValueService {
	if registry == nil {
		panic("extension: registry must not be nil")
	}
	if repo == nil {
		panic("extension: value repository must not be nil")
	}
	return &ValueService{registry: registry, repo: repo}
}

// List returns stored values for target, filtering private fields unless allowed.
func (s *ValueService) List(ctx context.Context, target domainext.Target, includePrivate bool) ([]domainext.Value, error) {
	target.Type = domainext.TargetType(strings.TrimSpace(string(target.Type)))
	target.ID = strings.TrimSpace(target.ID)
	if target.Type == "" || target.ID == "" {
		return nil, domainext.ValidationErr("target type and id must not be empty")
	}

	stored, err := s.repo.ListByTarget(ctx, target)
	if err != nil {
		return nil, err
	}
	out := make([]domainext.Value, 0, len(stored))
	for _, value := range stored {
		field, ok := s.registry.Get(value.FieldCode)
		if !ok {
			continue
		}
		if field.Visibility == domainext.VisibilityPrivate && !includePrivate {
			continue
		}
		out = append(out, value)
	}
	return out, nil
}

// UpsertBatch validates and stores values for target.
func (s *ValueService) UpsertBatch(ctx context.Context, target domainext.Target, inputs []domainext.ValueInput, updatedBy string, canAccessPrivate bool) ([]domainext.Value, error) {
	target.Type = domainext.TargetType(strings.TrimSpace(string(target.Type)))
	target.ID = strings.TrimSpace(target.ID)
	if target.Type == "" || target.ID == "" {
		return nil, domainext.ValidationErr("target type and id must not be empty")
	}
	if len(inputs) == 0 {
		return nil, domainext.ValidationErr("values must not be empty")
	}

	now := time.Now().UTC()
	out := make([]domainext.Value, 0, len(inputs))
	for _, input := range inputs {
		code := strings.TrimSpace(input.FieldCode)
		if code == "" {
			return nil, domainext.ValidationErr("field_code must not be empty")
		}
		field, ok := s.registry.Get(code)
		if !ok {
			return nil, domainext.ErrUnknownFieldCode
		}
		if field.Scope != target.Type {
			return nil, domainext.ValidationErrf("extension field %q does not apply to target type %q", code, target.Type)
		}
		if field.StorageMode == domainext.StorageComputed {
			return nil, domainext.ValidationErrf("extension field %q is computed and cannot be stored", code)
		}
		if field.Visibility == domainext.VisibilityPrivate && !canAccessPrivate {
			return nil, domainext.ErrForbiddenPrivateField
		}

		payload, err := domainext.PayloadFromInput(field, input.Value)
		if err != nil {
			if domainext.IsValidationError(err) {
				return nil, err
			}
			return nil, err
		}
		if err := domainext.ValidatePayload(field, payload); err != nil {
			return nil, err
		}

		value := domainext.Value{
			FieldCode:  code,
			TargetType: target.Type,
			TargetID:   target.ID,
			Payload:    payload,
			UpdatedBy:  strings.TrimSpace(updatedBy),
			UpdatedAt:  now,
		}
		if err := s.repo.Upsert(ctx, value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}

// Delete removes a stored value for target and field code.
func (s *ValueService) Delete(ctx context.Context, target domainext.Target, fieldCode string, canAccessPrivate bool) error {
	target.Type = domainext.TargetType(strings.TrimSpace(string(target.Type)))
	target.ID = strings.TrimSpace(target.ID)
	fieldCode = strings.TrimSpace(fieldCode)
	if target.Type == "" || target.ID == "" || fieldCode == "" {
		return domainext.ValidationErr("target type, id, and field_code must not be empty")
	}
	field, ok := s.registry.Get(fieldCode)
	if !ok {
		return domainext.ErrUnknownFieldCode
	}
	if field.Visibility == domainext.VisibilityPrivate && !canAccessPrivate {
		return domainext.ErrForbiddenPrivateField
	}
	return s.repo.Delete(ctx, target, fieldCode)
}
