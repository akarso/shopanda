package pluginsdk

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	domaincart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domaincustomer "github.com/akarso/shopanda/internal/domain/customer"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/domain/inventory"
	domainorder "github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/storefrontsession"
)

// NewCatalogPort adapts catalog repositories to CatalogPort.
func NewCatalogPort(products catalog.ProductRepository, variants catalog.VariantRepository) CatalogPort {
	return &catalogPort{products: products, variants: variants}
}

type catalogPort struct {
	products catalog.ProductRepository
	variants catalog.VariantRepository
}

func (p *catalogPort) FindVariant(ctx context.Context, id string) (*extapi.CatalogVariant, error) {
	if p == nil || p.variants == nil {
		return nil, fmt.Errorf("pluginsdk: catalog port: variants not configured")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		// Same as FindVariants: empty id is a miss, not a repository call.
		return nil, nil
	}
	out, err := p.FindVariants(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	return out[id], nil
}

func (p *catalogPort) FindVariants(ctx context.Context, ids []string) (map[string]*extapi.CatalogVariant, error) {
	if p == nil || p.variants == nil {
		return nil, fmt.Errorf("pluginsdk: catalog port: variants not configured")
	}
	variants, err := catalog.LookupVariants(ctx, p.variants, ids)
	if err != nil {
		return nil, err
	}
	productIDs := make([]string, 0, len(variants))
	seenProduct := make(map[string]struct{}, len(variants))
	for _, v := range variants {
		if v == nil || strings.TrimSpace(v.ProductID) == "" {
			continue
		}
		if _, ok := seenProduct[v.ProductID]; ok {
			continue
		}
		seenProduct[v.ProductID] = struct{}{}
		productIDs = append(productIDs, v.ProductID)
	}
	products, err := catalog.LookupProducts(ctx, p.products, productIDs)
	if err != nil {
		return nil, fmt.Errorf("pluginsdk: catalog port: parent products: %w", err)
	}

	out := make(map[string]*extapi.CatalogVariant, len(variants))
	for reqID, v := range variants {
		if v == nil {
			continue
		}
		cv := &extapi.CatalogVariant{
			ID:        v.ID,
			ProductID: v.ProductID,
			SKU:       v.SKU,
			Name:      v.Name,
		}
		if prod := products[v.ProductID]; prod != nil {
			cv.ProductVisible = true
			cv.ProductSlug = prod.Slug
		}
		// !ProductVisible: parent missing, non-active / visibility-gated under
		// storefront read scope, or products repo nil. Hooks must not treat
		// empty ProductSlug alone as "not my product" (fail-open).
		out[reqID] = cv
	}
	return out, nil
}

func (p *catalogPort) FindProduct(ctx context.Context, id string) (*extapi.CatalogProduct, error) {
	if p == nil || p.products == nil {
		return nil, fmt.Errorf("pluginsdk: catalog port: products not configured")
	}
	prod, err := p.products.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if prod == nil {
		return nil, nil
	}
	return &extapi.CatalogProduct{
		ID:     prod.ID,
		Slug:   prod.Slug,
		Name:   prod.Name,
		Type:   string(prod.Type),
		Status: string(prod.Status),
	}, nil
}

// catalogSeedTx is Commit/Rollback only so unit tests can fake SQL without
// *sql.Tx. Production wraps *sql.Tx via sqlCatalogSeedTx.
type catalogSeedTx interface {
	Commit() error
	Rollback() error
}

// catalogSeedTxStarter begins a transaction for Import and standalone priceTx.
type catalogSeedTxStarter interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (catalogSeedTx, error)
}

type catalogSeedTxProducts interface {
	catalog.ProductRepository
	WithTx(tx catalogSeedTx) catalog.ProductRepository
}

type catalogSeedTxVariants interface {
	catalog.VariantRepository
	WithTx(tx catalogSeedTx) catalog.VariantRepository
}

type catalogSeedTxPrice interface {
	pricing.PriceRepository
	WithTx(tx catalogSeedTx) pricing.PriceRepository
}

type catalogSeedTxHistory interface {
	pricing.PriceHistoryRepository
	WithTx(tx catalogSeedTx) pricing.PriceHistoryRepository
}

type catalogSeedTxStock interface {
	inventory.StockRepository
	WithTx(tx catalogSeedTx) inventory.StockRepository
}

// sqlCatalogSeedTx adapts *sql.Tx to catalogSeedTx for postgres WithTx repos.
type sqlCatalogSeedTx struct{ tx *sql.Tx }

func (t sqlCatalogSeedTx) Commit() error   { return t.tx.Commit() }
func (t sqlCatalogSeedTx) Rollback() error { return t.tx.Rollback() }
func (t sqlCatalogSeedTx) SQLTx() *sql.Tx  { return t.tx }

