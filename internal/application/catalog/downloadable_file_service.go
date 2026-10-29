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

// GrantConflictRevoked is a grant expired during Replace retarget because a
// live UNIQUE (order_item_id, new_asset_id) row already existed.
type GrantConflictRevoked struct {
	ID          string
	OrderItemID string
	CustomerID  string
	AssetID     string
}

// GrantRetargetOutcome is the result of moving grants old→new during Replace.
// Moved counts rows rewritten in the free UNIQUE UPDATE. Revoked are live
// grants expired for a live UNIQUE conflict (step 4). Expired old rows
// deleted to free UNIQUE are not counted here — those grant IDs vanish.
type GrantRetargetOutcome struct {
	Moved   int
	Revoked []GrantConflictRevoked
}

// GrantRetargetInTx moves DownloadGrant rows from old->new asset inside the
// Replace transaction (PR-1078). Asset-scoped (no product/order_item join).
// Callers publish download.grant.revoked after the outer TX commits for
// outcome.Revoked.
type GrantRetargetInTx func(ctx context.Context, tx *sql.Tx, oldAssetID, newAssetID string) (GrantRetargetOutcome, error)

// GrantConflictRevokeNotifier publishes observability for grants expired
// during retarget conflict handling (after Replace commits).
type GrantConflictRevokeNotifier func(ctx context.Context, revoked []GrantConflictRevoked)

