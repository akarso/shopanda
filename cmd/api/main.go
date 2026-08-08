package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
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
	"github.com/akarso/shopanda/internal/application/importer"
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
	"github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/domain/shipping"
	domtheme "github.com/akarso/shopanda/internal/domain/theme"
	"github.com/akarso/shopanda/internal/domain/translation"
	"github.com/akarso/shopanda/internal/infrastructure/cron"

	"github.com/akarso/shopanda/internal/infrastructure/imaging"
	"github.com/akarso/shopanda/internal/infrastructure/invoicepdf"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	smtpmail "github.com/akarso/shopanda/internal/infrastructure/smtp"
	"github.com/akarso/shopanda/internal/infrastructure/webhook"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/db"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/jwt"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/migrate"

	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/internal/platform/runtime"
	"github.com/akarso/shopanda/internal/seed"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"

	"gopkg.in/yaml.v3"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	result, err := config.Load(config.FindConfigFile())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg := result.Config

	log := logger.New(cfg.Log.Level)

	if result.DotEnvUsed {
		log.Warn("app.config.dotenv", map[string]interface{}{
			"path": result.DotEnvPath,
			"message": ".env file loaded — this is a development convenience; " +
				"in production, prefer configs/config.yaml or export variables in your shell " +
				"before starting the binary",
		})
	}

	log.Info("app.config.loaded", map[string]interface{}{
		"config": cfg.String(),
	})

	// Subcommand dispatch.
	if len(os.Args) > 1 {
		var pluginCLIRegistry *plugin.Registry
		pluginCLIRegistryFn := func() *plugin.Registry {
			if pluginCLIRegistry == nil {
				pluginCLIRegistry = bootstrapPluginCLIRegistry(cfg, log)
			}
			return pluginCLIRegistry
		}
		switch os.Args[1] {
		case "help":
			printHelp(cfg, log, pluginCLIRegistryFn)
			return nil
		case "setup":
			return runSetup(cfg, log)
		case "migrate":
			return runMigrate(cfg, log)
		case "serve":
			return runServe(cfg, log, false)
		case "dev":
			return runServe(cfg, log, cfg.Dev.EmbedScheduler)
		case "worker":
			return runWorker(cfg, log)
		case "scheduler":
			return runScheduler(cfg, log)
		case "seed":
			return runSeed(cfg, log)
		case "search:reindex":
			return runSearchReindex(cfg, log)
		case "config:export":
			return runConfigExport(cfg, log)
		case "config:import":
			return runConfigImport(cfg, log)
		case "import:products":
			return runImportProducts(cfg, log)
		case "export:products":
			return runExportProducts(cfg, log)
		case "import:stock":
			return runImportStock(cfg, log)
		case "export:stock":
			return runExportStock(cfg, log)
		case "import:customers":
			return runImportCustomers(cfg, log)
		case "export:customers":
			return runExportCustomers(cfg, log)
		case "import:attributes":
			return runImportAttributes(cfg, log)
		case "export:attributes":
			return runExportAttributes(cfg, log)
		case "import:categories":
			return runImportCategories(cfg, log)
		case "export:categories":
			return runExportCategories(cfg, log)
		case "import:prices":
			return runImportPrices(cfg, log)
		case "export:prices":
			return runExportPrices(cfg, log)
		case "export:epr":
			return runExportEpr(cfg, log)
		case "export:oss":
			return runExportOss(cfg, log)
		case "plugins":
			if len(os.Args) < 3 {
				return fmt.Errorf("usage: app plugins report [--json]")
			}
			switch os.Args[2] {
			case "report":
				return runPluginsReport(os.Stdout, cfg, log, os.Args[3:])
			default:
				return fmt.Errorf("unknown plugins command: %s (try: plugins report)", os.Args[2])
			}
		default:
			if ran, err := runPluginCLICommand(cfg, log, pluginCLIRegistryFn, os.Args[1], os.Args[2:]); err != nil {
				return err
			} else if ran {
				return nil
			}
			return fmt.Errorf("unknown command: %s (run 'help' for usage)", os.Args[1])
		}
	}

	// Default: start HTTP server (production-style, no embedded scheduler).
	return runServe(cfg, log, false)
}