type sqlDBSeedStarter struct {
	begin func(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

func (s sqlDBSeedStarter) BeginTx(ctx context.Context, opts *sql.TxOptions) (catalogSeedTx, error) {
	tx, err := s.begin(ctx, opts)
	if err != nil {
		return nil, err
	}
	return sqlCatalogSeedTx{tx: tx}, nil
}

func catalogSeedTxStarterOf(db any) (catalogSeedTxStarter, error) {
	if db == nil {
		return nil, nil
	}
	if s, ok := db.(catalogSeedTxStarter); ok {
		return s, nil
	}
	switch v := db.(type) {
	case *sql.DB:
		return sqlDBSeedStarter{begin: v.BeginTx}, nil
	case interface {
		BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
	}:
		return sqlDBSeedStarter{begin: v.BeginTx}, nil
	default:
		return nil, fmt.Errorf("pluginsdk: catalog seed port: unsupported db type %T", db)
	}
}

type sqlWithTxPrice interface {
	pricing.PriceRepository
	WithTx(tx *sql.Tx) pricing.PriceRepository
}
type sqlWithTxHistory interface {
	pricing.PriceHistoryRepository
	WithTx(tx *sql.Tx) pricing.PriceHistoryRepository
}
type sqlWithTxProducts interface {
	catalog.ProductRepository
	WithTx(tx *sql.Tx) catalog.ProductRepository
}
type sqlWithTxVariants interface {
	catalog.VariantRepository
	WithTx(tx *sql.Tx) catalog.VariantRepository
}
type sqlWithTxStock interface {
	inventory.StockRepository
	WithTx(tx *sql.Tx) inventory.StockRepository
}

type bridgedPriceRepo struct {
	pricing.PriceRepository
	with func(*sql.Tx) pricing.PriceRepository
}

func (b bridgedPriceRepo) WithTx(tx catalogSeedTx) pricing.PriceRepository {
	st, ok := tx.(interface{ SQLTx() *sql.Tx })
	if !ok || st.SQLTx() == nil {
		return b.PriceRepository
	}
	inner := b.with(st.SQLTx())
	return bridgedPriceRepo{PriceRepository: inner, with: b.with}
}

type bridgedHistoryRepo struct {
	pricing.PriceHistoryRepository
	with func(*sql.Tx) pricing.PriceHistoryRepository
}

func (b bridgedHistoryRepo) WithTx(tx catalogSeedTx) pricing.PriceHistoryRepository {
	st, ok := tx.(interface{ SQLTx() *sql.Tx })
	if !ok || st.SQLTx() == nil {
		return b.PriceHistoryRepository
	}
	inner := b.with(st.SQLTx())
	return bridgedHistoryRepo{PriceHistoryRepository: inner, with: b.with}
}

type bridgedProductsRepo struct {
	catalog.ProductRepository
	with func(*sql.Tx) catalog.ProductRepository
}

func (b bridgedProductsRepo) WithTx(tx catalogSeedTx) catalog.ProductRepository {
	st, ok := tx.(interface{ SQLTx() *sql.Tx })
	if !ok || st.SQLTx() == nil {
		return b.ProductRepository
	}
	inner := b.with(st.SQLTx())
	return bridgedProductsRepo{ProductRepository: inner, with: b.with}
}

type bridgedVariantsRepo struct {
	catalog.VariantRepository
	with func(*sql.Tx) catalog.VariantRepository
}

func (b bridgedVariantsRepo) WithTx(tx catalogSeedTx) catalog.VariantRepository {
	st, ok := tx.(interface{ SQLTx() *sql.Tx })
	if !ok || st.SQLTx() == nil {
		return b.VariantRepository
	}
	inner := b.with(st.SQLTx())
	return bridgedVariantsRepo{VariantRepository: inner, with: b.with}
}

type bridgedStockRepo struct {
	inventory.StockRepository
	with func(*sql.Tx) inventory.StockRepository
}

func (b bridgedStockRepo) WithTx(tx catalogSeedTx) inventory.StockRepository {
	st, ok := tx.(interface{ SQLTx() *sql.Tx })
	if !ok || st.SQLTx() == nil {
		return b.StockRepository
	}
	inner := b.with(st.SQLTx())
	return bridgedStockRepo{StockRepository: inner, with: b.with}
}

func asCatalogSeedTxPrice(p pricing.PriceRepository) (catalogSeedTxPrice, bool) {
	if p == nil {
		return nil, false
	}
	if x, ok := p.(catalogSeedTxPrice); ok {
		return x, true
	}
	if x, ok := p.(sqlWithTxPrice); ok {
		return bridgedPriceRepo{PriceRepository: x, with: x.WithTx}, true
	}
	return nil, false
}

func asCatalogSeedTxHistory(h pricing.PriceHistoryRepository) (catalogSeedTxHistory, bool) {
	if h == nil {
		return nil, false
	}
	if x, ok := h.(catalogSeedTxHistory); ok {
		return x, true
	}
	if x, ok := h.(sqlWithTxHistory); ok {
		return bridgedHistoryRepo{PriceHistoryRepository: x, with: x.WithTx}, true
	}
	return nil, false
}

func asCatalogSeedTxProducts(p catalog.ProductRepository) (catalogSeedTxProducts, bool) {
	if p == nil {
		return nil, true // optional
	}
	if x, ok := p.(catalogSeedTxProducts); ok {
		return x, true
	}
	if x, ok := p.(sqlWithTxProducts); ok {
		return bridgedProductsRepo{ProductRepository: x, with: x.WithTx}, true
	}
	return nil, false
}

func asCatalogSeedTxVariants(v catalog.VariantRepository) (catalogSeedTxVariants, bool) {
	if v == nil {
		return nil, true
	}
	if x, ok := v.(catalogSeedTxVariants); ok {
		return x, true
	}
	if x, ok := v.(sqlWithTxVariants); ok {
		return bridgedVariantsRepo{VariantRepository: x, with: x.WithTx}, true
	}
	return nil, false
}

func asCatalogSeedTxStock(s inventory.StockRepository) (catalogSeedTxStock, bool) {
	if s == nil {
		return nil, true
	}
	if x, ok := s.(catalogSeedTxStock); ok {
		return x, true
	}
	if x, ok := s.(sqlWithTxStock); ok {
		return bridgedStockRepo{StockRepository: x, with: x.WithTx}, true
	}
	return nil, false
}

// catalogSeedPriceTx runs fn with price+history repos that share one
// transaction. Used by standalone EnsurePrice when db + WithTx repos are
// bound. Import sets db=nil on the tx-bound port and relies on the outer
// BeginTx instead.
type catalogSeedPriceTx func(ctx context.Context, fn func(prices pricing.PriceRepository, history pricing.PriceHistoryRepository) error) error

func newCatalogSeedPriceTx(db catalogSeedTxStarter, tp catalogSeedTxPrice, th catalogSeedTxHistory) catalogSeedPriceTx {
	return func(ctx context.Context, fn func(pricing.PriceRepository, pricing.PriceHistoryRepository) error) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("pluginsdk: catalog seed port: begin tx: %w", err)
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()
		if err := fn(tp.WithTx(tx), th.WithTx(tx)); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("pluginsdk: catalog seed port: commit price+history: %w", err)
		}
		committed = true
		return nil
	}
}

