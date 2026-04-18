package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	accountApp "github.com/akarso/shopanda/internal/application/account"
	adminApp "github.com/akarso/shopanda/internal/application/admin"
	authApp "github.com/akarso/shopanda/internal/application/auth"
	cacheApp "github.com/akarso/shopanda/internal/application/cache"
	cartApp "github.com/akarso/shopanda/internal/application/cart"
	checkoutApp "github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/application/exporter"
	"github.com/akarso/shopanda/internal/application/importer"
	mediaApp "github.com/akarso/shopanda/internal/application/media"
	"github.com/akarso/shopanda/internal/application/notification"
	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	"github.com/akarso/shopanda/internal/application/rewrite"
	"github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/invoice"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/domain/theme"
	"github.com/akarso/shopanda/internal/domain/translation"
	"github.com/akarso/shopanda/internal/infrastructure/cron"

	"github.com/akarso/shopanda/internal/infrastructure/flatrate"
	"github.com/akarso/shopanda/internal/infrastructure/invoicepdf"
	"github.com/akarso/shopanda/internal/infrastructure/localfs"
	"github.com/akarso/shopanda/internal/infrastructure/manualpay"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	smtpmail "github.com/akarso/shopanda/internal/infrastructure/smtp"
	"github.com/akarso/shopanda/internal/infrastructure/stripepay"
	"github.com/akarso/shopanda/internal/infrastructure/webhook"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/db"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/jwt"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/migrate"

	"github.com/akarso/shopanda/internal/platform/plugin"
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
		switch os.Args[1] {
		case "help":
			printHelp()
			return nil
		case "setup":
			return runSetup(cfg, log)
		case "migrate":
			return runMigrate(cfg, log)
		case "serve":
			return runServe(cfg, log)
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
		default:
			return fmt.Errorf("unknown command: %s (run 'help' for usage)", os.Args[1])
		}
	}

	// Default: start HTTP server.
	return runServe(cfg, log)
}

