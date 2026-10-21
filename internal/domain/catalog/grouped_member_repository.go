package catalog

import "context"

// GroupedMemberRepository persists display-only grouped product membership.
// Rows do not affect a member's Purchasable result or visibility.
//
// Transaction binding is not part of this port. WithTx is implemented on the
// postgres adapter only (postgres.GroupedMemberRepo) and is used by
// GroupedService.Add and the admin type-change guard.
type GroupedMemberRepository interface {
	// ListByGroupID returns members for groupProductID ordered by created_at,
	// then member_product_id. Always returns a non-nil slice (possibly empty).
	ListByGroupID(ctx context.Context, groupProductID string) ([]GroupedMember, error)

	// CountByGroupID returns how many member rows belong to the group product.
	CountByGroupID(ctx context.Context, groupProductID string) (int, error)

	// CountByMemberID returns how many groups reference the product as a member.
	CountByMemberID(ctx context.Context, memberProductID string) (int, error)

	// Insert adds a membership row. A duplicate composite key is a no-op
	// (insert-ignore); there are no mutable columns on the link.
	//
	// Insert stays on this port because GroupedService.Add calls it. It checks
	// field validation only. ValidateComposition (grouped parent, leaf member)
	// is service-only and is not enforced here. This port does not read or
	// store product types. The safety net for a direct Insert is the admin
	// type-change guard: CountByGroupID and CountByMemberID are row-gated, so
	// membership rows block a conflicting type change even when the stored
	// product type does not match them.
	Insert(ctx context.Context, m *GroupedMember) error

	// Remove deletes one group-member link. NotFound when the row did not exist.
	Remove(ctx context.Context, groupProductID, memberProductID string) error
}
