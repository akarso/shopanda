package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// txLinkedEligibilityRepo is a LinkedEligibilityRepository that can bind to a
// transaction. WithTx lives on the postgres adapter only; the domain port
// does not declare it.
type txLinkedEligibilityRepo interface {
	catalog.LinkedEligibilityRepository
	WithTx(tx *sql.Tx) catalog.LinkedEligibilityRepository
}

// LinkedEligibilityService manages catalog-time linked-product eligibility.
// A child product declares which parents it may later be assigned to, via an
// explicit parent-product allowlist and/or a category rule (any product in
// that category). Evaluation is OR across both mechanisms.
//
// Eligibility is not composition and does not change Purchasable or
// visibility. IsEligible and HasRules are join-table predicates: they
// do not read product status or visibility. Callers that already loaded
// the product apply those gates themselves (PR-1068).
type LinkedEligibilityService struct {
	products   catalog.ProductRepository
	categories catalog.CategoryRepository
	rules      catalog.LinkedEligibilityRepository
	txStarter  TxStarter
}

// NewLinkedEligibilityService creates a linked-eligibility service.
// txStarter is required so AddParent / AddCategory can lock product rows in
// the same transaction as the insert. Pass *sql.DB from process wiring.
func NewLinkedEligibilityService(
	products catalog.ProductRepository,
	categories catalog.CategoryRepository,
	rules catalog.LinkedEligibilityRepository,
	txStarter TxStarter,
) *LinkedEligibilityService {
	if products == nil {
		panic("catalog.NewLinkedEligibilityService: nil products repository")
	}
	if categories == nil {
		panic("catalog.NewLinkedEligibilityService: nil categories repository")
	}
	if rules == nil {
		panic("catalog.NewLinkedEligibilityService: nil eligibility repository")
	}
	if txStarter == nil {
		panic("catalog.NewLinkedEligibilityService: nil tx starter")
	}
	return &LinkedEligibilityService{
		products:   products,
		categories: categories,
		rules:      rules,
		txStarter:  txStarter,
	}
}

// ListParents returns explicit parent allowlist rows for a child product.
func (s *LinkedEligibilityService) ListParents(ctx context.Context, childID string) ([]catalog.LinkedEligibleParent, error) {
	childID = strings.TrimSpace(childID)
	if _, err := s.loadProduct(ctx, childID); err != nil {
		return nil, err
	}
	rows, err := s.rules.ListParentsByChildID(ctx, childID)
	if err != nil {
		return nil, fmt.Errorf("linked eligibility: list parents: %w", err)
	}
	if rows == nil {
		rows = []catalog.LinkedEligibleParent{}
	}
	return rows, nil
}

// ListCategories returns category rules for a child product.
func (s *LinkedEligibilityService) ListCategories(ctx context.Context, childID string) ([]catalog.LinkedEligibleCategory, error) {
	childID = strings.TrimSpace(childID)
	if _, err := s.loadProduct(ctx, childID); err != nil {
		return nil, err
	}
	rows, err := s.rules.ListCategoriesByChildID(ctx, childID)
	if err != nil {
		return nil, fmt.Errorf("linked eligibility: list categories: %w", err)
	}
	if rows == nil {
		rows = []catalog.LinkedEligibleCategory{}
	}
	return rows, nil
}


// ListAllParentSlugRows returns every allowlist row with product slugs for CSV export.
func (s *LinkedEligibilityService) ListAllParentSlugRows(ctx context.Context) ([]catalog.LinkedEligibleParentSlugRow, error) {
	rows, err := s.rules.ListAllParentSlugRows(ctx)
	if err != nil {
		return nil, fmt.Errorf("linked eligibility: list all parent slug rows: %w", err)
	}
	if rows == nil {
		rows = []catalog.LinkedEligibleParentSlugRow{}
	}
	return rows, nil
}

// ListAllCategorySlugRows returns every category rule with slugs for CSV export.
func (s *LinkedEligibilityService) ListAllCategorySlugRows(ctx context.Context) ([]catalog.LinkedEligibleCategorySlugRow, error) {
	rows, err := s.rules.ListAllCategorySlugRows(ctx)
	if err != nil {
		return nil, fmt.Errorf("linked eligibility: list all category slug rows: %w", err)
	}
	if rows == nil {
		rows = []catalog.LinkedEligibleCategorySlugRow{}
	}
	return rows, nil
}