func runServe(cfg *config.Config, log logger.Logger, embedScheduler bool) error {
	// Database.
	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	// Repositories.
	productRepo, err := postgres.NewProductRepo(conn)
	if err != nil {
		return err
	}
	variantRepo, err := postgres.NewVariantRepo(conn)
	if err != nil {
		return err
	}
	cartRepo, err := postgres.NewCartRepo(conn)
	if err != nil {
		return err
	}
	priceRepo, err := postgres.NewPriceRepo(conn)
	if err != nil {
		return err
	}
	priceHistoryRepo, err := postgres.NewPriceHistoryRepo(conn)
	if err != nil {
		return err
	}
	customerRepo, err := postgres.NewCustomerRepo(conn)
	if err != nil {
		return err
	}
	resetTokenRepo, err := postgres.NewResetTokenRepo(conn)
	if err != nil {
		return err
	}
	reservationRepo, err := postgres.NewReservationRepo(conn)
	if err != nil {
		return err
	}
	stockRepo, err := postgres.NewStockRepo(conn)
	if err != nil {
		return err
	}
	orderRepo, err := postgres.NewOrderRepo(conn)
	if err != nil {
		return err
	}
	storeCreditRepo, err := postgres.NewStoreCreditRepo(conn)
	if err != nil {
		return err
	}
	returnRepo, err := postgres.NewReturnRepo(conn)
	if err != nil {
		return err
	}
	reviewRepo, err := postgres.NewReviewRepo(conn)
	if err != nil {
		return err
	}
	statsRepo, err := postgres.NewStatsRepo(conn)
	if err != nil {
		return err
	}
	paymentRepo, err := postgres.NewPaymentRepo(conn)
	if err != nil {
		return err
	}
	shippingRepo, err := postgres.NewShippingRepo(conn)
	if err != nil {
		return err
	}
	configRepo := postgres.NewConfigRepo(conn)
	zoneRepo, err := postgres.NewZoneRepo(conn)
	if err != nil {
		return err
	}
	categoryRepo, err := postgres.NewCategoryRepo(conn)
	if err != nil {
		return err
	}
	collectionRepo, err := postgres.NewCollectionRepo(conn)
	if err != nil {
		return err
	}
	_ = collectionRepo // wired in collection HTTP handlers PR

	taxRateRepo, err := postgres.NewTaxRateRepo(conn)
	if err != nil {
		return err
	}

	promotionRepo, err := postgres.NewPromotionRepo(conn)
	if err != nil {
		return err
	}

	couponRepo, err := postgres.NewCouponRepo(conn)
	if err != nil {
		return err
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
				return fmt.Errorf("theme slot markers: %w", valErr)
			}
			slotRegistry.SetThemeMarkers(anchors)
		}
	}
	if os.Getenv("SHOPANDA_DEV_MODE") != "" {
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
	integrationIdempotencyRepo, err := postgres.NewIntegrationIdempotencyRepo(conn)
	if err != nil {
		return err
	}
	pluginApp.SetIntegrationIdempotencyStore(integrationIdempotencyRepo)
	orderStatusService := orderApp.NewStatusService(orderRepo)
	pluginApp.SetIntegrationOrderStatusUpdater(plugin.NewIntegrationOrderStatusUpdater(orderStatusService))
	wireIntegrationStockSyncer(pluginApp, variantRepo, stockRepo)
	summary := registry.InitAll(pluginApp)
	extensionFieldRepo, err := postgres.NewExtensionFieldRepo(conn)
	if err != nil {
		return err
	}
	if err := extensionRegistry.LoadPersisted(context.Background(), extensionFieldRepo, log); err != nil {
		return fmt.Errorf("load extension fields: %w", err)
	}
	extensionFieldService := extensionApp.NewFieldService(extensionRegistry, extensionFieldRepo)
	extensionValueRepo, err := postgres.NewExtensionValueRepo(conn)
	if err != nil {
		return err
	}
	extensionValueService := extensionApp.NewValueService(extensionRegistry, extensionValueRepo)
	if err := plugin.LoadPersisted(context.Background(), configRepo, cfg, registry.ConfigRegistry()); err != nil {
		return fmt.Errorf("load plugin config: %w", err)
	}
	plugin.LogStartup(log, registry, cfg)
	pluginreport.LogSummary(log, pluginreport.Build(registry, pluginApp, cfg))
	log.Info("plugin.init.summary", map[string]interface{}{
		"registered":  summary.Registered,
		"initialized": summary.Initialized,
		"failed":      summary.Failed,
	})

	rolePermRepo, err := postgres.NewRolePermissionRepo(conn)
	if err != nil {
		return err
	}
	adminRoleService := adminroleApp.NewService(rolePermRepo)
	if err := adminRoleService.SyncPluginDefaults(context.Background()); err != nil {
		return fmt.Errorf("sync role permissions: %w", err)
	}

	// Search engine.
	searchEngine, err := resolveSearchEngine(pluginApp, conn, cfg)
	if err != nil {
		return err
	}

	// Job queue, worker, mailer, cache — shared setup.
	jobWorker, jobQueue, appCache, err := setupWorker(conn, cfg, log, pluginApp)
	if err != nil {
		return err
	}
	if err := integrationApp.RegisterSyncJobEventTriggers(pluginApp, bus, jobQueue, log); err != nil {
		return fmt.Errorf("sync job event triggers: %w", err)
	}

	// Email notifications (needs jobQueue from setupWorker).
	mailTemplates := mail.NewTemplates()
	notification.RegisterTemplates(mailTemplates)

	invoiceRepo, err := postgres.NewInvoiceRepo(conn)
	if err != nil {
		return err
	}

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
		return err
	}

	// Asset repository.
	assetRepo, err := postgres.NewAssetRepo(conn)
	if err != nil {
		return err
	}

	// Rewrite repository.
	rewriteRepo, err := postgres.NewRewriteRepo(conn)
	if err != nil {
		return err
	}

	// Page repository.
	pageRepo, err := postgres.NewPageRepo(conn)
	if err != nil {
		return err
	}

	// Menu repository.
	menuRepo, err := postgres.NewMenuRepo(conn)
	if err != nil {
		return err
	}
	menuResolver := cmsApp.NewMenuResolver(categoryRepo, pageRepo)

	contentBlockRepo, err := postgres.NewContentBlockRepo(conn)
	if err != nil {
		return err
	}
	blockResolver := cmsApp.NewBlockResolver(productRepo)

	// Store repository.
	storeRepo, err := postgres.NewStoreRepo(conn)
	if err != nil {
		return err
	}

	// Translation repository.
	translationRepo, err := postgres.NewTranslationRepo(conn)
	if err != nil {
		return err
	}
	_ = translationRepo // wired in translation admin PR

	// Content translation repository + translator.
	contentTranslationRepo, err := postgres.NewContentTranslationRepo(conn)
	if err != nil {
		return err
	}
	contentTranslator := translation.NewContentTranslator(contentTranslationRepo, log)

	// Consent repository.
	consentRepo, err := postgres.NewConsentRepo(conn)
	if err != nil {
		return err
	}

	// Saved customer addresses repository.
	customerAddressRepo, err := postgres.NewCustomerAddressRepo(conn)
	if err != nil {
		return err
	}

	// Providers.
	shippingReg, err := resolveShippingRegistry(pluginApp)
	if err != nil {
		return err
	}

	payRegistry, err := resolvePaymentRegistry(pluginApp)
	if err != nil {
		return err
	}

	// Dev handler: log password reset tokens alongside email delivery.
	if os.Getenv("SHOPANDA_DEV_MODE") != "" {
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
		return fmt.Errorf("tax calculator: %w", err)
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
		return fmt.Errorf("pricing pipeline: %w", err)
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
		return fmt.Errorf("checkout workflow: %w", err)
	}
	checkoutWorkflow := checkoutApp.NewWorkflow(checkoutSteps, bus, log)
	checkoutService := checkoutApp.NewService(cartRepo, checkoutWorkflow, log)
	checkoutHandler := shophttp.NewCheckoutHandler(checkoutService, extensionValueService)

	// JWT.
	jwtTTL, err := time.ParseDuration(cfg.Auth.JWTTTL)
	if err != nil {
		return fmt.Errorf("invalid auth.jwt_ttl: %w", err)
	}
	jwtIssuer, err := jwt.NewIssuer(cfg.Auth.JWTSecret, jwtTTL)
	if err != nil {
		return fmt.Errorf("jwt issuer: %w", err)
	}
	tokenParser := authApp.NewValidatingTokenParser(jwtIssuer, customerRepo, 30*time.Second)

	authService := authApp.NewService(customerRepo, resetTokenRepo, jwtIssuer, bus, log, time.Hour)
	if cfg.Auth.Lockout.Enabled {
		lockoutWindow, err := time.ParseDuration(cfg.Auth.Lockout.Window)
		if err != nil {
			return fmt.Errorf("invalid auth.lockout.window: %w", err)
		}
		attemptStore, err := authApp.NewAttemptStore(cfg.Auth.Lockout.Store, appCache, log)
		if err != nil {
			return fmt.Errorf("auth lockout store: %w", err)
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
	mfaRepo, err := postgres.NewMFARepo(conn)
	if err != nil {
		return err
	}
	mfaService := mfaApp.NewService(mfaRepo, customerRepo, configRepo, cfg.Auth.JWTSecret, cfg.Auth.MFAEnabled)
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
		return fmt.Errorf("configure search attribute facets: %w", err)
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
			return fmt.Errorf("admin schema permission wiring failed for %s %q: %w", sp.kind, sp.name, sp.err)
		}
	}

	// Shared admin auditor with optional persistent audit log.
	auditLogRepo, err := postgres.NewAuditLogRepo(conn)
	if err != nil {
		return err
	}
	webhookEndpointRepo, err := postgres.NewWebhookEndpointRepo(conn)
	if err != nil {
		return err
	}
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

	router := shophttp.NewRouter()

	// Middleware: outermost first.
	router.Use(shophttp.RecoveryMiddleware(log))
	router.Use(shophttp.RequestIDMiddleware())
	router.Use(shophttp.RateLimitMiddleware(cfg.RateLimit, log))
	router.Use(shophttp.LoggingMiddleware(log))
	router.Use(shophttp.AuthMiddleware(tokenParser))
	router.Use(shophttp.AdminContextMiddleware())
	router.Use(shophttp.CSRFMiddleware(cfg.RateLimit.TrustedProxies...))
	router.Use(shophttp.StoreMiddleware(storeRepo, log))
	router.Use(shophttp.LanguageMiddleware())
	router.Use(shophttp.CacheControlMiddleware([]string{
		"/healthz",
		"/setup",
		"/api/v1/setup",
		"/api/v1/carts",
		"/api/v1/checkout",
		"/api/v1/orders",
		"/api/v1/account",
		"/api/v1/auth",
		"/api/v1/shipping",
		"/api/v1/admin",
	}))

	// Routes.
	router.HandleFunc("GET /healthz", shophttp.HealthHandler())
	router.HandleFunc("GET /setup", setupHandler.Page())
	router.HandleFunc("GET /api/v1/setup/status", setupHandler.Status())
	router.HandleFunc("POST /api/v1/setup/install", setupHandler.Install())
	router.HandleFunc("GET /sitemap.xml", sitemapHandler.Serve())
	router.HandleFunc("GET /robots.txt", robotsHandler.Serve())
	router.HandleFunc("GET /docs", docsHandler.UI())
	router.HandleFunc("GET /docs/openapi.yaml", docsHandler.Spec())

	requireAuth := shophttp.RequireAuth()

	// Permission-based middleware for admin routes.
	requireProductsRead := shophttp.RequirePermission(rbac.ProductsRead)
	requireProductsWrite := shophttp.RequirePermission(rbac.ProductsWrite)
	requireCategoriesRead := shophttp.RequirePermission(rbac.CategoriesRead)
	requireCategoriesWrite := shophttp.RequirePermission(rbac.CategoriesWrite)
	requireCustomersRead := shophttp.RequirePermission(rbac.CustomersRead)
	requireCustomersWrite := shophttp.RequirePermission(rbac.CustomersWrite)
	requireOrdersRead := shophttp.RequirePermission(rbac.OrdersRead)
	requireOrdersWrite := shophttp.RequirePermission(rbac.OrdersWrite)
	requireInvoicesRead := shophttp.RequirePermission(rbac.InvoicesRead)
	requireMediaRead := shophttp.RequirePermission(rbac.MediaRead)
	requireMediaWrite := shophttp.RequirePermission(rbac.MediaWrite)
	requireSettingsRead := shophttp.RequirePermission(rbac.SettingsRead)
	requireSettingsWrite := shophttp.RequirePermission(rbac.SettingsWrite)
	requireContentRead := shophttp.RequirePermission(rbac.ContentRead)
	requireContentWrite := shophttp.RequirePermission(rbac.ContentWrite)
	requireShippingRead := shophttp.RequirePermission(rbac.ShippingRead)
	requireShippingWrite := shophttp.RequirePermission(rbac.ShippingWrite)
	requireAuditRead := shophttp.RequirePermission(rbac.AuditRead)
	requireExtensionsRead := shophttp.RequirePermission(rbac.ExtensionsRead)
	requireExtensionsWrite := shophttp.RequirePermission(rbac.ExtensionsWrite)

	// Auth routes.
	router.HandleFunc("POST /api/v1/auth/register", authHandler.Register())
	router.HandleFunc("POST /api/v1/auth/login", authHandler.Login())
	if cfg.Auth.MFAEnabled {
		router.HandleFunc("POST /api/v1/auth/login/mfa", authHandler.LoginMFA())
	}
	router.Handle("POST /api/v1/auth/logout", requireAuth(authHandler.Logout()))
	router.Handle("GET /api/v1/auth/me", requireAuth(authHandler.Me()))
	router.Handle("PUT /api/v1/auth/me/profile", requireAuth(authHandler.UpdateProfile()))
	router.Handle("POST /api/v1/auth/me/password", requireAuth(authHandler.ChangePassword()))
	router.HandleFunc("POST /api/v1/auth/password-reset/request", authHandler.RequestPasswordReset())
	router.HandleFunc("POST /api/v1/auth/password-reset/confirm", authHandler.ConfirmPasswordReset())

	router.HandleFunc("GET /api/v1/products", productHandler.List())
	router.HandleFunc("GET /api/v1/products/{id}", productHandler.Get())
	router.HandleFunc("GET /api/v1/products/{id}/reviews", reviewHandler.List())
	router.Handle("POST /api/v1/products/{id}/reviews", requireAuth(reviewAccount.Submit()))
	router.HandleFunc("GET /api/v1/products/{id}/variants", variantHandler.List())
	router.HandleFunc("GET /api/v1/products/{id}/variants/{variantId}", variantHandler.Get())

	// Category routes (public).
	router.HandleFunc("GET /api/v1/categories", categoryHandler.Tree())
	router.HandleFunc("GET /api/v1/categories/{id}", categoryHandler.Get())
	router.HandleFunc("GET /api/v1/categories/{id}/products", categoryHandler.Products())

	// Search routes (public).
	router.HandleFunc("GET /api/v1/search", searchHandler.Search())
	router.HandleFunc("GET /api/v1/search/suggest", searchHandler.Suggest())

	// Page routes (public).
	router.HandleFunc("GET /api/v1/pages/{slug}", pageHandler.Get())

	// Menu routes (public).
	router.HandleFunc("GET /api/v1/menus/{code}", menuHandler.GetByCode())

	// Content block routes (public).
	router.HandleFunc("GET /api/v1/content-blocks/{targetType}/{targetKey}", contentBlockHandler.GetByTarget())

	// Plugin public routes (registered during plugin Init). Use TryHandle so a
	// pattern that conflicts with a core route fails startup instead of panicking.
	for _, route := range pluginApp.PublicRoutes() {
		if err := router.TryHandle(route.Pattern, route.Handler); err != nil {
			return fmt.Errorf("register plugin public route: %w", err)
		}
	}

	// Admin routes (behind RequirePermission).
	router.Handle("GET /api/v1/admin/products", requireProductsRead(productAdmin.List()))
	router.Handle("GET /api/v1/admin/products/{id}", requireProductsRead(productAdmin.Get()))
	router.Handle("POST /api/v1/admin/products", requireProductsWrite(productAdmin.Create()))
	router.Handle("PUT /api/v1/admin/products/{id}", requireProductsWrite(productAdmin.Update()))
	router.Handle("GET /api/v1/admin/categories", requireCategoriesRead(categoryHandler.Tree()))
	router.Handle("GET /api/v1/admin/categories/{id}", requireCategoriesRead(categoryHandler.Get()))
	router.Handle("GET /api/v1/admin/categories/{id}/products", requireCategoriesRead(categoryHandler.Products()))
	router.Handle("POST /api/v1/admin/categories", requireCategoriesWrite(categoryAdmin.Create()))
	router.Handle("PUT /api/v1/admin/categories/{id}", requireCategoriesWrite(categoryAdmin.Update()))
	router.Handle("DELETE /api/v1/admin/categories/{id}", requireCategoriesWrite(categoryAdmin.Delete()))
	router.Handle("POST /api/v1/admin/categories/{id}/products/{productId}", requireCategoriesWrite(categoryProductAssignmentAdmin.Assign()))
	router.Handle("DELETE /api/v1/admin/categories/{id}/products/{productId}", requireCategoriesWrite(categoryProductAssignmentAdmin.Unassign()))
	router.Handle("GET /api/v1/admin/products/{id}/translations", requireProductsRead(productTranslationAdmin.Get()))
	router.Handle("PUT /api/v1/admin/products/{id}/translations", requireProductsWrite(productTranslationAdmin.Update()))
	router.Handle("POST /api/v1/admin/products/{id}/variants", requireProductsWrite(variantHandler.Create()))
	router.Handle("PUT /api/v1/admin/products/{id}/variants/{variantId}", requireProductsWrite(variantHandler.Update()))
	router.Handle("GET /api/v1/admin/products/{id}/variants/{variantId}/price", requireProductsRead(productPriceAdmin.Get()))
	router.Handle("PUT /api/v1/admin/products/{id}/variants/{variantId}/price", requireProductsWrite(productPriceAdmin.Update()))
	router.Handle("GET /api/v1/admin/stats/overview", requireOrdersRead(statsAdmin.Overview()))
	router.Handle("GET /api/v1/admin/customers", requireCustomersRead(customerAdmin.List()))
	router.Handle("GET /api/v1/admin/customers/{customerId}", requireCustomersRead(customerAdmin.Get()))
	router.Handle("DELETE /api/v1/admin/customers/{customerId}", requireCustomersWrite(customerAdmin.Delete()))
	router.Handle("POST /api/v1/admin/customers/{customerId}/revoke-sessions", requireCustomersWrite(customerAdmin.RevokeSessions()))
	router.Handle("GET /api/v1/admin/customers/{customerId}/store-credit", requireCustomersRead(storeCreditAdmin.Get()))
	router.Handle("POST /api/v1/admin/customers/{customerId}/store-credit/issue", requireCustomersWrite(storeCreditAdmin.Issue()))
	router.Handle("GET /api/v1/admin/orders", requireOrdersRead(orderAdmin.List()))
	router.Handle("GET /api/v1/admin/orders/{orderId}", requireOrdersRead(orderAdmin.Get()))
	router.Handle("PUT /api/v1/admin/orders/{orderId}", requireOrdersWrite(orderAdmin.Update()))
	router.Handle("GET /api/v1/admin/orders/{orderId}/invoices", requireInvoicesRead(invoiceAdmin.ListByOrder()))
	router.Handle("GET /api/v1/admin/invoices/{invoiceId}/pdf", requireInvoicesRead(invoiceAdmin.DownloadPDF()))
	if refundHandler != nil {
		router.Handle("POST /api/v1/admin/orders/{orderId}/refund", requireOrdersWrite(refundHandler.Refund()))
	}
	router.Handle("GET /api/v1/admin/returns", requireOrdersRead(returnAdmin.List()))
	router.Handle("GET /api/v1/admin/returns/{returnId}", requireOrdersRead(returnAdmin.Get()))
	router.Handle("POST /api/v1/admin/returns/{returnId}/approve", requireOrdersWrite(returnAdmin.Approve()))
	router.Handle("POST /api/v1/admin/returns/{returnId}/reject", requireOrdersWrite(returnAdmin.Reject()))
	router.Handle("POST /api/v1/admin/returns/{returnId}/receive", requireOrdersWrite(returnAdmin.Receive()))
	router.Handle("POST /api/v1/admin/returns/{returnId}/refund", requireOrdersWrite(returnAdmin.Refund()))
	router.Handle("GET /api/v1/admin/reviews", requireProductsRead(reviewAdmin.List()))
	router.Handle("GET /api/v1/admin/reviews/{reviewId}", requireProductsRead(reviewAdmin.Get()))
	router.Handle("POST /api/v1/admin/reviews/{reviewId}/approve", requireProductsWrite(reviewAdmin.Approve()))
	router.Handle("POST /api/v1/admin/reviews/{reviewId}/reject", requireProductsWrite(reviewAdmin.Reject()))
	router.Handle("GET /api/v1/admin/reports/epr", requireProductsRead(eprReportAdmin.Export()))
	router.Handle("GET /api/v1/admin/reports/oss", requireOrdersRead(ossReportAdmin.Export()))
	router.Handle("GET /api/v1/admin/payments", requireOrdersRead(paymentAdmin.List()))
	router.Handle("GET /api/v1/admin/payments/{paymentId}", requireOrdersRead(paymentAdmin.Get()))
	router.Handle("GET /api/v1/admin/media", requireMediaRead(mediaHandler.List()))
	router.Handle("POST /api/v1/admin/media", requireMediaWrite(mediaHandler.Upload()))
	router.Handle("POST /api/v1/admin/media/upload", requireMediaWrite(mediaHandler.Upload()))
	router.Handle("DELETE /api/v1/admin/media/{assetId}", requireMediaWrite(mediaHandler.Delete()))
	router.Handle("GET /api/v1/admin/config", requireSettingsRead(configAdmin.Get()))
	router.Handle("PUT /api/v1/admin/config", requireSettingsWrite(configAdmin.Update()))
	router.Handle("POST /api/v1/admin/config/test-email", requireSettingsWrite(configAdmin.TestEmail()))
	router.Handle("GET /api/v1/admin/users", requireSettingsRead(adminUserHandler.List()))
	router.Handle("GET /api/v1/admin/users/{userId}", requireSettingsRead(adminUserHandler.Get()))
	router.Handle("POST /api/v1/admin/users", requireSettingsWrite(adminUserHandler.Create()))
	router.Handle("PUT /api/v1/admin/users/{userId}", requireSettingsWrite(adminUserHandler.Update()))
	router.Handle("POST /api/v1/admin/users/{userId}/reset-password", requireSettingsWrite(adminUserHandler.ResetPassword()))
	router.Handle("GET /api/v1/admin/permissions", requireSettingsRead(adminRoleHandler.Catalog()))
	router.Handle("GET /api/v1/admin/roles", requireSettingsRead(adminRoleHandler.List()))
	router.Handle("GET /api/v1/admin/roles/{role}", requireSettingsRead(adminRoleHandler.Get()))
	router.Handle("PUT /api/v1/admin/roles/{role}", requireSettingsWrite(adminRoleHandler.Update()))
	if cfg.Auth.MFAEnabled {
		router.Handle("GET /api/v1/admin/mfa", requireAuth(adminMFAHandler.Status()))
		router.Handle("POST /api/v1/admin/mfa/enroll", requireAuth(adminMFAHandler.EnrollBegin()))
		router.Handle("POST /api/v1/admin/mfa/enroll/confirm", requireAuth(adminMFAHandler.EnrollConfirm()))
		router.Handle("DELETE /api/v1/admin/mfa", requireAuth(adminMFAHandler.Disable()))
		router.Handle("POST /api/v1/admin/mfa/recovery/regenerate", requireAuth(adminMFAHandler.RegenerateRecoveryCodes()))
	}
	router.Handle("GET /api/v1/admin/audit", requireAuditRead(auditLogAdmin.List()))
	router.Handle("GET /api/v1/admin/audit/export", requireAuditRead(auditLogAdmin.Export()))
	router.Handle("GET /api/v1/admin/webhooks/events", requireSettingsRead(webhookEndpointAdmin.Catalog()))
	router.Handle("GET /api/v1/admin/webhooks", requireSettingsRead(webhookEndpointAdmin.List()))
	router.Handle("POST /api/v1/admin/webhooks", requireSettingsWrite(webhookEndpointAdmin.Create()))
	router.Handle("GET /api/v1/admin/webhooks/{id}", requireSettingsRead(webhookEndpointAdmin.Get()))
	router.Handle("PUT /api/v1/admin/webhooks/{id}", requireSettingsWrite(webhookEndpointAdmin.Update()))
	router.Handle("DELETE /api/v1/admin/webhooks/{id}", requireSettingsWrite(webhookEndpointAdmin.Delete()))
	router.Handle("GET /api/v1/admin/integrations/idempotency", requireSettingsRead(integrationIdempotencyAdmin.List()))
	router.Handle("GET /api/v1/admin/integrations/idempotency/{plugin}/{key}", requireSettingsRead(integrationIdempotencyAdmin.Get()))
	router.Handle("POST /api/v1/admin/integrations/idempotency/{plugin}/{key}/replay", requireSettingsRead(integrationIdempotencyAdmin.Replay()))
	router.Handle("GET /api/v1/admin/forms/{name}", requireAuth(schemaHandler.GetForm()))
	router.Handle("GET /api/v1/admin/grids/{name}", requireAuth(schemaHandler.GetGrid()))
	router.Handle("GET /api/v1/admin/pages", requireContentRead(pageAdmin.List()))
	router.Handle("POST /api/v1/admin/pages", requireContentWrite(pageAdmin.Create()))
	router.Handle("PUT /api/v1/admin/pages/{id}", requireContentWrite(pageAdmin.Update()))
	router.Handle("DELETE /api/v1/admin/pages/{id}", requireContentWrite(pageAdmin.Delete()))
	router.Handle("GET /api/v1/admin/menus", requireContentRead(menuAdmin.List()))
	router.Handle("GET /api/v1/admin/menus/{id}", requireContentRead(menuAdmin.Get()))
	router.Handle("PUT /api/v1/admin/menus/{id}", requireContentWrite(menuAdmin.Update()))
	router.Handle("GET /api/v1/admin/content-blocks", requireContentRead(contentBlockAdmin.List()))
	router.Handle("POST /api/v1/admin/content-blocks", requireContentWrite(contentBlockAdmin.Create()))
	router.Handle("GET /api/v1/admin/content-blocks/{id}", requireContentRead(contentBlockAdmin.Get()))
	router.Handle("PUT /api/v1/admin/content-blocks/{id}", requireContentWrite(contentBlockAdmin.Update()))
	router.Handle("DELETE /api/v1/admin/content-blocks/{id}", requireContentWrite(contentBlockAdmin.Delete()))
	router.Handle("GET /api/v1/admin/content-block-targets/{targetType}/{targetKey}", requireContentRead(contentBlockAdmin.GetTarget()))
	router.Handle("PUT /api/v1/admin/content-block-targets/{targetType}/{targetKey}", requireContentWrite(contentBlockAdmin.UpdateTarget()))
	router.Handle("GET /api/v1/admin/coupons", requireProductsRead(couponAdmin.List()))
	router.Handle("GET /api/v1/admin/coupons/{id}", requireProductsRead(couponAdmin.Get()))
	router.Handle("POST /api/v1/admin/coupons", requireProductsWrite(couponAdmin.Create()))
	router.Handle("PUT /api/v1/admin/coupons/{id}", requireProductsWrite(couponAdmin.Update()))
	router.Handle("DELETE /api/v1/admin/coupons/{id}", requireProductsWrite(couponAdmin.Delete()))
	router.Handle("GET /api/v1/admin/promotions", requireProductsRead(promotionAdmin.List()))
	router.Handle("GET /api/v1/admin/promotions/{id}", requireProductsRead(promotionAdmin.Get()))
	router.Handle("POST /api/v1/admin/promotions", requireProductsWrite(promotionAdmin.Create()))
	router.Handle("PUT /api/v1/admin/promotions/{id}", requireProductsWrite(promotionAdmin.Update()))
	router.Handle("DELETE /api/v1/admin/promotions/{id}", requireProductsWrite(promotionAdmin.Delete()))
	router.Handle("GET /api/v1/admin/attributes", requireCategoriesRead(attributeAdmin.ListAttributes()))
	router.Handle("GET /api/v1/admin/attributes/{code}", requireCategoriesRead(attributeAdmin.GetAttribute()))
	router.Handle("POST /api/v1/admin/attributes", requireCategoriesWrite(attributeAdmin.CreateAttribute()))
	router.Handle("PUT /api/v1/admin/attributes/{code}", requireCategoriesWrite(attributeAdmin.UpdateAttribute()))
	router.Handle("DELETE /api/v1/admin/attributes/{code}", requireCategoriesWrite(attributeAdmin.DeleteAttribute()))
	router.Handle("GET /api/v1/admin/attribute-groups", requireCategoriesRead(attributeAdmin.ListGroups()))
	router.Handle("GET /api/v1/admin/attribute-groups/{code}", requireCategoriesRead(attributeAdmin.GetGroup()))
	router.Handle("POST /api/v1/admin/attribute-groups", requireCategoriesWrite(attributeAdmin.CreateGroup()))
	router.Handle("PUT /api/v1/admin/attribute-groups/{code}", requireCategoriesWrite(attributeAdmin.UpdateGroup()))
	router.Handle("DELETE /api/v1/admin/attribute-groups/{code}", requireCategoriesWrite(attributeAdmin.DeleteGroup()))
	router.Handle("GET /api/v1/admin/extensions/fields", requireExtensionsRead(extensionFieldAdmin.ListFields()))
	router.Handle("GET /api/v1/admin/extensions/fields/{code}", requireExtensionsRead(extensionFieldAdmin.GetField()))
	router.Handle("POST /api/v1/admin/extensions/fields", requireExtensionsWrite(extensionFieldAdmin.CreateField()))
	router.Handle("PUT /api/v1/admin/extensions/fields/{code}", requireExtensionsWrite(extensionFieldAdmin.UpdateField()))
	router.Handle("DELETE /api/v1/admin/extensions/fields/{code}", requireExtensionsWrite(extensionFieldAdmin.DeleteField()))
	router.Handle("GET /api/v1/admin/extensions/values/{targetType}/{targetID}", requireExtensionsRead(extensionValueAdmin.ListValues()))
	router.Handle("PUT /api/v1/admin/extensions/values/{targetType}/{targetID}", requireExtensionsWrite(extensionValueAdmin.PutValues()))
	router.Handle("DELETE /api/v1/admin/extensions/values/{targetType}/{targetID}/{fieldCode}", requireExtensionsWrite(extensionValueAdmin.DeleteValue()))
	router.Handle("GET /api/v1/admin/products/{id}/extensions", requireExtensionsRead(extensionValueAdmin.ListProductExtensions()))
	router.Handle("PUT /api/v1/admin/products/{id}/extensions", requireExtensionsWrite(extensionValueAdmin.PutProductExtensions()))
	router.Handle("GET /api/v1/admin/extensions/hooks", requireExtensionsRead(extensionHookAdmin.ListHooks()))
	router.Handle("GET /api/v1/admin/extensions/slots", requireExtensionsRead(extensionSlotAdmin.ListSlots()))
	router.Handle("GET /api/v1/admin/extensions/ports", requireExtensionsRead(extensionPortAdmin.ListPorts()))
	router.Handle("GET /api/v1/admin/inventory", requireProductsRead(inventoryAdmin.List()))
	router.Handle("PUT /api/v1/admin/inventory/{variantId}", requireProductsWrite(inventoryAdmin.Adjust()))
	router.Handle("GET /api/v1/admin/stores", requireSettingsRead(storeAdmin.List()))
	router.Handle("POST /api/v1/admin/stores", requireSettingsWrite(storeAdmin.Create()))
	router.Handle("PUT /api/v1/admin/stores/{id}", requireSettingsWrite(storeAdmin.Update()))

	// Plugin admin routes (permission-guarded; registered during plugin Init).
	for _, route := range pluginApp.AdminRoutes() {
		router.Handle(route.Pattern, shophttp.RequirePermission(route.Permission)(route.Handler))
	}

	// Shipping zone admin routes.
	router.Handle("GET /api/v1/admin/shipping/zones", requireShippingRead(shippingZoneAdmin.ListZones()))
	router.Handle("POST /api/v1/admin/shipping/zones", requireShippingWrite(shippingZoneAdmin.CreateZone()))
	router.Handle("PUT /api/v1/admin/shipping/zones/{id}", requireShippingWrite(shippingZoneAdmin.UpdateZone()))
	router.Handle("DELETE /api/v1/admin/shipping/zones/{id}", requireShippingWrite(shippingZoneAdmin.DeleteZone()))
	router.Handle("GET /api/v1/admin/shipping/zones/{id}/rates", requireShippingRead(shippingZoneAdmin.ListRates()))
	router.Handle("POST /api/v1/admin/shipping/zones/{id}/rates", requireShippingWrite(shippingZoneAdmin.CreateRate()))
	router.Handle("PUT /api/v1/admin/shipping/zones/{zoneId}/rates/{rateId}", requireShippingWrite(shippingZoneAdmin.UpdateRate()))
	router.Handle("DELETE /api/v1/admin/shipping/zones/{zoneId}/rates/{rateId}", requireShippingWrite(shippingZoneAdmin.DeleteRate()))

	// Cart routes (guest-capable; ownership enforced in cart service).
	router.Handle("POST /api/v1/carts", cartHandler.Create())
	router.Handle("GET /api/v1/carts/{cartId}", cartHandler.Get())
	router.Handle("POST /api/v1/carts/{cartId}/items", cartHandler.AddItem())
	router.Handle("PUT /api/v1/carts/{cartId}/items/{variantId}", cartHandler.UpdateItem())
	router.Handle("DELETE /api/v1/carts/{cartId}/items/{variantId}", cartHandler.RemoveItem())
	router.Handle("POST /api/v1/carts/{cartId}/coupon", cartHandler.ApplyCoupon())
	router.Handle("DELETE /api/v1/carts/{cartId}/coupon", cartHandler.RemoveCoupon())

	// Checkout route (guest-capable when contact_email is provided).
	router.Handle("POST /api/v1/checkout", checkoutHandler.StartCheckout())

	// Order routes (behind RequireAuth).
	router.Handle("GET /api/v1/orders", requireAuth(orderHandler.List()))
	router.Handle("GET /api/v1/orders/{orderId}", requireAuth(orderHandler.Get()))
	router.Handle("GET /api/v1/orders/{orderId}/returns", requireAuth(returnAccount.ListByOrder()))
	router.Handle("GET /api/v1/orders/{orderId}/returnable-lines", requireAuth(returnAccount.ReturnableLines()))
	router.Handle("POST /api/v1/orders/{orderId}/returns", requireAuth(returnAccount.Request()))

	// Account routes (behind RequireAuth).
	router.Handle("GET /api/v1/account/returns", requireAuth(returnAccount.List()))
	router.Handle("GET /api/v1/account/returns/{returnId}", requireAuth(returnAccount.Get()))
	router.Handle("POST /api/v1/account/returns/{returnId}/cancel", requireAuth(returnAccount.Cancel()))
	router.Handle("GET /api/v1/account/consent", requireAuth(accountHandler.GetConsent()))
	router.Handle("GET /api/v1/account/store-credit", requireAuth(storeCreditAccount.GetBalance()))
	router.Handle("PUT /api/v1/account/consent", requireAuth(accountHandler.UpdateConsent()))
	router.Handle("GET /api/v1/account/data", requireAuth(accountHandler.GetData()))
	router.Handle("GET /api/v1/account/export", requireAuth(accountHandler.Export()))
	router.Handle("DELETE /api/v1/account", requireAuth(accountHandler.Delete()))

	// Shipping rates (behind RequireAuth — used during checkout).
	router.Handle("GET /api/v1/shipping/rates", requireAuth(shippingRates.List()))

	// Payment webhook (public — called by external payment providers).
	// Stripe-specific route first (exact match takes priority over {provider}).
	if stripeWebhook != nil {
		router.HandleFunc("POST /api/v1/payments/webhook/stripe", stripeWebhook.Handle())
	}
	router.HandleFunc("POST /api/v1/payments/webhook/{provider}", paymentWebhook.Handle())

	// Admin SPA — embedded static files served at /admin.
	adminHandler, adminErr := shophttp.NewAdminHandler()
	if adminErr != nil {
		return fmt.Errorf("admin handler: %w", adminErr)
	}
	adminWithSetup := shophttp.SetupGate(setupService, adminHandler)
	router.Handle("GET /admin", adminWithSetup)
	router.Handle("GET /admin/{path...}", adminWithSetup)

	// Storefront SSR routes (optional, gated by frontend.enabled).
	if cfg.Frontend.Enabled {
		themeEngine, thErr := themeapp.Load(cfg.Frontend.ThemePath, domtheme.WithSlotSource(slotRegistryThemeSource{reg: slotRegistry}))
		if thErr != nil {
			return fmt.Errorf("theme load: %w", thErr)
		}
		claimService := orderApp.NewClaimService(orderRepo)
		claimEmailer := storefrontOrderClaimEmailer{
			mailer: smtpmail.New(smtpmail.Config{
				Host:     cfg.Mail.SMTP.Host,
				Port:     cfg.Mail.SMTP.Port,
				User:     cfg.Mail.SMTP.User,
				Password: cfg.Mail.SMTP.Password,
				From:     cfg.Mail.SMTP.From,
			}),
			storeBaseURL: cfg.Server.PublicBaseURL,
		}
		linkService := orderApp.NewLinkOrderService(orderRepo, authService, jwtIssuer)
		linkLinker := shophttp.NewStorefrontOrderLinkerAdapter(linkService)

		storefront := shophttp.NewStorefrontHandler(themeEngine, productRepo, categoryRepo, pdp, plp, searchEngine).
			WithLegalConfig(configRepo).
			WithMenus(menuRepo, menuResolver).
			WithContentBlocks(contentBlockRepo, blockResolver, pageRepo).
			WithCart(variantRepo, cartService).
			WithExtensions(extensionValueService).
			WithCheckout(shippingReg.Providers(), payRegistry, checkoutService).
			WithAccount(authService, orderRepo, accountService).
			WithTrustedProxies(cfg.RateLimit.TrustedProxies...).
			WithReturns(returnService).
			WithAccountProfile(customerAddressRepo, consentRepo).
			WithOrderClaim(claimService).
			WithOrderClaimEmailer(claimEmailer).
			WithOrderLinker(linkLinker).
			WithAccountSecurity(cfg.Auth.JWTSecret, 10*time.Minute).
			WithAccountSecurityEmailLinks(cfg.Server.PublicBaseURL, 45*time.Minute).
			WithAssets(assetRegistry).
			WithLayeredNavAttributes(attributeStore).
			WithAdvancedSearchAttributes(attributeStore).
			WithCSPEnabled(cfg.Frontend.CSPEnabled)
		staticDir := filepath.Join(cfg.Frontend.ThemePath, "static")
		staticHandler := http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir)))
		router.Handle("GET /static/{path...}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "public, max-age=31536000")
			staticHandler.ServeHTTP(w, r)
		}))
		router.HandleFunc("GET /cart", storefront.Cart())
		router.HandleFunc("GET /account/login", storefront.Login())
		router.HandleFunc("POST /account/login", storefront.Login())
		router.HandleFunc("GET /account/register", storefront.Register())
		router.HandleFunc("POST /account/register", storefront.Register())
		router.HandleFunc("GET /account/verify-email", storefront.AccountVerifyEmail())
		router.HandleFunc("POST /account/logout", storefront.Logout())
		router.HandleFunc("GET /account/orders", storefront.AccountOrders())
		router.HandleFunc("GET /account/orders/claim", storefront.AccountOrdersClaim())
		router.HandleFunc("POST /account/orders/claim", storefront.AccountOrdersClaim())
		router.HandleFunc("GET /account/orders/{orderId}", storefront.AccountOrderDetail())
		router.HandleFunc("POST /account/orders/{orderId}/returns", storefront.AccountOrderReturnRequest())
		router.HandleFunc("GET /account/returns", storefront.AccountReturns())
		router.HandleFunc("POST /account/returns/{returnId}/cancel", storefront.AccountReturnCancel())
		router.HandleFunc("GET /account/profile", storefront.AccountProfile())
		router.HandleFunc("POST /account/profile", storefront.AccountProfile())
		router.HandleFunc("GET /account/addresses", storefront.AccountAddresses())
		router.HandleFunc("POST /account/addresses", storefront.AccountAddressCreate())
		router.HandleFunc("POST /account/addresses/{addressId}", storefront.AccountAddressUpdate())
		router.HandleFunc("POST /account/addresses/{addressId}/default", storefront.AccountAddressSetDefault())
		router.HandleFunc("POST /account/addresses/{addressId}/delete", storefront.AccountAddressDelete())
		router.HandleFunc("GET /account/preferences", storefront.AccountPreferences())
		router.HandleFunc("POST /account/preferences", storefront.AccountPreferences())
		router.HandleFunc("GET /account/security", storefront.AccountSecurity())
		router.HandleFunc("GET /account/security/verify", storefront.AccountSecurityVerify())
		router.HandleFunc("POST /account/security/verify", storefront.AccountSecurityVerify())
		router.HandleFunc("POST /account/security/password", storefront.AccountPassword())
		router.HandleFunc("POST /account/security/email", storefront.AccountEmailChange())
		router.HandleFunc("GET /account/security/email/confirm", storefront.AccountEmailChangeConfirm())
		router.HandleFunc("POST /account/security/delete", storefront.AccountDelete())
		router.HandleFunc("POST /account/profile/password", storefront.AccountPassword())
		router.HandleFunc("POST /account/profile/delete", storefront.AccountDelete())
		router.HandleFunc("GET /checkout/address", storefront.CheckoutAddress())
		router.HandleFunc("GET /checkout/shipping", storefront.CheckoutShipping())
		router.HandleFunc("POST /checkout/shipping", storefront.CheckoutShipping())
		router.HandleFunc("GET /checkout/payment", storefront.CheckoutPayment())
		router.HandleFunc("POST /checkout/payment", storefront.CheckoutPayment())
		router.HandleFunc("GET /checkout/confirm", storefront.CheckoutConfirm())
		router.HandleFunc("POST /checkout/confirm", storefront.CheckoutConfirm())
		router.HandleFunc("GET /fragments/cart-count", storefront.CartCountFragment())
		router.HandleFunc("GET /fragments/mini-cart", storefront.MiniCartFragment())
		router.HandleFunc("GET /fragments/search-suggest", storefront.SearchSuggestFragment())
		router.HandleFunc("GET /{$}", storefront.Home())
		router.HandleFunc("GET /pages/{slug}", storefront.CMSPage())
		router.HandleFunc("GET /categories", storefront.Categories())
		router.HandleFunc("GET /categories/{slug}", storefront.Category())
		router.HandleFunc("POST /cart/add", storefront.AddToCart())
		router.HandleFunc("POST /cart/update", storefront.UpdateCart())
		router.HandleFunc("POST /cart/remove", storefront.RemoveCartItem())
		router.HandleFunc("POST /fragments/cart/add", storefront.AddToCart())
		router.HandleFunc("POST /fragments/cart/update", storefront.UpdateCart())
		router.HandleFunc("POST /fragments/cart/remove", storefront.RemoveCartItem())
		router.HandleFunc("GET /products", storefront.Products())
		router.HandleFunc("GET /products/{slug}", storefront.Product())
		router.HandleFunc("GET /search", storefront.Search())

		// Guest order claim routes (public, no auth required).
		router.HandleFunc("POST /api/v1/orders/claim-search", storefront.ClaimOrderSearch())
		router.HandleFunc("POST /api/v1/orders/claim-register", storefront.ClaimLink())
	}

	srv := shophttp.NewServer(cfg.Server.Host, cfg.Server.Port, router.Handler(), log)

	var sched scheduler.Scheduler
	var schedulerCancel context.CancelFunc
	var schedulerDone chan struct{}
	if embedScheduler {
		sched = cron.New(log)
		runtime.RegisterCacheCleanup(jobQueue, cacheApp.JobType, log, sched)
		runtime.RegisterCartRecovery(jobQueue, log, sched)
		runtime.RegisterAuditRetention(jobQueue, log, sched)
		if err := integrationApp.RegisterSyncJobCronTriggers(pluginApp, jobQueue, sched, log); err != nil {
			return fmt.Errorf("sync job cron triggers: %w", err)
		}
		schedulerCtx, cancel := context.WithCancel(context.Background())
		schedulerCancel = cancel
		schedulerDone = make(chan struct{})
		go func() {
			defer close(schedulerDone)
			sched.Start(schedulerCtx)
		}()
		log.Info("app.dev.scheduler.embedded", nil)
	}

	// Start job worker in background.
	workerCtx, workerCancel := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() {
		jobWorker.Start(workerCtx)
		close(workerDone)
	}()

	// Block until server shuts down (handles SIGINT/SIGTERM internally).
	err = srv.ListenAndServe()

	var cancels []func()
	var dones []<-chan struct{}
	if schedulerCancel != nil {
		cancels = append(cancels, schedulerCancel)
		dones = append(dones, schedulerDone)
	}
	cancels = append(cancels, workerCancel)
	dones = append(dones, workerDone)
	runtime.ShutdownBackground(log, 10*time.Second, sched, cancels, dones)

	return err
}