func runServe(cfg *config.Config, log logger.Logger) error {
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
	paymentRepo, err := postgres.NewPaymentRepo(conn)
	if err != nil {
		return err
	}
	shippingRepo, err := postgres.NewShippingRepo(conn)
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

	// Search engine.
	searchEngine, err := postgres.NewSearchEngine(conn)
	if err != nil {
		return err
	}

	// Job queue, worker, mailer, cache — shared setup.
	jobWorker, jobQueue, appCache, err := setupWorker(conn, cfg, log)
	if err != nil {
		return err
	}

	// Email notifications (needs jobQueue from setupWorker).
	mailTemplates := mail.NewTemplates()
	notification.RegisterTemplates(mailTemplates)

	invoiceRepo, err := postgres.NewInvoiceRepo(conn)
	if err != nil {
		return err
	}

	notifSvc := notification.New(mailTemplates, customerRepo, orderRepo, jobQueue, log,
		notification.WithStoreURL(cfg.Server.PublicBaseURL),
		notification.WithResetBaseURL(cfg.Server.PublicBaseURL+"/auth/reset-password"),
		notification.WithInvoices(invoiceRepo),
		notification.WithPDFRenderer(invoicepdf.NewRenderer()),
	)

	// Media storage.
	var mediaStorage media.Storage
	switch cfg.Media.Storage {
	case "local":
		mediaStorage = localfs.New(cfg.Media.Local.BasePath, cfg.Media.Local.BaseURL)
	default:
		return fmt.Errorf("unsupported media.storage: %s", cfg.Media.Storage)
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

	// Providers.
	manualPayProvider := manualpay.NewProvider()
	flatRateProvider := flatrate.NewProvider(shared.MustNewMoney(500, "USD"))

	// Payment provider: use Stripe when configured, otherwise manual.
	// The Stripe secret key is sourced exclusively from the
	// SHOPANDA_PAYMENT_STRIPE_SECRET_KEY environment variable.
	// YAML values for secret_key are ignored to avoid secrets in config files.
	var payProvider payment.Provider = manualPayProvider
	if cfg.Payment.Stripe.Enabled {
		stripeKey := os.Getenv("SHOPANDA_PAYMENT_STRIPE_SECRET_KEY")
		if stripeKey == "" {
			if cfg.Payment.Stripe.SecretKey != "" {
				log.Warn("payment.stripe.yaml_secret_ignored", map[string]interface{}{
					"message": "Stripe secret_key in YAML is ignored; set SHOPANDA_PAYMENT_STRIPE_SECRET_KEY env var",
				})
			}
			log.Warn("payment.stripe.no_secret", map[string]interface{}{
				"message": "Stripe enabled but SHOPANDA_PAYMENT_STRIPE_SECRET_KEY not set; falling back to manual provider",
			})
		} else {
			sp, err := stripepay.NewProvider(stripeKey)
			if err != nil {
				return fmt.Errorf("stripe provider: %w", err)
			}
			payProvider = sp
			log.Info("payment.provider.stripe", nil)
		}
	}

	// Event bus.
	bus := event.NewBus(log)

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

	// Plugin registry.
	registry := plugin.NewRegistry(log)
	// Register plugins here:
	// registry.Register(myplugin.New())
	pluginApp := &plugin.App{
		Logger: log,
		Bus:    bus,
		Config: cfg,
	}
	summary := registry.InitAll(pluginApp)
	log.Info("plugin.init.summary", map[string]interface{}{
		"registered":  summary.Registered,
		"initialized": summary.Initialized,
		"failed":      summary.Failed,
	})

	// Base URL for SEO (sitemap, canonical, robots).
	// Normalized at config load time (scheme defaulted, trailing slash stripped).
	baseURL := cfg.Server.PublicBaseURL

	// Composition pipelines (core SEO steps + plugin steps).
	pdp := composition.NewPipeline[composition.ProductContext]()
	pdp.AddStep(composition.ProductMetaStep{})
	pdp.AddStep(composition.NewJSONLDProductStep(variantRepo, priceRepo, stockRepo))
	pdp.AddStep(composition.NewCanonicalURLStep(baseURL))
	pdp.AddStep(composition.NewPriceIndicationStep(variantRepo, priceRepo, priceHistoryRepo))
	plp := composition.NewPipeline[composition.ListingContext]()
	plp.AddStep(composition.ListingMetaStep{})
	plp.AddStep(composition.NewListingCanonicalURLStep(baseURL))
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

	// Pricing pipeline (core + plugin steps + finalize).
	pricingSteps := []pricing.PricingStep{
		appPricing.NewBasePriceStep(priceRepo),
		appPricing.NewCatalogPromotionStep(promotionRepo, couponRepo),
		appPricing.NewTaxStep(taxRateRepo, "standard"),
	}
	for _, s := range pluginApp.PricingSteps() {
		if v, ok := s.(pricing.PricingStep); ok {
			pricingSteps = append(pricingSteps, v)
		} else {
			log.Error("plugin.step.invalid_type", fmt.Errorf("expected pricing.PricingStep, got %T", s), map[string]interface{}{
				"pipeline": "pricing",
			})
		}
	}
	pricingSteps = append(pricingSteps, pricing.NewFinalizeStep())
	pricingPipeline := pricing.NewPipeline(pricingSteps...)

	// Application services.
	cartService := cartApp.NewService(cartRepo, priceRepo, promotionRepo, couponRepo, pricingPipeline, log, bus)

	// Checkout workflow.
	validateCartStep := checkoutApp.NewValidateCartStep(variantRepo)
	recalculatePricingStep := checkoutApp.NewRecalculatePricingStep(pricingPipeline)
	reserveInventoryStep := checkoutApp.NewReserveInventoryStep(reservationRepo)
	createOrderStep := checkoutApp.NewCreateOrderStep(orderRepo, variantRepo)
	selectShippingStep := checkoutApp.NewSelectShippingStep(flatRateProvider, shippingRepo)
	initiatePaymentStep := checkoutApp.NewInitiatePaymentStep(payProvider, paymentRepo)
	checkoutSteps := []checkoutApp.Step{
		validateCartStep,
		recalculatePricingStep,
		reserveInventoryStep,
		createOrderStep,
		selectShippingStep,
		initiatePaymentStep,
	}
	for _, s := range pluginApp.CheckoutSteps() {
		if v, ok := s.(checkoutApp.Step); ok {
			checkoutSteps = append(checkoutSteps, v)
		} else {
			log.Error("plugin.step.invalid_type", fmt.Errorf("expected checkout.Step, got %T", s), map[string]interface{}{
				"pipeline": "checkout",
			})
		}
	}
	checkoutWorkflow := checkoutApp.NewWorkflow(checkoutSteps, bus, log)
	checkoutService := checkoutApp.NewService(cartRepo, checkoutWorkflow, log)
	checkoutHandler := shophttp.NewCheckoutHandler(checkoutService)

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

	// Admin schema registry.
	adminRegistry := admin.NewRegistry()
	adminApp.RegisterProductSchemas(adminRegistry)
	adminApp.RegisterPageSchemas(adminRegistry)

	// Associate permissions with schemas so the schema handler can
	// filter access per role. Fail closed if any wiring is broken.
	for _, sp := range []struct {
		kind string
		name string
		err  error
	}{
		{"form", "product.form", adminRegistry.SetFormPermission("product.form", rbac.ProductsWrite)},
		{"grid", "product.grid", adminRegistry.SetGridPermission("product.grid", rbac.ProductsRead)},
		{"form", "page.form", adminRegistry.SetFormPermission("page.form", rbac.SettingsWrite)},
		{"grid", "page.grid", adminRegistry.SetGridPermission("page.grid", rbac.SettingsRead)},
	} {
		if sp.err != nil {
			return fmt.Errorf("admin schema permission wiring failed for %s %q: %w", sp.kind, sp.name, sp.err)
		}
	}

	// Handlers.
	productHandler := shophttp.NewProductHandler(productRepo, pdp, plp, contentTranslator)
	productAdmin := shophttp.NewProductAdminHandler(productRepo, bus)
	variantHandler := shophttp.NewVariantHandler(productRepo, variantRepo, bus)
	cartHandler := shophttp.NewCartHandler(cartService)
	orderHandler := shophttp.NewOrderHandler(orderRepo)
	orderAdmin := shophttp.NewOrderAdminHandler(orderRepo)
	authHandler := shophttp.NewAuthHandler(authService)
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
	if refunder, ok := payProvider.(payment.Refunder); ok {
		refundHandler = shophttp.NewRefundHandler(paymentRepo, refunder, bus)
		log.Info("payment.refund_handler_enabled", nil)
	}

	shippingRates := shophttp.NewShippingRatesHandler(flatRateProvider)
	categoryHandler := shophttp.NewCategoryHandler(categoryRepo, productRepo)
	searchHandler := shophttp.NewSearchHandler(searchEngine)
	mediaService := mediaApp.NewService(mediaStorage, assetRepo, bus, log)
	mediaHandler := shophttp.NewMediaHandler(mediaService)
	schemaHandler := shophttp.NewSchemaHandler(adminRegistry)
	pageHandler := shophttp.NewPageHandler(pageRepo, contentTranslator)
	pageAdmin := shophttp.NewPageAdminHandler(pageRepo, bus)
	storeAdmin := shophttp.NewStoreAdminHandler(storeRepo, bus)
	accountService := accountApp.NewService(customerRepo, consentRepo, bus, log, conn)
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
	router.Use(shophttp.StoreMiddleware(storeRepo, log))
	router.Use(shophttp.LanguageMiddleware())
	router.Use(shophttp.CacheControlMiddleware([]string{
		"/healthz",
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
	router.HandleFunc("GET /sitemap.xml", sitemapHandler.Serve())
	router.HandleFunc("GET /robots.txt", robotsHandler.Serve())
	router.HandleFunc("GET /docs", docsHandler.UI())
	router.HandleFunc("GET /docs/openapi.yaml", docsHandler.Spec())

	requireAuth := shophttp.RequireAuth()

	// Permission-based middleware for admin routes.
	requireProductsRead := shophttp.RequirePermission(rbac.ProductsRead)
	requireProductsWrite := shophttp.RequirePermission(rbac.ProductsWrite)
	requireOrdersRead := shophttp.RequirePermission(rbac.OrdersRead)
	requireOrdersWrite := shophttp.RequirePermission(rbac.OrdersWrite)
	requireMediaWrite := shophttp.RequirePermission(rbac.MediaWrite)
	requireSettingsRead := shophttp.RequirePermission(rbac.SettingsRead)
	requireSettingsWrite := shophttp.RequirePermission(rbac.SettingsWrite)

	// Auth routes.
	router.HandleFunc("POST /api/v1/auth/register", authHandler.Register())
	router.HandleFunc("POST /api/v1/auth/login", authHandler.Login())
	router.Handle("POST /api/v1/auth/logout", requireAuth(authHandler.Logout()))
	router.Handle("GET /api/v1/auth/me", requireAuth(authHandler.Me()))
	router.HandleFunc("POST /api/v1/auth/password-reset/request", authHandler.RequestPasswordReset())
	router.HandleFunc("POST /api/v1/auth/password-reset/confirm", authHandler.ConfirmPasswordReset())

	router.HandleFunc("GET /api/v1/products", productHandler.List())
	router.HandleFunc("GET /api/v1/products/{id}", productHandler.Get())
	router.HandleFunc("GET /api/v1/products/{id}/variants", variantHandler.List())
	router.HandleFunc("GET /api/v1/products/{id}/variants/{variantId}", variantHandler.Get())

	// Category routes (public).
	router.HandleFunc("GET /api/v1/categories", categoryHandler.Tree())
	router.HandleFunc("GET /api/v1/categories/{id}", categoryHandler.Get())
	router.HandleFunc("GET /api/v1/categories/{id}/products", categoryHandler.Products())

	// Search route (public).
	router.HandleFunc("GET /api/v1/search", searchHandler.Search())

	// Page routes (public).
	router.HandleFunc("GET /api/v1/pages/{slug}", pageHandler.Get())

	// Admin routes (behind RequirePermission).
	router.Handle("GET /api/v1/admin/products", requireProductsRead(productAdmin.List()))
	router.Handle("POST /api/v1/admin/products", requireProductsWrite(productAdmin.Create()))
	router.Handle("PUT /api/v1/admin/products/{id}", requireProductsWrite(productAdmin.Update()))
	router.Handle("POST /api/v1/admin/products/{id}/variants", requireProductsWrite(variantHandler.Create()))
	router.Handle("PUT /api/v1/admin/products/{id}/variants/{variantId}", requireProductsWrite(variantHandler.Update()))
	router.Handle("GET /api/v1/admin/orders", requireOrdersRead(orderAdmin.List()))
	router.Handle("GET /api/v1/admin/orders/{orderId}", requireOrdersRead(orderAdmin.Get()))
	if refundHandler != nil {
		router.Handle("POST /api/v1/admin/orders/{orderId}/refund", requireOrdersWrite(refundHandler.Refund()))
	}
	router.Handle("POST /api/v1/admin/media/upload", requireMediaWrite(mediaHandler.Upload()))
	router.Handle("GET /api/v1/admin/forms/{name}", requireAuth(schemaHandler.GetForm()))
	router.Handle("GET /api/v1/admin/grids/{name}", requireAuth(schemaHandler.GetGrid()))
	router.Handle("GET /api/v1/admin/pages", requireSettingsRead(pageAdmin.List()))
	router.Handle("POST /api/v1/admin/pages", requireSettingsWrite(pageAdmin.Create()))
	router.Handle("PUT /api/v1/admin/pages/{id}", requireSettingsWrite(pageAdmin.Update()))
	router.Handle("DELETE /api/v1/admin/pages/{id}", requireSettingsWrite(pageAdmin.Delete()))
	router.Handle("GET /api/v1/admin/stores", requireSettingsRead(storeAdmin.List()))
	router.Handle("POST /api/v1/admin/stores", requireSettingsWrite(storeAdmin.Create()))
	router.Handle("PUT /api/v1/admin/stores/{id}", requireSettingsWrite(storeAdmin.Update()))

	// Cart routes (behind RequireAuth).
	router.Handle("POST /api/v1/carts", requireAuth(cartHandler.Create()))
	router.Handle("GET /api/v1/carts/{cartId}", requireAuth(cartHandler.Get()))
	router.Handle("POST /api/v1/carts/{cartId}/items", requireAuth(cartHandler.AddItem()))
	router.Handle("PUT /api/v1/carts/{cartId}/items/{variantId}", requireAuth(cartHandler.UpdateItem()))
	router.Handle("DELETE /api/v1/carts/{cartId}/items/{variantId}", requireAuth(cartHandler.RemoveItem()))
	router.Handle("POST /api/v1/carts/{cartId}/coupon", requireAuth(cartHandler.ApplyCoupon()))
	router.Handle("DELETE /api/v1/carts/{cartId}/coupon", requireAuth(cartHandler.RemoveCoupon()))

	// Checkout route (behind RequireAuth).
	router.Handle("POST /api/v1/checkout", requireAuth(checkoutHandler.StartCheckout()))

	// Order routes (behind RequireAuth).
	router.Handle("GET /api/v1/orders", requireAuth(orderHandler.List()))
	router.Handle("GET /api/v1/orders/{orderId}", requireAuth(orderHandler.Get()))

	// Account routes (behind RequireAuth).
	router.Handle("GET /api/v1/account/consent", requireAuth(accountHandler.GetConsent()))
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

	// Storefront SSR routes (optional, gated by frontend.enabled).
	if cfg.Frontend.Enabled {
		themeEngine, thErr := theme.Load(cfg.Frontend.ThemePath)
		if thErr != nil {
			return fmt.Errorf("theme load: %w", thErr)
		}
		storefront := shophttp.NewStorefrontHandler(themeEngine, productRepo, pdp)
		router.HandleFunc("GET /products/{slug}", storefront.Product())
	}

	srv := shophttp.NewServer(cfg.Server.Host, cfg.Server.Port, router.Handler(), log)

	// Start job worker in background.
	workerCtx, workerCancel := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() {
		jobWorker.Start(workerCtx)
		close(workerDone)
	}()

	// Block until server shuts down (handles SIGINT/SIGTERM internally).
	err = srv.ListenAndServe()

	// Gracefully stop the worker, giving in-flight jobs time to finish.
	workerCancel()
	select {
	case <-workerDone:
	case <-time.After(10 * time.Second):
		log.Info("worker.shutdown.timeout", nil)
	}

	return err
}

func runSetup(cfg *config.Config, log logger.Logger) error {
	skipSeed := false
	verbose := false

	for _, arg := range os.Args[2:] {
		switch arg {
		case "--skip-seed":
			skipSeed = true
		case "--verbose":
			verbose = true
		case "--non-interactive":
			// Accepted for forward compatibility; currently the default.
		case "--help", "-h":
			fmt.Println(`Usage: shopanda setup [flags]

Flags:
  --skip-seed          Skip the seeding step
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

		deps := seed.Deps{DB: conn, Logger: log}
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
	imp := importer.NewProductImporter(productRepo, variantRepo, conn)

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
	exp := exporter.NewProductExporter(productRepo, variantRepo)

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
	})

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
	imp := importer.NewStockImporter(variantRepo, stockRepo)

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
	exp := exporter.NewStockExporter(stockRepo, variantRepo)

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
	})

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
	imp := importer.NewCustomerImporter(customerRepo)

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
	exp := exporter.NewCustomerExporter(customerRepo)

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
	})

	return nil
}

func runScheduler(cfg *config.Config, log logger.Logger) error {
	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	jobQueue, err := postgres.NewJobQueue(conn)
	if err != nil {
		return fmt.Errorf("job queue: %w", err)
	}
	var sched scheduler.Scheduler = cron.New(log)

	sched.Register("cache.cleanup", "*/5 * * * *", func() {
		job, err := jobs.NewJob(id.New(), cacheApp.JobType, nil)
		if err != nil {
			log.Error("cache.cleanup.schedule", err, nil)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := jobQueue.Enqueue(ctx, job); err != nil {
			log.Error("cache.cleanup.enqueue", err, nil)
		}
	})

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
	imp := importer.NewAttributeImporter(configRepo)

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
	exp := exporter.NewAttributeExporter(configRepo)

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
	})

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
	imp := importer.NewCategoryImporter(categoryRepo)

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
	exp := exporter.NewCategoryExporter(categoryRepo)

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
	})

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
	imp := importer.NewPriceImporter(variantRepo, priceRepo, priceHistoryRepo, conn, nil)

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
	exp := exporter.NewPriceExporter(priceRepo, variantRepo)

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
	})

	return nil
}

func runSeed(cfg *config.Config, log logger.Logger) error {
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
		DB:     conn,
		Logger: log,
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
	reg.Register(&seed.AdminSeeder{})
	reg.Register(&seed.CatalogSeeder{})
}

func printHelp() {
	fmt.Println(`Usage: app <command> [arguments]

Commands:
  serve                Start the HTTP server (default)
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
  help                 Show this help message`)
}

// setupWorker creates a job queue, worker, mail handler, and cache cleanup
// handler. It returns the configured worker, the job queue (needed by
// notification services), and the cache instance.
func setupWorker(conn *sql.DB, cfg *config.Config, log logger.Logger) (*jobs.Worker, jobs.Queue, cache.Cache, error) {
	jobQueue, err := postgres.NewJobQueue(conn)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("job queue: %w", err)
	}
	jobWorker := jobs.NewWorker(jobQueue, log, time.Second)

	mailer := smtpmail.New(smtpmail.Config{
		Host:     cfg.Mail.SMTP.Host,
		Port:     cfg.Mail.SMTP.Port,
		User:     cfg.Mail.SMTP.User,
		Password: cfg.Mail.SMTP.Password,
		From:     cfg.Mail.SMTP.From,
	})
	jobWorker.Register(notification.NewEmailSendHandler(mailer))

	var appCache cache.Cache
	switch cfg.Cache.Driver {
	case "postgres":
		cs, csErr := postgres.NewCacheStore(conn)
		if csErr != nil {
			return nil, nil, nil, fmt.Errorf("cache store: %w", csErr)
		}
		appCache = cs
	default:
		return nil, nil, nil, fmt.Errorf("unsupported cache.driver: %s", cfg.Cache.Driver)
	}
	ed, ok := appCache.(cacheApp.ExpiredDeleter)
	if !ok {
		return nil, nil, nil, fmt.Errorf("cache driver %q does not support expired entry cleanup", cfg.Cache.Driver)
	}
	jobWorker.Register(cacheApp.NewCleanupHandler(ed, log))

	return jobWorker, jobQueue, appCache, nil
}

func runWorker(cfg *config.Config, log logger.Logger) error {
	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	jobWorker, _, _, err := setupWorker(conn, cfg, log)
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

	searchEngine, err := postgres.NewSearchEngine(conn)
	if err != nil {
		return fmt.Errorf("search engine: %w", err)
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