// NewCatalogSeedPort adapts catalog/pricing/inventory repositories to CatalogSeedPort.
// db may be nil, *sql.DB, or a catalogSeedTxStarter. When db is set, prices and
// history must support WithTx — otherwise NewCatalogSeedPort returns an error
// (no silent sequential Upsert/Record split brain). Import uses one transaction;
// standalone EnsurePrice opens one short price+history transaction per call
// (prefer Import for large step-wise loops).
func NewCatalogSeedPort(
	products catalog.ProductRepository,
	variants catalog.VariantRepository,
	prices pricing.PriceRepository,
	history pricing.PriceHistoryRepository,
	stock inventory.StockRepository,
	db any,
) (CatalogSeedPort, error) {
	starter, err := catalogSeedTxStarterOf(db)
	if err != nil {
		return nil, err
	}
	p := &catalogSeedPort{
		products: products,
		variants: variants,
		prices:   prices,
		history:  history,
		stock:    stock,
		db:       starter,
	}
	if starter != nil {
		tp, okP := asCatalogSeedTxPrice(prices)
		th, okH := asCatalogSeedTxHistory(history)
		if !okP || !okH {
			return nil, fmt.Errorf("pluginsdk: catalog seed port: db set but prices/history repos do not support WithTx")
		}
		// Import also requires product/variant/stock WithTx when those repos are bound.
		if _, ok := asCatalogSeedTxProducts(products); !ok {
			return nil, fmt.Errorf("pluginsdk: catalog seed port: db set but products repo does not support WithTx")
		}
		if _, ok := asCatalogSeedTxVariants(variants); !ok {
			return nil, fmt.Errorf("pluginsdk: catalog seed port: db set but variants repo does not support WithTx")
		}
		if _, ok := asCatalogSeedTxStock(stock); !ok {
			return nil, fmt.Errorf("pluginsdk: catalog seed port: db set but stock repo does not support WithTx")
		}
		p.priceTx = newCatalogSeedPriceTx(starter, tp, th)
	}
	return p, nil
}

type catalogSeedPort struct {
	products catalog.ProductRepository
	variants catalog.VariantRepository
	prices   pricing.PriceRepository
	history  pricing.PriceHistoryRepository
	stock    inventory.StockRepository
	db       catalogSeedTxStarter
	priceTx  catalogSeedPriceTx // nil → sequential Upsert then Record
}

func (p *catalogSeedPort) withTx(tx catalogSeedTx) (*catalogSeedPort, error) {
	out := *p
	// Nested under Import's outer BeginTx: use tx-bound repos sequentially;
	// do not open another price+history transaction per EnsurePrice.
	out.priceTx = nil
	if tp, ok := asCatalogSeedTxProducts(p.products); ok && tp != nil {
		out.products = tp.WithTx(tx)
	} else if p.products != nil {
		return nil, fmt.Errorf("pluginsdk: catalog seed port: products repo does not support WithTx")
	}
	if tv, ok := asCatalogSeedTxVariants(p.variants); ok && tv != nil {
		out.variants = tv.WithTx(tx)
	} else if p.variants != nil {
		return nil, fmt.Errorf("pluginsdk: catalog seed port: variants repo does not support WithTx")
	}
	if tp, ok := asCatalogSeedTxPrice(p.prices); ok {
		out.prices = tp.WithTx(tx)
	} else if p.prices != nil {
		return nil, fmt.Errorf("pluginsdk: catalog seed port: prices repo does not support WithTx")
	}
	if th, ok := asCatalogSeedTxHistory(p.history); ok {
		out.history = th.WithTx(tx)
	} else if p.history != nil {
		return nil, fmt.Errorf("pluginsdk: catalog seed port: price history repo does not support WithTx")
	}
	if ts, ok := asCatalogSeedTxStock(p.stock); ok && ts != nil {
		out.stock = ts.WithTx(tx)
	} else if p.stock != nil {
		return nil, fmt.Errorf("pluginsdk: catalog seed port: stock repo does not support WithTx")
	}
	out.db = nil // nested Import must not open another tx
	return &out, nil
}