func runSetup(cfg *config.Config, log logger.Logger) error {
	skipSeed := false
	demoSeed := false
	verbose := false

	for _, arg := range os.Args[2:] {
		switch arg {
		case "--skip-seed":
			skipSeed = true
		case "--demo-seed":
			demoSeed = true
		case "--verbose":
			verbose = true
		case "--non-interactive":
			// Accepted for forward compatibility; currently the default.
		case "--help", "-h":
			fmt.Println(`Usage: shopanda setup [flags]

Flags:
  --skip-seed          Skip the seeding step
  --demo-seed          Populate demo compliance metadata on seed catalog products
  --verbose            Print structured log entries during setup
  --non-interactive    Use env vars only, no prompts (default)
  --help, -h           Show this help`)
			return nil
		default:
			if strings.HasPrefix(arg, "--") {
				return fmt.Errorf("setup: unknown flag %q (boolean flags do not accept =value syntax)", arg)
			}
			return fmt.Errorf("setup: unexpected argument %q", arg)
		}
	}

	// Step 1: Database connectivity.
	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("setup: database: %w", err)
	}
	defer conn.Close()
	fmt.Println("✓ Database connected")
	if verbose {
		log.Info("setup.db.connected", map[string]interface{}{
			"host":     cfg.Database.Host,
			"port":     cfg.Database.Port,
			"database": cfg.Database.Name,
		})
	}

	// Step 2: Migrations.
	applied, err := migrate.Run(conn, "migrations")
	if err != nil {
		return fmt.Errorf("setup: migrate: %w", err)
	}
	if applied > 0 {
		fmt.Printf("✓ %d migrations applied\n", applied)
	} else {
		fmt.Println("✓ Migrations up to date")
	}
	if verbose {
		log.Info("setup.migrate", map[string]interface{}{"applied": applied})
	}

	// Step 3: Seeders.
	if skipSeed {
		fmt.Println("– Seeding skipped (--skip-seed)")
	} else {
		reg := seed.NewRegistry()
		registerDefaultSeeders(reg)

		deps := seed.Deps{DB: conn, Logger: log, DemoData: demoSeed}
		result, seedErr := reg.Run(context.Background(), deps)
		if seedErr != nil {
			return fmt.Errorf("setup: seed: %w", seedErr)
		}
		fmt.Printf("✓ Seed complete (executed: %d, skipped: %d)\n",
			result.Executed, result.Skipped)
		if verbose {
			log.Info("setup.seed", map[string]interface{}{
				"executed": result.Executed,
				"skipped":  result.Skipped,
			})
		}
	}

	// Summary.
	baseURL := cfg.Server.PublicBaseURL
	if baseURL == "" {
		baseURL = fmt.Sprintf("http://localhost:%d", cfg.Server.Port)
	}
	fmt.Println()
	fmt.Printf("Store is ready at %s\n", baseURL)
	fmt.Printf("Admin API: %s/api/v1/admin\n", baseURL)
	fmt.Printf("API Docs:  %s/docs\n", baseURL)

	return nil
}

