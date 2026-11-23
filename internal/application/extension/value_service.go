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

// IsPluginOnlyWrite reports whether code is registered with PluginOnlyWrite.
// Used by cart hooks to redact values from untrusted hook payloads.
func (s *ValueService) IsPluginOnlyWrite(code string) bool {
	if s == nil || s.registry == nil {
		return false
	}
	field, ok := s.registry.Get(code)
	return ok && field.Access.PluginOnlyWrite
}

// IsExclusiveString reports whether code opts into exclusive string upsert
// (differing non-empty stored value → conflict).
func (s *ValueService) IsExclusiveString(code string) bool {
	if s == nil || s.registry == nil {
		return false
	}
	field, ok := s.registry.Get(code)
	return ok && field.Access.ExclusiveString
}

// FieldOwner returns the registrant that owns code, or "" when the field is
// unowned or unknown. Used by cart hooks: a plugin-only value is shown only
// when the handler's registrant matches this owner. An empty owner means
// redacted for every handler (fields registered outside plugin Init).
func (s *ValueService) FieldOwner(code string) string {
	if s == nil || s.registry == nil {
		return ""
	}
	return s.registry.Owner(code)
}

// IsPrivate reports whether code is registered with VisibilityPrivate.
func (s *ValueService) IsPrivate(code string) bool {
	if s == nil || s.registry == nil {
		return false
	}
	field, ok := s.registry.Get(code)
	return ok && field.Visibility == domainext.VisibilityPrivate
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

// pluginWriter returns the trusted registrant for code and whether it may write
// the field: the context must name code and, when the field has a recorded owner,
// the registrant must be that owner. Unowned fields (admin-created, or registered
// outside plugin Init) accept any registrant. The owner is read at write time, so a
// field removed and re-registered under another owner is judged by the new owner.
// The registrant is still a plain string: this stops accidental and cross-plugin
// writes; it is not a security boundary against in-process code that impersonates
// the owner's name.
func (s *ValueService) pluginWriter(ctx context.Context, code string) (string, bool) {
	registrant, ok := pluginWriteFor(ctx, code)
	if !ok {
		return "", false
	}
	owner := s.registry.Owner(code)
	return registrant, owner == "" || owner == registrant
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

// ListByCartItems returns stored values grouped by variant ID for a cart,
// filtering private fields unless includePrivate is true. One repo round-trip.
func (s *ValueService) ListByCartItems(ctx context.Context, cartID string, variantIDs []string, includePrivate bool) (map[string][]domainext.Value, error) {
	cartID = strings.TrimSpace(cartID)
	if cartID == "" || len(variantIDs) == 0 {
		return map[string][]domainext.Value{}, nil
	}

	targetIDs := make([]string, 0, len(variantIDs))
	variantByTargetID := make(map[string]string, len(variantIDs))
	for _, variantID := range variantIDs {
		variantID = strings.TrimSpace(variantID)
		if variantID == "" {
			continue
		}
		targetID := domainext.CartItemTargetID(cartID, variantID)
		targetIDs = append(targetIDs, targetID)
		variantByTargetID[targetID] = variantID
	}
	if len(targetIDs) == 0 {
		return map[string][]domainext.Value{}, nil
	}

	stored, err := s.repo.ListByTargets(ctx, domainext.TargetCartItem, targetIDs)
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

// ListByOrderItems returns public stored values grouped by variant ID for an order.
func (s *ValueService) ListByOrderItems(ctx context.Context, orderID string, variantIDs []string, includePrivate bool) (map[string][]domainext.Value, error) {
	batch, err := s.ListByOrderItemsBatch(ctx, map[string][]string{orderID: variantIDs}, includePrivate)
	if err != nil {
		return nil, err
	}
	if out := batch[strings.TrimSpace(orderID)]; out != nil {
		return out, nil
	}
	return map[string][]domainext.Value{}, nil
}

// ListByOrderItemsBatch returns values keyed by orderID then variantID.
// One ListByTargets round-trip for the whole page.
func (s *ValueService) ListByOrderItemsBatch(ctx context.Context, byOrder map[string][]string, includePrivate bool) (map[string]map[string][]domainext.Value, error) {
	if len(byOrder) == 0 {
		return map[string]map[string][]domainext.Value{}, nil
	}
	targetIDs := make([]string, 0)
	orderVariantByTarget := make(map[string][2]string)
	for orderID, variantIDs := range byOrder {
		orderID = strings.TrimSpace(orderID)
		if orderID == "" {
			continue
		}
		for _, variantID := range variantIDs {
			variantID = strings.TrimSpace(variantID)
			if variantID == "" {
				continue
			}
			targetID := domainext.OrderItemTargetID(orderID, variantID)
			targetIDs = append(targetIDs, targetID)
			orderVariantByTarget[targetID] = [2]string{orderID, variantID}
		}
	}
	if len(targetIDs) == 0 {
		return map[string]map[string][]domainext.Value{}, nil
	}
	stored, err := s.repo.ListByTargets(ctx, domainext.TargetOrderItem, targetIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]map[string][]domainext.Value)
	for _, value := range stored {
		field, ok := s.registry.Get(value.FieldCode)
		if !ok {
			continue
		}
		if field.Visibility == domainext.VisibilityPrivate && !includePrivate {
			continue
		}
		pair, ok := orderVariantByTarget[value.TargetID]
		if !ok {
			continue
		}
		orderID, variantID := pair[0], pair[1]
		if out[orderID] == nil {
			out[orderID] = make(map[string][]domainext.Value)
		}
		out[orderID][variantID] = append(out[orderID][variantID], value)
	}
	return out, nil
}

// ValidateBatch validates extension inputs without persisting them.
func (s *ValueService) ValidateBatch(ctx context.Context, target domainext.Target, inputs []domainext.ValueInput, canAccessPrivate bool) error {
	_, err := s.validateBatchValues(ctx, target, inputs, "validate", canAccessPrivate, false)
	return err
}

// DeleteOrphanCartItemExtensions removes cart_item extension rows for cartID whose variant is not in keepVariantIDs.
func (s *ValueService) DeleteOrphanCartItemExtensions(ctx context.Context, cartID string, keepVariantIDs []string) error {
	cartID = strings.TrimSpace(cartID)
	if cartID == "" {
		return domainext.ValidationErr("cart id must not be empty")
	}
	keep := make(map[string]struct{}, len(keepVariantIDs))
	for _, variantID := range keepVariantIDs {
		variantID = strings.TrimSpace(variantID)
		if variantID == "" {
			continue
		}
		keep[variantID] = struct{}{}
	}
	targetIDs, err := s.repo.ListTargetIDsByTypeAndIDPrefix(ctx, domainext.TargetCartItem, cartID+":%")
	if err != nil {
		return err
	}
	for _, targetID := range targetIDs {
		_, variantID, ok := domainext.ParseCartItemTargetID(targetID)
		if !ok {
			continue
		}
		if _, keepIt := keep[variantID]; keepIt {
			continue
		}
		if err := s.DeleteAllForTarget(ctx, domainext.CartItemTarget(cartID, variantID)); err != nil {
			return err
		}
	}
	return nil
}

// DeleteAllForTarget removes all stored values for target.
// Core-internal (cart line removal, orphan cleanup): intentionally not gated by
// PluginOnlyWrite. Do not expose it to request-driven callers.
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
	out, err := s.validateBatchValues(ctx, target, inputs, updatedBy, canAccessPrivate, true)
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpsertBatch(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpsertBatchSelective validates then upserts plain and ExclusiveString fields
// in one repository transaction (exclusive codes use the conditional predicate).
func (s *ValueService) UpsertBatchSelective(ctx context.Context, target domainext.Target, inputs []domainext.ValueInput, updatedBy string, canAccessPrivate bool, exclusiveFieldCodes map[string]struct{}) ([]domainext.Value, error) {
	if len(inputs) == 0 {
		return nil, domainext.ValidationErr("values must not be empty")
	}
	out, err := s.validateBatchValues(ctx, target, inputs, updatedBy, canAccessPrivate, true)
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpsertBatchSelective(ctx, out, exclusiveFieldCodes); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *ValueService) validateBatchValues(ctx context.Context, target domainext.Target, inputs []domainext.ValueInput, updatedBy string, canAccessPrivate, requireUpdatedBy bool) ([]domainext.Value, error) {
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
		rowUpdatedBy := updatedBy
		if field.Access.PluginOnlyWrite {
			registrant, allowed := s.pluginWriter(ctx, code)
			if !allowed {
				return nil, domainext.ErrForbiddenPluginOnlyField
			}
			rowUpdatedBy = "plugin:" + registrant
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
			UpdatedBy:  rowUpdatedBy,
			UpdatedAt:  now,
		})
	}
	return out, nil
}

// ListByTarget returns every stored value for target, unfiltered by
// visibility or scope (core-internal, like CopyTarget/DeleteAllForTarget).
// Used to snapshot a target's rows before a risky write (e.g. guest-cart
// merge CopyTarget, which commits independently of the cart Save that
// follows it) so a later failure can restore exactly what was there.
func (s *ValueService) ListByTarget(ctx context.Context, target domainext.Target) ([]domainext.Value, error) {
	target.Type = domainext.TargetType(strings.TrimSpace(string(target.Type)))
	target.ID = strings.TrimSpace(target.ID)
	if target.Type == "" || target.ID == "" {
		return nil, domainext.ValidationErr("target type and id must not be empty")
	}
	return s.repo.ListByTarget(ctx, target)
}

// ListTargetIDsByTypeAndIDPrefix lists distinct target_ids for type with the
// given SQL LIKE prefix (core-internal; used to find merge-backup rows).
func (s *ValueService) ListTargetIDsByTypeAndIDPrefix(ctx context.Context, targetType domainext.TargetType, idPrefix string) ([]string, error) {
	targetType = domainext.TargetType(strings.TrimSpace(string(targetType)))
	idPrefix = strings.TrimSpace(idPrefix)
	if targetType == "" || idPrefix == "" {
		return nil, domainext.ValidationErr("target type and id prefix must not be empty")
	}
	return s.repo.ListTargetIDsByTypeAndIDPrefix(ctx, targetType, idPrefix)
}

// atomicTargetReplacer is implemented by repos that can replace a target's
// rows in one database transaction (upsert the snapshot, then delete extras).
type atomicTargetReplacer interface {
	ReplaceTarget(ctx context.Context, target domainext.Target, values []domainext.Value) error
}

// ReplaceTarget restores target to exactly values (already-validated Value
// rows, typically a prior ListByTarget snapshot). An empty values leaves
// the target with no rows.
//
// Order is upsert-then-delete-extras (never delete-all then upsert): a
// failed write must not leave the target empty. When the repo implements
// atomicTargetReplacer, the whole replace runs in one transaction.
// Core-internal (merge compensation): skips validation and PluginOnlyWrite
// like CopyTarget — it restores a prior state, it does not accept new input.
func (s *ValueService) ReplaceTarget(ctx context.Context, target domainext.Target, values []domainext.Value) error {
	target.Type = domainext.TargetType(strings.TrimSpace(string(target.Type)))
	target.ID = strings.TrimSpace(target.ID)
	if target.Type == "" || target.ID == "" {
		return domainext.ValidationErr("target type and id must not be empty")
	}
	normalized := make([]domainext.Value, len(values))
	keep := make(map[string]struct{}, len(values))
	for i, v := range values {
		v.TargetType = target.Type
		v.TargetID = target.ID
		normalized[i] = v
		keep[v.FieldCode] = struct{}{}
	}
	if r, ok := s.repo.(atomicTargetReplacer); ok {
		return r.ReplaceTarget(ctx, target, normalized)
	}
	// Fallback for in-memory / test repos: upsert first so a failed write
	// never wipes existing rows the way DeleteAll-then-UpsertBatch did.
	if len(normalized) > 0 {
		if err := s.repo.UpsertBatch(ctx, normalized); err != nil {
			return err
		}
	}
	stored, err := s.repo.ListByTarget(ctx, target)
	if err != nil {
		return err
	}
	for _, value := range stored {
		if _, ok := keep[value.FieldCode]; ok {
			continue
		}
		if err := s.repo.Delete(ctx, target, value.FieldCode); err != nil {
			return err
		}
	}
	return nil
}

// CopyTarget copies stored values from one target to another.
// Core-internal (guest claim / merge): skips validation and PluginOnlyWrite on
// purpose so plugin-set values survive. Sources must be trusted core targets.
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
// Delete is the untrusted-facing delete (admin API, GraphQL): PluginOnlyWrite
// fields need a WithPluginWrite context naming the field. Core line removal and
// orphan cleanup use DeleteAllForTarget, which is deliberately not gated.
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
	if field.Access.PluginOnlyWrite {
		if _, allowed := s.pluginWriter(ctx, fieldCode); !allowed {
			return domainext.ErrForbiddenPluginOnlyField
		}
	}
	return s.repo.Delete(ctx, target, fieldCode)
}