// Import applies the seed batch in one transaction when db + WithTx repos are bound.
func (p *catalogSeedPort) Import(ctx context.Context, batch CatalogSeedBatch) (*CatalogSeedResult, error) {
	if p == nil {
		return nil, fmt.Errorf("pluginsdk: catalog seed port: not configured")
	}
	run := p
	var tx catalogSeedTx
	if p.db != nil {
		var err error
		tx, err = p.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("pluginsdk: catalog seed port: begin tx: %w", err)
		}
		defer func() {
			if tx != nil {
				_ = tx.Rollback()
			}
		}()
		run, err = p.withTx(tx)
		if err != nil {
			return nil, err
		}
	}

	result := &CatalogSeedResult{
		ProductIDs: make(map[string]string, len(batch.Products)),
		VariantIDs: make(map[string]string, len(batch.Variants)),
	}
	for _, prod := range batch.Products {
		id, _, err := run.EnsureProduct(ctx, prod)
		if err != nil {
			return nil, err
		}
		result.ProductIDs[strings.TrimSpace(prod.Slug)] = id
	}
	for _, v := range batch.Variants {
		in := v
		if strings.TrimSpace(in.ProductID) == "" {
			slug := strings.TrimSpace(in.ProductSlug)
			pid, ok := result.ProductIDs[slug]
			if !ok || pid == "" {
				return nil, fmt.Errorf("pluginsdk: catalog seed port: variant SKU %q: unknown product slug %q", in.SKU, slug)
			}
			in.ProductID = pid
		}
		id, _, err := run.EnsureVariant(ctx, in)
		if err != nil {
			return nil, err
		}
		result.VariantIDs[strings.TrimSpace(in.SKU)] = id
	}
	for _, price := range batch.Prices {
		in := price
		if strings.TrimSpace(in.VariantID) == "" {
			sku := strings.TrimSpace(in.VariantSKU)
			vid, ok := result.VariantIDs[sku]
			if !ok || vid == "" {
				return nil, fmt.Errorf("pluginsdk: catalog seed port: price: unknown variant sku %q", sku)
			}
			in.VariantID = vid
		}
		if _, err := run.EnsurePrice(ctx, in); err != nil {
			return nil, err
		}
	}
	for _, st := range batch.Stock {
		vid := strings.TrimSpace(st.VariantID)
		if vid == "" {
			sku := strings.TrimSpace(st.VariantSKU)
			id, ok := result.VariantIDs[sku]
			if !ok || id == "" {
				return nil, fmt.Errorf("pluginsdk: catalog seed port: stock: unknown variant sku %q", sku)
			}
			vid = id
		}
		if err := run.SetStock(ctx, vid, st.Qty); err != nil {
			return nil, err
		}
	}

	if tx != nil {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("pluginsdk: catalog seed port: commit: %w", err)
		}
		tx = nil
	}
	return result, nil
}

func (p *catalogSeedPort) EnsureProduct(ctx context.Context, in CatalogProductSeed) (string, bool, error) {
	if p == nil || p.products == nil {
		return "", false, fmt.Errorf("pluginsdk: catalog seed port: products not configured")
	}
	slug := strings.TrimSpace(in.Slug)
	if slug == "" {
		return "", false, fmt.Errorf("pluginsdk: catalog seed port: product slug must not be empty")
	}
	ctx = catalog.WithOperatorProductReadScope(ctx)
	existing, err := p.products.FindBySlug(ctx, slug)
	if err != nil {
		return "", false, err
	}
	if existing != nil {
		if err := catalogSeedProductMatch(existing, in); err != nil {
			return "", false, err
		}
		return existing.ID, false, nil
	}

	prod, err := catalog.NewProduct(id.New(), strings.TrimSpace(in.Name), slug)
	if err != nil {
		return "", false, err
	}
	prod.Description = strings.TrimSpace(in.Description)
	// NewProduct already defaults StatusDraft / TypeSimple. Never force active.
	if s := strings.TrimSpace(in.Status); s != "" {
		st := catalog.Status(s)
		if !st.IsValid() {
			return "", false, fmt.Errorf("pluginsdk: catalog seed port: %s", catalog.InvalidStatusMessage(st))
		}
		prod.Status = st
	}
	if t := strings.TrimSpace(in.Type); t != "" {
		typ := catalog.Type(t)
		if !typ.IsValid() {
			return "", false, fmt.Errorf("pluginsdk: catalog seed port: %s", catalog.InvalidTypeMessage(typ))
		}
		prod.Type = typ
	}
	if in.Attributes != nil {
		cloned, err := cloneStringAnyMap(in.Attributes)
		if err != nil {
			return "", false, err
		}
		prod.Attributes = cloned
	}
	// Same guard as admin/CSV create: seed never attaches downloadable files.
	if prod.Type == catalog.TypeDownloadable && prod.Status == catalog.StatusActive {
		return "", false, fmt.Errorf("pluginsdk: catalog seed port: %s", catalog.DownloadableRequiresFileMessage)
	}
	if err := prod.Validate(); err != nil {
		return "", false, fmt.Errorf("pluginsdk: catalog seed port: %w", err)
	}

	if err := p.products.Create(ctx, &prod); err != nil {
		if apperror.Is(err, apperror.CodeConflict) {
			ret, err2 := p.products.FindBySlug(ctx, slug)
			if err2 != nil {
				return "", false, err2
			}
			if ret == nil {
				return "", false, err
			}
			if err := catalogSeedProductMatch(ret, in); err != nil {
				return "", false, err
			}
			return ret.ID, false, nil
		}
		return "", false, err
	}
	return prod.ID, true, nil
}