func runMigrate(cfg *config.Config, log logger.Logger) error {
	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	log.Info("migrate.start", nil)

	applied, err := migrate.Run(conn, "migrations")
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	log.Info("migrate.complete", map[string]interface{}{
		"applied": applied,
	})
	return nil
}

func runImportProducts(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app import:products <file.csv>")
	}
	filePath := os.Args[2]

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open csv: %w", err)
	}
	defer f.Close()

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	productRepo, err := postgres.NewProductRepo(conn)
	if err != nil {
		return fmt.Errorf("product repo: %w", err)
	}
	variantRepo, err := postgres.NewVariantRepo(conn)
	if err != nil {
		return fmt.Errorf("variant repo: %w", err)
	}
	imp := importer.NewProductImporter(productRepo, variantRepo, conn).WithRowHooks(bootstrapImportRegistry(cfg, log))

	log.Info("import.start", map[string]interface{}{"file": filePath})

	result, err := imp.Import(context.Background(), f)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}

	log.Info("import.complete", map[string]interface{}{
		"products": result.Products,
		"variants": result.Variants,
		"skipped":  result.Skipped,
		"errors":   len(result.Errors),
	})

	for _, e := range result.Errors {
		log.Error("import.row_error", errors.New(e), map[string]interface{}{})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("import completed with %d row-level errors", len(result.Errors))
	}

	return nil
}

