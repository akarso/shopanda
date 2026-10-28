package catalog

import "context"

// LinkedEligibilityRepository persists catalog-time linked-product eligibility
// rules. Rows do not affect a child's Purchasable result or visibility.
//
// Transaction binding is not part of this port. WithTx is implemented on the
// postgres adapter only (postgres.LinkedEligibilityRepo) and is used by
// LinkedEligibilityService.AddParent and AddCategory.
type LinkedEligibilityRepository interface {
	// ListParentsByChildID returns explicit parent allowlist rows for
	// childProductID ordered by created_at, then parent_product_id.
	// Always returns a non-nil slice (possibly empty).
	ListParentsByChildID(ctx context.Context, childProductID string) ([]LinkedEligibleParent, error)

	// ListCategoriesByChildID returns category rules for childProductID
	// ordered by created_at, then category_id. Always returns a non-nil
	// slice (possibly empty).
	ListCategoriesByChildID(ctx context.Context, childProductID string) ([]LinkedEligibleCategory, error)

	// CountRulesByChildID returns how many allowlist rows plus category
	// rules belong to the child product. Missing products are zero, not
	// an error. This is the row count behind HasRules; it does not read
	// product status.
	CountRulesByChildID(ctx context.Context, childProductID string) (int, error)

	// InsertParent adds an allowlist row. A duplicate composite key is a
	// no-op (insert-ignore); there are no mutable columns on the link.
	//
	// InsertParent stays on this port because LinkedEligibilityService.AddParent
	// calls it. It checks field validation only. Product types are not stored
	// or checked here.
	InsertParent(ctx context.Context, p *LinkedEligibleParent) error

	// InsertCategory adds a category rule. A duplicate composite key is a
	// no-op (insert-ignore). Field validation only.
	InsertCategory(ctx context.Context, c *LinkedEligibleCategory) error

	// RemoveParent deletes one allowlist row. NotFound when the row did not exist.
	RemoveParent(ctx context.Context, childProductID, parentProductID string) error

	// RemoveCategory deletes one category rule. NotFound when the row did not exist.
	RemoveCategory(ctx context.Context, childProductID, categoryID string) error

	// ListAllParentSlugRows returns every allowlist row joined to product
	// slugs, ordered by child_slug then parent_slug. Used by CSV export so
	// callers do not page the product catalog. Always returns a non-nil slice.
	ListAllParentSlugRows(ctx context.Context) ([]LinkedEligibleParentSlugRow, error)

	// ListAllCategorySlugRows returns every category rule joined to product
	// and category slugs, ordered by child_slug then category_slug. Always
	// returns a non-nil slice.
	ListAllCategorySlugRows(ctx context.Context) ([]LinkedEligibleCategorySlugRow, error)

	// IsEligible is true when an explicit parent row exists or the parent
	// product is assigned to any of the child's eligible categories via
	// product_categories. childID == parentID is false. Missing products
	// or empty rule sets are false, not an error. Product status and
	// visibility are not consulted.
	IsEligible(ctx context.Context, childProductID, parentProductID string) (bool, error)
}
