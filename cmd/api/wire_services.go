package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/akarso/shopanda/internal/interfaces/http/storefront"

	"github.com/akarso/shopanda/internal/interfaces/http/admin"

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
	jobsApp "github.com/akarso/shopanda/internal/application/jobs"
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
	domainadmin "github.com/akarso/shopanda/internal/domain/admin"
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
	"github.com/akarso/shopanda/internal/platform/metrics"
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

	metricsRecorder metrics.Recorder

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

	productHandler                 *storefront.ProductHandler
	productAdmin                   *admin.ProductAdminHandler
	productTranslationAdmin        *admin.ProductTranslationAdminHandler
	productPriceAdmin              *admin.ProductPriceAdminHandler
	variantHandler                 *storefront.VariantHandler
	cartHandler                    *storefront.CartHandler
	orderHandler                   *storefront.OrderHandler
	orderAdmin                     *admin.OrderAdminHandler
	invoiceAdmin                   *admin.InvoiceAdminHandler
	statsAdmin                     *admin.StatsAdminHandler
	authHandler                    *storefront.AuthHandler
	adminMFAHandler                *admin.AdminMFAHandler
	paymentWebhook                 *shophttp.PaymentWebhookHandler
	stripeWebhook                  *shophttp.StripeWebhookHandler
	refundHandler                  *admin.RefundHandler
	returnAdmin                    *admin.ReturnAdminHandler
	returnAccount                  *storefront.ReturnAccountHandler
	reviewHandler                  *storefront.ReviewHandler
	reviewAccount                  *storefront.ReviewAccountHandler
	reviewAdmin                    *admin.ReviewAdminHandler
	eprReportAdmin                 *admin.EprReportHandler
	ossReportAdmin                 *admin.OssReportHandler
	paymentAdmin                   *admin.PaymentAdminHandler
	shippingRates                  *storefront.ShippingRatesHandler
	categoryHandler                *storefront.CategoryHandler
	categoryAdmin                  *admin.CategoryAdminHandler
	categoryProductAssignmentAdmin *admin.CategoryProductAssignmentAdminHandler
	searchHandler                  *storefront.SearchHandler
	mediaHandler                   *admin.MediaHandler
	configAdmin                    *admin.ConfigAdminHandler
	schemaHandler                  *admin.SchemaHandler
	pageHandler                    *storefront.PageHandler
	pageAdmin                      *admin.PageAdminHandler
	menuHandler                    *storefront.MenuHandler
	menuAdmin                      *admin.MenuAdminHandler
	contentBlockHandler            *storefront.ContentBlockHandler
	contentBlockAdmin              *admin.ContentBlockAdminHandler
	couponAdmin                    *admin.CouponAdminHandler
	promotionAdmin                 *admin.PromotionAdminHandler
	attributeAdmin                 *admin.AttributeAdminHandler
	extensionFieldAdmin            *admin.ExtensionFieldAdminHandler
	extensionValueAdmin            *admin.ExtensionValueAdminHandler
	extensionHookAdmin             *admin.ExtensionHookAdminHandler
	extensionSlotAdmin             *admin.ExtensionSlotAdminHandler
	extensionPortAdmin             *admin.ExtensionPortAdminHandler
	inventoryAdmin                 *admin.InventoryAdminHandler
	storeAdmin                     *admin.StoreAdminHandler
	auditLogAdmin                  *admin.AuditLogAdminHandler
	webhookEndpointAdmin           *admin.WebhookEndpointAdminHandler
	integrationIdempotencyAdmin    *admin.IntegrationIdempotencyAdminHandler
	shippingZoneAdmin              *admin.ShippingZoneAdminHandler
	customerAdmin                  *admin.CustomerAdminHandler
	adminUserHandler               *admin.AdminUserHandler
	setupHandler                   *shophttp.SetupHandler
	adminRoleHandler               *admin.AdminRoleHandler
	storeCreditAdmin               *admin.StoreCreditAdminHandler
	storeCreditAccount             *storefront.StoreCreditAccountHandler
	jobAdmin                       *admin.JobAdminHandler
	accountHandler                 *storefront.AccountHandler
	sitemapHandler                 *storefront.SitemapHandler
	robotsHandler                  *storefront.RobotsHandler
	docsHandler                    *shophttp.DocsHandler
	checkoutHandler                *storefront.CheckoutHandler
}

