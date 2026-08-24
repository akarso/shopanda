package main

import (
	"database/sql"
	"fmt"

	"github.com/akarso/shopanda/internal/infrastructure/postgres"
)

// serveRepos holds every postgres repository opened for the HTTP serve path.
type serveRepos struct {
	productRepo                *postgres.ProductRepo
	variantRepo                *postgres.VariantRepo
	cartRepo                   *postgres.CartRepo
	priceRepo                  *postgres.PriceRepo
	priceHistoryRepo           *postgres.PriceHistoryRepo
	customerRepo               *postgres.CustomerRepo
	resetTokenRepo             *postgres.ResetTokenRepo
	reservationRepo            *postgres.ReservationRepo
	stockRepo                  *postgres.StockRepo
	orderRepo                  *postgres.OrderRepo
	storeCreditRepo            *postgres.StoreCreditRepo
	returnRepo                 *postgres.ReturnRepo
	reviewRepo                 *postgres.ReviewRepo
	statsRepo                  *postgres.StatsRepo
	paymentRepo                *postgres.PaymentRepo
	shippingRepo               *postgres.ShippingRepo
	configRepo                 *postgres.ConfigRepo
	zoneRepo                   *postgres.ZoneRepo
	categoryRepo               *postgres.CategoryRepo
	collectionRepo             *postgres.CollectionRepo
	taxRateRepo                *postgres.TaxRateRepo
	promotionRepo              *postgres.PromotionRepo
	couponRepo                 *postgres.CouponRepo
	invoiceRepo                *postgres.InvoiceRepo
	assetRepo                  *postgres.AssetRepo
	rewriteRepo                *postgres.RewriteRepo
	pageRepo                   *postgres.PageRepo
	menuRepo                   *postgres.MenuRepo
	contentBlockRepo           *postgres.ContentBlockRepo
	storeRepo                  *postgres.StoreRepo
	translationRepo            *postgres.TranslationRepo
	contentTranslationRepo     *postgres.ContentTranslationRepo
	consentRepo                *postgres.ConsentRepo
	customerAddressRepo        *postgres.CustomerAddressRepo
	mfaRepo                    *postgres.MFARepo
	auditLogRepo               *postgres.AuditLogRepo
	webhookEndpointRepo        *postgres.WebhookEndpointRepo
	integrationIdempotencyRepo *postgres.IntegrationIdempotencyRepo
	extensionFieldRepo         *postgres.ExtensionFieldRepo
	extensionValueRepo         *postgres.ExtensionValueRepo
	rolePermRepo               *postgres.RolePermissionRepo
}

func wrapRepoOpen(name string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("open %s: %w", name, err)
}

func openServeRepos(conn *sql.DB) (*serveRepos, error) {
	var err error
	repos := &serveRepos{}

	repos.productRepo, err = postgres.NewProductRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("productRepo", err)
	}
	repos.variantRepo, err = postgres.NewVariantRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("variantRepo", err)
	}
	repos.cartRepo, err = postgres.NewCartRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("cartRepo", err)
	}
	repos.priceRepo, err = postgres.NewPriceRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("priceRepo", err)
	}
	repos.priceHistoryRepo, err = postgres.NewPriceHistoryRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("priceHistoryRepo", err)
	}
	repos.customerRepo, err = postgres.NewCustomerRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("customerRepo", err)
	}
	repos.resetTokenRepo, err = postgres.NewResetTokenRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("resetTokenRepo", err)
	}
	repos.reservationRepo, err = postgres.NewReservationRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("reservationRepo", err)
	}
	repos.stockRepo, err = postgres.NewStockRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("stockRepo", err)
	}
	repos.orderRepo, err = postgres.NewOrderRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("orderRepo", err)
	}
	repos.storeCreditRepo, err = postgres.NewStoreCreditRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("storeCreditRepo", err)
	}
	repos.returnRepo, err = postgres.NewReturnRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("returnRepo", err)
	}
	repos.reviewRepo, err = postgres.NewReviewRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("reviewRepo", err)
	}
	repos.statsRepo, err = postgres.NewStatsRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("statsRepo", err)
	}
	repos.paymentRepo, err = postgres.NewPaymentRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("paymentRepo", err)
	}
	repos.shippingRepo, err = postgres.NewShippingRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("shippingRepo", err)
	}
	repos.configRepo = postgres.NewConfigRepo(conn)
	repos.zoneRepo, err = postgres.NewZoneRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("zoneRepo", err)
	}
	repos.categoryRepo, err = postgres.NewCategoryRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("categoryRepo", err)
	}
	repos.collectionRepo, err = postgres.NewCollectionRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("collectionRepo", err)
	}
	// collectionRepo: opened at serve start; HTTP handlers not wired yet.

	repos.taxRateRepo, err = postgres.NewTaxRateRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("taxRateRepo", err)
	}

	repos.promotionRepo, err = postgres.NewPromotionRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("promotionRepo", err)
	}

	repos.couponRepo, err = postgres.NewCouponRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("couponRepo", err)
	}

	repos.invoiceRepo, err = postgres.NewInvoiceRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("invoiceRepo", err)
	}

	repos.assetRepo, err = postgres.NewAssetRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("assetRepo", err)
	}

	repos.rewriteRepo, err = postgres.NewRewriteRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("rewriteRepo", err)
	}

	repos.pageRepo, err = postgres.NewPageRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("pageRepo", err)
	}

	repos.menuRepo, err = postgres.NewMenuRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("menuRepo", err)
	}

	repos.contentBlockRepo, err = postgres.NewContentBlockRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("contentBlockRepo", err)
	}

	repos.storeRepo, err = postgres.NewStoreRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("storeRepo", err)
	}

	repos.translationRepo, err = postgres.NewTranslationRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("translationRepo", err)
	}
	// translationRepo: opened at serve start; admin handlers not wired yet.

	repos.contentTranslationRepo, err = postgres.NewContentTranslationRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("contentTranslationRepo", err)
	}

	repos.consentRepo, err = postgres.NewConsentRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("consentRepo", err)
	}

	repos.customerAddressRepo, err = postgres.NewCustomerAddressRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("customerAddressRepo", err)
	}

	repos.mfaRepo, err = postgres.NewMFARepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("mfaRepo", err)
	}

	repos.auditLogRepo, err = postgres.NewAuditLogRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("auditLogRepo", err)
	}

	repos.webhookEndpointRepo, err = postgres.NewWebhookEndpointRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("webhookEndpointRepo", err)
	}

	repos.integrationIdempotencyRepo, err = postgres.NewIntegrationIdempotencyRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("integrationIdempotencyRepo", err)
	}

	repos.extensionFieldRepo, err = postgres.NewExtensionFieldRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("extensionFieldRepo", err)
	}

	repos.extensionValueRepo, err = postgres.NewExtensionValueRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("extensionValueRepo", err)
	}

	repos.rolePermRepo, err = postgres.NewRolePermissionRepo(conn)
	if err != nil {
		return nil, wrapRepoOpen("rolePermRepo", err)
	}

	return repos, nil
}
