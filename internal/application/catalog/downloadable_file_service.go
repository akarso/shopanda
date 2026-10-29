package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// txDownloadableFileRepo is a DownloadableFileRepository that can bind to a
// transaction. WithTx lives on the postgres adapter only; the domain port
// does not declare it.
type txDownloadableFileRepo interface {
	catalog.DownloadableFileRepository
	WithTx(tx *sql.Tx) catalog.DownloadableFileRepository
}

// txAssetRepo binds a media asset repository to a transaction.
type txAssetRepo interface {
	WithTx(tx *sql.Tx) media.AssetRepository
}

// lockingAssetRepo locks an asset row inside a transaction (SELECT FOR UPDATE)
// and returns the locked row so Attach need not FindByID again.
type lockingAssetRepo interface {
	LockForUpdate(ctx context.Context, id string) (*media.Asset, error)
}

// DownloadableFileService manages product → media asset attachments for
// TypeDownloadable products (PR-1074). Storage stays on media.Storage /
// media.AssetRepository; this service only owns the join rows.
//
// Admin HTTP / CSV / GUI remain PR-1078.
type DownloadableFileService struct {
	products  catalog.ProductRepository
	files     catalog.DownloadableFileRepository
	assets    media.AssetRepository
	txStarter TxStarter
}

// NewDownloadableFileService creates a downloadable-file attachment service.
// txStarter is required so Attach can lock the product row in the same
// transaction as the upsert. Pass *sql.DB from process wiring.
func NewDownloadableFileService(
	products catalog.ProductRepository,
	files catalog.DownloadableFileRepository,
	assets media.AssetRepository,
	txStarter TxStarter,
) *DownloadableFileService {
	if products == nil {
		panic("catalog.NewDownloadableFileService: nil products repository")
	}
	if files == nil {
		panic("catalog.NewDownloadableFileService: nil files repository")
	}
	if assets == nil {
		panic("catalog.NewDownloadableFileService: nil assets repository")
	}
	if txStarter == nil {
		panic("catalog.NewDownloadableFileService: nil tx starter")
	}
	return &DownloadableFileService{
		products:  products,
		files:     files,
		assets:    assets,
		txStarter: txStarter,
	}
}

// List returns all downloadable_files join rows for a product regardless of
// type (like Remove). Attach stays type-gated. Operators can see illicit
// asset IDs on a non-downloadable product before Remove or a type change.
func (s *DownloadableFileService) List(ctx context.Context, productID string) ([]catalog.DownloadableFile, error) {
	productID = strings.TrimSpace(productID)
	if _, err := s.loadProduct(ctx, productID); err != nil {
		return nil, err
	}
	rows, err := s.files.ListByProductID(ctx, productID)
	if err != nil {
		return nil, fmt.Errorf("downloadable file: list: %w", err)
	}
	if rows == nil {
		rows = []catalog.DownloadableFile{}
	}
	return rows, nil
}

// Attach validates and upserts a product → asset link.
// The product and asset rows are locked FOR UPDATE in the same transaction as
// the upsert so a concurrent media delete cannot race past the join insert.
func (s *DownloadableFileService) Attach(ctx context.Context, f *catalog.DownloadableFile) error {
	if f == nil {
		return apperror.Validation("downloadable file must not be nil")
	}
	f.ProductID = strings.TrimSpace(f.ProductID)
	f.AssetID = strings.TrimSpace(f.AssetID)
	if err := f.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}

	tx, err := s.txStarter.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("downloadable file: begin tx: %w", err)
	}
	defer tx.Rollback()

	txProducts, okProducts := s.products.(txProductRepo)
	txFiles, okFiles := s.files.(txDownloadableFileRepo)
	txAssets, okAssets := s.assets.(txAssetRepo)
	if !okProducts || !okFiles || !okAssets {
		return fmt.Errorf("downloadable file: repositories do not support transactions")
	}
	productRepo := txProducts.WithTx(tx)
	fileRepo := txFiles.WithTx(tx)
	assetRepo := txAssets.WithTx(tx)
	locker, ok := productRepo.(lockingProductRepo)
	if !ok {
		return fmt.Errorf("downloadable file: product repository does not support row locks")
	}
	assetLocker, ok := assetRepo.(lockingAssetRepo)
	if !ok {
		return fmt.Errorf("downloadable file: asset repository does not support row locks")
	}

	typ, err := locker.LockForUpdate(ctx, f.ProductID)
	if err != nil {
		if apperror.Is(err, apperror.CodeNotFound) {
			return err
		}
		return fmt.Errorf("downloadable file: lock product: %w", err)
	}
	asset, err := assetLocker.LockForUpdate(ctx, f.AssetID)
	if err != nil {
		if apperror.Is(err, apperror.CodeNotFound) {
			return err
		}
		return fmt.Errorf("downloadable file: lock asset: %w", err)
	}
	if asset == nil {
		return apperror.NotFound("asset not found")
	}
	// Grant targets must live under private/downloads/ so public uploads/
	// objects cannot be attached and hotlinked past delivery limits (PR-1077).
	if !media.IsPrivateDownloadsPath(asset.Path) {
		return apperror.Validation("downloadable file asset must be stored under private/downloads/")
	}
	if _, err := requireActiveCompositionProducts(ctx, productRepo, f.ProductID); err != nil {
		if apperror.Is(err, apperror.CodeNotFound) {
			return err
		}
		return fmt.Errorf("downloadable file: find product: %w", err)
	}
	if err := f.ValidateAttachment(typ); err != nil {
		return apperror.Validation(err.Error())
	}
	if err := fileRepo.Upsert(ctx, f); err != nil {
		return fmt.Errorf("downloadable file: upsert: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("downloadable file: commit: %w", err)
	}
	return nil
}