func runExportProducts(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app export:products <file.csv>")
	}
	filePath := os.Args[2]

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	productRepo, err := postgres.NewProductRepo(conn)
	if err != nil {
		return fmt.Errorf("product repo: %w", err)
	}
	variantRepo, err := postgres.NewVariantRepo(conn)
	if err != nil {
		return fmt.Errorf("variant repo: %w", err)
	}
	exp := exporter.NewProductExporter(productRepo, variantRepo).WithRowHooks(bootstrapExportRegistry(cfg, log))

	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("create csv: %w", err)
	}
	defer f.Close()

	log.Info("export.start", map[string]interface{}{"file": filePath})

	result, err := exp.Export(context.Background(), f)
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}

	log.Info("export.complete", map[string]interface{}{
		"products": result.Products,
		"variants": result.Variants,
		"skipped":  result.Skipped,
		"errors":   len(result.Errors),
	})

	for _, e := range result.Errors {
		log.Error("export.row_error", errors.New(e), map[string]interface{}{})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("export completed with %d row-level errors", len(result.Errors))
	}

	return nil
}

func runImportStock(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app import:stock <file.csv>")
	}
	filePath := os.Args[2]

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open csv: %w", err)
	}
	defer f.Close()

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	variantRepo, err := postgres.NewVariantRepo(conn)
	if err != nil {
		return fmt.Errorf("variant repo: %w", err)
	}
	stockRepo, err := postgres.NewStockRepo(conn)
	if err != nil {
		return fmt.Errorf("stock repo: %w", err)
	}
	imp := importer.NewStockImporter(variantRepo, stockRepo).WithRowHooks(bootstrapImportRegistry(cfg, log))

	log.Info("import.stock.start", map[string]interface{}{"file": filePath})

	result, err := imp.Import(context.Background(), f)
	if err != nil {
		return fmt.Errorf("import stock: %w", err)
	}

	log.Info("import.stock.complete", map[string]interface{}{
		"updated": result.Updated,
		"skipped": result.Skipped,
		"errors":  len(result.Errors),
	})

	for _, e := range result.Errors {
		log.Error("import.stock.row_error", errors.New(e), map[string]interface{}{})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("import completed with %d row-level errors", len(result.Errors))
	}

	return nil
}

