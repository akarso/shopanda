package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

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
	"github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/theme"
	"github.com/akarso/shopanda/internal/infrastructure/cron"
	"github.com/akarso/shopanda/internal/infrastructure/flatrate"
	"github.com/akarso/shopanda/internal/infrastructure/localfs"
	"github.com/akarso/shopanda/internal/infrastructure/manualpay"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	smtpmail "github.com/akarso/shopanda/internal/infrastructure/smtp"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/db"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/jwt"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/migrate"

	"github.com/akarso/shopanda/internal/platform/plugin"

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
	cfg, err := config.Load(config.FindConfigFile())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log := logger.New(cfg.Log.Level)

	log.Info("app.config.loaded", map[string]interface{}{
		"config": cfg.String(),
	})

	// Subcommand dispatch.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "migrate":
			return runMigrate(cfg, log)
		case "serve":
			return runServe(cfg, log)
		case "import:products":
			return runImportProducts(cfg, log)
		case "export:products":
			return runExportProducts(cfg, log)
		case "import:stock":
			return runImportStock(cfg, log)
		case "export:stock":
			return runExportStock(cfg, log)
		case "scheduler":
			return runScheduler(cfg, log)
		case "config:export":
			return runConfigExport(cfg, log)
		case "config:import":
			return runConfigImport(cfg, log)
		default:
			return fmt.Errorf("unknown command: %s", os.Args[1])
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
	productRepo := postgres.NewProductRepo(conn)
	variantRepo := postgres.NewVariantRepo(conn)
	cartRepo := postgres.NewCartRepo(conn)
	priceRepo := postgres.NewPriceRepo(conn)
	customerRepo := postgres.NewCustomerRepo(conn)
	resetTokenRepo := postgres.NewResetTokenRepo(conn)
	reservationRepo := postgres.NewReservationRepo(conn)
	orderRepo := postgres.NewOrderRepo(conn)
	paymentRepo := postgres.NewPaymentRepo(conn)
	shippingRepo := postgres.NewShippingRepo(conn)
	categoryRepo := postgres.NewCategoryRepo(conn)
	collectionRepo := postgres.NewCollectionRepo(conn)
	_ = collectionRepo // wired in collection HTTP handlers PR

	// Search engine.
	searchEngine := postgres.NewSearchEngine(conn)

	// Job queue + worker.
	jobQueue := postgres.NewJobQueue(conn)
	jobWorker := jobs.NewWorker(jobQueue, log, time.Second)

	// Email notifications.
	mailTemplates := mail.NewTemplates()
	notification.RegisterTemplates(mailTemplates)
	mailer := smtpmail.New(smtpmail.Config{
		Host:     cfg.Mail.SMTP.Host,
		Port:     cfg.Mail.SMTP.Port,
		User:     cfg.Mail.SMTP.User,
		Password: cfg.Mail.SMTP.Password,
		From:     cfg.Mail.SMTP.From,
	})
	notifSvc := notification.New(mailTemplates, customerRepo, orderRepo, jobQueue, log)
	jobWorker.Register(notification.NewEmailSendHandler(mailer))

	// Media storage.
	var mediaStorage media.Storage
	switch cfg.Media.Storage {
	case "local":
		mediaStorage = localfs.New(cfg.Media.Local.BasePath, cfg.Media.Local.BaseURL)
	default:
		return fmt.Errorf("unsupported media.storage: %s", cfg.Media.Storage)
	}

	// Asset repository.
	assetRepo := postgres.NewAssetRepo(conn)

	// Cache.
	var appCache cache.Cache
	switch cfg.Cache.Driver {
	case "postgres":
		appCache = postgres.NewCacheStore(conn)
	default:
		return fmt.Errorf("unsupported cache.driver: %s", cfg.Cache.Driver)
	}
	_ = appCache // wired by consumers in upcoming PRs

	// Cache cleanup job handler.
	jobWorker.Register(cacheApp.NewCleanupHandler(appCache.(cacheApp.ExpiredDeleter), log))

	// Providers.
	manualPayProvider := manualpay.NewProvider()
	flatRateProvider := flatrate.NewProvider(shared.MustNewMoney(500, "USD"))

	// Event bus.
	bus := event.NewBus(log)

	// Dev handler: log password reset tokens (replace with email plugin in production).
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

	// Composition pipelines (core + plugin steps).
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
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
	pricingSteps := []pricing.PricingStep{appPricing.NewBasePriceStep(priceRepo)}
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
	cartService := cartApp.NewService(cartRepo, priceRepo, pricingPipeline, log, bus)

	// Checkout workflow.
	validateCartStep := checkoutApp.NewValidateCartStep(variantRepo)
	recalculatePricingStep := checkoutApp.NewRecalculatePricingStep(pricingPipeline)
	reserveInventoryStep := checkoutApp.NewReserveInventoryStep(reservationRepo)
	createOrderStep := checkoutApp.NewCreateOrderStep(orderRepo, variantRepo)
	selectShippingStep := checkoutApp.NewSelectShippingStep(flatRateProvider, shippingRepo)
	initiatePaymentStep := checkoutApp.NewInitiatePaymentStep(manualPayProvider, paymentRepo)
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

	// Handlers.
	productHandler := shophttp.NewProductHandler(productRepo, pdp, plp)
	productAdmin := shophttp.NewProductAdminHandler(productRepo, bus)
	variantHandler := shophttp.NewVariantHandler(productRepo, variantRepo, bus)
	cartHandler := shophttp.NewCartHandler(cartService)
	orderHandler := shophttp.NewOrderHandler(orderRepo)
	orderAdmin := shophttp.NewOrderAdminHandler(orderRepo)
	authHandler := shophttp.NewAuthHandler(authService)
	paymentWebhook := shophttp.NewPaymentWebhookHandler(paymentRepo, bus)
	shippingRates := shophttp.NewShippingRatesHandler(flatRateProvider)
	categoryHandler := shophttp.NewCategoryHandler(categoryRepo, productRepo)
	searchHandler := shophttp.NewSearchHandler(searchEngine)
	mediaService := mediaApp.NewService(mediaStorage, assetRepo, bus, log)
	mediaHandler := shophttp.NewMediaHandler(mediaService)
	schemaHandler := shophttp.NewSchemaHandler(adminRegistry)

	router := shophttp.NewRouter()

	// Middleware: outermost first.
	router.Use(shophttp.RecoveryMiddleware(log))
	router.Use(shophttp.RequestIDMiddleware())
	router.Use(shophttp.LoggingMiddleware(log))
	router.Use(shophttp.AuthMiddleware(tokenParser))

	// Routes.
	router.HandleFunc("GET /healthz", shophttp.HealthHandler())

	requireAuth := shophttp.RequireAuth()
	requireAdmin := shophttp.RequireRole(identity.RoleAdmin)

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

	// Admin routes (behind RequireRole(admin)).
	router.Handle("GET /api/v1/admin/products", requireAdmin(productAdmin.List()))
	router.Handle("POST /api/v1/admin/products", requireAdmin(productAdmin.Create()))
	router.Handle("PUT /api/v1/admin/products/{id}", requireAdmin(productAdmin.Update()))
	router.Handle("POST /api/v1/admin/products/{id}/variants", requireAdmin(variantHandler.Create()))
	router.Handle("PUT /api/v1/admin/products/{id}/variants/{variantId}", requireAdmin(variantHandler.Update()))
	router.Handle("GET /api/v1/admin/orders", requireAdmin(orderAdmin.List()))
	router.Handle("GET /api/v1/admin/orders/{orderId}", requireAdmin(orderAdmin.Get()))
	router.Handle("POST /api/v1/admin/media/upload", requireAdmin(mediaHandler.Upload()))
	router.Handle("GET /api/v1/admin/forms/{name}", requireAdmin(schemaHandler.GetForm()))
	router.Handle("GET /api/v1/admin/grids/{name}", requireAdmin(schemaHandler.GetGrid()))

	// Cart routes (behind RequireAuth).
	router.Handle("POST /api/v1/carts", requireAuth(cartHandler.Create()))
	router.Handle("GET /api/v1/carts/{cartId}", requireAuth(cartHandler.Get()))
	router.Handle("POST /api/v1/carts/{cartId}/items", requireAuth(cartHandler.AddItem()))
	router.Handle("PUT /api/v1/carts/{cartId}/items/{variantId}", requireAuth(cartHandler.UpdateItem()))
	router.Handle("DELETE /api/v1/carts/{cartId}/items/{variantId}", requireAuth(cartHandler.RemoveItem()))

	// Checkout route (behind RequireAuth).
	router.Handle("POST /api/v1/checkout", requireAuth(checkoutHandler.StartCheckout()))

	// Order routes (behind RequireAuth).
	router.Handle("GET /api/v1/orders", requireAuth(orderHandler.List()))
	router.Handle("GET /api/v1/orders/{orderId}", requireAuth(orderHandler.Get()))

	// Shipping rates (behind RequireAuth — used during checkout).
	router.Handle("GET /api/v1/shipping/rates", requireAuth(shippingRates.List()))

	// Payment webhook (public — called by external payment providers).
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

	productRepo := postgres.NewProductRepo(conn)
	variantRepo := postgres.NewVariantRepo(conn)
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

	productRepo := postgres.NewProductRepo(conn)
	variantRepo := postgres.NewVariantRepo(conn)
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

	variantRepo := postgres.NewVariantRepo(conn)
	stockRepo := postgres.NewStockRepo(conn)
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

	stockRepo := postgres.NewStockRepo(conn)
	variantRepo := postgres.NewVariantRepo(conn)
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

func runScheduler(cfg *config.Config, log logger.Logger) error {
	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	jobQueue := postgres.NewJobQueue(conn)
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