// Remove deletes one attachment regardless of product type (404 if the pair
// is missing). Last-file-while-active is enforced only when the locked type is
// downloadable and status is active — so illicit join rows on a simple product
// can be cleared before a type change proceeds.
func (s *DownloadableFileService) Remove(ctx context.Context, productID, assetID string) error {
	productID = strings.TrimSpace(productID)
	assetID = strings.TrimSpace(assetID)
	if productID == "" {
		return apperror.Validation("product id must not be empty")
	}
	if assetID == "" {
		return apperror.Validation("asset id must not be empty")
	}

	tx, err := s.txStarter.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("downloadable file: begin tx: %w", err)
	}
	defer tx.Rollback()

	txProducts, okProducts := s.products.(txProductRepo)
	txFiles, okFiles := s.files.(txDownloadableFileRepo)
	if !okProducts || !okFiles {
		return fmt.Errorf("downloadable file: repositories do not support transactions")
	}
	productRepo := txProducts.WithTx(tx)
	fileRepo := txFiles.WithTx(tx)
	locker, ok := productRepo.(lockingProductRepo)
	if !ok {
		return fmt.Errorf("downloadable file: product repository does not support row locks")
	}

	typ, err := locker.LockForUpdate(ctx, productID)
	if err != nil {
		if apperror.Is(err, apperror.CodeNotFound) {
			return err
		}
		return fmt.Errorf("downloadable file: lock product: %w", err)
	}
	product, err := productRepo.FindByID(ctx, productID)
	if err != nil {
		return fmt.Errorf("downloadable file: find product: %w", err)
	}
	if product == nil {
		return apperror.NotFound("product not found")
	}

	// Load rows first so a missing (product, asset) pair stays NotFound even
	// when the product is active with exactly one other attachment.
	rows, err := fileRepo.ListByProductID(ctx, productID)
	if err != nil {
		return fmt.Errorf("downloadable file: list: %w", err)
	}
	found := false
	for _, row := range rows {
		if row.AssetID == assetID {
			found = true
			break
		}
	}
	if !found {
		return apperror.NotFound("downloadable file not found")
	}
	if typ == catalog.TypeDownloadable && product.Status == catalog.StatusActive && len(rows) == 1 {
		return apperror.Validation(catalog.DownloadableRequiresFileMessage)
	}

	if err := fileRepo.Remove(ctx, productID, assetID); err != nil {
		return fmt.Errorf("downloadable file: remove: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("downloadable file: commit: %w", err)
	}
	return nil
}

func (s *DownloadableFileService) loadProduct(ctx context.Context, productID string) (*catalog.Product, error) {
	if productID == "" {
		return nil, apperror.Validation("product id must not be empty")
	}
	product, err := s.products.FindByID(ctx, productID)
	if err != nil {
		return nil, fmt.Errorf("downloadable file: find product: %w", err)
	}
	if product == nil {
		return nil, apperror.NotFound("product not found")
	}
	return product, nil
}