func runExportStock(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app export:stock <file.csv>")
	}
	filePath := os.Args[2]

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	stockRepo, err := postgres.NewStockRepo(conn)
	if err != nil {
		return fmt.Errorf("stock repo: %w", err)
	}
	variantRepo, err := postgres.NewVariantRepo(conn)
	if err != nil {
		return fmt.Errorf("variant repo: %w", err)
	}
	exp := exporter.NewStockExporter(stockRepo, variantRepo).WithRowHooks(bootstrapExportRegistry(cfg, log))

	tmpFile, err := os.CreateTemp(filepath.Dir(filePath), "stock-export-*.csv")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	log.Info("export.stock.start", map[string]interface{}{"file": filePath})

	result, err := exp.Export(context.Background(), tmpFile)
	tmpFile.Close()
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("export stock: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	log.Info("export.stock.complete", map[string]interface{}{
		"entries": result.Entries,
		"skipped": result.Skipped,
		"errors":  len(result.Errors),
	})

	for _, e := range result.Errors {
		log.Error("export.stock.row_error", errors.New(e), map[string]interface{}{})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("export completed with %d row-level errors", len(result.Errors))
	}

	return nil
}

func runImportCustomers(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app import:customers <file.csv>")
	}
	filePath := os.Args[2]

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open csv: %w", err)
	}
	defer f.Close()

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	customerRepo, err := postgres.NewCustomerRepo(conn)
	if err != nil {
		return fmt.Errorf("customer repo: %w", err)
	}
	imp := importer.NewCustomerImporter(customerRepo).WithRowHooks(bootstrapImportRegistry(cfg, log))

	log.Info("import.customers.start", map[string]interface{}{"file": filePath})

	result, err := imp.Import(context.Background(), f)
	if err != nil {
		return fmt.Errorf("import customers: %w", err)
	}

	log.Info("import.customers.complete", map[string]interface{}{
		"created": result.Created,
		"skipped": result.Skipped,
		"errors":  len(result.Errors),
	})

	for _, e := range result.Errors {
		log.Error("import.customers.row_error", errors.New(e), map[string]interface{}{})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("import completed with %d row-level errors", len(result.Errors))
	}

	return nil
}

func runExportCustomers(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app export:customers <file.csv>")
	}
	filePath := os.Args[2]

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	customerRepo, err := postgres.NewCustomerRepo(conn)
	if err != nil {
		return fmt.Errorf("customer repo: %w", err)
	}
	exp := exporter.NewCustomerExporter(customerRepo).WithRowHooks(bootstrapExportRegistry(cfg, log))

	tmpFile, err := os.CreateTemp(filepath.Dir(filePath), "customer-export-*.csv")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	log.Info("export.customers.start", map[string]interface{}{"file": filePath})

	result, err := exp.Export(context.Background(), tmpFile)
	if closeErr := tmpFile.Close(); closeErr != nil {
		os.Remove(tmpPath)
		if err != nil {
			return fmt.Errorf("export customers: %w", err)
		}
		return fmt.Errorf("close temp file: %w", closeErr)
	}
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("export customers: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	log.Info("export.customers.complete", map[string]interface{}{
		"entries": result.Entries,
		"skipped": result.Skipped,
		"errors":  len(result.Errors),
	})

	for _, e := range result.Errors {
		log.Error("export.customers.row_error", errors.New(e), map[string]interface{}{})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("export completed with %d row-level errors", len(result.Errors))
	}

	return nil
}

func runScheduler(cfg *config.Config, log logger.Logger) error {
	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	registry := plugin.NewRegistry(log)
	registerPlugins(registry, cfg)
	pluginApp := &plugin.App{
		Logger:    log,
		Config:    cfg,
		Bootstrap: &plugin.Bootstrap{DB: conn},
	}
	pluginApp.SetExtensionRegistry(extensionApp.NewRegistry())
	if err := wireIntegrationStockSyncerFromDB(conn, pluginApp); err != nil {
		return err
	}
	if summary := registry.InitAll(pluginApp); summary.Failed > 0 {
		return fmt.Errorf("plugin init failed: %d plugin(s) failed to initialize", summary.Failed)
	}

	jobQueue, err := postgres.NewJobQueue(conn)
	if err != nil {
		return fmt.Errorf("job queue: %w", err)
	}
	var sched scheduler.Scheduler = cron.New(log)
	runtime.RegisterCacheCleanup(jobQueue, cacheApp.JobType, log, sched)
	runtime.RegisterCartRecovery(jobQueue, log, sched)
	runtime.RegisterAuditRetention(jobQueue, log, sched)
	if err := integrationApp.RegisterSyncJobCronTriggers(pluginApp, jobQueue, sched, log); err != nil {
		return fmt.Errorf("sync job cron triggers: %w", err)
	}

	// Block until interrupted (context cancelled via signal).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Listen for shutdown signals (same as server).
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Info("scheduler.shutdown.signal", nil)
		cancel()
	}()

	defer sched.Stop()
	sched.Start(ctx)
	return nil
}

func runConfigExport(cfg *config.Config, log logger.Logger) error {
	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	repo := postgres.NewConfigRepo(conn)
	entries, err := repo.All(context.Background())
	if err != nil {
		return fmt.Errorf("config export: %w", err)
	}

	// Build flat map keyed by full dot-notation keys.
	// This avoids ambiguity between branch maps and map-valued leaves.
	root := make(map[string]interface{}, len(entries))
	for _, e := range entries {
		if _, dup := root[e.Key]; dup {
			return fmt.Errorf("config export: duplicate key %q", e.Key)
		}
		root[e.Key] = e.Value
	}

	out, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("config export: marshal: %w", err)
	}
	fmt.Print(string(out))
	return nil
}

func runConfigImport(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app config:import <file.yaml>")
	}
	filePath := os.Args[2]

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("config import: read %s: %w", filePath, err)
	}

	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("config import: parse %s: %w", filePath, err)
	}

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	ctx := context.Background()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("config import: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	repo := postgres.NewConfigRepo(tx)
	var count int
	for k, v := range raw {
		if err := repo.Set(ctx, k, v); err != nil {
			return fmt.Errorf("config import: set %q: %w", k, err)
		}
		count++
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("config import: commit: %w", err)
	}

	log.Info("config.import.complete", map[string]interface{}{
		"file":    filePath,
		"entries": count,
	})
	return nil
}

func runImportAttributes(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app import:attributes <file.csv>")
	}
	filePath := os.Args[2]

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", filePath, err)
	}
	defer f.Close()

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	ctx := context.Background()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("import attributes: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	configRepo := postgres.NewConfigRepo(tx)
	imp := importer.NewAttributeImporter(configRepo).WithRowHooks(bootstrapImportRegistry(cfg, log))

	log.Info("import.attributes.start", map[string]interface{}{"file": filePath})

	result, err := imp.Import(ctx, f)
	if err != nil {
		return fmt.Errorf("import attributes: %w", err)
	}

	for _, e := range result.Errors {
		log.Warn("import.attributes.row_error", map[string]interface{}{"error": e})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("import completed with %d errors", len(result.Errors))
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("import attributes: commit: %w", err)
	}

	if err := syncDiscoveryFacetsFromDB(cfg, log, conn); err != nil {
		return fmt.Errorf("import committed but discovery facet sync failed: %w", err)
	}

	log.Info("import.attributes.complete", map[string]interface{}{
		"attributes": result.Attributes,
		"groups":     result.Groups,
		"skipped":    result.Skipped,
	})
	return nil
}

func runExportAttributes(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app export:attributes <file.csv>")
	}
	filePath := os.Args[2]

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	configRepo := postgres.NewConfigRepo(conn)
	exp := exporter.NewAttributeExporter(configRepo).WithRowHooks(bootstrapExportRegistry(cfg, log))

	tmpFile, err := os.CreateTemp(filepath.Dir(filePath), "attribute-export-*.csv")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	log.Info("export.attributes.start", map[string]interface{}{"file": filePath})

	result, err := exp.Export(context.Background(), tmpFile)
	if closeErr := tmpFile.Close(); closeErr != nil {
		os.Remove(tmpPath)
		if err != nil {
			return fmt.Errorf("export attributes: %w", err)
		}
		return fmt.Errorf("close temp file: %w", closeErr)
	}
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("export attributes: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	log.Info("export.attributes.complete", map[string]interface{}{
		"entries": result.Entries,
		"skipped": result.Skipped,
		"errors":  len(result.Errors),
	})

	for _, e := range result.Errors {
		log.Error("export.attributes.row_error", errors.New(e), map[string]interface{}{})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("export completed with %d row-level errors", len(result.Errors))
	}

	return nil
}

