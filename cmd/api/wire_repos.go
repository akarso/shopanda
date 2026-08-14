package main

import (
	"database/sql"

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

func openServeRepos(conn *sql.DB) (*serveRepos, error) {
	var err error
	repos := &serveRepos{}

	repos.productRepo, err = postgres.NewProductRepo(conn)
	if err != nil {
		return nil, err
	}
	repos.variantRepo, err = postgres.NewVariantRepo(conn)
	if err != nil {
		return nil, err
	}
	repos.cartRepo, err = postgres.NewCartRepo(conn)
	if err != nil {
		return nil, err
	}
	repos.priceRepo, err = postgres.NewPriceRepo(conn)
	if err != nil {
		return nil, err
	}
	repos.priceHistoryRepo, err = postgres.NewPriceHistoryRepo(conn)
	if err != nil {
		return nil, err
	}
	repos.customerRepo, err = postgres.NewCustomerRepo(conn)
	if err != nil {
		return nil, err
	}
	repos.resetTokenRepo, err = postgres.NewResetTokenRepo(conn)
	if err != nil {
		return nil, err
	}
	repos.reservationRepo, err = postgres.NewReservationRepo(conn)
	if err != nil {
		return nil, err
	}
	repos.stockRepo, err = postgres.NewStockRepo(conn)
	if err != nil {
		return nil, err
	}
	repos.orderRepo, err = postgres.NewOrderRepo(conn)
	if err != nil {
		return nil, err
	}
	repos.storeCreditRepo, err = postgres.NewStoreCreditRepo(conn)
	if err != nil {
		return nil, err
	}
	repos.returnRepo, err = postgres.NewReturnRepo(conn)
	if err != nil {
		return nil, err
	}
	repos.reviewRepo, err = postgres.NewReviewRepo(conn)
	if err != nil {
		return nil, err
	}
	repos.statsRepo, err = postgres.NewStatsRepo(conn)
	if err != nil {
		return nil, err
	}
	repos.paymentRepo, err = postgres.NewPaymentRepo(conn)
	if err != nil {
		return nil, err
	}
	repos.shippingRepo, err = postgres.NewShippingRepo(conn)
	if err != nil {
		return nil, err
	}
	repos.configRepo = postgres.NewConfigRepo(conn)
	repos.zoneRepo, err = postgres.NewZoneRepo(conn)
	if err != nil {
		return nil, err
	}
	repos.categoryRepo, err = postgres.NewCategoryRepo(conn)
	if err != nil {
		return nil, err
	}
	repos.collectionRepo, err = postgres.NewCollectionRepo(conn)
	if err != nil {
		return nil, err
	}
	// collectionRepo: opened at serve start; HTTP handlers not wired yet.

	repos.taxRateRepo, err = postgres.NewTaxRateRepo(conn)
	if err != nil {
		return nil, err
	}

	repos.promotionRepo, err = postgres.NewPromotionRepo(conn)
	if err != nil {
		return nil, err
	}

	repos.couponRepo, err = postgres.NewCouponRepo(conn)
	if err != nil {
		return nil, err
	}

	repos.invoiceRepo, err = postgres.NewInvoiceRepo(conn)
	if err != nil {
		return nil, err
	}

	repos.assetRepo, err = postgres.NewAssetRepo(conn)
	if err != nil {
		return nil, err
	}

	repos.rewriteRepo, err = postgres.NewRewriteRepo(conn)
	if err != nil {
		return nil, err
	}

	repos.pageRepo, err = postgres.NewPageRepo(conn)
	if err != nil {
		return nil, err
	}

	repos.menuRepo, err = postgres.NewMenuRepo(conn)
	if err != nil {
		return nil, err
	}

	repos.contentBlockRepo, err = postgres.NewContentBlockRepo(conn)
	if err != nil {
		return nil, err
	}

	repos.storeRepo, err = postgres.NewStoreRepo(conn)
	if err != nil {
		return nil, err
	}

	repos.translationRepo, err = postgres.NewTranslationRepo(conn)
	if err != nil {
		return nil, err
	}
	// translationRepo: opened at serve start; admin handlers not wired yet.

	repos.contentTranslationRepo, err = postgres.NewContentTranslationRepo(conn)
	if err != nil {
		return nil, err
	}

	repos.consentRepo, err = postgres.NewConsentRepo(conn)
	if err != nil {
		return nil, err
	}

	repos.customerAddressRepo, err = postgres.NewCustomerAddressRepo(conn)
	if err != nil {
		return nil, err
	}

	repos.mfaRepo, err = postgres.NewMFARepo(conn)
	if err != nil {
		return nil, err
	}

	repos.auditLogRepo, err = postgres.NewAuditLogRepo(conn)
	if err != nil {
		return nil, err
	}

	repos.webhookEndpointRepo, err = postgres.NewWebhookEndpointRepo(conn)
	if err != nil {
		return nil, err
	}

	repos.integrationIdempotencyRepo, err = postgres.NewIntegrationIdempotencyRepo(conn)
	if err != nil {
		return nil, err
	}

	repos.extensionFieldRepo, err = postgres.NewExtensionFieldRepo(conn)
	if err != nil {
		return nil, err
	}

	repos.extensionValueRepo, err = postgres.NewExtensionValueRepo(conn)
	if err != nil {
		return nil, err
	}

	repos.rolePermRepo, err = postgres.NewRolePermissionRepo(conn)
	if err != nil {
		return nil, err
	}

	return repos, nil
}