func (p *catalogSeedPort) EnsureVariant(ctx context.Context, in CatalogVariantSeed) (string, bool, error) {
	if p == nil || p.variants == nil {
		return "", false, fmt.Errorf("pluginsdk: catalog seed port: variants not configured")
	}
	productID := strings.TrimSpace(in.ProductID)
	if productID == "" {
		slug := strings.TrimSpace(in.ProductSlug)
		if slug == "" {
			return "", false, fmt.Errorf("pluginsdk: catalog seed port: variant product_id or product_slug must not be empty")
		}
		if p.products == nil {
			return "", false, fmt.Errorf("pluginsdk: catalog seed port: products not configured")
		}
		ctx = catalog.WithOperatorProductReadScope(ctx)
		prod, err := p.products.FindBySlug(ctx, slug)
		if err != nil {
			return "", false, err
		}
		if prod == nil {
			return "", false, fmt.Errorf("pluginsdk: catalog seed port: product slug %q not found", slug)
		}
		productID = prod.ID
	}
	sku := strings.TrimSpace(in.SKU)
	if sku == "" {
		return "", false, fmt.Errorf("pluginsdk: catalog seed port: variant sku must not be empty")
	}

	existing, err := p.variants.FindBySKU(ctx, sku)
	if err != nil {
		return "", false, err
	}
	if existing != nil {
		if existing.ProductID != productID {
			return "", false, fmt.Errorf("pluginsdk: catalog seed port: variant SKU %q belongs to product %q, expected %q",
				sku, existing.ProductID, productID)
		}
		if err := catalogSeedVariantMatch(existing, in); err != nil {
			return "", false, err
		}
		return existing.ID, false, nil
	}

	v, err := catalog.NewVariant(id.New(), productID, sku)
	if err != nil {
		return "", false, err
	}
	v.Name = strings.TrimSpace(in.Name)
	if in.Attributes != nil {
		cloned, err := cloneStringAnyMap(in.Attributes)
		if err != nil {
			return "", false, err
		}
		v.Attributes = cloned
	}

	if err := p.variants.Create(ctx, &v); err != nil {
		if apperror.Is(err, apperror.CodeConflict) {
			ret, err2 := p.variants.FindBySKU(ctx, sku)
			if err2 != nil {
				return "", false, err2
			}
			if ret == nil {
				return "", false, err
			}
			if ret.ProductID != productID {
				return "", false, fmt.Errorf("pluginsdk: catalog seed port: variant SKU %q belongs to product %q, expected %q",
					sku, ret.ProductID, productID)
			}
			if err := catalogSeedVariantMatch(ret, in); err != nil {
				return "", false, err
			}
			return ret.ID, false, nil
		}
		return "", false, err
	}
	return v.ID, true, nil
}

func (p *catalogSeedPort) EnsurePrice(ctx context.Context, in CatalogPriceSeed) (bool, error) {
	if p == nil || p.prices == nil {
		return false, fmt.Errorf("pluginsdk: catalog seed port: prices not configured")
	}
	if p.history == nil {
		return false, fmt.Errorf("pluginsdk: catalog seed port: price history not configured")
	}
	variantID := strings.TrimSpace(in.VariantID)
	if variantID == "" {
		return false, fmt.Errorf("pluginsdk: catalog seed port: price variant_id must not be empty")
	}
	currency := strings.TrimSpace(in.Currency)
	if currency == "" {
		return false, fmt.Errorf("pluginsdk: catalog seed port: price currency must not be empty")
	}
	storeID := strings.TrimSpace(in.StoreID)
	existing, err := p.prices.FindByVariantCurrencyAndStore(ctx, variantID, currency, storeID)
	if err != nil {
		return false, err
	}
	// Create-only hit before money validation: garbage currency/amount must
	// not fail a soft no-op (or an Import batch that only restates prices).
	if existing != nil && !in.Update {
		return false, nil
	}
	money, err := shared.NewMoney(in.Amount, currency)
	if err != nil {
		return false, fmt.Errorf("pluginsdk: catalog seed port: invalid money: %w", err)
	}
	if existing != nil {
		if existing.Amount.Equal(money) {
			return false, nil
		}
		price := *existing
		price.Amount = money
		if err := p.writePriceWithHistory(ctx, &price); err != nil {
			return false, err
		}
		return true, nil
	}

	price, err := pricing.NewPrice(id.New(), variantID, storeID, money)
	if err != nil {
		return false, err
	}
	if err := p.writePriceWithHistory(ctx, &price); err != nil {
		return false, err
	}
	return true, nil
}