func runImportCategories(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app import:categories <file.csv>")
	}
	filePath := os.Args[2]

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", filePath, err)
	}
	defer f.Close()

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	categoryRepo, err := postgres.NewCategoryRepo(conn)
	if err != nil {
		return fmt.Errorf("category repo: %w", err)
	}
	imp := importer.NewCategoryImporter(categoryRepo).WithRowHooks(bootstrapImportRegistry(cfg, log))

	log.Info("import.categories.start", map[string]interface{}{"file": filePath})

	result, err := imp.Import(context.Background(), f)
	if err != nil {
		return fmt.Errorf("import categories: %w", err)
	}

	for _, w := range result.Warnings {
		log.Warn("import.categories.row_warning", map[string]interface{}{"warning": w})
	}

	for _, e := range result.Errors {
		log.Warn("import.categories.row_error", map[string]interface{}{"error": e})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("import completed with %d errors", len(result.Errors))
	}

	log.Info("import.categories.complete", map[string]interface{}{
		"created": result.Created,
		"updated": result.Updated,
		"skipped": result.Skipped,
	})

	return nil
}

func runExportCategories(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app export:categories <file.csv>")
	}
	filePath := os.Args[2]

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	categoryRepo, err := postgres.NewCategoryRepo(conn)
	if err != nil {
		return fmt.Errorf("category repo: %w", err)
	}
	exp := exporter.NewCategoryExporter(categoryRepo).WithRowHooks(bootstrapExportRegistry(cfg, log))

	tmpFile, err := os.CreateTemp(filepath.Dir(filePath), "category-export-*.csv")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	log.Info("export.categories.start", map[string]interface{}{"file": filePath})

	result, err := exp.Export(context.Background(), tmpFile)
	if closeErr := tmpFile.Close(); closeErr != nil {
		os.Remove(tmpPath)
		if err != nil {
			return fmt.Errorf("export categories: %w", err)
		}
		return fmt.Errorf("close temp file: %w", closeErr)
	}
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("export categories: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	log.Info("export.categories.complete", map[string]interface{}{
		"entries": result.Entries,
		"skipped": result.Skipped,
		"errors":  len(result.Errors),
	})

	for _, e := range result.Errors {
		log.Error("export.categories.row_error", errors.New(e), map[string]interface{}{})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("export completed with %d row-level errors", len(result.Errors))
	}

	if result.Orphans > 0 {
		log.Warn("export.categories.orphans", map[string]interface{}{
			"count": result.Orphans,
		})
	}

	return nil
}

func runImportPrices(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app import:prices <file.csv>")
	}
	filePath := os.Args[2]

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open csv: %w", err)
	}
	defer f.Close()

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	variantRepo, err := postgres.NewVariantRepo(conn)
	if err != nil {
		return fmt.Errorf("variant repo: %w", err)
	}
	priceRepo, err := postgres.NewPriceRepo(conn)
	if err != nil {
		return fmt.Errorf("price repo: %w", err)
	}
	priceHistoryRepo, err := postgres.NewPriceHistoryRepo(conn)
	if err != nil {
		return fmt.Errorf("price history repo: %w", err)
	}
	imp := importer.NewPriceImporter(variantRepo, priceRepo, priceHistoryRepo, conn, nil).WithRowHooks(bootstrapImportRegistry(cfg, log))

	log.Info("import.prices.start", map[string]interface{}{"file": filePath})

	result, err := imp.Import(context.Background(), f)
	if err != nil {
		return fmt.Errorf("import prices: %w", err)
	}

	for _, e := range result.Errors {
		log.Warn("import.prices.row_error", map[string]interface{}{"error": e})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("import completed with %d errors", len(result.Errors))
	}

	log.Info("import.prices.complete", map[string]interface{}{
		"created": result.Created,
		"updated": result.Updated,
		"skipped": result.Skipped,
	})

	return nil
}

func runExportPrices(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app export:prices <file.csv>")
	}
	filePath := os.Args[2]

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	variantRepo, err := postgres.NewVariantRepo(conn)
	if err != nil {
		return fmt.Errorf("variant repo: %w", err)
	}
	priceRepo, err := postgres.NewPriceRepo(conn)
	if err != nil {
		return fmt.Errorf("price repo: %w", err)
	}
	exp := exporter.NewPriceExporter(priceRepo, variantRepo).WithRowHooks(bootstrapExportRegistry(cfg, log))

	tmpFile, err := os.CreateTemp(filepath.Dir(filePath), "price-export-*.csv")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	log.Info("export.prices.start", map[string]interface{}{"file": filePath})

	result, err := exp.Export(context.Background(), tmpFile)
	if closeErr := tmpFile.Close(); closeErr != nil {
		os.Remove(tmpPath)
		if err != nil {
			return fmt.Errorf("export prices: %w", err)
		}
		return fmt.Errorf("close temp file: %w", closeErr)
	}
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("export prices: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	log.Info("export.prices.complete", map[string]interface{}{
		"entries": result.Entries,
		"skipped": result.Skipped,
		"errors":  len(result.Errors),
	})

	for _, e := range result.Errors {
		log.Error("export.prices.row_error", errors.New(e), map[string]interface{}{})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("export completed with %d row-level errors", len(result.Errors))
	}

	return nil
}

func runExportEpr(cfg *config.Config, log logger.Logger) error {
	filePath, includeEmpty, err := parseEprExportArgs(os.Args[2:])
	if err != nil {
		return err
	}

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	productRepo, err := postgres.NewProductRepo(conn)
	if err != nil {
		return fmt.Errorf("product repo: %w", err)
	}
	variantRepo, err := postgres.NewVariantRepo(conn)
	if err != nil {
		return fmt.Errorf("variant repo: %w", err)
	}
	configRepo := postgres.NewConfigRepo(conn)
	exp := exporter.NewEprExporter(productRepo, variantRepo, configRepo)

	tmpFile, err := os.CreateTemp(filepath.Dir(filePath), "epr-export-*.csv")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	log.Info("export.epr.start", map[string]interface{}{"file": filePath})

	result, err := exp.Export(context.Background(), tmpFile, exporter.EprExportOptions{IncludeEmpty: includeEmpty})
	if closeErr := tmpFile.Close(); closeErr != nil {
		os.Remove(tmpPath)
		if err != nil {
			return fmt.Errorf("export epr: %w", err)
		}
		return fmt.Errorf("close temp file: %w", closeErr)
	}
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("export epr: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	log.Info("export.epr.complete", map[string]interface{}{
		"rows": result.Rows,
	})

	return nil
}

func parseEprExportArgs(args []string) (filePath string, includeEmpty bool, err error) {
	for _, arg := range args {
		if arg == "--include-empty" {
			includeEmpty = true
			continue
		}
		if strings.HasPrefix(arg, "--") {
			return "", false, fmt.Errorf("export:epr: unknown flag %q", arg)
		}
		if filePath != "" {
			return "", false, fmt.Errorf("export:epr: unexpected argument %q", arg)
		}
		filePath = arg
	}
	if filePath == "" {
		return "", false, fmt.Errorf("usage: app export:epr [--include-empty] <file.csv>")
	}
	return filePath, includeEmpty, nil
}

func runExportOss(cfg *config.Config, log logger.Logger) error {
	filePath, summary, from, to, err := parseOssExportArgs(os.Args[2:])
	if err != nil {
		return err
	}

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	orderRepo, err := postgres.NewOrderRepo(conn)
	if err != nil {
		return err
	}
	configRepo := postgres.NewConfigRepo(conn)

	exp := exporter.NewOssExporter(orderRepo, configRepo)
	tmpFile, err := os.CreateTemp(filepath.Dir(filePath), "oss-export-*.csv")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	log.Info("export.oss.start", map[string]interface{}{"file": filePath, "summary": summary})

	result, err := exp.Export(context.Background(), tmpFile, exporter.OssExportOptions{
		From:    from,
		To:      to,
		Summary: summary,
	})
	if closeErr := tmpFile.Close(); closeErr != nil {
		os.Remove(tmpPath)
		if err != nil {
			return fmt.Errorf("export oss: %w", err)
		}
		return fmt.Errorf("close temp file: %w", closeErr)
	}
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("export oss: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	log.Info("export.oss.complete", map[string]interface{}{
		"rows":    result.Rows,
		"summary": summary,
	})

	return nil
}

func parseOssExportArgs(args []string) (filePath string, summary bool, from, to time.Time, err error) {
	var fromRaw, toRaw string
	for _, arg := range args {
		switch {
		case arg == "--summary":
			summary = true
		case strings.HasPrefix(arg, "--from="):
			fromRaw = strings.TrimPrefix(arg, "--from=")
		case strings.HasPrefix(arg, "--to="):
			toRaw = strings.TrimPrefix(arg, "--to=")
		case strings.HasPrefix(arg, "--"):
			return "", false, time.Time{}, time.Time{}, fmt.Errorf("export:oss: unknown flag %q", arg)
		default:
			if filePath != "" {
				return "", false, time.Time{}, time.Time{}, fmt.Errorf("export:oss: unexpected argument %q", arg)
			}
			filePath = arg
		}
	}
	if filePath == "" {
		return "", false, time.Time{}, time.Time{}, fmt.Errorf("usage: app export:oss [--summary] [--from=YYYY-MM-DD] [--to=YYYY-MM-DD] <file.csv>")
	}

	fromDate, err := exporter.ParseReportDate(fromRaw)
	if err != nil {
		return "", false, time.Time{}, time.Time{}, fmt.Errorf("export:oss: %w", err)
	}
	toDate, err := exporter.ParseReportDate(toRaw)
	if err != nil {
		return "", false, time.Time{}, time.Time{}, fmt.Errorf("export:oss: %w", err)
	}
	if fromDate.IsZero() && toDate.IsZero() {
		now := time.Now().UTC()
		year, month, _ := now.Date()
		quarterStartMonth := time.Month(((int(month)-1)/3)*3 + 1)
		fromDate = time.Date(year, quarterStartMonth, 1, 0, 0, 0, 0, time.UTC)
		toDate = now
	} else if fromDate.IsZero() || toDate.IsZero() {
		return "", false, time.Time{}, time.Time{}, fmt.Errorf("export:oss: --from and --to are required unless both are omitted")
	}
	toExclusive := exporter.ReportDateRangeEnd(toDate)
	if !toExclusive.After(fromDate) {
		return "", false, time.Time{}, time.Time{}, fmt.Errorf("export:oss: --to must be on or after --from")
	}
	return filePath, summary, fromDate, toExclusive, nil
}

