package extension

import (
	"context"
	"strings"
	"time"

	domainext "github.com/akarso/shopanda/internal/domain/extension"
)

// SnapshotCartItemToOrderItem copies cart_item extension values with storage_mode=snapshot
// onto the matching order_item target.
func (s *ValueService) SnapshotCartItemToOrderItem(ctx context.Context, cartID, orderID, variantID, updatedBy string) error {
	cartID = strings.TrimSpace(cartID)
	orderID = strings.TrimSpace(orderID)
	variantID = strings.TrimSpace(variantID)
	updatedBy = strings.TrimSpace(updatedBy)
	if cartID == "" || orderID == "" || variantID == "" {
		return domainext.ValidationErr("cart_id, order_id, and variant_id must not be empty")
	}
	if updatedBy == "" {
		return domainext.ValidationErr("updated_by must not be empty")
	}

	from := domainext.CartItemTarget(cartID, variantID)
	to := domainext.OrderItemTarget(orderID, variantID)

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
		field, ok := s.registry.Get(value.FieldCode)
		if !ok || field.StorageMode != domainext.StorageSnapshot {
			continue
		}
		out = append(out, domainext.Value{
			FieldCode:  value.FieldCode,
			TargetType: to.Type,
			TargetID:   to.ID,
			Payload:    value.Payload,
			UpdatedBy:  updatedBy,
			UpdatedAt:  now,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return s.repo.UpsertBatch(ctx, out)
}
