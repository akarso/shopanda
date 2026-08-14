package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	accountApp "github.com/akarso/shopanda/internal/application/account"
	adminApp "github.com/akarso/shopanda/internal/application/admin"
	adminroleApp "github.com/akarso/shopanda/internal/application/adminrole"
	adminuserApp "github.com/akarso/shopanda/internal/application/adminuser"
	assetsApp "github.com/akarso/shopanda/internal/application/assets"
	authApp "github.com/akarso/shopanda/internal/application/auth"
	cacheApp "github.com/akarso/shopanda/internal/application/cache"
	cartApp "github.com/akarso/shopanda/internal/application/cart"
	checkoutApp "github.com/akarso/shopanda/internal/application/checkout"
	cmsApp "github.com/akarso/shopanda/internal/application/cms"
	"github.com/akarso/shopanda/internal/application/composition"
	exportctxApp "github.com/akarso/shopanda/internal/application/exportctx"
	"github.com/akarso/shopanda/internal/application/exporter"
	extensionApp "github.com/akarso/shopanda/internal/application/extension"
	hooksApp "github.com/akarso/shopanda/internal/application/hooks"
	importctxApp "github.com/akarso/shopanda/internal/application/importctx"
	integrationApp "github.com/akarso/shopanda/internal/application/integration"
	mediaApp "github.com/akarso/shopanda/internal/application/media"
	mfaApp "github.com/akarso/shopanda/internal/application/mfa"
	"github.com/akarso/shopanda/internal/application/notification"
	orderApp "github.com/akarso/shopanda/internal/application/order"
	"github.com/akarso/shopanda/internal/application/pluginreport"
	portsapp "github.com/akarso/shopanda/internal/application/ports"
	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	returnsApp "github.com/akarso/shopanda/internal/application/returns"
	reviewsApp "github.com/akarso/shopanda/internal/application/reviews"
	"github.com/akarso/shopanda/internal/application/rewrite"
	setupApp "github.com/akarso/shopanda/internal/application/setup"
	slotsApp "github.com/akarso/shopanda/internal/application/slots"
	storecreditApp "github.com/akarso/shopanda/internal/application/storecredit"
	themeapp "github.com/akarso/shopanda/internal/application/theme"
	webhookApp "github.com/akarso/shopanda/internal/application/webhook"
	"github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/invoice"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/promotion"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/domain/translation"
	"github.com/akarso/shopanda/internal/infrastructure/imaging"
	"github.com/akarso/shopanda/internal/infrastructure/invoicepdf"
	smtpmail "github.com/akarso/shopanda/internal/infrastructure/smtp"
	"github.com/akarso/shopanda/internal/infrastructure/webhook"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/jwt"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/internal/seed"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

type serveRuntime struct {
	repos *serveRepos

	pluginApp *plugin.App
	bus       *event.Bus
	registry  *plugin.Registry

	jobWorker *jobs.Worker
	jobQueue  jobs.Queue
	appCache  cache.Cache

	searchEngine search.SearchEngine

	tokenParser  *authApp.ValidatingTokenParser
	jwtIssuer    *jwt.Issuer
	jwtSecretStr string

	pdp *composition.Pipeline[composition.ProductContext]
	plp *composition.Pipeline[composition.ListingContext]

	cartService           *cartApp.Service
	checkoutService       *checkoutApp.Service
	authService           *authApp.Service
	accountService        *accountApp.Service
	returnService         *returnsApp.Service
	extensionValueService *extensionApp.ValueService
	setupService          *setupApp.Service

	menuResolver   *cmsApp.MenuResolver
	blockResolver  *cmsApp.BlockResolver
	shippingReg    *shipping.ProviderRegistry
	payRegistry    *payment.ProviderRegistry
	slotRegistry   *slotsApp.Registry
	assetRegistry  *assetsApp.Registry
	attributeStore *adminApp.AttributeStore

	log logger.Logger

	productHandler                 *shophttp.ProductHandler
	productAdmin                   *shophttp.ProductAdminHandler
	productTranslationAdmin        *shophttp.ProductTranslationAdminHandler
	productPriceAdmin              *shophttp.ProductPriceAdminHandler
	variantHandler                 *shophttp.VariantHandler
	cartHandler                    *shophttp.CartHandler
	orderHandler                   *shophttp.OrderHandler
	orderAdmin                     *shophttp.OrderAdminHandler
	invoiceAdmin                   *shophttp.InvoiceAdminHandler
	statsAdmin                     *shophttp.StatsAdminHandler
	authHandler                    *shophttp.AuthHandler
	adminMFAHandler                *shophttp.AdminMFAHandler
	paymentWebhook                 *shophttp.PaymentWebhookHandler
	stripeWebhook                  *shophttp.StripeWebhookHandler
	refundHandler                  *shophttp.RefundHandler
	returnAdmin                    *shophttp.ReturnAdminHandler
	returnAccount                  *shophttp.ReturnAccountHandler
	reviewHandler                  *shophttp.ReviewHandler
	reviewAccount                  *shophttp.ReviewAccountHandler
	reviewAdmin                    *shophttp.ReviewAdminHandler
	eprReportAdmin                 *shophttp.EprReportHandler
	ossReportAdmin                 *shophttp.OssReportHandler
	paymentAdmin                   *shophttp.PaymentAdminHandler
	shippingRates                  *shophttp.ShippingRatesHandler
	categoryHandler                *shophttp.CategoryHandler
	categoryAdmin                  *shophttp.CategoryAdminHandler
	categoryProductAssignmentAdmin *shophttp.CategoryProductAssignmentAdminHandler
	searchHandler                  *shophttp.SearchHandler
	mediaHandler                   *shophttp.MediaHandler
	configAdmin                    *shophttp.ConfigAdminHandler
	schemaHandler                  *shophttp.SchemaHandler
	pageHandler                    *shophttp.PageHandler
	pageAdmin                      *shophttp.PageAdminHandler
	menuHandler                    *shophttp.MenuHandler
	menuAdmin                      *shophttp.MenuAdminHandler
	contentBlockHandler            *shophttp.ContentBlockHandler
	contentBlockAdmin              *shophttp.ContentBlockAdminHandler
	couponAdmin                    *shophttp.CouponAdminHandler
	promotionAdmin                 *shophttp.PromotionAdminHandler
	attributeAdmin                 *shophttp.AttributeAdminHandler
	extensionFieldAdmin            *shophttp.ExtensionFieldAdminHandler
	extensionValueAdmin            *shophttp.ExtensionValueAdminHandler
	extensionHookAdmin             *shophttp.ExtensionHookAdminHandler
	extensionSlotAdmin             *shophttp.ExtensionSlotAdminHandler
	extensionPortAdmin             *shophttp.ExtensionPortAdminHandler
	inventoryAdmin                 *shophttp.InventoryAdminHandler
	storeAdmin                     *shophttp.StoreAdminHandler
	auditLogAdmin                  *shophttp.AuditLogAdminHandler
	webhookEndpointAdmin           *shophttp.WebhookEndpointAdminHandler
	integrationIdempotencyAdmin    *shophttp.IntegrationIdempotencyAdminHandler
	shippingZoneAdmin              *shophttp.ShippingZoneAdminHandler
	customerAdmin                  *shophttp.CustomerAdminHandler
	adminUserHandler               *shophttp.AdminUserHandler
	setupHandler                   *shophttp.SetupHandler
	adminRoleHandler               *shophttp.AdminRoleHandler
	storeCreditAdmin               *shophttp.StoreCreditAdminHandler
	storeCreditAccount             *shophttp.StoreCreditAccountHandler
	accountHandler                 *shophttp.AccountHandler
	sitemapHandler                 *shophttp.SitemapHandler
	robotsHandler                  *shophttp.RobotsHandler
	docsHandler                    *shophttp.DocsHandler
	checkoutHandler                *shophttp.CheckoutHandler
}