// ReplaceResult surfaces grant retarget counts from a successful Replace.
type ReplaceResult struct {
	GrantsMoved   int
	GrantsRevoked int
}

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
// Admin HTTP / CSV / GUI are PR-1078.
type DownloadableFileService struct {
	products             catalog.ProductRepository
	files                catalog.DownloadableFileRepository
	assets               media.AssetRepository
	txStarter            TxStarter
	grantRetarget        GrantRetargetInTx           // required for real Replace (different asset ids)
	grantConflictRevokes GrantConflictRevokeNotifier // optional; events after commit
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

// WithGrantRetarget wires DownloadGrant asset retargeting into Replace.
// Required for a real replace (different asset ids); nil fails closed.
func (s *DownloadableFileService) WithGrantRetarget(fn GrantRetargetInTx) *DownloadableFileService {
	if s == nil {
		return nil
	}
	s.grantRetarget = fn
	return s
}

// WithGrantConflictRevokeNotifier wires post-commit revoke event publishing
// for grants expired when retarget hits a live UNIQUE conflict.
func (s *DownloadableFileService) WithGrantConflictRevokeNotifier(fn GrantConflictRevokeNotifier) *DownloadableFileService {
	if s == nil {
		return nil
	}
	s.grantConflictRevokes = fn
	return s
}

// ReplaceMeta controls whether omitted position/label keep the old join values
// and whether a shared asset may retarget every grant on the blob.
type ReplaceMeta struct {
	Position *int
	Label    *string
	// GlobalRetarget allows Replace when other products still attach
	// old_asset_id. Without it, a shared attachment returns 422 — Replace
	// never rewrites "this product only". When true, every grant on the
	// blob moves (asset-scoped RetargetAsset).
	GlobalRetarget bool
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

// Replace swaps oldAssetID for a new attachment in one transaction and
// retargets DownloadGrant rows (live and expired) from old->new asset by
// asset id (covers bundle-parent units). Expired conflicting new-asset
// rows are cleared first; live UNIQUE conflict revokes the old grant.
// When meta Position/Label are nil, the old join's values are preserved.
// Same asset id is a plain Attach (position/label update).
//
// Shared assets (other products still attach oldAssetID) refuse with 422
// unless meta.GlobalRetarget is true. Without the flag, Replace does not
// rewrite "this product only" — it fails closed. With the flag, every
// grant on the blob moves.
func (s *DownloadableFileService) Replace(ctx context.Context, productID, oldAssetID string, f *catalog.DownloadableFile, meta ReplaceMeta) (ReplaceResult, error) {
	productID = strings.TrimSpace(productID)
	oldAssetID = strings.TrimSpace(oldAssetID)
	if productID == "" {
		return ReplaceResult{}, apperror.Validation("product id must not be empty")
	}
	if oldAssetID == "" {
		return ReplaceResult{}, apperror.Validation("old asset id must not be empty")
	}
	if f == nil {
		return ReplaceResult{}, apperror.Validation("downloadable file must not be nil")
	}
	f.ProductID = productID
	f.AssetID = strings.TrimSpace(f.AssetID)

	if f.AssetID == oldAssetID {
		if meta.Position != nil {
			f.Position = *meta.Position
		}
		if meta.Label != nil {
			f.Label = *meta.Label
		}
		if err := f.Validate(); err != nil {
			return ReplaceResult{}, apperror.Validation(err.Error())
		}
		return ReplaceResult{}, s.Attach(ctx, f)
	}

	tx, err := s.txStarter.BeginTx(ctx, nil)
	if err != nil {
		return ReplaceResult{}, fmt.Errorf("downloadable file: begin tx: %w", err)
	}
	defer tx.Rollback()

	txProducts, okProducts := s.products.(txProductRepo)
	txFiles, okFiles := s.files.(txDownloadableFileRepo)
	txAssets, okAssets := s.assets.(txAssetRepo)
	if !okProducts || !okFiles || !okAssets {
		return ReplaceResult{}, fmt.Errorf("downloadable file: repositories do not support transactions")
	}
	productRepo := txProducts.WithTx(tx)
	fileRepo := txFiles.WithTx(tx)
	assetRepo := txAssets.WithTx(tx)
	locker, ok := productRepo.(lockingProductRepo)
	if !ok {
		return ReplaceResult{}, fmt.Errorf("downloadable file: product repository does not support row locks")
	}
	assetLocker, ok := assetRepo.(lockingAssetRepo)
	if !ok {
		return ReplaceResult{}, fmt.Errorf("downloadable file: asset repository does not support row locks")
	}

	typ, err := locker.LockForUpdate(ctx, productID)
	if err != nil {
		if apperror.Is(err, apperror.CodeNotFound) {
			return ReplaceResult{}, err
		}
		return ReplaceResult{}, fmt.Errorf("downloadable file: lock product: %w", err)
	}
	asset, err := assetLocker.LockForUpdate(ctx, f.AssetID)
	if err != nil {
		if apperror.Is(err, apperror.CodeNotFound) {
			return ReplaceResult{}, err
		}
		return ReplaceResult{}, fmt.Errorf("downloadable file: lock asset: %w", err)
	}
	if asset == nil {
		return ReplaceResult{}, apperror.NotFound("asset not found")
	}
	if !media.IsPrivateDownloadsPath(asset.Path) {
		return ReplaceResult{}, apperror.Validation("downloadable file asset must be stored under private/downloads/")
	}
	if _, err := requireActiveCompositionProducts(ctx, productRepo, productID); err != nil {
		if apperror.Is(err, apperror.CodeNotFound) {
			return ReplaceResult{}, err
		}
		return ReplaceResult{}, fmt.Errorf("downloadable file: find product: %w", err)
	}
	if err := f.ValidateAttachment(typ); err != nil {
		return ReplaceResult{}, apperror.Validation(err.Error())
	}

	rows, err := fileRepo.ListByProductID(ctx, productID)
	if err != nil {
		return ReplaceResult{}, fmt.Errorf("downloadable file: list: %w", err)
	}
	var oldRow *catalog.DownloadableFile
	for i := range rows {
		if rows[i].AssetID == oldAssetID {
			oldRow = &rows[i]
			break
		}
	}
	if oldRow == nil {
		return ReplaceResult{}, apperror.NotFound("downloadable file not found")
	}
	if meta.Position != nil {
		f.Position = *meta.Position
	} else {
		f.Position = oldRow.Position
	}
	if meta.Label != nil {
		f.Label = *meta.Label
	} else {
		f.Label = oldRow.Label
	}
	if err := f.Validate(); err != nil {
		return ReplaceResult{}, apperror.Validation(err.Error())
	}

	// Fail closed before join swap: asset-scoped retarget would move every
	// grant on the blob, including other products' order lines.
	attachCount, err := fileRepo.CountByAssetID(ctx, oldAssetID)
	if err != nil {
		return ReplaceResult{}, fmt.Errorf("downloadable file: count asset attachments: %w", err)
	}
	if attachCount > 1 && !meta.GlobalRetarget {
		return ReplaceResult{}, apperror.Validation(
			"downloadable file asset is attached to other products; pass global_retarget=true to retarget every grant on this asset",
		)
	}

	if err := fileRepo.Upsert(ctx, f); err != nil {
		return ReplaceResult{}, fmt.Errorf("downloadable file: upsert: %w", err)
	}
	if err := fileRepo.Remove(ctx, productID, oldAssetID); err != nil {
		return ReplaceResult{}, fmt.Errorf("downloadable file: remove old: %w", err)
	}
	outcome, err := s.retargetGrantsInTx(ctx, tx, oldAssetID, f.AssetID)
	if err != nil {
		return ReplaceResult{}, err
	}
	if len(outcome.Revoked) > 0 && s.grantConflictRevokes == nil {
		return ReplaceResult{}, apperror.Internal(
			"downloadable file: grant conflict revoke notifier is required when replace revokes grants",
		)
	}
	if err := tx.Commit(); err != nil {
		return ReplaceResult{}, fmt.Errorf("downloadable file: commit: %w", err)
	}
	if len(outcome.Revoked) > 0 {
		s.grantConflictRevokes(ctx, outcome.Revoked)
	}
	return ReplaceResult{
		GrantsMoved:   outcome.Moved,
		GrantsRevoked: len(outcome.Revoked),
	}, nil
}

func (s *DownloadableFileService) retargetGrantsInTx(ctx context.Context, tx *sql.Tx, oldAssetID, newAssetID string) (GrantRetargetOutcome, error) {
	if s.grantRetarget == nil {
		return GrantRetargetOutcome{}, apperror.Internal("downloadable file: grant retarget is required for replace")
	}
	outcome, err := s.grantRetarget(ctx, tx, oldAssetID, newAssetID)
	if err != nil {
		return GrantRetargetOutcome{}, fmt.Errorf("downloadable file: retarget grants: %w", err)
	}
	return outcome, nil
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
