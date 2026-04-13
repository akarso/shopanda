package seed

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

type seedCategory struct {
	Name string
	Slug string
}

type seedVariant struct {
	SKU      string
	Name     string
	Amount   int64
	Currency string
	Stock    int
}

type seedProduct struct {
	Name     string
	Slug     string
	Desc     string
	Variants []seedVariant
}

var defaultCategories = []seedCategory{
	{Name: "Electronics", Slug: "electronics"},
	{Name: "Clothing", Slug: "clothing"},
}

var defaultProducts = []seedProduct{
	{
		Name: "Wireless Mouse",
		Slug: "wireless-mouse",
		Desc: "Ergonomic wireless mouse with USB receiver.",
		Variants: []seedVariant{
			{SKU: "MOUSE-BLK", Name: "Black", Amount: 2999, Currency: "EUR", Stock: 50},
		},
	},
	{
		Name: "USB-C Cable",
		Slug: "usb-c-cable",
		Desc: "Durable braided USB-C to USB-C cable, 1 meter.",
		Variants: []seedVariant{
			{SKU: "USBC-1M", Name: "1M", Amount: 999, Currency: "EUR", Stock: 200},
		},
	},
	{
		Name: "Cotton T-Shirt",
		Slug: "cotton-tshirt",
		Desc: "Plain cotton t-shirt, available in multiple sizes.",
		Variants: []seedVariant{
			{SKU: "TSHIRT-M", Name: "Medium", Amount: 1999, Currency: "EUR", Stock: 100},
			{SKU: "TSHIRT-L", Name: "Large", Amount: 1999, Currency: "EUR", Stock: 100},
		},
	},
}

// CatalogSeeder creates the default categories, products, variants, prices,
// and stock entries.
type CatalogSeeder struct{}

func (s *CatalogSeeder) Name() string { return "catalog" }

func (s *CatalogSeeder) Seed(ctx context.Context, deps Deps) error {
	catRepo, err := postgres.NewCategoryRepo(deps.DB)
	if err != nil {
		return err
	}
	prodRepo, err := postgres.NewProductRepo(deps.DB)
	if err != nil {
		return err
	}
	variantRepo, err := postgres.NewVariantRepo(deps.DB)
	if err != nil {
		return err
	}
	priceRepo, err := postgres.NewPriceRepo(deps.DB)
	if err != nil {
		return err
	}
	priceHistoryRepo, err := postgres.NewPriceHistoryRepo(deps.DB)
	if err != nil {
		return err
	}
	stockRepo, err := postgres.NewStockRepo(deps.DB)
	if err != nil {
		return err
	}

	if err := s.seedCategories(ctx, deps, catRepo); err != nil {
		return err
	}
	return s.seedProducts(ctx, deps, prodRepo, variantRepo, priceRepo, priceHistoryRepo, stockRepo)
}

func (s *CatalogSeeder) seedCategories(ctx context.Context, deps Deps, repo *postgres.CategoryRepo) error {
	for _, sc := range defaultCategories {
		existing, err := repo.FindBySlug(ctx, sc.Slug)
		if err != nil {
			return err
		}
		if existing != nil {
			deps.Logger.Info("seed.category.skip", map[string]interface{}{
				"slug": sc.Slug,
			})
			continue
		}

		c, err := catalog.NewCategory(id.New(), sc.Name, sc.Slug)
		if err != nil {
			return err
		}
		if err := repo.Create(ctx, &c); err != nil {
			if apperror.Is(err, apperror.CodeConflict) {
				deps.Logger.Info("seed.category.skip", map[string]interface{}{
					"slug": sc.Slug,
				})
				continue
			}
			return err
		}
		deps.Logger.Info("seed.category.created", map[string]interface{}{
			"slug": sc.Slug,
		})
	}
	return nil
}

