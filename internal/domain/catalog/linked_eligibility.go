package catalog

import (
	"errors"
	"strings"
	"time"
)

// LinkedEligibleParent is an explicit allowlist row: this child product may
// later be assigned to this parent product. It is not composition (bundle or
// grouped) and does not change Purchasable or visibility (PR-1068 / PR-1055).
//
// The same child may list many parents. The same parent may appear on many
// children. The primary key is the pair.
type LinkedEligibleParent struct {
	ChildProductID  string
	ParentProductID string
	CreatedAt       time.Time
}

// LinkedEligibleCategory is a category-based eligibility rule: this child
// may later be assigned to any product that belongs to CategoryID via
// product_categories. Membership is exact (the assigned category), not an
// ancestor walk.
//
// This is the same catalog-scoping idea promotions use when they match
// catalog entities (typed condition + catalog membership) — not a second
// rule engine. Evaluation is OR with any explicit parent allowlist rows.
//
// Adding or removing a rule does not change Purchasable or visibility.
type LinkedEligibleCategory struct {
	ChildProductID string
	CategoryID     string
	CreatedAt      time.Time
}

// NewLinkedEligibleParent builds an allowlist row. IDs are trimmed; field
// validation runs before return.
func NewLinkedEligibleParent(childID, parentID string) (*LinkedEligibleParent, error) {
	p := &LinkedEligibleParent{
		ChildProductID:  strings.TrimSpace(childID),
		ParentProductID: strings.TrimSpace(parentID),
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

// NewLinkedEligibleCategory builds a category rule. IDs are trimmed; field
// validation runs before return.
func NewLinkedEligibleCategory(childID, categoryID string) (*LinkedEligibleCategory, error) {
	c := &LinkedEligibleCategory{
		ChildProductID: strings.TrimSpace(childID),
		CategoryID:     strings.TrimSpace(categoryID),
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// Validate checks field-level constraints before persist.
// It does not trim IDs. NewLinkedEligibleParent and LinkedEligibilityService
// trim before calling Validate.
func (p *LinkedEligibleParent) Validate() error {
	if p == nil {
		return errors.New("linked eligible parent must not be nil")
	}
	childID := strings.TrimSpace(p.ChildProductID)
	parentID := strings.TrimSpace(p.ParentProductID)
	if childID == "" {
		return errors.New("child product id must not be empty")
	}
	if parentID == "" {
		return errors.New("parent product id must not be empty")
	}
	if childID == parentID {
		return errors.New("linked child cannot list itself as an eligible parent")
	}
	return nil
}

// Validate checks field-level constraints before persist.
// It does not trim IDs. NewLinkedEligibleCategory and LinkedEligibilityService
// trim before calling Validate.
func (c *LinkedEligibleCategory) Validate() error {
	if c == nil {
		return errors.New("linked eligible category must not be nil")
	}
	childID := strings.TrimSpace(c.ChildProductID)
	categoryID := strings.TrimSpace(c.CategoryID)
	if childID == "" {
		return errors.New("child product id must not be empty")
	}
	if categoryID == "" {
		return errors.New("category id must not be empty")
	}
	return nil
}

// LinkedEligibleParentSlugRow is one allowlist rule with product slugs for CSV export.
type LinkedEligibleParentSlugRow struct {
	ChildSlug  string
	ParentSlug string
}

// LinkedEligibleCategorySlugRow is one category rule with product and category
// slugs for CSV export.
type LinkedEligibleCategorySlugRow struct {
	ChildSlug    string
	CategorySlug string
}