func runSeed(cfg *config.Config, log logger.Logger) error {
	demoData := false
	for _, arg := range os.Args[2:] {
		switch arg {
		case "--demo-seed":
			demoData = true
		case "--help", "-h":
			fmt.Println(`Usage: shopanda seed [flags]

Flags:
  --demo-seed   Populate demo compliance metadata on seed catalog products
  --help, -h    Show this help`)
			return nil
		default:
			if strings.HasPrefix(arg, "--") {
				return fmt.Errorf("seed: unknown flag %q", arg)
			}
			return fmt.Errorf("seed: unexpected argument %q", arg)
		}
	}

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	log.Info("seed.start", nil)

	reg := seed.NewRegistry()
	registerDefaultSeeders(reg)

	deps := seed.Deps{
		DB:       conn,
		Logger:   log,
		DemoData: demoData,
	}

	result, err := reg.Run(context.Background(), deps)
	if err != nil {
		return fmt.Errorf("seed: %w", err)
	}

	log.Info("seed.complete", map[string]interface{}{
		"executed": result.Executed,
		"skipped":  result.Skipped,
	})

	return nil
}

func registerDefaultSeeders(reg *seed.Registry) {
	reg.Register(&seed.ConfigSeeder{})
	reg.Register(&seed.StoreSeeder{})
	reg.Register(&seed.TaxSeeder{})
	reg.Register(&seed.AdminSeeder{})
	reg.Register(&seed.CatalogSeeder{})
	reg.Register(&seed.WeeeAttributesSeeder{})
	reg.Register(&seed.EprAttributesSeeder{})
	reg.Register(&seed.GpsrAttributesSeeder{})
}

func printHelp(cfg *config.Config, log logger.Logger, pluginCLIRegistryFn func() *plugin.Registry) {
	fmt.Print(appendPluginCLIHelp(pluginCLIRegistryFn, `Usage: app <command> [arguments]

Commands:
  dev                  Start HTTP server with embedded worker and scheduler (local dev)
  serve                Start the HTTP server with embedded worker (default)
  setup                Run first-time setup (migrate + seed + verify)
  worker               Start the background job worker
  scheduler            Start the cron scheduler
  migrate              Run database migrations
  seed                 Seed the database with initial data
  search:reindex       Re-index all products in the search engine
  config:export        Export configuration to stdout (YAML)
  config:import <file> Import configuration from a YAML file
  import:products <f>  Import products from a CSV file
  export:products <f>  Export products to a CSV file
  import:stock <f>     Import stock from a CSV file
  export:stock <f>     Export stock to a CSV file
  import:customers <f> Import customers from a CSV file
  export:customers <f> Export customers to a CSV file
  import:attributes <f> Import attributes from a CSV file
  export:attributes <f> Export attributes to a CSV file
  import:categories <f> Import categories from a CSV file
  export:categories <f> Export categories to a CSV file
  import:prices <f>    Import prices from a CSV file
  export:prices <f>    Export prices to a CSV file
  export:epr <f>       Export EPR packaging metadata ([--include-empty] <file.csv>)
  export:oss <f>       Export OSS/IOSS tax report ([--summary] [--from=YYYY-MM-DD] [--to=YYYY-MM-DD] <file.csv>)
  plugins report       Print registered extension points and ports ([--json])
  help                 Show this help message
`))
}

type storefrontOrderClaimEmailer struct {
	mailer       mail.Mailer
	storeBaseURL string
}

func (e storefrontOrderClaimEmailer) SendClaimEmail(contactEmail, claimToken string) error {
	if e.mailer == nil {
		return fmt.Errorf("storefront order claim emailer: mailer not configured")
	}
	baseURL, err := url.Parse(strings.TrimSpace(e.storeBaseURL))
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return fmt.Errorf("storefront order claim emailer: invalid store base URL")
	}
	// Preserve any configured base path so deployments mounted under a
	// subpath still produce valid claim links.
	baseURL.Path = path.Join("/", baseURL.Path, "account/orders/claim")
	q := baseURL.Query()
	q.Set("claim_token", claimToken)
	baseURL.RawQuery = q.Encode()

	body := "Use the link below to claim your guest order and view it in your account:\n\n" + baseURL.String()
	return e.mailer.Send(context.Background(), mail.Message{
		To:      contactEmail,
		Subject: "Claim your guest order",
		Body:    body,
	})
}

// setupWorker creates a job queue, worker, mail handler, and cache cleanup
// handler. It returns the configured worker, the job queue (needed by
// notification services), and the cache instance.
func setupWorker(conn *sql.DB, cfg *config.Config, log logger.Logger, app *plugin.App) (*jobs.Worker, jobs.Queue, cache.Cache, error) {
	jobQueue, err := resolveJobQueue(app, conn, cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	jobWorker := jobs.NewWorker(jobQueue, log, time.Second)

	mailer, err := resolveMailer(app, cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	jobWorker.Register(notification.NewEmailSendHandler(mailer))

	appCache, err := resolveCache(app, conn, cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	ed, ok := appCache.(cacheApp.ExpiredDeleter)
	if !ok {
		return nil, nil, nil, fmt.Errorf("cache driver %q does not support expired entry cleanup", cfg.Cache.Driver)
	}
	jobWorker.Register(cacheApp.NewCleanupHandler(ed, log))

	mailTemplates := mail.NewTemplates()
	notification.RegisterTemplates(mailTemplates)
	cartRepo, err := postgres.NewCartRepo(conn)
	if err != nil {
		return nil, nil, nil, err
	}
	customerRepo, err := postgres.NewCustomerRepo(conn)
	if err != nil {
		return nil, nil, nil, err
	}
	variantRepo, err := postgres.NewVariantRepo(conn)
	if err != nil {
		return nil, nil, nil, err
	}
	productRepo, err := postgres.NewProductRepo(conn)
	if err != nil {
		return nil, nil, nil, err
	}
	configRepo := postgres.NewConfigRepo(conn)
	jobWorker.Register(cartApp.NewRecoveryHandler(cartApp.RecoveryHandlerConfig{
		Carts:     cartRepo,
		Customers: customerRepo,
		Variants:  variantRepo,
		Products:  productRepo,
		Templates: mailTemplates,
		Queue:     jobQueue,
		StoreURL:  cfg.Server.PublicBaseURL,
		Settings:  configRepo,
		Log:       log,
	}))

	auditLogRepo, err := postgres.NewAuditLogRepo(conn)
	if err != nil {
		return nil, nil, nil, err
	}
	jobWorker.Register(adminApp.NewRetentionHandler(auditLogRepo, configRepo, log))

	merchantWebhookRepo, err := postgres.NewWebhookEndpointRepo(conn)
	if err != nil {
		return nil, nil, nil, err
	}
	jobWorker.Register(webhookApp.NewDeliverHandler(merchantWebhookRepo, webhookApp.NewDefaultHTTPPoster(), log))

	if err := integrationApp.RegisterSyncJobHandlers(app, jobWorker); err != nil {
		return nil, nil, nil, fmt.Errorf("sync job handlers: %w", err)
	}

	return jobWorker, jobQueue, appCache, nil
}

func runWorker(cfg *config.Config, log logger.Logger) error {
	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	registry := plugin.NewRegistry(log)
	registerPlugins(registry, cfg)
	pluginApp := &plugin.App{
		Logger:    log,
		Config:    cfg,
		Bootstrap: &plugin.Bootstrap{DB: conn},
	}
	pluginApp.SetExtensionRegistry(extensionApp.NewRegistry())
	if err := wireIntegrationStockSyncerFromDB(conn, pluginApp); err != nil {
		return err
	}
	registry.InitAll(pluginApp)

	jobWorker, _, _, err := setupWorker(conn, cfg, log, pluginApp)
	if err != nil {
		return err
	}

	log.Info("worker.start", nil)

	// Block until interrupted.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Info("worker.shutdown.signal", nil)
		cancel()
	}()

	jobWorker.Start(ctx)
	return nil
}

func runSearchReindex(cfg *config.Config, log logger.Logger) error {
	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	registry := plugin.NewRegistry(log)
	registerPlugins(registry, cfg)
	pluginApp := &plugin.App{
		Logger:    log,
		Config:    cfg,
		Bootstrap: &plugin.Bootstrap{DB: conn},
	}
	pluginApp.SetExtensionRegistry(extensionApp.NewRegistry())
	registry.InitAll(pluginApp)

	searchEngine, err := resolveSearchEngine(pluginApp, conn, cfg)
	if err != nil {
		return err
	}

	log.Info("search.reindex.start", map[string]interface{}{
		"engine": searchEngine.Name(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		cancel()
	}()

	// Use a repeatable-read transaction so offset-based pagination sees a
	// stable snapshot even if products are inserted/deleted concurrently.
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return fmt.Errorf("search reindex: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	tmpProductRepo, err := postgres.NewProductRepo(conn)
	if err != nil {
		return fmt.Errorf("product repo: %w", err)
	}
	productRepo := tmpProductRepo.WithTx(tx)

	const batchSize = 100
	var offset, indexed int

	for {
		if err := ctx.Err(); err != nil {
			log.Info("search.reindex.interrupted", map[string]interface{}{
				"indexed": indexed,
			})
			return ctx.Err()
		}

		products, err := productRepo.List(ctx, offset, batchSize)
		if err != nil {
			if ctx.Err() != nil {
				log.Info("search.reindex.interrupted", map[string]interface{}{
					"indexed": indexed,
				})
				return ctx.Err()
			}
			return fmt.Errorf("search reindex: list products (offset=%d): %w", offset, err)
		}
		if len(products) == 0 {
			break
		}

		for _, p := range products {
			sp := search.Product{
				ID:          p.ID,
				Name:        p.Name,
				Slug:        p.Slug,
				Description: p.Description,
				CreatedAt:   p.CreatedAt,
				Attributes:  p.Attributes,
			}
			if err := searchEngine.IndexProduct(ctx, sp); err != nil {
				if ctx.Err() != nil {
					log.Info("search.reindex.interrupted", map[string]interface{}{
						"indexed": indexed,
					})
					return ctx.Err()
				}
				return fmt.Errorf("search reindex: index product %s: %w", p.ID, err)
			}
			indexed++
		}

		offset += len(products)
	}

	log.Info("search.reindex.complete", map[string]interface{}{
		"indexed": indexed,
	})

	return nil
}

type setupAdminUserCreator struct {
	svc *adminuserApp.Service
}

func (a setupAdminUserCreator) Create(ctx context.Context, in setupApp.AdminUserCreateInput) (*customer.Customer, error) {
	return a.svc.Create(ctx, adminuserApp.CreateInput{
		Email:     in.Email,
		Password:  in.Password,
		FirstName: in.FirstName,
		LastName:  in.LastName,
		Role:      in.Role,
	})
}

type slotRegistryThemeSource struct {
	reg *slotsApp.Registry
}

func (s slotRegistryThemeSource) Render(anchor, placement string, data interface{}) string {
	p, err := slotsApp.ParsePlacement(placement)
	if err != nil {
		return ""
	}
	return s.reg.Render(anchor, p, data)
}
