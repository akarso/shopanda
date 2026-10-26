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

// txGroupedMemberRepo is a GroupedMemberRepository that can bind to a transaction.
// WithTx lives on the postgres adapter only; the domain port does not declare it.
type txGroupedMemberRepo interface {
	catalog.GroupedMemberRepository
	WithTx(tx *sql.Tx) catalog.GroupedMemberRepository
}

// GroupedService manages display-only grouped product membership.
// Adding or removing a member never changes that member's Purchasable result or visibility.
type GroupedService struct {
	products  catalog.ProductRepository
	members   catalog.GroupedMemberRepository
	txStarter TxStarter
}

// NewGroupedService creates a grouped membership service.
// txStarter is required so Add can lock both product rows in the same
// transaction as the membership insert. Pass *sql.DB from process wiring.
func NewGroupedService(products catalog.ProductRepository, members catalog.GroupedMemberRepository, txStarter TxStarter) *GroupedService {
	if products == nil {
		panic("catalog.NewGroupedService: nil products repository")
	}
	if members == nil {
		panic("catalog.NewGroupedService: nil members repository")
	}
	if txStarter == nil {
		panic("catalog.NewGroupedService: nil tx starter")
	}
	return &GroupedService{products: products, members: members, txStarter: txStarter}
}

// List returns all members for a grouped product.
func (s *GroupedService) List(ctx context.Context, groupID string) ([]catalog.GroupedMember, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil, apperror.Validation("group id must not be empty")
	}
	group, err := s.loadProduct(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if group.Type != catalog.TypeGrouped {
		return nil, apperror.Validation(fmt.Sprintf("only products with type %q can have grouped members", catalog.TypeGrouped))
	}
	rows, err := s.members.ListByGroupID(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("grouped: list members: %w", err)
	}
	if rows == nil {
		rows = []catalog.GroupedMember{}
	}
	return rows, nil
}

// Add validates and inserts a grouped membership link.
// A duplicate link is a no-op. The member's Purchasable and visibility are unchanged.
// After both rows are locked, a product that is not active is NotFound unless
// the context includes non-active products (catalog.WithOperatorProductReadScope).
// That matches List, which loads the parent through FindByID.
func (s *GroupedService) Add(ctx context.Context, m *catalog.GroupedMember) error {
	if m == nil {
		return apperror.Validation("grouped member must not be nil")
	}
	m.GroupProductID = strings.TrimSpace(m.GroupProductID)
	m.MemberProductID = strings.TrimSpace(m.MemberProductID)
	if err := m.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}

	tx, err := s.txStarter.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("grouped: begin tx: %w", err)
	}
	defer tx.Rollback()

	txProducts, okProducts := s.products.(txProductRepo)
	txMembers, okMembers := s.members.(txGroupedMemberRepo)
	if !okProducts || !okMembers {
		return fmt.Errorf("grouped: repositories do not support transactions")
	}
	productRepo := txProducts.WithTx(tx)
	memberRepo := txMembers.WithTx(tx)
	locker, ok := productRepo.(lockingProductRepo)
	if !ok {
		return fmt.Errorf("grouped: product repository does not support row locks")
	}

	// Lock in sorted id order so Add and a concurrent type change cannot deadlock.
	ids := []string{m.GroupProductID, m.MemberProductID}
	sort.Strings(ids)
	locked := make(map[string]catalog.Type, len(ids))
	for _, id := range ids {
		typ, err := locker.LockForUpdate(ctx, id)
		if err != nil {
			if apperror.Is(err, apperror.CodeNotFound) {
				return err
			}
			return fmt.Errorf("grouped: lock product: %w", err)
		}
		locked[id] = typ
	}
	if _, err := requireActiveCompositionProducts(ctx, productRepo, m.GroupProductID, m.MemberProductID); err != nil {
		if apperror.Is(err, apperror.CodeNotFound) {
			return err
		}
		return fmt.Errorf("grouped: find product: %w", err)
	}
	if err := m.ValidateComposition(locked[m.GroupProductID], locked[m.MemberProductID]); err != nil {
		return apperror.Validation(err.Error())
	}
	if err := memberRepo.Insert(ctx, m); err != nil {
		return fmt.Errorf("grouped: insert member: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("grouped: commit: %w", err)
	}
	return nil
}

// Remove deletes one member from a group. The member product itself is unchanged.
// It checks the parent type with one FindByID and does not list the membership.
// It does not lock either product row. Membership is a display link: a future
// product delete API unlinks members by ON DELETE CASCADE and does not return
// 422 because a row exists. Silent unlink is intentional.
func (s *GroupedService) Remove(ctx context.Context, groupProductID, memberProductID string) error {
	groupProductID = strings.TrimSpace(groupProductID)
	memberProductID = strings.TrimSpace(memberProductID)
	if groupProductID == "" {
		return apperror.Validation("group product id must not be empty")
	}
	if memberProductID == "" {
		return apperror.Validation("member product id must not be empty")
	}
	group, err := s.loadProduct(ctx, groupProductID)
	if err != nil {
		return err
	}
	if group.Type != catalog.TypeGrouped {
		return apperror.Validation(fmt.Sprintf("only products with type %q can have grouped members", catalog.TypeGrouped))
	}
	if err := s.members.Remove(ctx, groupProductID, memberProductID); err != nil {
		return fmt.Errorf("grouped: remove member: %w", err)
	}
	return nil
}

func (s *GroupedService) loadProduct(ctx context.Context, productID string) (*catalog.Product, error) {
	if productID == "" {
		return nil, apperror.Validation("product id must not be empty")
	}
	product, err := s.products.FindByID(ctx, productID)
	if err != nil {
		return nil, fmt.Errorf("grouped: find product: %w", err)
	}
	if product == nil {
		return nil, apperror.NotFound("product not found")
	}
	return product, nil
}