func wireServeRuntime(cfg *config.Config, log logger.Logger, conn *sql.DB, repos *serveRepos) (*serveRuntime, error) {
	productRepo := repos.productRepo
	variantRepo := repos.variantRepo
	cartRepo := repos.cartRepo
	priceRepo := repos.priceRepo
	priceHistoryRepo := repos.priceHistoryRepo
	customerRepo := repos.customerRepo
	resetTokenRepo := repos.resetTokenRepo
	reservationRepo := repos.reservationRepo
	stockRepo := repos.stockRepo
	orderRepo := repos.orderRepo
	storeCreditRepo := repos.storeCreditRepo
	returnRepo := repos.returnRepo
	reviewRepo := repos.reviewRepo
	statsRepo := repos.statsRepo
	paymentRepo := repos.paymentRepo
	shippingRepo := repos.shippingRepo
	configRepo := repos.configRepo
	zoneRepo := repos.zoneRepo
	categoryRepo := repos.categoryRepo
	taxRateRepo := repos.taxRateRepo
	promotionRepo := repos.promotionRepo
	couponRepo := repos.couponRepo
	invoiceRepo := repos.invoiceRepo
	assetRepo := repos.assetRepo
	rewriteRepo := repos.rewriteRepo
	pageRepo := repos.pageRepo
	menuRepo := repos.menuRepo
	contentBlockRepo := repos.contentBlockRepo
	storeRepo := repos.storeRepo
	contentTranslationRepo := repos.contentTranslationRepo
	consentRepo := repos.consentRepo
	mfaRepo := repos.mfaRepo
	auditLogRepo := repos.auditLogRepo
	webhookEndpointRepo := repos.webhookEndpointRepo
	integrationIdempotencyRepo := repos.integrationIdempotencyRepo
	extensionFieldRepo := repos.extensionFieldRepo
	extensionValueRepo := repos.extensionValueRepo
	rolePermRepo := repos.rolePermRepo

	// Event bus (created early for plugin init and later handlers).
	bus := event.NewBus(log)

	// Core plugin registry — infrastructure providers register during Init.
	registry := plugin.NewRegistry(log)
	registerPlugins(registry, cfg)
	extensionRegistry := extensionApp.NewRegistry()
	hookRegistry := hooksApp.NewRegistry(log)
	importRegistry := importctxApp.NewRegistry(log)
	exportRegistry := exportctxApp.NewRegistry(log)
	promotionEvaluators := promotion.NewEvaluatorRegistry()
	slotRegistry := slotsApp.NewRegistry(log)
	if cfg.Frontend.Enabled && cfg.Frontend.ThemePath != "" {
		if anchors, anchorErr := themeapp.DeclaredAnchorsFromDir(cfg.Frontend.ThemePath); anchorErr != nil {
			log.Warn("slots.theme_markers.load_failed", map[string]interface{}{
				"theme_path": cfg.Frontend.ThemePath,
				"error":      anchorErr.Error(),
			})
		} else {
			unknown, valErr := themeapp.ValidateDeclaredAnchors(anchors, cfg.Frontend.StrictSlotMarkers)
			for _, anchor := range unknown {
				log.Warn("slots.theme.unknown_anchor", map[string]interface{}{
					"anchor":     anchor,
					"theme_path": cfg.Frontend.ThemePath,
				})
			}
			if valErr != nil {
				return nil, fmt.Errorf("theme slot markers: %w", valErr)
			}
			slotRegistry.SetThemeMarkers(anchors)
		}
	}
	if config.DevModeEnabled() {
		slotRegistry.SetDevMode(true)
	}
	assetRegistry := assetsApp.NewRegistry()
	pluginApp := &plugin.App{
		Logger:    log,
		Bus:       bus,
		Config:    cfg,
		Bootstrap: &plugin.Bootstrap{DB: conn},
	}
	pluginApp.SetExtensionRegistry(extensionRegistry)
	pluginApp.SetHookRegistry(hookRegistry)
	pluginApp.SetImportRegistry(importRegistry)
	pluginApp.SetExportRegistry(exportRegistry)
	pluginApp.SetPromotionEvaluatorRegistry(promotionEvaluators)
	pluginApp.SetSlotRegistry(slotRegistry)
	pluginApp.SetAssetRegistry(assetRegistry)
	pluginApp.SetIntegrationIdempotencyStore(integrationIdempotencyRepo)
	orderStatusService := orderApp.NewStatusService(orderRepo)
	pluginApp.SetIntegrationOrderStatusUpdater(plugin.NewIntegrationOrderStatusUpdater(orderStatusService))
	wireIntegrationStockSyncer(pluginApp, variantRepo, stockRepo)
	summary := registry.InitAll(pluginApp)
	if err := extensionRegistry.LoadPersisted(context.Background(), extensionFieldRepo, log); err != nil {
		return nil, fmt.Errorf("load extension fields: %w", err)
	}
	extensionFieldService := extensionApp.NewFieldService(extensionRegistry, extensionFieldRepo)
	extensionValueService := extensionApp.NewValueService(extensionRegistry, extensionValueRepo)
	if err := plugin.LoadPersisted(context.Background(), configRepo, cfg, registry.ConfigRegistry()); err != nil {
		return nil, fmt.Errorf("load plugin config: %w", err)
	}
	plugin.LogStartup(log, registry, cfg)
	pluginreport.LogSummary(log, pluginreport.Build(registry, pluginApp, cfg))
	log.Info("plugin.init.summary", map[string]interface{}{
		"registered":  summary.Registered,
		"initialized": summary.Initialized,
		"failed":      summary.Failed,
	})

	adminRoleService := adminroleApp.NewService(rolePermRepo)
	if err := adminRoleService.SyncPluginDefaults(context.Background()); err != nil {
		return nil, fmt.Errorf("sync role permissions: %w", err)
	}

	// Search engine.
	searchEngine, err := resolveSearchEngine(pluginApp, conn, cfg)
	if err != nil {
		return nil, err
	}

	// Job queue, worker, mailer, cache — shared setup.
	jobWorker, jobQueue, appCache, err := setupWorker(conn, cfg, log, pluginApp)
	if err != nil {
		return nil, err
	}
	if err := integrationApp.RegisterSyncJobEventTriggers(pluginApp, bus, jobQueue, log); err != nil {
		return nil, fmt.Errorf("sync job event triggers: %w", err)
	}

	// Email notifications (needs jobQueue from setupWorker).
	mailTemplates := mail.NewTemplates()
	notification.RegisterTemplates(mailTemplates)

	invoicePDFRenderer := invoicepdf.NewRenderer()

	notifSvc := notification.New(mailTemplates, customerRepo, orderRepo, jobQueue, log,
		notification.WithStoreURL(cfg.Server.PublicBaseURL),
		notification.WithResetBaseURL(cfg.Server.PublicBaseURL+"/auth/reset-password"),
		notification.WithInvoices(invoiceRepo),
		notification.WithPDFRenderer(invoicePDFRenderer),
	)

	// Media storage.
	mediaStorage, err := resolveMediaStorage(pluginApp, cfg)
	if err != nil {
		return nil, err
	}

	menuResolver := cmsApp.NewMenuResolver(categoryRepo, pageRepo)

	blockResolver := cmsApp.NewBlockResolver(productRepo)

	contentTranslator := translation.NewContentTranslator(contentTranslationRepo, log)

	// Providers.
	shippingReg, err := resolveShippingRegistry(pluginApp)
	if err != nil {
		return nil, err
	}

	payRegistry, err := resolvePaymentRegistry(pluginApp)
	if err != nil {
		return nil, err
	}

	// Dev-only: log password reset tokens when explicitly opted in.
	// Requires SHOPANDA_DEV_MODE and SHOPANDA_DEV_LOG_RESET_TOKENS both truthy.
	if config.ShouldLogPasswordResetTokens() {
		bus.On(customer.EventPasswordResetRequested, func(_ context.Context, evt event.Event) error {
			if data, ok := evt.Data.(customer.PasswordResetRequestedData); ok {
				log.Info("dev.password_reset.token", map[string]interface{}{
					"customer_id": data.CustomerID,
					"token":       data.Token,
				})
			}
			return nil
		})
	}

	// Wire order.paid → email notification.
	bus.OnAsync(order.EventOrderPaid, notifSvc.HandleOrderPaid)

	// Wire password reset → email notification.
	bus.OnAsync(customer.EventPasswordResetRequested, notifSvc.HandlePasswordReset)
	bus.OnAsync(customer.EventEmailVerificationRequested, notifSvc.HandleEmailVerification)

	// Wire storefront security verification → email notification.
	bus.OnAsync(customer.EventSecurityVerificationRequested, notifSvc.HandleSecurityVerification)

	// Wire account email change → verification link (new address) + notice (old address).
	bus.OnAsync(customer.EventEmailChangeRequested, notifSvc.HandleEmailChangeRequested)
	bus.OnAsync(customer.EventEmailChangeNotified, notifSvc.HandleEmailChangeNotified)

	// Wire shipment.shipped → email notification.
	bus.OnAsync(shipping.EventShipmentShipped, notifSvc.HandleShipmentShipped)

	// Wire invoice.created → email notification with PDF attachment.
	bus.OnAsync(invoice.EventInvoiceCreated, notifSvc.HandleInvoiceCreated)

	// Wire catalog events → URL rewrites.
	rewriteSub := rewrite.NewSubscriber(rewriteRepo, log)
	rewriteSub.Register(bus)

	// Wire product/price changes → cache invalidation.
	cacheInvalidation := cacheApp.NewInvalidationSubscriber(appCache, log)
	cacheInvalidation.Register(bus)

	// Wire catalog events → search index sync.
	bus.OnAsync(catalog.EventProductCreated, func(ctx context.Context, evt event.Event) error {
		data, ok := evt.Data.(catalog.ProductCreatedData)
		if !ok {
			return nil
		}
		p, err := productRepo.FindByID(ctx, data.ProductID)
		if err != nil {
			return fmt.Errorf("search sync: load product %s: %w", data.ProductID, err)
		}
		if p == nil {
			return nil
		}
		return searchEngine.IndexProduct(ctx, search.Product{
			ID:          p.ID,
			Name:        p.Name,
			Slug:        p.Slug,
			Description: p.Description,
			CreatedAt:   p.CreatedAt,
			Attributes:  p.Attributes,
		})
	})
	bus.OnAsync(catalog.EventProductUpdated, func(ctx context.Context, evt event.Event) error {
		data, ok := evt.Data.(catalog.ProductUpdatedData)
		if !ok {
			return nil
		}
		p, err := productRepo.FindByID(ctx, data.ProductID)
		if err != nil {
			return fmt.Errorf("search sync: load product %s: %w", data.ProductID, err)
		}
		if p == nil {
			return nil
		}
		return searchEngine.IndexProduct(ctx, search.Product{
			ID:          p.ID,
			Name:        p.Name,
			Slug:        p.Slug,
			Description: p.Description,
			CreatedAt:   p.CreatedAt,
			Attributes:  p.Attributes,
		})
	})

	// Base URL for SEO (sitemap, canonical, robots).
	// Normalized at config load time (scheme defaulted, trailing slash stripped).
	baseURL := cfg.Server.PublicBaseURL

	// Composition pipelines (core SEO steps + plugin steps).
	reviewService := reviewsApp.NewService(reviewRepo, productRepo)
	pdp := composition.NewPipeline[composition.ProductContext]()
	pdp.AddStep(composition.ProductMetaStep{})
	pdp.AddStep(composition.NewJSONLDProductStep(variantRepo, priceRepo, stockRepo))
	pdp.AddStep(composition.NewCanonicalURLStep(baseURL))
	pdp.AddStep(composition.NewPriceIndicationStep(variantRepo, priceRepo, priceHistoryRepo, configRepo))
	pdp.AddStep(composition.NewWeeeStep(configRepo))
	pdp.AddStep(composition.NewGpsrStep(configRepo))
	pdp.AddStep(composition.NewReviewsStep(reviewService))
	plp := composition.NewPipeline[composition.ListingContext]()
	plp.AddStep(composition.ListingMetaStep{})
	plp.AddStep(composition.NewListingCanonicalURLStep(baseURL))
	plp.AddStep(composition.NewListingPriceIndicationStep(variantRepo, priceRepo, priceHistoryRepo, configRepo))
	for _, s := range pluginApp.CompositionSteps("pdp") {
		if v, ok := s.(composition.Step[composition.ProductContext]); ok {
			pdp.AddStep(v)
		} else {
			log.Error("plugin.step.invalid_type", fmt.Errorf("expected composition.Step[ProductContext], got %T", s), map[string]interface{}{
				"pipeline": "pdp",
			})
		}
	}
	for _, s := range pluginApp.CompositionSteps("plp") {
		if v, ok := s.(composition.Step[composition.ListingContext]); ok {
			plp.AddStep(v)
		} else {
			log.Error("plugin.step.invalid_type", fmt.Errorf("expected composition.Step[ListingContext], got %T", s), map[string]interface{}{
				"pipeline": "plp",
			})
		}
	}

	// Pricing pipeline (core + positioned plugin steps).
	taxCalculator, err := resolveTaxCalculator(pluginApp, taxRateRepo)
	if err != nil {
		return nil, fmt.Errorf("tax calculator: %w", err)
	}
	corePricingSteps := []pricing.PricingStep{
		appPricing.NewBasePriceStep(priceRepo),
		appPricing.NewCatalogPromotionStep(promotionRepo, couponRepo, promotionEvaluators),
		appPricing.NewCartPromotionStep(promotionRepo, couponRepo, promotionEvaluators),
		appPricing.NewTaxStep(taxCalculator),
		pricing.NewFinalizeStep(),
	}
	pluginPricingRegs := make([]appPricing.PluginStepRegistration, 0)
	for _, reg := range pluginApp.PricingStepRegistrations() {
		step, ok := reg.Step.(pricing.PricingStep)
		if !ok {
			log.Error("plugin.step.invalid_type", fmt.Errorf("expected pricing.PricingStep, got %T", reg.Step), map[string]interface{}{
				"pipeline": "pricing",
			})
			continue
		}
		pluginPricingRegs = append(pluginPricingRegs, appPricing.PluginStepRegistration{
			Step:     step,
			Position: reg.Position,
		})
	}
	pricingSteps, err := appPricing.MergePluginSteps(corePricingSteps, pluginPricingRegs)
	if err != nil {
		return nil, fmt.Errorf("pricing pipeline: %w", err)
	}
	pricingPipeline := pricing.NewPipeline(pricingSteps...)

	// Application services.
	cartService := cartApp.NewService(cartRepo, priceRepo, promotionRepo, couponRepo, pricingPipeline, log, bus, extensionValueService, hookRegistry)
	storeCreditService := storecreditApp.NewService(storeCreditRepo, customerRepo)

	// Checkout workflow.
	validateCartStep := checkoutApp.NewValidateCartStep(variantRepo)
	recalculatePricingStep := checkoutApp.NewRecalculatePricingStep(pricingPipeline)
	reserveInventoryStep := checkoutApp.NewReserveInventoryStep(reservationRepo)
	createOrderStep := checkoutApp.NewCreateOrderStep(orderRepo, variantRepo, storeCreditService, extensionValueService)
	selectShippingStep := checkoutApp.NewSelectShippingStep(shippingReg, shippingRepo)
	initiatePaymentStep := checkoutApp.NewInitiatePaymentStep(payRegistry, paymentRepo)
	checkoutSteps := []checkoutApp.Step{
		validateCartStep,
		recalculatePricingStep,
		reserveInventoryStep,
		createOrderStep,
		selectShippingStep,
		initiatePaymentStep,
	}
	pluginCheckoutRegs := make([]checkoutApp.PluginStepRegistration, 0)
	for _, reg := range pluginApp.CheckoutStepRegistrations() {
		step, ok := reg.Step.(checkoutApp.Step)
		if !ok {
			log.Error("plugin.step.invalid_type", fmt.Errorf("expected checkout.Step, got %T", reg.Step), map[string]interface{}{
				"pipeline": "checkout",
			})
			continue
		}
		pluginCheckoutRegs = append(pluginCheckoutRegs, checkoutApp.PluginStepRegistration{
			Step:     step,
			Position: reg.Position,
		})
	}
	checkoutSteps, err = checkoutApp.MergePluginSteps(checkoutSteps, pluginCheckoutRegs)
	if err != nil {
		return nil, fmt.Errorf("checkout workflow: %w", err)
	}
	checkoutWorkflow := checkoutApp.NewWorkflow(checkoutSteps, bus, log)
	checkoutService := checkoutApp.NewService(cartRepo, checkoutWorkflow, log)
	checkoutHandler := shophttp.NewCheckoutHandler(checkoutService, extensionValueService)

	// JWT.
	jwtTTL, err := time.ParseDuration(cfg.Auth.JWTTTL)
	if err != nil {
		return nil, fmt.Errorf("invalid auth.jwt_ttl: %w", err)
	}
	jwtKey, err := jwt.ParseSecret(cfg.Auth.JWTSecret)
	if err != nil {
		return nil, fmt.Errorf("jwt secret: %w", err)
	}
	jwtIssuer, err := jwt.NewIssuerFromKey(jwtKey, jwtTTL)
	if err != nil {
		return nil, fmt.Errorf("jwt issuer: %w", err)
	}
	tokenParser := authApp.NewValidatingTokenParser(jwtIssuer, customerRepo, 30*time.Second)

	authService := authApp.NewService(customerRepo, resetTokenRepo, jwtIssuer, bus, log, time.Hour)
	if cfg.Auth.Lockout.Enabled {
		lockoutWindow, err := time.ParseDuration(cfg.Auth.Lockout.Window)
		if err != nil {
			return nil, fmt.Errorf("invalid auth.lockout.window: %w", err)
		}
		attemptStore, err := authApp.NewAttemptStore(cfg.Auth.Lockout.Store, appCache, log)
		if err != nil {
			return nil, fmt.Errorf("auth lockout store: %w", err)
		}
		if cfg.Auth.Lockout.Store == "memory" {
			log.Warn("auth.lockout.store_memory", map[string]interface{}{
				"message": "auth.lockout.store=memory is single-instance only; use store=cache for multi-instance",
			})
		}
		authService.SetLockout(authApp.LockoutSettings{
			Enabled:     true,
			MaxFailures: cfg.Auth.Lockout.MaxFailures,
			Window:      lockoutWindow,
		}, attemptStore)
	}
	// Same trimmed key material as jwt issuer (64-hex kept as ASCII, not decoded).
	jwtSecretStr := string(jwtKey)
	mfaService := mfaApp.NewService(mfaRepo, customerRepo, configRepo, jwtSecretStr, cfg.Auth.MFAEnabled)
	if cfg.Auth.MFAEnabled {
		authService.SetMFAClient(mfaService)
	}

	// Admin schema registry.
	adminRegistry := admin.NewRegistry()
	adminApp.RegisterProductSchemas(adminRegistry)
	adminApp.RegisterPageSchemas(adminRegistry)

	attributeStore := adminApp.NewAttributeStore(configRepo)

	discoveryFacetSync := newDiscoveryFacetSyncer(attributeStore, searchEngine)
	if err := discoveryFacetSync.Sync(context.Background()); err != nil {
		return nil, fmt.Errorf("configure search attribute facets: %w", err)
	}

	// Associate permissions with schemas so the schema handler can
	// filter access per role. Fail closed if any wiring is broken.
	for _, sp := range []struct {
		kind string
		name string
		err  error
	}{
		{"form", "product.form", adminRegistry.SetFormPermission("product.form", rbac.ProductsWrite)},
		{"grid", "product.grid", adminRegistry.SetGridPermission("product.grid", rbac.ProductsRead)},
		{"form", "page.form", adminRegistry.SetFormPermission("page.form", rbac.ContentWrite)},
		{"grid", "page.grid", adminRegistry.SetGridPermission("page.grid", rbac.ContentRead)},
	} {
		if sp.err != nil {
			return nil, fmt.Errorf("admin schema permission wiring failed for %s %q: %w", sp.kind, sp.name, sp.err)
		}
	}

	// Shared admin auditor with optional persistent audit log.
	sharedAuditor := adminApp.NewAuditor(log)
	sharedAuditor.SetAuditLogRepository(auditLogRepo)

	// Handlers.
	productHandler := shophttp.NewProductHandler(productRepo, pdp, plp, contentTranslator)
	productAdmin := shophttp.NewProductAdminHandlerWithAuditor(productRepo, bus, sharedAuditor, log)
	productTranslationAdmin := shophttp.NewProductTranslationAdminHandler(productRepo, contentTranslationRepo, sharedAuditor, log)
	productPriceAdmin := shophttp.NewProductPriceAdminHandler(productRepo, variantRepo, priceRepo, sharedAuditor, log)
	variantHandler := shophttp.NewVariantHandler(productRepo, variantRepo, bus)
	cartHandler := shophttp.NewCartHandler(cartService, extensionValueService)
	orderHandler := shophttp.NewOrderHandler(orderRepo, extensionValueService)
	orderAdmin := shophttp.NewOrderAdminHandlerWithAuditor(orderRepo, sharedAuditor, extensionValueService)
	invoiceAdmin := shophttp.NewInvoiceAdminHandler(invoiceRepo, orderRepo, invoicePDFRenderer, mediaStorage)
	statsAdmin := shophttp.NewStatsAdminHandler(statsRepo)
	authHandler := shophttp.NewAuthHandler(authService, cfg.RateLimit.TrustedProxies...)
	adminMFAHandler := shophttp.NewAdminMFAHandler(mfaService)
	webhookVerifier := webhook.NewHMACVerifier(cfg.Webhooks.Secrets)
	paymentWebhook := shophttp.NewPaymentWebhookHandler(paymentRepo, bus, webhookVerifier)

	// Stripe-specific webhook handler: verifies Stripe-Signature and parses
	// Stripe event types (payment_intent.succeeded / payment_failed).
	// The webhook secret is sourced exclusively from the
	// SHOPANDA_PAYMENT_STRIPE_WEBHOOK_SECRET environment variable.
	var stripeWebhook *shophttp.StripeWebhookHandler
	webhookSecret := os.Getenv("SHOPANDA_PAYMENT_STRIPE_WEBHOOK_SECRET")
	if webhookSecret == "" && cfg.Payment.Stripe.WebhookSecret != "" {
		log.Warn("payment.stripe.webhook_secret_ignored", map[string]interface{}{
			"message": "Stripe webhook_secret in YAML is ignored; set SHOPANDA_PAYMENT_STRIPE_WEBHOOK_SECRET env var",
		})
	}
	if cfg.Payment.Stripe.Enabled && webhookSecret != "" {
		stripeWebhook = shophttp.NewStripeWebhookHandler(paymentRepo, bus, webhookSecret)
		log.Info("payment.stripe.webhook_handler_enabled", nil)
	} else if cfg.Payment.Stripe.Enabled {
		log.Warn("payment.stripe.no_webhook_secret", map[string]interface{}{
			"message": "Stripe enabled but SHOPANDA_PAYMENT_STRIPE_WEBHOOK_SECRET not set; Stripe webhooks will not be handled",
		})
	}

	// Refund handler: only available when the payment provider supports refunds.
	var refundHandler *shophttp.RefundHandler
	var stripeRefunder payment.Refunder
	if refunder, ok := payRegistry.Refunder(payment.MethodStripe); ok {
		stripeRefunder = refunder
		refundHandler = shophttp.NewRefundHandler(paymentRepo, refunder, bus)
		log.Info("payment.refund_handler_enabled", nil)
	}

	returnService := returnsApp.NewService(returnRepo, orderRepo, stockRepo, paymentRepo, stripeRefunder, bus, log)
	returnAdmin := shophttp.NewReturnAdminHandler(returnService, sharedAuditor)
	returnAccount := shophttp.NewReturnAccountHandler(returnService)
	reviewHandler := shophttp.NewReviewHandler(reviewService)
	reviewAccount := shophttp.NewReviewAccountHandler(reviewService)
	reviewAdmin := shophttp.NewReviewAdminHandler(reviewService, sharedAuditor)
	eprExporter := exporter.NewEprExporter(productRepo, variantRepo, configRepo)
	eprReportAdmin := shophttp.NewEprReportHandler(eprExporter)
	ossExporter := exporter.NewOssExporter(orderRepo, configRepo)
	ossReportAdmin := shophttp.NewOssReportHandler(ossExporter)
	paymentAdmin := shophttp.NewPaymentAdminHandler(paymentRepo, sharedAuditor)

	shippingRates := shophttp.NewShippingRatesHandler(shippingReg.Providers()...)
	categoryHandler := shophttp.NewCategoryHandler(categoryRepo, productRepo)
	categoryAdmin := shophttp.NewCategoryAdminHandlerWithAuditor(categoryRepo, bus, sharedAuditor)
	categoryProductAssignmentAdmin := shophttp.NewCategoryProductAssignmentAdminHandlerWithAuditor(categoryRepo, productRepo, productRepo, sharedAuditor)
	searchHandler := shophttp.NewSearchHandler(searchEngine).WithAdvancedSearchAttributes(attributeStore)
	mediaService := mediaApp.NewService(mediaStorage, assetRepo, bus, log)
	if thumbCfg := cfg.Media.Thumbnails; len(thumbCfg) > 0 {
		names := make([]string, 0, len(thumbCfg))
		for name := range thumbCfg {
			names = append(names, name)
		}
		sort.Strings(names)
		presets := make([]media.ThumbnailPreset, 0, len(names))
		for _, name := range names {
			tc := thumbCfg[name]
			presets = append(presets, media.ThumbnailPreset{
				Name:   name,
				Width:  tc.Width,
				Height: tc.Height,
				Fit:    tc.Fit,
			})
		}
		mediaService.SetImageProcessor(imaging.New(), presets)
	}
	if cfg.Media.WebP.Enabled {
		mediaService.SetWebPConfig(cfg.Media.WebP.Enabled, cfg.Media.WebP.Quality)
		if len(cfg.Media.Thumbnails) == 0 {
			log.Warn("media.webp.no_thumbnails", map[string]interface{}{
				"hint": "webp is enabled but no thumbnail presets are configured; webp variants will not be generated",
			})
		}
	}
	mediaHandler := shophttp.NewMediaHandlerWithAuditor(mediaService, sharedAuditor)
	mediaHandler.SetMaxUploadBytes(cfg.HTTP.MediaMaxBodyBytes)
	configAdmin := shophttp.NewConfigAdminHandler(configRepo, cfg, func(ctx context.Context, smtpCfg shophttp.SMTPTestConfig, to string) error {
		mailer := smtpmail.New(smtpmail.Config{
			Host:     smtpCfg.Host,
			Port:     smtpCfg.Port,
			User:     smtpCfg.User,
			Password: smtpCfg.Password,
			From:     smtpCfg.From,
		})
		return mailer.Send(ctx, mail.Message{
			To:      to,
			Subject: "Shopanda SMTP test",
			Body:    "<p>This is a test email from Shopanda admin settings.</p>",
		})
	}, sharedAuditor, registry.ConfigRegistry())
	schemaHandler := shophttp.NewSchemaHandler(adminRegistry, attributeStore)
	pageHandler := shophttp.NewPageHandler(pageRepo, contentTranslator)
	pageAdmin := shophttp.NewPageAdminHandlerWithAuditor(pageRepo, bus, sharedAuditor)
	menuHandler := shophttp.NewMenuHandler(menuRepo, menuResolver)
	menuAdmin := shophttp.NewMenuAdminHandler(menuRepo, sharedAuditor)
	contentBlockHandler := shophttp.NewContentBlockHandler(contentBlockRepo, pageRepo, blockResolver)
	contentBlockAdmin := shophttp.NewContentBlockAdminHandler(contentBlockRepo, sharedAuditor)
	couponAdmin := shophttp.NewCouponAdminHandlerWithAuditor(couponRepo, promotionRepo, sharedAuditor)
	promotionAdmin := shophttp.NewPromotionAdminHandlerWithAuditor(promotionRepo, sharedAuditor)
	attributeAdmin := shophttp.NewAttributeAdminHandlerWithAuditor(attributeStore, sharedAuditor).
		WithDiscoveryFacetSync(discoveryFacetSync)
	extensionFieldAdmin := shophttp.NewExtensionFieldAdminHandlerWithAuditor(extensionFieldService, sharedAuditor)
	extensionValueAdmin := shophttp.NewExtensionValueAdminHandlerWithAuditor(extensionValueService, sharedAuditor)
	extensionHookAdmin := shophttp.NewExtensionHookAdminHandler(hookRegistry)
	extensionSlotAdmin := shophttp.NewExtensionSlotAdminHandler(slotRegistry)
	portSnapshot := portsapp.BuildSnapshot(pluginApp, cfg)
	extensionPortAdmin := shophttp.NewExtensionPortAdminHandler(portSnapshot)
	inventoryAdmin := shophttp.NewInventoryAdminHandlerWithAuditor(stockRepo, variantRepo, sharedAuditor)
	storeAdmin := shophttp.NewStoreAdminHandlerWithAuditor(storeRepo, bus, sharedAuditor)
	auditLogAdmin := shophttp.NewAuditLogAdminHandler(auditLogRepo, sharedAuditor)
	webhookService := webhookApp.NewService(webhookEndpointRepo)
	webhookEndpointAdmin := shophttp.NewWebhookEndpointAdminHandler(webhookService)
	integrationIdempotencyAdmin := shophttp.NewIntegrationIdempotencyAdminHandler(integrationIdempotencyRepo)
	webhookApp.NewDispatcher(webhookEndpointRepo, jobQueue, log).Register(bus)
	shippingZoneAdmin := shophttp.NewShippingZoneAdminHandler(zoneRepo)
	accountService := accountApp.NewService(customerRepo, consentRepo, bus, log, conn)
	customerAdmin := shophttp.NewCustomerAdminHandlerWithAuditorAndDeleter(customerRepo, accountService, sharedAuditor)
	adminUserService := adminuserApp.NewService(customerRepo)
	adminUserHandler := shophttp.NewAdminUserHandler(adminUserService, sharedAuditor)
	setupService := setupApp.NewService(
		conn,
		"migrations",
		customerRepo,
		storeRepo,
		setupAdminUserCreator{svc: adminUserService},
		func(ctx context.Context, deps seed.Deps) (*seed.Result, error) {
			reg := seed.NewRegistry()
			registerDefaultSeeders(reg)
			return reg.Run(ctx, deps)
		},
		log,
	)
	setupHandler := shophttp.NewSetupHandler(setupService, log)
	adminRoleHandler := shophttp.NewAdminRoleHandler(adminRoleService, sharedAuditor)
	storeCreditAdmin := shophttp.NewStoreCreditAdminHandler(storeCreditService)
	storeCreditAccount := shophttp.NewStoreCreditAccountHandler(storeCreditService)
	accountHandler := shophttp.NewAccountHandler(customerRepo, orderRepo, consentRepo, accountService)
	sitemapHandler := shophttp.NewSitemapHandler(baseURL, productRepo, categoryRepo, pageRepo)
	robotsHandler := shophttp.NewRobotsHandler(baseURL)

	specBytes, err := os.ReadFile(filepath.Join(filepath.Dir(config.FindConfigFile()), "openapi.yaml"))
	if err != nil {
		specBytes, _ = os.ReadFile("openapi.yaml")
	}
	docsHandler := shophttp.NewDocsHandler(specBytes)

	return &serveRuntime{
		repos:                          repos,
		pluginApp:                      pluginApp,
		bus:                            bus,
		registry:                       registry,
		jobWorker:                      jobWorker,
		jobQueue:                       jobQueue,
		appCache:                       appCache,
		searchEngine:                   searchEngine,
		tokenParser:                    tokenParser,
		jwtIssuer:                      jwtIssuer,
		jwtSecretStr:                   jwtSecretStr,
		pdp:                            pdp,
		plp:                            plp,
		cartService:                    cartService,
		checkoutService:                checkoutService,
		authService:                    authService,
		accountService:                 accountService,
		returnService:                  returnService,
		extensionValueService:          extensionValueService,
		setupService:                   setupService,
		menuResolver:                   menuResolver,
		blockResolver:                  blockResolver,
		shippingReg:                    shippingReg,
		payRegistry:                    payRegistry,
		slotRegistry:                   slotRegistry,
		assetRegistry:                  assetRegistry,
		attributeStore:                 attributeStore,
		log:                            log,
		productHandler:                 productHandler,
		productAdmin:                   productAdmin,
		productTranslationAdmin:        productTranslationAdmin,
		productPriceAdmin:              productPriceAdmin,
		variantHandler:                 variantHandler,
		cartHandler:                    cartHandler,
		orderHandler:                   orderHandler,
		orderAdmin:                     orderAdmin,
		invoiceAdmin:                   invoiceAdmin,
		statsAdmin:                     statsAdmin,
		authHandler:                    authHandler,
		adminMFAHandler:                adminMFAHandler,
		paymentWebhook:                 paymentWebhook,
		stripeWebhook:                  stripeWebhook,
		refundHandler:                  refundHandler,
		returnAdmin:                    returnAdmin,
		returnAccount:                  returnAccount,
		reviewHandler:                  reviewHandler,
		reviewAccount:                  reviewAccount,
		reviewAdmin:                    reviewAdmin,
		eprReportAdmin:                 eprReportAdmin,
		ossReportAdmin:                 ossReportAdmin,
		paymentAdmin:                   paymentAdmin,
		shippingRates:                  shippingRates,
		categoryHandler:                categoryHandler,
		categoryAdmin:                  categoryAdmin,
		categoryProductAssignmentAdmin: categoryProductAssignmentAdmin,
		searchHandler:                  searchHandler,
		mediaHandler:                   mediaHandler,
		configAdmin:                    configAdmin,
		schemaHandler:                  schemaHandler,
		pageHandler:                    pageHandler,
		pageAdmin:                      pageAdmin,
		menuHandler:                    menuHandler,
		menuAdmin:                      menuAdmin,
		contentBlockHandler:            contentBlockHandler,
		contentBlockAdmin:              contentBlockAdmin,
		couponAdmin:                    couponAdmin,
		promotionAdmin:                 promotionAdmin,
		attributeAdmin:                 attributeAdmin,
		extensionFieldAdmin:            extensionFieldAdmin,
		extensionValueAdmin:            extensionValueAdmin,
		extensionHookAdmin:             extensionHookAdmin,
		extensionSlotAdmin:             extensionSlotAdmin,
		extensionPortAdmin:             extensionPortAdmin,
		inventoryAdmin:                 inventoryAdmin,
		storeAdmin:                     storeAdmin,
		auditLogAdmin:                  auditLogAdmin,
		webhookEndpointAdmin:           webhookEndpointAdmin,
		integrationIdempotencyAdmin:    integrationIdempotencyAdmin,
		shippingZoneAdmin:              shippingZoneAdmin,
		customerAdmin:                  customerAdmin,
		adminUserHandler:               adminUserHandler,
		setupHandler:                   setupHandler,
		adminRoleHandler:               adminRoleHandler,
		storeCreditAdmin:               storeCreditAdmin,
		storeCreditAccount:             storeCreditAccount,
		accountHandler:                 accountHandler,
		sitemapHandler:                 sitemapHandler,
		robotsHandler:                  robotsHandler,
		docsHandler:                    docsHandler,
		checkoutHandler:                checkoutHandler,
	}, nil
}