func (s *CatalogSeeder) seedProducts(
	ctx context.Context,
	deps Deps,
	prodRepo *postgres.ProductRepo,
	variantRepo *postgres.VariantRepo,
	priceRepo *postgres.PriceRepo,
	priceHistoryRepo *postgres.PriceHistoryRepo,
	stockRepo *postgres.StockRepo,
) error {
	for _, sp := range defaultProducts {
		var productID string

		existing, err := prodRepo.FindBySlug(ctx, sp.Slug)
		if err != nil {
			return err
		}
		if existing != nil {
			productID = existing.ID
			deps.Logger.Info("seed.product.skip", map[string]interface{}{
				"slug": sp.Slug,
			})
		} else {
			p, err := catalog.NewProduct(id.New(), sp.Name, sp.Slug)
			if err != nil {
				return err
			}
			p.Description = sp.Desc
			p.Status = catalog.StatusActive

			if err := prodRepo.Create(ctx, &p); err != nil {
				if apperror.Is(err, apperror.CodeConflict) {
					ret, err2 := prodRepo.FindBySlug(ctx, sp.Slug)
					if err2 != nil {
						return err2
					}
					if ret == nil {
						return err
					}
					productID = ret.ID
					deps.Logger.Info("seed.product.skip", map[string]interface{}{
						"slug": sp.Slug,
					})
				} else {
					return err
				}
			} else {
				productID = p.ID
				deps.Logger.Info("seed.product.created", map[string]interface{}{
					"slug": sp.Slug,
				})
			}
		}

		for _, sv := range sp.Variants {
			variantID, err := s.ensureVariant(ctx, deps, variantRepo, productID, sv)
			if err != nil {
				return err
			}

			money, err := shared.NewMoney(sv.Amount, sv.Currency)
			if err != nil {
				return fmt.Errorf("seed: invalid money for variant %q: %w", sv.SKU, err)
			}
			price, err := pricing.NewPrice(id.New(), variantID, "", money)
			if err != nil {
				return err
			}
			if err := priceRepo.Upsert(ctx, &price); err != nil {
				return err
			}

			snap, err := pricing.NewPriceSnapshot(id.New(), variantID, "", money)
			if err != nil {
				return err
			}
			if err := priceHistoryRepo.Record(ctx, &snap); err != nil {
				return err
			}

			stock, err := inventory.NewStockEntry(variantID, sv.Stock)
			if err != nil {
				return err
			}
			if err := stockRepo.SetStock(ctx, &stock); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *CatalogSeeder) ensureVariant(
	ctx context.Context,
	deps Deps,
	repo *postgres.VariantRepo,
	productID string,
	sv seedVariant,
) (string, error) {
	existing, err := repo.FindBySKU(ctx, sv.SKU)
	if err != nil {
		return "", err
	}
	if existing != nil {
		if existing.ProductID != productID {
			return "", fmt.Errorf("seed: variant SKU %q belongs to product %q, expected %q",
				sv.SKU, existing.ProductID, productID)
		}
		deps.Logger.Info("seed.variant.skip", map[string]interface{}{
			"sku": sv.SKU,
		})
		return existing.ID, nil
	}

	v, err := catalog.NewVariant(id.New(), productID, sv.SKU)
	if err != nil {
		return "", err
	}
	v.Name = sv.Name

	if err := repo.Create(ctx, &v); err != nil {
		if apperror.Is(err, apperror.CodeConflict) {
			ret, err2 := repo.FindBySKU(ctx, sv.SKU)
			if err2 != nil {
				return "", err2
			}
			if ret == nil {
				return "", err
			}
			if ret.ProductID != productID {
				return "", fmt.Errorf("seed: variant SKU %q belongs to product %q, expected %q",
					sv.SKU, ret.ProductID, productID)
			}
			deps.Logger.Info("seed.variant.skip", map[string]interface{}{
				"sku": sv.SKU,
			})
			return ret.ID, nil
		}
		return "", err
	}
	deps.Logger.Info("seed.variant.created", map[string]interface{}{
		"sku": sv.SKU,
	})
	return v.ID, nil
}