// AddParent validates and insert-ignores an explicit parent allowlist row.
// After both rows are locked, a product that is not active is NotFound unless
// the context includes non-active products (catalog.WithOperatorProductReadScope).
// That matches ListParents, which loads the child through FindByID.
func (s *LinkedEligibilityService) AddParent(ctx context.Context, p *catalog.LinkedEligibleParent) error {
	if p == nil {
		return apperror.Validation("linked eligible parent must not be nil")
	}
	p.ChildProductID = strings.TrimSpace(p.ChildProductID)
	p.ParentProductID = strings.TrimSpace(p.ParentProductID)
	if err := p.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}

	tx, err := s.txStarter.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("linked eligibility: begin tx: %w", err)
	}
	defer tx.Rollback()

	txProducts, okProducts := s.products.(txProductRepo)
	txRules, okRules := s.rules.(txLinkedEligibilityRepo)
	if !okProducts || !okRules {
		return fmt.Errorf("linked eligibility: repositories do not support transactions")
	}
	productRepo := txProducts.WithTx(tx)
	ruleRepo := txRules.WithTx(tx)
	locker, ok := productRepo.(lockingProductRepo)
	if !ok {
		return fmt.Errorf("linked eligibility: product repository does not support row locks")
	}

	// Fold case before sorting. Postgres locks the UUID row, so two
	// spellings of the same pair must take one lock order.
	ids := []string{strings.ToLower(p.ChildProductID), strings.ToLower(p.ParentProductID)}
	sort.Strings(ids)
	for _, id := range ids {
		if _, err := locker.LockForUpdate(ctx, id); err != nil {
			if apperror.Is(err, apperror.CodeNotFound) {
				return err
			}
			return fmt.Errorf("linked eligibility: lock product: %w", err)
		}
	}
	if _, err := requireActiveCompositionProducts(ctx, productRepo, p.ChildProductID, p.ParentProductID); err != nil {
		if apperror.Is(err, apperror.CodeNotFound) {
			return err
		}
		return fmt.Errorf("linked eligibility: find product: %w", err)
	}
	if err := ruleRepo.InsertParent(ctx, p); err != nil {
		return fmt.Errorf("linked eligibility: insert parent: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("linked eligibility: commit: %w", err)
	}
	return nil
}

// AddCategory validates and insert-ignores a category eligibility rule.
// After the child row is locked, a product that is not active is NotFound
// unless the context includes non-active products.
func (s *LinkedEligibilityService) AddCategory(ctx context.Context, c *catalog.LinkedEligibleCategory) error {
	if c == nil {
		return apperror.Validation("linked eligible category must not be nil")
	}
	c.ChildProductID = strings.TrimSpace(c.ChildProductID)
	c.CategoryID = strings.TrimSpace(c.CategoryID)
	if err := c.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}

	tx, err := s.txStarter.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("linked eligibility: begin tx: %w", err)
	}
	defer tx.Rollback()

	txProducts, okProducts := s.products.(txProductRepo)
	txRules, okRules := s.rules.(txLinkedEligibilityRepo)
	if !okProducts || !okRules {
		return fmt.Errorf("linked eligibility: repositories do not support transactions")
	}
	productRepo := txProducts.WithTx(tx)
	ruleRepo := txRules.WithTx(tx)
	locker, ok := productRepo.(lockingProductRepo)
	if !ok {
		return fmt.Errorf("linked eligibility: product repository does not support row locks")
	}

	if _, err := locker.LockForUpdate(ctx, c.ChildProductID); err != nil {
		if apperror.Is(err, apperror.CodeNotFound) {
			return err
		}
		return fmt.Errorf("linked eligibility: lock product: %w", err)
	}
	if _, err := requireActiveCompositionProducts(ctx, productRepo, c.ChildProductID); err != nil {
		if apperror.Is(err, apperror.CodeNotFound) {
			return err
		}
		return fmt.Errorf("linked eligibility: find product: %w", err)
	}
	// CategoryRepo has no WithTx; this read is on a different connection
	// than the child lock. A concurrent category delete can still fail
	// the insert via FK 23503 (mapped to "category not found" for
	// linked_eligible_categories_category_id_fkey) or CASCADE the new
	// row after commit.
	cat, err := s.categories.FindByID(ctx, c.CategoryID)
	if err != nil {
		return fmt.Errorf("linked eligibility: find category: %w", err)
	}
	if cat == nil {
		return apperror.NotFound("category not found")
	}
	if err := ruleRepo.InsertCategory(ctx, c); err != nil {
		return fmt.Errorf("linked eligibility: insert category: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("linked eligibility: commit: %w", err)
	}
	return nil
}