// writePriceWithHistory upserts the price and records a snapshot derived from
// that row. When priceTx is set (standalone Ensure with db+WithTx), both run
// in one transaction. Under Import, priceTx is nil and repos are already
// bound to the outer tx (or sequential when no db was configured).
func (p *catalogSeedPort) writePriceWithHistory(ctx context.Context, price *pricing.Price) error {
	if price == nil {
		return fmt.Errorf("pluginsdk: catalog seed port: price must not be nil")
	}
	snap, err := pricing.NewPriceSnapshot(id.New(), price.VariantID, price.StoreID, price.Amount)
	if err != nil {
		return err
	}
	write := func(prices pricing.PriceRepository, history pricing.PriceHistoryRepository) error {
		if err := prices.Upsert(ctx, price); err != nil {
			return err
		}
		return history.Record(ctx, &snap)
	}
	if p.priceTx != nil {
		return p.priceTx(ctx, write)
	}
	return write(p.prices, p.history)
}

func (p *catalogSeedPort) SetStock(ctx context.Context, variantID string, qty int) error {
	if p == nil || p.stock == nil {
		return fmt.Errorf("pluginsdk: catalog seed port: stock not configured")
	}
	variantID = strings.TrimSpace(variantID)
	if variantID == "" {
		return fmt.Errorf("pluginsdk: catalog seed port: stock variant_id must not be empty")
	}
	entry, err := inventory.NewStockEntry(variantID, qty)
	if err != nil {
		return err
	}
	return p.stock.SetStock(ctx, &entry)
}

// catalogSeedProductMatch enforces create-only honesty: when the seed sets
// Status / Type / Attributes, they must already agree with the stored product.
// Name / Description are still soft-ignored on an existing slug (create-only).
func catalogSeedProductMatch(existing *catalog.Product, in CatalogProductSeed) error {
	if existing == nil {
		return nil
	}
	slug := strings.TrimSpace(in.Slug)
	if s := strings.TrimSpace(in.Status); s != "" {
		st := catalog.Status(s)
		if !st.IsValid() {
			return fmt.Errorf("pluginsdk: catalog seed port: %s", catalog.InvalidStatusMessage(st))
		}
		if existing.Status != st {
			return fmt.Errorf("pluginsdk: catalog seed port: product slug %q already exists with status %q, seed requested %q (create-only)",
				slug, existing.Status, st)
		}
	}
	if t := strings.TrimSpace(in.Type); t != "" {
		typ := catalog.Type(t)
		if !typ.IsValid() {
			return fmt.Errorf("pluginsdk: catalog seed port: %s", catalog.InvalidTypeMessage(typ))
		}
		if existing.Type != typ {
			return fmt.Errorf("pluginsdk: catalog seed port: product slug %q already exists with type %q, seed requested %q (create-only)",
				slug, existing.Type, typ)
		}
	}
	if in.Attributes != nil {
		if err := catalogSeedAttributesMatch(existing.Attributes, in.Attributes); err != nil {
			return fmt.Errorf("pluginsdk: catalog seed port: product slug %q already exists with different attributes (create-only): %w", slug, err)
		}
	}
	return nil
}

func catalogSeedVariantMatch(existing *catalog.Variant, in CatalogVariantSeed) error {
	if existing == nil || in.Attributes == nil {
		return nil
	}
	if err := catalogSeedAttributesMatch(existing.Attributes, in.Attributes); err != nil {
		return fmt.Errorf("pluginsdk: catalog seed port: variant SKU %q already exists with different attributes (create-only): %w",
			strings.TrimSpace(in.SKU), err)
	}
	return nil
}

// catalogSeedAttributesMatch: nil seed Attributes is not checked (caller skips).
// Non-nil empty map asserts the stored map is empty. Non-empty seed requires
// every seed key to equal the stored value (extra stored keys are OK).
func catalogSeedAttributesMatch(stored map[string]interface{}, seed map[string]any) error {
	if seed == nil {
		return nil
	}
	if len(seed) == 0 {
		if len(stored) != 0 {
			return fmt.Errorf("attributes not empty")
		}
		return nil
	}
	for k, want := range seed {
		got, ok := stored[k]
		if !ok || !jsonValuesEqual(got, want) {
			return fmt.Errorf("attribute %q mismatch", k)
		}
	}
	return nil
}

func jsonValuesEqual(a, b any) bool {
	aj, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bj, err := json.Marshal(b)
	if err != nil {
		return false
	}
	var an, bn any
	if err := json.Unmarshal(aj, &an); err != nil {
		return false
	}
	if err := json.Unmarshal(bj, &bn); err != nil {
		return false
	}
	return reflect.DeepEqual(an, bn)
}

