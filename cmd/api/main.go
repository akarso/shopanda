package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	authApp "github.com/akarso/shopanda/internal/application/auth"
	cartApp "github.com/akarso/shopanda/internal/application/cart"
	checkoutApp "github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/application/importer"
	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/db"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/jwt"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/migrate"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
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

	// Composition pipelines (empty; plugins add steps later).
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()

	// Pricing pipeline.
	pricingPipeline := pricing.NewPipeline(
		appPricing.NewBasePriceStep(priceRepo),
		pricing.NewFinalizeStep(),
	)

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

	// Application services.
	cartService := cartApp.NewService(cartRepo, priceRepo, pricingPipeline, log, bus)

	// Checkout workflow.
	validateCartStep := checkoutApp.NewValidateCartStep(variantRepo)
	recalculatePricingStep := checkoutApp.NewRecalculatePricingStep(pricingPipeline)
	reserveInventoryStep := checkoutApp.NewReserveInventoryStep(reservationRepo)
	createOrderStep := checkoutApp.NewCreateOrderStep(orderRepo, variantRepo)
	checkoutWorkflow := checkoutApp.NewWorkflow([]checkoutApp.Step{
		validateCartStep,
		recalculatePricingStep,
		reserveInventoryStep,
		createOrderStep,
	}, bus, log)
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

	// Handlers.
	productHandler := shophttp.NewProductHandler(productRepo, pdp, plp)
	productAdmin := shophttp.NewProductAdminHandler(productRepo, bus)
	variantHandler := shophttp.NewVariantHandler(productRepo, variantRepo, bus)
	cartHandler := shophttp.NewCartHandler(cartService)
	orderHandler := shophttp.NewOrderHandler(orderRepo)
	orderAdmin := shophttp.NewOrderAdminHandler(orderRepo)
	authHandler := shophttp.NewAuthHandler(authService)

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

	// Admin routes (behind RequireRole(admin)).
	router.Handle("GET /api/v1/admin/products", requireAdmin(productAdmin.List()))
	router.Handle("POST /api/v1/admin/products", requireAdmin(productAdmin.Create()))
	router.Handle("PUT /api/v1/admin/products/{id}", requireAdmin(productAdmin.Update()))
	router.Handle("POST /api/v1/admin/products/{id}/variants", requireAdmin(variantHandler.Create()))
	router.Handle("PUT /api/v1/admin/products/{id}/variants/{variantId}", requireAdmin(variantHandler.Update()))
	router.Handle("GET /api/v1/admin/orders", requireAdmin(orderAdmin.List()))
	router.Handle("GET /api/v1/admin/orders/{orderId}", requireAdmin(orderAdmin.Get()))

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

	srv := shophttp.NewServer(cfg.Server.Host, cfg.Server.Port, router.Handler(), log)
	return srv.ListenAndServe()
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