// RemoveParent deletes one allowlist row. It loads the child with one
// FindByID and does not lock product rows.
func (s *LinkedEligibilityService) RemoveParent(ctx context.Context, childProductID, parentProductID string) error {
	childProductID = strings.TrimSpace(childProductID)
	parentProductID = strings.TrimSpace(parentProductID)
	if _, err := s.loadProduct(ctx, childProductID); err != nil {
		return err
	}
	if parentProductID == "" {
		return apperror.Validation("parent product id must not be empty")
	}
	if err := s.rules.RemoveParent(ctx, childProductID, parentProductID); err != nil {
		return fmt.Errorf("linked eligibility: remove parent: %w", err)
	}
	return nil
}

// RemoveCategory deletes one category rule. It loads the child with one
// FindByID and does not lock product rows.
func (s *LinkedEligibilityService) RemoveCategory(ctx context.Context, childProductID, categoryID string) error {
	childProductID = strings.TrimSpace(childProductID)
	categoryID = strings.TrimSpace(categoryID)
	if _, err := s.loadProduct(ctx, childProductID); err != nil {
		return err
	}
	if categoryID == "" {
		return apperror.Validation("category id must not be empty")
	}
	if err := s.rules.RemoveCategory(ctx, childProductID, categoryID); err != nil {
		return fmt.Errorf("linked eligibility: remove category: %w", err)
	}
	return nil
}

// IsEligible reports whether parentID may later receive an assignment of
// childID. Empty IDs are validation. A child listing itself is false.
// Missing products or an empty rule set are false, not NotFound — this is
// a catalog-rule predicate, not a product read. It does not consult
// product status or visibility: a draft or archived pair with a matching
// rule is true. Eligibility is not purchasable and not assignable;
// PR-1068 / PR-1069 must check active/visible on both sides.
func (s *LinkedEligibilityService) IsEligible(ctx context.Context, childID, parentID string) (bool, error) {
	childID = strings.TrimSpace(childID)
	parentID = strings.TrimSpace(parentID)
	if childID == "" {
		return false, apperror.Validation("child product id must not be empty")
	}
	if parentID == "" {
		return false, apperror.Validation("parent product id must not be empty")
	}
	if strings.EqualFold(childID, parentID) {
		return false, nil
	}
	ok, err := s.rules.IsEligible(ctx, childID, parentID)
	if err != nil {
		return false, fmt.Errorf("linked eligibility: is eligible: %w", err)
	}
	return ok, nil
}

// HasRules reports whether the child has at least one allowlist row or
// category rule. Empty IDs are validation. A missing child or an empty
// rule set is false, not NotFound — same failure model as IsEligible.
// It does not consult product status or visibility. Eligibility is not
// purchasable and not assignable; PR-1068 / PR-1069 must check
// active/visible on both sides. Do not derive RequiresLinkedParent from
// this method, and do not treat errors.Is(err, NotFound) as "no rules."
func (s *LinkedEligibilityService) HasRules(ctx context.Context, childID string) (bool, error) {
	childID = strings.TrimSpace(childID)
	if childID == "" {
		return false, apperror.Validation("child product id must not be empty")
	}
	n, err := s.rules.CountRulesByChildID(ctx, childID)
	if err != nil {
		return false, fmt.Errorf("linked eligibility: count rules: %w", err)
	}
	return n > 0, nil
}

func (s *LinkedEligibilityService) loadProduct(ctx context.Context, productID string) (*catalog.Product, error) {
	if productID == "" {
		return nil, apperror.Validation("product id must not be empty")
	}
	product, err := s.products.FindByID(ctx, productID)
	if err != nil {
		return nil, fmt.Errorf("linked eligibility: find product: %w", err)
	}
	if product == nil {
		return nil, apperror.NotFound("product not found")
	}
	return product, nil
}