func wireServeRuntime(cfg *config.Config, log logger.Logger, conn *sql.DB, repos *serveRepos, metricsRecorder metrics.Recorder) (rt *serveRuntime, err error) {
	if metricsRecorder == nil {
		metricsRecorder = metrics.Noop()
	}
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
		Logger: log,
		Bus:    bus,
		Config: cfg,
		Bootstrap: &plugin.Bootstrap{
			DB:        conn,
			Customers: repos.customerRepo,
			Variants:  repos.variantRepo,
		},
	}
	pluginApp.SetExtensionRegistry(extensionRegistry)
	pluginApp.SetHookRegistry(hookRegistry)
	pluginApp.SetImportRegistry(importRegistry)
	pluginApp.SetExportRegistry(exportRegistry)
	pluginApp.SetPromotionEvaluatorRegistry(promotionEvaluators)
	pluginApp.SetSlotRegistry(slotRegistry)
	pluginApp.SetAssetRegistry(assetRegistry)
	pluginApp.SetIntegrationIdempotencyStore(repos.integrationIdempotencyRepo)
	orderStatusService := orderApp.NewStatusService(repos.orderRepo)
	pluginApp.SetIntegrationOrderStatusUpdater(plugin.NewIntegrationOrderStatusUpdater(orderStatusService))
	wireIntegrationStockSyncer(pluginApp, repos.variantRepo, repos.stockRepo)
	permReg := preparePermissionRegistry(pluginApp)
	summary := registry.InitAll(pluginApp)
	sealPermissionRegistry(pluginApp) // serve: freeze + BindRuntime for HTTP auth/catalog
	defer func() {
		if err != nil {
			rbac.UnbindRuntime()
		}
	}()
	if err = extensionRegistry.LoadPersisted(context.Background(), repos.extensionFieldRepo, log); err != nil {
		return nil, fmt.Errorf("load extension fields: %w", err)
	}
	extensionFieldService := extensionApp.NewFieldService(extensionRegistry, repos.extensionFieldRepo)
	extensionValueService := extensionApp.NewValueService(extensionRegistry, repos.extensionValueRepo)
	if err := plugin.LoadPersisted(context.Background(), repos.configRepo, cfg, registry.ConfigRegistry()); err != nil {
		return nil, fmt.Errorf("load plugin config: %w", err)
	}
	plugin.LogStartup(log, registry, cfg)
	pluginreport.LogSummary(log, pluginreport.Build(registry, pluginApp, cfg))
	log.Info("plugin.init.summary", map[string]interface{}{
		"registered":  summary.Registered,
		"initialized": summary.Initialized,
		"failed":      summary.Failed,
	})

	adminRoleService := adminroleApp.NewService(repos.rolePermRepo, permReg)
	if err := adminRoleService.SyncPluginDefaults(context.Background()); err != nil {
		return nil, fmt.Errorf("sync role permissions: %w", err)
	}

	// Search engine.
	searchEngine, err := resolveSearchEngine(pluginApp, conn, cfg)
	if err != nil {
		return nil, err
	}

	// Job queue, worker, mailer, cache — shared setup.
	jobWorker, jobQueue, appCache, err := setupWorker(conn, cfg, log, pluginApp, metricsRecorder)
	if err != nil {
		return nil, err
	}
	if err := integrationApp.RegisterSyncJobEventTriggers(pluginApp, bus, jobQueue, log); err != nil {
		return nil, fmt.Errorf("sync job event triggers: %w", err)
	}

	// Job admin introspection/retry/cancel (PR-1028/PR-1029) — Postgres-queue-
	// only, same constraint as resolveCache's ExpiredDeleter assertion above:
	// a broker-backed Queue driver has no queryable/updatable job table, so
	// fail startup clearly rather than silently omitting the admin routes.
	jobsReader, ok := jobQueue.(jobs.Reader)
	if !ok {
		return nil, fmt.Errorf("queue driver %q does not support job admin introspection", cfg.Queue.Driver)
	}
	jobsAdminPort, ok := jobQueue.(jobs.Admin)
	if !ok {
		return nil, fmt.Errorf("queue driver %q does not support job admin actions", cfg.Queue.Driver)
	}
	jobsService, err := jobsApp.NewService(jobsReader, jobsAdminPort)
	if err != nil {
		return nil, fmt.Errorf("jobs service: %w", err)
	}

	// Email notifications (needs jobQueue from setupWorker).
	mailTemplates := mail.NewTemplates()
	notification.RegisterTemplates(mailTemplates)

	invoicePDFRenderer := invoicepdf.NewRenderer()

	notifSvc := notification.New(mailTemplates, repos.customerRepo, repos.orderRepo, jobQueue, log,
		notification.WithStoreURL(cfg.Server.PublicBaseURL),
		notification.WithResetBaseURL(cfg.Server.PublicBaseURL+"/auth/reset-password"),
		notification.WithInvoices(repos.invoiceRepo),
		notification.WithPDFRenderer(invoicePDFRenderer),
	)

	// Media storage.
	mediaStorage, err := resolveMediaStorage(pluginApp, cfg)
	if err != nil {
		return nil, err
	}

	menuResolver := cmsApp.NewMenuResolver(repos.categoryRepo, repos.pageRepo)

	blockResolver := cmsApp.NewBlockResolver(repos.productRepo)

	contentTranslator := translation.NewContentTranslator(repos.contentTranslationRepo, log)

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
	rewriteSub := rewrite.NewSubscriber(repos.rewriteRepo, log)
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
		p, err := repos.productRepo.FindByID(ctx, data.ProductID)
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
		p, err := repos.productRepo.FindByID(ctx, data.ProductID)
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
	reviewService := reviewsApp.NewService(repos.reviewRepo, repos.productRepo)
	pdp := composition.NewPipeline[composition.ProductContext]()
	pdp.AddStep(composition.ProductMetaStep{})
	pdp.AddStep(composition.NewJSONLDProductStep(repos.variantRepo, repos.priceRepo, repos.stockRepo))
	pdp.AddStep(composition.NewCanonicalURLStep(baseURL))
	pdp.AddStep(composition.NewPriceIndicationStep(repos.variantRepo, repos.priceRepo, repos.priceHistoryRepo, repos.configRepo))
	pdp.AddStep(composition.NewWeeeStep(repos.configRepo))
	pdp.AddStep(composition.NewGpsrStep(repos.configRepo))
	pdp.AddStep(composition.NewReviewsStep(reviewService))
	plp := composition.NewPipeline[composition.ListingContext]()
	plp.AddStep(composition.ListingMetaStep{})
	plp.AddStep(composition.NewListingCanonicalURLStep(baseURL))
	plp.AddStep(composition.NewListingPriceIndicationStep(repos.variantRepo, repos.priceRepo, repos.priceHistoryRepo, repos.configRepo))
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
	taxCalculator, err := resolveTaxCalculator(pluginApp, repos.taxRateRepo)
	if err != nil {
		return nil, fmt.Errorf("tax calculator: %w", err)
	}
	corePricingSteps := []pricing.PricingStep{
		appPricing.NewBasePriceStep(repos.priceRepo),
		appPricing.NewCatalogPromotionStep(repos.promotionRepo, repos.couponRepo, promotionEvaluators),
		appPricing.NewCartPromotionStep(repos.promotionRepo, repos.couponRepo, promotionEvaluators),
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
	cartService := cartApp.NewService(repos.cartRepo, repos.priceRepo, repos.promotionRepo, repos.couponRepo, pricingPipeline, log, bus, extensionValueService, hookRegistry)
	storeCreditService := storecreditApp.NewService(repos.storeCreditRepo, repos.customerRepo).
		WithMaxIssueAmount(cfg.StoreCredit.MaxIssueAmount)

	// Checkout workflow.
	validateCartStep := checkoutApp.NewValidateCartStep(repos.variantRepo)
	recalculatePricingStep := checkoutApp.NewRecalculatePricingStep(pricingPipeline)
	reserveInventoryStep := checkoutApp.NewReserveInventoryStep(repos.reservationRepo)
	createOrderStep := checkoutApp.NewCreateOrderStep(repos.orderRepo, repos.variantRepo, storeCreditService, extensionValueService)
	selectShippingStep := checkoutApp.NewSelectShippingStep(shippingReg, repos.shippingRepo)
	initiatePaymentStep := checkoutApp.NewInitiatePaymentStep(payRegistry, repos.paymentRepo)
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
	checkoutWorkflow := checkoutApp.NewWorkflow(checkoutSteps, bus, log).WithMetrics(metricsRecorder)
	checkoutService := checkoutApp.NewService(repos.cartRepo, checkoutWorkflow, log)
	checkoutHandler := storefront.NewCheckoutHandler(checkoutService, extensionValueService)

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
	tokenParser := authApp.NewValidatingTokenParser(jwtIssuer, repos.customerRepo, 30*time.Second)

	authService := authApp.NewService(repos.customerRepo, repos.resetTokenRepo, jwtIssuer, bus, log, time.Hour)
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
	mfaService := mfaApp.NewService(repos.mfaRepo, repos.customerRepo, repos.configRepo, jwtSecretStr, cfg.Auth.MFAEnabled)
	if cfg.Auth.MFAEnabled {
		authService.SetMFAClient(mfaService)
	}

	// Admin schema registry.
	adminRegistry := domainadmin.NewRegistry()
	adminApp.RegisterProductSchemas(adminRegistry)
	adminApp.RegisterPageSchemas(adminRegistry)

	attributeStore := adminApp.NewAttributeStore(repos.configRepo)

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
	sharedAuditor.SetAuditLogRepository(repos.auditLogRepo)

	// Handlers.
	productHandler := storefront.NewProductHandler(repos.productRepo, pdp, plp, contentTranslator)
	productAdmin := admin.NewProductAdminHandlerWithAuditor(repos.productRepo, bus, sharedAuditor, log)
	productTranslationAdmin := admin.NewProductTranslationAdminHandler(repos.productRepo, repos.contentTranslationRepo, sharedAuditor, log)
	productPriceAdmin := admin.NewProductPriceAdminHandler(repos.productRepo, repos.variantRepo, repos.priceRepo, sharedAuditor, log)
	variantHandler := storefront.NewVariantHandler(repos.productRepo, repos.variantRepo, bus)
	cartHandler := storefront.NewCartHandler(cartService, extensionValueService)
	orderHandler := storefront.NewOrderHandler(repos.orderRepo, extensionValueService)
	orderAdmin := admin.NewOrderAdminHandlerWithAuditor(repos.orderRepo, sharedAuditor, extensionValueService)
	invoiceAdmin := admin.NewInvoiceAdminHandler(repos.invoiceRepo, repos.orderRepo, invoicePDFRenderer, mediaStorage)
	statsAdmin := admin.NewStatsAdminHandler(repos.statsRepo)
	authHandler := storefront.NewAuthHandler(authService, cfg.RateLimit.TrustedProxies...)
	adminMFAHandler := admin.NewAdminMFAHandler(mfaService)
	webhookVerifier := webhook.NewHMACVerifier(cfg.Webhooks.Secrets)
	paymentWebhook := shophttp.NewPaymentWebhookHandler(repos.paymentRepo, bus, webhookVerifier)

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
		stripeWebhook = shophttp.NewStripeWebhookHandler(repos.paymentRepo, bus, webhookSecret)
		log.Info("payment.stripe.webhook_handler_enabled", nil)
	} else if cfg.Payment.Stripe.Enabled {
		log.Warn("payment.stripe.no_webhook_secret", map[string]interface{}{
			"message": "Stripe enabled but SHOPANDA_PAYMENT_STRIPE_WEBHOOK_SECRET not set; Stripe webhooks will not be handled",
		})
	}

	// Refund handler: only available when the payment provider supports refunds.
	var refundHandler *admin.RefundHandler
	var stripeRefunder payment.Refunder
	if refunder, ok := payRegistry.Refunder(payment.MethodStripe); ok {
		stripeRefunder = refunder
		refundHandler = admin.NewRefundHandler(repos.paymentRepo, refunder, bus)
		log.Info("payment.refund_handler_enabled", nil)
	}

	returnService := returnsApp.NewService(repos.returnRepo, repos.orderRepo, repos.stockRepo, repos.paymentRepo, stripeRefunder, bus, log)
	returnAdmin := admin.NewReturnAdminHandler(returnService, sharedAuditor)
	returnAccount := storefront.NewReturnAccountHandler(returnService)
	reviewHandler := storefront.NewReviewHandler(reviewService)
	reviewAccount := storefront.NewReviewAccountHandler(reviewService)
	reviewAdmin := admin.NewReviewAdminHandler(reviewService, sharedAuditor)
	eprExporter := exporter.NewEprExporter(repos.productRepo, repos.variantRepo, repos.configRepo)
	eprReportAdmin := admin.NewEprReportHandler(eprExporter)
	ossExporter := exporter.NewOssExporter(repos.orderRepo, repos.configRepo)
	ossReportAdmin := admin.NewOssReportHandler(ossExporter)
	paymentAdmin := admin.NewPaymentAdminHandler(repos.paymentRepo, sharedAuditor)

	shippingRates := storefront.NewShippingRatesHandler(shippingReg.Providers()...)
	categoryHandler := storefront.NewCategoryHandler(repos.categoryRepo, repos.productRepo)
	categoryAdmin := admin.NewCategoryAdminHandlerWithAuditor(repos.categoryRepo, bus, sharedAuditor)
	categoryProductAssignmentAdmin := admin.NewCategoryProductAssignmentAdminHandlerWithAuditor(repos.categoryRepo, repos.productRepo, repos.productRepo, sharedAuditor)
	searchHandler := storefront.NewSearchHandler(searchEngine).WithAdvancedSearchAttributes(attributeStore)
	mediaService := mediaApp.NewService(mediaStorage, repos.assetRepo, bus, log)
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
	mediaHandler := admin.NewMediaHandlerWithAuditor(mediaService, sharedAuditor)
	mediaHandler.SetMaxUploadBytes(cfg.HTTP.MediaMaxBodyBytes)
	configAdmin := admin.NewConfigAdminHandler(repos.configRepo, cfg, func(ctx context.Context, smtpCfg admin.SMTPTestConfig, to string) error {
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
	schemaHandler := admin.NewSchemaHandler(adminRegistry, attributeStore)
	pageHandler := storefront.NewPageHandler(repos.pageRepo, contentTranslator)
	pageAdmin := admin.NewPageAdminHandlerWithAuditor(repos.pageRepo, bus, sharedAuditor)
	menuHandler := storefront.NewMenuHandler(repos.menuRepo, menuResolver)
	menuAdmin := admin.NewMenuAdminHandler(repos.menuRepo, sharedAuditor)
	contentBlockHandler := storefront.NewContentBlockHandler(repos.contentBlockRepo, repos.pageRepo, blockResolver)
	contentBlockAdmin := admin.NewContentBlockAdminHandler(repos.contentBlockRepo, sharedAuditor)
	couponAdmin := admin.NewCouponAdminHandlerWithAuditor(repos.couponRepo, repos.promotionRepo, sharedAuditor)
	promotionAdmin := admin.NewPromotionAdminHandlerWithAuditor(repos.promotionRepo, sharedAuditor)
	attributeAdmin := admin.NewAttributeAdminHandlerWithAuditor(attributeStore, sharedAuditor).
		WithDiscoveryFacetSync(discoveryFacetSync)
	extensionFieldAdmin := admin.NewExtensionFieldAdminHandlerWithAuditor(extensionFieldService, sharedAuditor)
	extensionValueAdmin := admin.NewExtensionValueAdminHandlerWithAuditor(extensionValueService, sharedAuditor)
	extensionHookAdmin := admin.NewExtensionHookAdminHandler(hookRegistry)
	extensionSlotAdmin := admin.NewExtensionSlotAdminHandler(slotRegistry)
	portSnapshot := portsapp.BuildSnapshot(pluginApp, cfg)
	extensionPortAdmin := admin.NewExtensionPortAdminHandler(portSnapshot)
	inventoryAdmin := admin.NewInventoryAdminHandlerWithAuditor(repos.stockRepo, repos.variantRepo, sharedAuditor)
	storeAdmin := admin.NewStoreAdminHandlerWithAuditor(repos.storeRepo, bus, sharedAuditor)
	auditLogAdmin := admin.NewAuditLogAdminHandler(repos.auditLogRepo, sharedAuditor)
	webhookService := webhookApp.NewService(repos.webhookEndpointRepo)
	webhookEndpointAdmin := admin.NewWebhookEndpointAdminHandler(webhookService)
	integrationIdempotencyAdmin := admin.NewIntegrationIdempotencyAdminHandler(repos.integrationIdempotencyRepo)
	webhookApp.NewDispatcher(repos.webhookEndpointRepo, jobQueue, log).Register(bus)
	shippingZoneAdmin := admin.NewShippingZoneAdminHandler(repos.zoneRepo)
	accountService := accountApp.NewService(repos.customerRepo, repos.consentRepo, bus, log, conn)
	customerAdmin := admin.NewCustomerAdminHandlerWithAuditorAndDeleter(repos.customerRepo, accountService, sharedAuditor)
	adminUserService := adminuserApp.NewService(repos.customerRepo)
	adminUserHandler := admin.NewAdminUserHandler(adminUserService, sharedAuditor)
	setupService := setupApp.NewService(
		conn,
		"migrations",
		repos.customerRepo,
		repos.storeRepo,
		setupAdminUserCreator{svc: adminUserService},
		func(ctx context.Context, deps seed.Deps) (*seed.Result, error) {
			reg := seed.NewRegistry()
			registerDefaultSeeders(reg)
			return reg.Run(ctx, deps)
		},
		log,
	)
	setupHandler := shophttp.NewSetupHandler(setupService, log)
	adminRoleHandler := admin.NewAdminRoleHandler(adminRoleService, sharedAuditor)
	storeCreditAdmin := admin.NewStoreCreditAdminHandler(storeCreditService, sharedAuditor)
	storeCreditAccount := storefront.NewStoreCreditAccountHandler(storeCreditService)
	jobAdmin := admin.NewJobAdminHandler(jobsService, sharedAuditor)
	accountHandler := storefront.NewAccountHandler(repos.customerRepo, repos.orderRepo, repos.consentRepo, accountService)
	sitemapHandler := storefront.NewSitemapHandler(baseURL, repos.productRepo, repos.categoryRepo, repos.pageRepo)
	robotsHandler := storefront.NewRobotsHandler(baseURL)

	specPath := filepath.Join(filepath.Dir(config.FindConfigFile()), "openapi.yaml")
	specBytes, err := os.ReadFile(specPath)
	if err != nil {
		var fallbackErr error
		specBytes, fallbackErr = os.ReadFile("openapi.yaml")
		if fallbackErr != nil {
			log.Warn("openapi.spec.load_failed", map[string]interface{}{
				"configured_path":  specPath,
				"configured_error": err.Error(),
				"fallback_path":    "openapi.yaml",
				"fallback_error":   fallbackErr.Error(),
			})
		}
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
		metricsRecorder:                metricsRecorder,
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
		jobAdmin:                       jobAdmin,
		accountHandler:                 accountHandler,
		sitemapHandler:                 sitemapHandler,
		robotsHandler:                  robotsHandler,
		docsHandler:                    docsHandler,
		checkoutHandler:                checkoutHandler,
	}, nil
}
