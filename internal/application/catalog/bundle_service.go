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

// TxStarter begins database transactions. *sql.DB implements it.
type TxStarter interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// txProductRepo is a ProductRepository that can bind to a transaction.
type txProductRepo interface {
	catalog.ProductRepository
	WithTx(tx *sql.Tx) catalog.ProductRepository
}

// lockingProductRepo is a transaction-bound product repository that can lock a row.
type lockingProductRepo interface {
	catalog.ProductRepository
	LockForUpdate(ctx context.Context, id string) (catalog.Type, error)
}

// txBundleComponentRepo is a BundleComponentRepository that can bind to a transaction.
type txBundleComponentRepo interface {
	catalog.BundleComponentRepository
	WithTx(tx *sql.Tx) catalog.BundleComponentRepository
}

// BundleService manages bundle product composition.
type BundleService struct {
	products   catalog.ProductRepository
	components catalog.BundleComponentRepository
	txStarter  TxStarter
}

// NewBundleService creates a bundle composition service.
// txStarter is required so Add can lock both product rows in the same
// transaction as the component upsert. Pass *sql.DB from process wiring.
func NewBundleService(products catalog.ProductRepository, components catalog.BundleComponentRepository, txStarter TxStarter) *BundleService {
	if products == nil {
		panic("catalog.NewBundleService: nil products repository")
	}
	if components == nil {
		panic("catalog.NewBundleService: nil components repository")
	}
	if txStarter == nil {
		panic("catalog.NewBundleService: nil tx starter")
	}
	return &BundleService{products: products, components: components, txStarter: txStarter}
}

// List returns all components for a bundle product.
func (s *BundleService) List(ctx context.Context, bundleID string) ([]catalog.BundleComponent, error) {
	bundleID = strings.TrimSpace(bundleID)
	if bundleID == "" {
		return nil, apperror.Validation("bundle id must not be empty")
	}
	bundle, err := s.loadProduct(ctx, bundleID)
	if err != nil {
		return nil, err
	}
	if bundle.Type != catalog.TypeBundle {
		return nil, apperror.Validation(fmt.Sprintf("only products with type %q can have bundle components", catalog.TypeBundle))
	}
	rows, err := s.components.ListByBundleID(ctx, bundleID)
	if err != nil {
		return nil, fmt.Errorf("bundle: list components: %w", err)
	}
	if rows == nil {
		rows = []catalog.BundleComponent{}
	}
	return rows, nil
}

// Add validates and upserts a bundle component link.
func (s *BundleService) Add(ctx context.Context, c *catalog.BundleComponent) error {
	if c == nil {
		return apperror.Validation("bundle component must not be nil")
	}
	c.BundleProductID = strings.TrimSpace(c.BundleProductID)
	c.ComponentProductID = strings.TrimSpace(c.ComponentProductID)
	if err := c.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}

	tx, err := s.txStarter.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("bundle: begin tx: %w", err)
	}
	defer tx.Rollback()

	txProducts, okProducts := s.products.(txProductRepo)
	txComponents, okComponents := s.components.(txBundleComponentRepo)
	if !okProducts || !okComponents {
		return fmt.Errorf("bundle: repositories do not support transactions")
	}
	productRepo := txProducts.WithTx(tx)
	componentRepo := txComponents.WithTx(tx)
	locker, ok := productRepo.(lockingProductRepo)
	if !ok {
		return fmt.Errorf("bundle: product repository does not support row locks")
	}

	// Lock in sorted id order so Add and a concurrent type change cannot deadlock.
	ids := []string{c.BundleProductID, c.ComponentProductID}
	sort.Strings(ids)
	locked := make(map[string]catalog.Type, len(ids))
	for _, id := range ids {
		typ, err := locker.LockForUpdate(ctx, id)
		if err != nil {
			if apperror.Is(err, apperror.CodeNotFound) {
				return err
			}
			return fmt.Errorf("bundle: lock product: %w", err)
		}
		locked[id] = typ
	}
	if err := requireActiveCompositionProducts(ctx, productRepo, c.BundleProductID, c.ComponentProductID); err != nil {
		if apperror.Is(err, apperror.CodeNotFound) {
			return err
		}
		return fmt.Errorf("bundle: find product: %w", err)
	}
	if err := c.ValidateComposition(locked[c.BundleProductID], locked[c.ComponentProductID]); err != nil {
		return apperror.Validation(err.Error())
	}
	if err := componentRepo.Upsert(ctx, c); err != nil {
		return fmt.Errorf("bundle: upsert component: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("bundle: commit: %w", err)
	}
	return nil
}

// Remove deletes one component from a bundle.
// It does not lock either product row. Callers that already hold the ids
// delete the link only; product deletion is a separate path.
func (s *BundleService) Remove(ctx context.Context, bundleProductID, componentProductID string) error {
	bundleProductID = strings.TrimSpace(bundleProductID)
	componentProductID = strings.TrimSpace(componentProductID)
	if bundleProductID == "" {
		return apperror.Validation("bundle product id must not be empty")
	}
	if componentProductID == "" {
		return apperror.Validation("component product id must not be empty")
	}
	if err := s.components.Remove(ctx, bundleProductID, componentProductID); err != nil {
		return fmt.Errorf("bundle: remove component: %w", err)
	}
	return nil
}

func (s *BundleService) loadProduct(ctx context.Context, productID string) (*catalog.Product, error) {
	if productID == "" {
		return nil, apperror.Validation("product id must not be empty")
	}
	product, err := s.products.FindByID(ctx, productID)
	if err != nil {
		return nil, fmt.Errorf("bundle: find product: %w", err)
	}
	if product == nil {
		return nil, apperror.NotFound("product not found")
	}
	return product, nil
}

// requireActiveCompositionProducts rejects a link when either product is missing
// or not active, unless ctx includes non-active products. That is the same gate
// ProductRepository.FindByID uses (catalog.IncludeNonActiveProducts), which
// catalog.WithOperatorProductReadScope sets for admin and other operator callers.
// Call it on the transaction-bound product repo after the rows are locked.
// A hidden product is NotFound, matching List.
func requireActiveCompositionProducts(ctx context.Context, products catalog.ProductRepository, ids ...string) error {
	if catalog.IncludeNonActiveProducts(ctx) {
		return nil
	}
	for _, id := range ids {
		p, err := products.FindByID(ctx, id)
		if err != nil {
			return err
		}
		if p == nil || p.Status != catalog.StatusActive {
			return apperror.NotFound("product not found")
		}
	}
	return nil
}