// cloneStringAnyMap deep-copies via JSON so concrete containers ([]string,
// map[string]string, …) are not shared with the caller after Ensure*.
// Non-JSON-serializable values return an error (no shallow fallback).
func cloneStringAnyMap(in map[string]any) (map[string]interface{}, error) {
	if in == nil {
		return nil, nil
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("pluginsdk: catalog seed port: attributes not JSON-serializable: %w", err)
	}
	out := make(map[string]interface{})
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("pluginsdk: catalog seed port: attributes not JSON-serializable: %w", err)
	}
	return out, nil
}

// NewOrdersPort adapts an order repository to OrderPort.
func NewOrdersPort(orders domainorder.OrderRepository) OrderPort {
	return &ordersPort{orders: orders}
}

type ordersPort struct {
	orders domainorder.OrderRepository
}

// FindByID returns an OrderView with ContactEmail blanked: the default
// read-only port is available to any plugin that declares Need(CapOrders),
// and PII should not ride along by default just because a plugin wanted
// order status/currency. There is currently no privileged accessor that
// returns the real address — add one (gated by an explicit, narrower
// capability) if a concrete integration needs it.
func (p *ordersPort) FindByID(ctx context.Context, id string) (*extapi.OrderView, error) {
	if p == nil || p.orders == nil {
		return nil, fmt.Errorf("pluginsdk: orders port: not configured")
	}
	o, err := p.orders.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, nil
	}
	return &extapi.OrderView{
		ID:         o.ID,
		CustomerID: o.CustomerID,
		Status:     string(o.Status()),
		Currency:   o.Currency,
	}, nil
}

// NewCustomerPort adapts a customer repository to CustomerPort.
func NewCustomerPort(customers domaincustomer.CustomerRepository) CustomerPort {
	return &customerPort{customers: customers}
}

type customerPort struct {
	customers domaincustomer.CustomerRepository
}

// FindByID returns a CustomerView with Email blanked — see ordersPort.FindByID.
func (p *customerPort) FindByID(ctx context.Context, id string) (*extapi.CustomerView, error) {
	if p == nil || p.customers == nil {
		return nil, fmt.Errorf("pluginsdk: customer port: not configured")
	}
	c, err := p.customers.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, nil
	}
	return &extapi.CustomerView{ID: c.ID}, nil
}

// cartService is the cart application surface CartPort needs (keeps plugins off internal/application).
type cartService interface {
	AddItem(ctx context.Context, cartID, customerID, variantID string, quantity int, opts cartApp.AddItemOptions) (*domaincart.Cart, error)
	GetCart(ctx context.Context, cartID, customerID string) (*domaincart.Cart, error)
	CreateCart(ctx context.Context, customerID, currency string) (*domaincart.Cart, error)
	EnsureActiveCart(ctx context.Context, customerID, currency string) (*domaincart.Cart, error)
	DiscardEmptyGuestCart(ctx context.Context, cartID string) (discarded bool, err error)
	CartView(ctx context.Context, c *domaincart.Cart) (*extapi.CartView, error)
	FilterCartViewForPlugin(view *extapi.CartView, pluginName string) *extapi.CartView
	DeleteLineExtension(ctx context.Context, cartID, customerID, variantID, fieldCode string) error
	UpsertLineExtension(ctx context.Context, cartID, customerID, variantID string, inputs []domainext.ValueInput, updatedBy string) error
	ExtensionFieldOwner(code string) string
	ExtensionFieldPluginOnly(code string) bool
}

// NewCartPort adapts cart application Service to CartPort.
func NewCartPort(svc *cartApp.Service) CartPort {
	return &cartPort{svc: svc}
}

type cartPort struct {
	svc   cartService
	owner string // non-empty when returned from AsHostFor / Adapt
}

func (p *cartPort) withOwner(pluginName string) *cartPort {
	if p == nil {
		return nil
	}
	out := *p
	out.owner = pluginName
	return &out
}

func (p *cartPort) AddItem(ctx context.Context, cartID, customerID, variantID string, qty int, opts CartAddItemOpts) error {
	if p == nil || p.svc == nil {
		return fmt.Errorf("pluginsdk: cart port: not configured")
	}
	domainOpts := cartApp.AddItemOptions{UpdatedBy: opts.UpdatedBy}
	if len(opts.Extensions) > 0 {
		domainOpts.Extensions = make([]domainext.ValueInput, 0, len(opts.Extensions))
		for _, e := range opts.Extensions {
			code := strings.TrimSpace(e.Code)
			if p.owner != "" && p.svc.ExtensionFieldPluginOnly(code) {
				owner := p.svc.ExtensionFieldOwner(code)
				if owner == "" || owner != p.owner {
					return apperror.Forbidden("cannot add another plugin's line extension")
				}
			}
			domainOpts.Extensions = append(domainOpts.Extensions, domainext.ValueInput{
				FieldCode: code,
				Value:     e.Value,
			})
		}
	}
	_, err := p.svc.AddItem(ctx, cartID, customerID, variantID, qty, domainOpts)
	return err
}

