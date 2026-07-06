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

// ListByOrderItems returns public stored values grouped by variant ID for an order.
func (s *ValueService) ListByOrderItems(ctx context.Context, orderID string, variantIDs []string, includePrivate bool) (map[string][]domainext.Value, error) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" || len(variantIDs) == 0 {
		return map[string][]domainext.Value{}, nil
	}

	targetIDs := make([]string, 0, len(variantIDs))
	variantByTargetID := make(map[string]string, len(variantIDs))
	for _, variantID := range variantIDs {
		variantID = strings.TrimSpace(variantID)
		if variantID == "" {
			continue
		}
		targetID := domainext.OrderItemTargetID(orderID, variantID)
		targetIDs = append(targetIDs, targetID)
		variantByTargetID[targetID] = variantID
	}
	if len(targetIDs) == 0 {
		return map[string][]domainext.Value{}, nil
	}

	stored, err := s.repo.ListByTargets(ctx, domainext.TargetOrderItem, targetIDs)
	if err != nil {
		return nil, err
	}

	out := make(map[string][]domainext.Value, len(variantByTargetID))
	for _, value := range stored {
		field, ok := s.registry.Get(value.FieldCode)
		if !ok {
			continue
		}
		if field.Visibility == domainext.VisibilityPrivate && !includePrivate {
			continue
		}
		variantID := variantByTargetID[value.TargetID]
		if variantID == "" {
			continue
		}
		out[variantID] = append(out[variantID], value)
	}
	return out, nil
}

// ValidateBatch validates extension inputs without persisting them.
func (s *ValueService) ValidateBatch(ctx context.Context, target domainext.Target, inputs []domainext.ValueInput, canAccessPrivate bool) error {
	_, err := s.validateBatchValues(target, inputs, "validate", canAccessPrivate, false)
	return err
}

// DeleteAllForTarget removes all stored values for target.
func (s *ValueService) DeleteAllForTarget(ctx context.Context, target domainext.Target) error {
	target.Type = domainext.TargetType(strings.TrimSpace(string(target.Type)))
	target.ID = strings.TrimSpace(target.ID)
	if target.Type == "" || target.ID == "" {
		return domainext.ValidationErr("target type and id must not be empty")
	}
	stored, err := s.repo.ListByTarget(ctx, target)
	if err != nil {
		return err
	}
	for _, value := range stored {
		if err := s.repo.Delete(ctx, target, value.FieldCode); err != nil {
			return err
		}
	}
	return nil
}

// UpsertBatch validates and stores values for target.
func (s *ValueService) UpsertBatch(ctx context.Context, target domainext.Target, inputs []domainext.ValueInput, updatedBy string, canAccessPrivate bool) ([]domainext.Value, error) {
	if len(inputs) == 0 {
		return nil, domainext.ValidationErr("values must not be empty")
	}
	out, err := s.validateBatchValues(target, inputs, updatedBy, canAccessPrivate, true)
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpsertBatch(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *ValueService) validateBatchValues(target domainext.Target, inputs []domainext.ValueInput, updatedBy string, canAccessPrivate, requireUpdatedBy bool) ([]domainext.Value, error) {
	target.Type = domainext.TargetType(strings.TrimSpace(string(target.Type)))
	target.ID = strings.TrimSpace(target.ID)
	if target.Type == "" || target.ID == "" {
		return nil, domainext.ValidationErr("target type and id must not be empty")
	}
	updatedBy = strings.TrimSpace(updatedBy)
	if requireUpdatedBy && updatedBy == "" {
		return nil, domainext.ValidationErr("updated_by must not be empty")
	}

	now := time.Now().UTC()
	out := make([]domainext.Value, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		code := strings.TrimSpace(input.FieldCode)
		if code == "" {
			return nil, domainext.ValidationErr("field_code must not be empty")
		}
		if _, dup := seen[code]; dup {
			return nil, domainext.ValidationErrf("duplicate field_code %q in batch", code)
		}
		seen[code] = struct{}{}

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
			return nil, err
		}
		if err := domainext.ValidatePayload(field, payload); err != nil {
			return nil, err
		}

		out = append(out, domainext.Value{
			FieldCode:  code,
			TargetType: target.Type,
			TargetID:   target.ID,
			Payload:    payload,
			UpdatedBy:  updatedBy,
			UpdatedAt:  now,
		})
	}
	return out, nil
}

// CopyTarget copies stored values from one target to another.
func (s *ValueService) CopyTarget(ctx context.Context, from, to domainext.Target, updatedBy string) error {
	from.Type = domainext.TargetType(strings.TrimSpace(string(from.Type)))
	from.ID = strings.TrimSpace(from.ID)
	to.Type = domainext.TargetType(strings.TrimSpace(string(to.Type)))
	to.ID = strings.TrimSpace(to.ID)
	if from.Type == "" || from.ID == "" || to.Type == "" || to.ID == "" {
		return domainext.ValidationErr("source and destination targets must not be empty")
	}
	updatedBy = strings.TrimSpace(updatedBy)
	if updatedBy == "" {
		return domainext.ValidationErr("updated_by must not be empty")
	}

	stored, err := s.repo.ListByTarget(ctx, from)
	if err != nil {
		return err
	}
	if len(stored) == 0 {
		return nil
	}
	now := time.Now().UTC()
	out := make([]domainext.Value, 0, len(stored))
	for _, value := range stored {
		out = append(out, domainext.Value{
			FieldCode:  value.FieldCode,
			TargetType: to.Type,
			TargetID:   to.ID,
			Payload:    value.Payload,
			UpdatedBy:  updatedBy,
			UpdatedAt:  now,
		})
	}
	return s.repo.UpsertBatch(ctx, out)
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