func (p *cartPort) viewFor(ctx context.Context, c *domaincart.Cart) (*extapi.CartView, error) {
	view, err := p.svc.CartView(ctx, c)
	if err != nil {
		return nil, err
	}
	if p.owner != "" {
		return p.svc.FilterCartViewForPlugin(view, p.owner), nil
	}
	return view, nil
}

func (p *cartPort) Get(ctx context.Context, cartID, customerID string) (*extapi.CartView, error) {
	if p == nil || p.svc == nil {
		return nil, fmt.Errorf("pluginsdk: cart port: not configured")
	}
	c, err := p.svc.GetCart(ctx, cartID, customerID)
	if err != nil {
		return nil, err
	}
	return p.viewFor(ctx, c)
}

func (p *cartPort) EnsureActive(ctx context.Context, customerID, currency string) (*extapi.CartView, error) {
	if p == nil || p.svc == nil {
		return nil, fmt.Errorf("pluginsdk: cart port: not configured")
	}
	c, err := p.svc.EnsureActiveCart(ctx, customerID, currency)
	if err != nil {
		return nil, err
	}
	return p.viewFor(ctx, c)
}

func resolveStorefrontCurrency(ctx context.Context, currency string) string {
	currency = strings.TrimSpace(currency)
	if currency != "" {
		return currency
	}
	if s := store.FromContext(ctx); s != nil && strings.TrimSpace(s.Currency) != "" {
		return strings.TrimSpace(s.Currency)
	}
	return "EUR"
}

func (p *cartPort) EnsureStorefrontCart(ctx context.Context, w http.ResponseWriter, r *http.Request, currency string) (*extapi.CartView, error) {
	if p == nil || p.svc == nil {
		return nil, fmt.Errorf("pluginsdk: cart port: not configured")
	}
	currency = resolveStorefrontCurrency(ctx, currency)
	customerID := CustomerID(r)
	if customerID != "" {
		return p.EnsureActive(ctx, customerID, currency)
	}
	if cartID, ok := storefrontsession.CartIDFromRequest(r); ok {
		c, err := p.svc.GetCart(ctx, cartID, "")
		if err != nil {
			if apperror.Is(err, apperror.CodeNotFound) || apperror.Is(err, apperror.CodeForbidden) {
				// Stale cookie or claimed (customer) cart id: replace below.
			} else {
				// DB / timeout / other — do not orphan by rewriting the cookie.
				return nil, err
			}
		} else if c != nil && c.IsActive() {
			// GetCart("", guest) only succeeds for guest-owned carts.
			return p.viewFor(ctx, c)
		}
		// Non-active guest cart: replace below (no silent reuse).
	}
	c, err := p.svc.CreateCart(ctx, "", currency)
	if err != nil {
		return nil, err
	}
	storefrontsession.SetCartCookieSecure(w, c.ID, storefrontsession.CookieSecure(ctx, r))
	return p.viewFor(ctx, c)
}

func (p *cartPort) DiscardEmptyGuestCart(ctx context.Context, w http.ResponseWriter, r *http.Request, cartID string) error {
	if p == nil || p.svc == nil {
		return fmt.Errorf("pluginsdk: cart port: not configured")
	}
	cartID = strings.TrimSpace(cartID)
	if cartID == "" {
		return nil
	}
	discarded, err := p.svc.DiscardEmptyGuestCart(ctx, cartID)
	// Clear the cookie as soon as the row is gone — even if orphan purge failed —
	// so Ensure's Set-Cookie is not left pointing at a deleted cart.
	if discarded && w != nil {
		storefrontsession.ClearCartCookieSecure(w, storefrontsession.CookieSecure(ctx, r))
	}
	return err
}

func (p *cartPort) DeleteLineExtension(ctx context.Context, cartID, customerID, variantID, fieldCode string) error {
	if p == nil || p.svc == nil {
		return fmt.Errorf("pluginsdk: cart port: not configured")
	}
	fieldCode = strings.TrimSpace(fieldCode)
	if p.owner != "" && p.svc.ExtensionFieldPluginOnly(fieldCode) {
		owner := p.svc.ExtensionFieldOwner(fieldCode)
		if owner == "" || owner != p.owner {
			return apperror.Forbidden("cannot delete another plugin's line extension")
		}
	}
	return p.svc.DeleteLineExtension(ctx, cartID, customerID, variantID, fieldCode)
}

func (p *cartPort) UpsertLineExtension(ctx context.Context, cartID, customerID, variantID string, values []CartExtensionValue, updatedBy string) error {
	if p == nil || p.svc == nil {
		return fmt.Errorf("pluginsdk: cart port: not configured")
	}
	if len(values) == 0 {
		return apperror.Validation("extension values must not be empty")
	}
	inputs := make([]domainext.ValueInput, 0, len(values))
	for _, e := range values {
		code := strings.TrimSpace(e.Code)
		if p.owner != "" && p.svc.ExtensionFieldPluginOnly(code) {
			owner := p.svc.ExtensionFieldOwner(code)
			if owner == "" || owner != p.owner {
				return apperror.Forbidden("cannot upsert another plugin's line extension")
			}
		}
		inputs = append(inputs, domainext.ValueInput{FieldCode: code, Value: e.Value})
	}
	return p.svc.UpsertLineExtension(ctx, cartID, customerID, variantID, inputs, updatedBy)
}
