package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	orderApp "github.com/akarso/shopanda/internal/application/order"
	themeapp "github.com/akarso/shopanda/internal/application/theme"
	"github.com/akarso/shopanda/internal/domain/rbac"
	domtheme "github.com/akarso/shopanda/internal/domain/theme"
	smtpmail "github.com/akarso/shopanda/internal/infrastructure/smtp"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

func buildServeHandler(cfg *config.Config, log logger.Logger, rt *serveRuntime, conn *sql.DB) (http.Handler, error) {
	router := shophttp.NewRouter()

	// Middleware: outermost first.
	router.Use(shophttp.RecoveryMiddleware(log))
	router.Use(shophttp.SecurityHeadersMiddleware(cfg.RateLimit.TrustedProxies...))
	router.Use(shophttp.RequestIDMiddleware())
	router.Use(shophttp.RateLimitMiddleware(cfg.RateLimit, log))
	// Logging wraps BodyLimit so 413 from MaxBytesReader is captured in access logs.
	router.Use(shophttp.LoggingMiddleware(log))
	router.Use(shophttp.BodyLimitMiddleware(cfg.HTTP.MaxBodyBytes, cfg.HTTP.MediaMaxBodyBytes))
	router.Use(shophttp.AuthMiddleware(rt.tokenParser))
	router.Use(shophttp.AdminContextMiddleware())
	router.Use(shophttp.CSRFMiddleware(cfg.RateLimit.TrustedProxies...))
	router.Use(shophttp.StoreMiddleware(rt.repos.storeRepo, log))
	router.Use(shophttp.LanguageMiddleware())
	router.Use(shophttp.CacheControlMiddleware([]string{
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

	// Routes. /healthz and /readyz are mounted outside this stack (see MountProbes below)
	// so StoreMiddleware / auth / rate-limit cannot hang probes on a dead DB.
	router.HandleFunc("GET /setup", rt.setupHandler.Page())
	router.HandleFunc("GET /api/v1/setup/status", rt.setupHandler.Status())
	router.HandleFunc("POST /api/v1/setup/install", rt.setupHandler.Install())
	router.HandleFunc("GET /sitemap.xml", rt.sitemapHandler.Serve())
	router.HandleFunc("GET /robots.txt", rt.robotsHandler.Serve())
	router.HandleFunc("GET /docs", rt.docsHandler.UI())
	router.HandleFunc("GET /docs/openapi.yaml", rt.docsHandler.Spec())

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
	router.HandleFunc("POST /api/v1/auth/register", rt.authHandler.Register())
	router.HandleFunc("POST /api/v1/auth/login", rt.authHandler.Login())
	if cfg.Auth.MFAEnabled {
		router.HandleFunc("POST /api/v1/auth/login/mfa", rt.authHandler.LoginMFA())
	}
	router.Handle("POST /api/v1/auth/logout", requireAuth(rt.authHandler.Logout()))
	router.Handle("GET /api/v1/auth/me", requireAuth(rt.authHandler.Me()))
	router.Handle("PUT /api/v1/auth/me/profile", requireAuth(rt.authHandler.UpdateProfile()))
	router.Handle("POST /api/v1/auth/me/password", requireAuth(rt.authHandler.ChangePassword()))
	router.HandleFunc("POST /api/v1/auth/password-reset/request", rt.authHandler.RequestPasswordReset())
	router.HandleFunc("POST /api/v1/auth/password-reset/confirm", rt.authHandler.ConfirmPasswordReset())

	router.HandleFunc("GET /api/v1/products", rt.productHandler.List())
	router.HandleFunc("GET /api/v1/products/{id}", rt.productHandler.Get())
	router.HandleFunc("GET /api/v1/products/{id}/reviews", rt.reviewHandler.List())
	router.Handle("POST /api/v1/products/{id}/reviews", requireAuth(rt.reviewAccount.Submit()))
	router.HandleFunc("GET /api/v1/products/{id}/variants", rt.variantHandler.List())
	router.HandleFunc("GET /api/v1/products/{id}/variants/{variantId}", rt.variantHandler.Get())

	// Category routes (public).
	router.HandleFunc("GET /api/v1/categories", rt.categoryHandler.Tree())
	router.HandleFunc("GET /api/v1/categories/{id}", rt.categoryHandler.Get())
	router.HandleFunc("GET /api/v1/categories/{id}/products", rt.categoryHandler.Products())

	// Search routes (public).
	router.HandleFunc("GET /api/v1/search", rt.searchHandler.Search())
	router.HandleFunc("GET /api/v1/search/suggest", rt.searchHandler.Suggest())

	// Page routes (public).
	router.HandleFunc("GET /api/v1/pages/{slug}", rt.pageHandler.Get())

	// Menu routes (public).
	router.HandleFunc("GET /api/v1/menus/{code}", rt.menuHandler.GetByCode())

	// Content block routes (public).
	router.HandleFunc("GET /api/v1/content-blocks/{targetType}/{targetKey}", rt.contentBlockHandler.GetByTarget())

	// Plugin public routes (registered during plugin Init). Use TryHandle so a
	// pattern that conflicts with a core route fails startup instead of panicking.
	for _, route := range rt.pluginApp.PublicRoutes() {
		if err := router.TryHandle(route.Pattern, route.Handler); err != nil {
			return nil, fmt.Errorf("register plugin public route: %w", err)
		}
	}

	// Admin routes (behind RequirePermission).
	router.Handle("GET /api/v1/admin/products", requireProductsRead(rt.productAdmin.List()))
	router.Handle("GET /api/v1/admin/products/{id}", requireProductsRead(rt.productAdmin.Get()))
	router.Handle("POST /api/v1/admin/products", requireProductsWrite(rt.productAdmin.Create()))
	router.Handle("PUT /api/v1/admin/products/{id}", requireProductsWrite(rt.productAdmin.Update()))
	router.Handle("GET /api/v1/admin/categories", requireCategoriesRead(rt.categoryHandler.Tree()))
	router.Handle("GET /api/v1/admin/categories/{id}", requireCategoriesRead(rt.categoryHandler.Get()))
	router.Handle("GET /api/v1/admin/categories/{id}/products", requireCategoriesRead(rt.categoryHandler.Products()))
	router.Handle("POST /api/v1/admin/categories", requireCategoriesWrite(rt.categoryAdmin.Create()))
	router.Handle("PUT /api/v1/admin/categories/{id}", requireCategoriesWrite(rt.categoryAdmin.Update()))
	router.Handle("DELETE /api/v1/admin/categories/{id}", requireCategoriesWrite(rt.categoryAdmin.Delete()))
	router.Handle("POST /api/v1/admin/categories/{id}/products/{productId}", requireCategoriesWrite(rt.categoryProductAssignmentAdmin.Assign()))
	router.Handle("DELETE /api/v1/admin/categories/{id}/products/{productId}", requireCategoriesWrite(rt.categoryProductAssignmentAdmin.Unassign()))
	router.Handle("GET /api/v1/admin/products/{id}/translations", requireProductsRead(rt.productTranslationAdmin.Get()))
	router.Handle("PUT /api/v1/admin/products/{id}/translations", requireProductsWrite(rt.productTranslationAdmin.Update()))
	router.Handle("POST /api/v1/admin/products/{id}/variants", requireProductsWrite(rt.variantHandler.Create()))
	router.Handle("PUT /api/v1/admin/products/{id}/variants/{variantId}", requireProductsWrite(rt.variantHandler.Update()))
	router.Handle("GET /api/v1/admin/products/{id}/variants/{variantId}/price", requireProductsRead(rt.productPriceAdmin.Get()))
	router.Handle("PUT /api/v1/admin/products/{id}/variants/{variantId}/price", requireProductsWrite(rt.productPriceAdmin.Update()))
	router.Handle("GET /api/v1/admin/stats/overview", requireOrdersRead(rt.statsAdmin.Overview()))
	router.Handle("GET /api/v1/admin/customers", requireCustomersRead(rt.customerAdmin.List()))
	router.Handle("GET /api/v1/admin/customers/{customerId}", requireCustomersRead(rt.customerAdmin.Get()))
	router.Handle("DELETE /api/v1/admin/customers/{customerId}", requireCustomersWrite(rt.customerAdmin.Delete()))
	router.Handle("POST /api/v1/admin/customers/{customerId}/revoke-sessions", requireCustomersWrite(rt.customerAdmin.RevokeSessions()))
	router.Handle("GET /api/v1/admin/customers/{customerId}/store-credit", requireCustomersRead(rt.storeCreditAdmin.Get()))
	router.Handle("POST /api/v1/admin/customers/{customerId}/store-credit/issue", requireCustomersWrite(rt.storeCreditAdmin.Issue()))
	router.Handle("GET /api/v1/admin/orders", requireOrdersRead(rt.orderAdmin.List()))
	router.Handle("GET /api/v1/admin/orders/{orderId}", requireOrdersRead(rt.orderAdmin.Get()))
	router.Handle("PUT /api/v1/admin/orders/{orderId}", requireOrdersWrite(rt.orderAdmin.Update()))
	router.Handle("GET /api/v1/admin/orders/{orderId}/invoices", requireInvoicesRead(rt.invoiceAdmin.ListByOrder()))
	router.Handle("GET /api/v1/admin/invoices/{invoiceId}/pdf", requireInvoicesRead(rt.invoiceAdmin.DownloadPDF()))
	if rt.refundHandler != nil {
		router.Handle("POST /api/v1/admin/orders/{orderId}/refund", requireOrdersWrite(rt.refundHandler.Refund()))
	}
	router.Handle("GET /api/v1/admin/returns", requireOrdersRead(rt.returnAdmin.List()))
	router.Handle("GET /api/v1/admin/returns/{returnId}", requireOrdersRead(rt.returnAdmin.Get()))
	router.Handle("POST /api/v1/admin/returns/{returnId}/approve", requireOrdersWrite(rt.returnAdmin.Approve()))
	router.Handle("POST /api/v1/admin/returns/{returnId}/reject", requireOrdersWrite(rt.returnAdmin.Reject()))
	router.Handle("POST /api/v1/admin/returns/{returnId}/receive", requireOrdersWrite(rt.returnAdmin.Receive()))
	router.Handle("POST /api/v1/admin/returns/{returnId}/refund", requireOrdersWrite(rt.returnAdmin.Refund()))
	router.Handle("GET /api/v1/admin/reviews", requireProductsRead(rt.reviewAdmin.List()))
	router.Handle("GET /api/v1/admin/reviews/{reviewId}", requireProductsRead(rt.reviewAdmin.Get()))
	router.Handle("POST /api/v1/admin/reviews/{reviewId}/approve", requireProductsWrite(rt.reviewAdmin.Approve()))
	router.Handle("POST /api/v1/admin/reviews/{reviewId}/reject", requireProductsWrite(rt.reviewAdmin.Reject()))
	router.Handle("GET /api/v1/admin/reports/epr", requireProductsRead(rt.eprReportAdmin.Export()))
	router.Handle("GET /api/v1/admin/reports/oss", requireOrdersRead(rt.ossReportAdmin.Export()))
	router.Handle("GET /api/v1/admin/payments", requireOrdersRead(rt.paymentAdmin.List()))
	router.Handle("GET /api/v1/admin/payments/{paymentId}", requireOrdersRead(rt.paymentAdmin.Get()))
	router.Handle("GET /api/v1/admin/media", requireMediaRead(rt.mediaHandler.List()))
	router.Handle("POST /api/v1/admin/media", requireMediaWrite(rt.mediaHandler.Upload()))
	router.Handle("POST /api/v1/admin/media/upload", requireMediaWrite(rt.mediaHandler.Upload()))
	router.Handle("DELETE /api/v1/admin/media/{assetId}", requireMediaWrite(rt.mediaHandler.Delete()))
	router.Handle("GET /api/v1/admin/config", requireSettingsRead(rt.configAdmin.Get()))
	router.Handle("PUT /api/v1/admin/config", requireSettingsWrite(rt.configAdmin.Update()))
	router.Handle("POST /api/v1/admin/config/test-email", requireSettingsWrite(rt.configAdmin.TestEmail()))
	router.Handle("GET /api/v1/admin/users", requireSettingsRead(rt.adminUserHandler.List()))
	router.Handle("GET /api/v1/admin/users/{userId}", requireSettingsRead(rt.adminUserHandler.Get()))
	router.Handle("POST /api/v1/admin/users", requireSettingsWrite(rt.adminUserHandler.Create()))
	router.Handle("PUT /api/v1/admin/users/{userId}", requireSettingsWrite(rt.adminUserHandler.Update()))
	router.Handle("POST /api/v1/admin/users/{userId}/reset-password", requireSettingsWrite(rt.adminUserHandler.ResetPassword()))
	router.Handle("GET /api/v1/admin/permissions", requireSettingsRead(rt.adminRoleHandler.Catalog()))
	router.Handle("GET /api/v1/admin/roles", requireSettingsRead(rt.adminRoleHandler.List()))
	router.Handle("GET /api/v1/admin/roles/{role}", requireSettingsRead(rt.adminRoleHandler.Get()))
	router.Handle("PUT /api/v1/admin/roles/{role}", requireSettingsWrite(rt.adminRoleHandler.Update()))
	if cfg.Auth.MFAEnabled {
		router.Handle("GET /api/v1/admin/mfa", requireAuth(rt.adminMFAHandler.Status()))
		router.Handle("POST /api/v1/admin/mfa/enroll", requireAuth(rt.adminMFAHandler.EnrollBegin()))
		router.Handle("POST /api/v1/admin/mfa/enroll/confirm", requireAuth(rt.adminMFAHandler.EnrollConfirm()))
		router.Handle("DELETE /api/v1/admin/mfa", requireAuth(rt.adminMFAHandler.Disable()))
		router.Handle("POST /api/v1/admin/mfa/recovery/regenerate", requireAuth(rt.adminMFAHandler.RegenerateRecoveryCodes()))
	}
	router.Handle("GET /api/v1/admin/audit", requireAuditRead(rt.auditLogAdmin.List()))
	router.Handle("GET /api/v1/admin/audit/export", requireAuditRead(rt.auditLogAdmin.Export()))
	router.Handle("GET /api/v1/admin/webhooks/events", requireSettingsRead(rt.webhookEndpointAdmin.Catalog()))
	router.Handle("GET /api/v1/admin/webhooks", requireSettingsRead(rt.webhookEndpointAdmin.List()))
	router.Handle("POST /api/v1/admin/webhooks", requireSettingsWrite(rt.webhookEndpointAdmin.Create()))
	router.Handle("GET /api/v1/admin/webhooks/{id}", requireSettingsRead(rt.webhookEndpointAdmin.Get()))
	router.Handle("PUT /api/v1/admin/webhooks/{id}", requireSettingsWrite(rt.webhookEndpointAdmin.Update()))
	router.Handle("DELETE /api/v1/admin/webhooks/{id}", requireSettingsWrite(rt.webhookEndpointAdmin.Delete()))
	router.Handle("GET /api/v1/admin/integrations/idempotency", requireSettingsRead(rt.integrationIdempotencyAdmin.List()))
	router.Handle("GET /api/v1/admin/integrations/idempotency/{plugin}/{key}", requireSettingsRead(rt.integrationIdempotencyAdmin.Get()))
	router.Handle("POST /api/v1/admin/integrations/idempotency/{plugin}/{key}/replay", requireSettingsRead(rt.integrationIdempotencyAdmin.Replay()))
	router.Handle("GET /api/v1/admin/forms/{name}", requireAuth(rt.schemaHandler.GetForm()))
	router.Handle("GET /api/v1/admin/grids/{name}", requireAuth(rt.schemaHandler.GetGrid()))
	router.Handle("GET /api/v1/admin/pages", requireContentRead(rt.pageAdmin.List()))
	router.Handle("POST /api/v1/admin/pages", requireContentWrite(rt.pageAdmin.Create()))
	router.Handle("PUT /api/v1/admin/pages/{id}", requireContentWrite(rt.pageAdmin.Update()))
	router.Handle("DELETE /api/v1/admin/pages/{id}", requireContentWrite(rt.pageAdmin.Delete()))
	router.Handle("GET /api/v1/admin/menus", requireContentRead(rt.menuAdmin.List()))
	router.Handle("GET /api/v1/admin/menus/{id}", requireContentRead(rt.menuAdmin.Get()))
	router.Handle("PUT /api/v1/admin/menus/{id}", requireContentWrite(rt.menuAdmin.Update()))
	router.Handle("GET /api/v1/admin/content-blocks", requireContentRead(rt.contentBlockAdmin.List()))
	router.Handle("POST /api/v1/admin/content-blocks", requireContentWrite(rt.contentBlockAdmin.Create()))
	router.Handle("GET /api/v1/admin/content-blocks/{id}", requireContentRead(rt.contentBlockAdmin.Get()))
	router.Handle("PUT /api/v1/admin/content-blocks/{id}", requireContentWrite(rt.contentBlockAdmin.Update()))
	router.Handle("DELETE /api/v1/admin/content-blocks/{id}", requireContentWrite(rt.contentBlockAdmin.Delete()))
	router.Handle("GET /api/v1/admin/content-block-targets/{targetType}/{targetKey}", requireContentRead(rt.contentBlockAdmin.GetTarget()))
	router.Handle("PUT /api/v1/admin/content-block-targets/{targetType}/{targetKey}", requireContentWrite(rt.contentBlockAdmin.UpdateTarget()))
	router.Handle("GET /api/v1/admin/coupons", requireProductsRead(rt.couponAdmin.List()))
	router.Handle("GET /api/v1/admin/coupons/{id}", requireProductsRead(rt.couponAdmin.Get()))
	router.Handle("POST /api/v1/admin/coupons", requireProductsWrite(rt.couponAdmin.Create()))
	router.Handle("PUT /api/v1/admin/coupons/{id}", requireProductsWrite(rt.couponAdmin.Update()))
	router.Handle("DELETE /api/v1/admin/coupons/{id}", requireProductsWrite(rt.couponAdmin.Delete()))
	router.Handle("GET /api/v1/admin/promotions", requireProductsRead(rt.promotionAdmin.List()))
	router.Handle("GET /api/v1/admin/promotions/{id}", requireProductsRead(rt.promotionAdmin.Get()))
	router.Handle("POST /api/v1/admin/promotions", requireProductsWrite(rt.promotionAdmin.Create()))
	router.Handle("PUT /api/v1/admin/promotions/{id}", requireProductsWrite(rt.promotionAdmin.Update()))
	router.Handle("DELETE /api/v1/admin/promotions/{id}", requireProductsWrite(rt.promotionAdmin.Delete()))
	router.Handle("GET /api/v1/admin/attributes", requireCategoriesRead(rt.attributeAdmin.ListAttributes()))
	router.Handle("GET /api/v1/admin/attributes/{code}", requireCategoriesRead(rt.attributeAdmin.GetAttribute()))
	router.Handle("POST /api/v1/admin/attributes", requireCategoriesWrite(rt.attributeAdmin.CreateAttribute()))
	router.Handle("PUT /api/v1/admin/attributes/{code}", requireCategoriesWrite(rt.attributeAdmin.UpdateAttribute()))
	router.Handle("DELETE /api/v1/admin/attributes/{code}", requireCategoriesWrite(rt.attributeAdmin.DeleteAttribute()))
	router.Handle("GET /api/v1/admin/attribute-groups", requireCategoriesRead(rt.attributeAdmin.ListGroups()))
	router.Handle("GET /api/v1/admin/attribute-groups/{code}", requireCategoriesRead(rt.attributeAdmin.GetGroup()))
	router.Handle("POST /api/v1/admin/attribute-groups", requireCategoriesWrite(rt.attributeAdmin.CreateGroup()))
	router.Handle("PUT /api/v1/admin/attribute-groups/{code}", requireCategoriesWrite(rt.attributeAdmin.UpdateGroup()))
	router.Handle("DELETE /api/v1/admin/attribute-groups/{code}", requireCategoriesWrite(rt.attributeAdmin.DeleteGroup()))
	router.Handle("GET /api/v1/admin/extensions/fields", requireExtensionsRead(rt.extensionFieldAdmin.ListFields()))
	router.Handle("GET /api/v1/admin/extensions/fields/{code}", requireExtensionsRead(rt.extensionFieldAdmin.GetField()))
	router.Handle("POST /api/v1/admin/extensions/fields", requireExtensionsWrite(rt.extensionFieldAdmin.CreateField()))
	router.Handle("PUT /api/v1/admin/extensions/fields/{code}", requireExtensionsWrite(rt.extensionFieldAdmin.UpdateField()))
	router.Handle("DELETE /api/v1/admin/extensions/fields/{code}", requireExtensionsWrite(rt.extensionFieldAdmin.DeleteField()))
	router.Handle("GET /api/v1/admin/extensions/values/{targetType}/{targetID}", requireExtensionsRead(rt.extensionValueAdmin.ListValues()))
	router.Handle("PUT /api/v1/admin/extensions/values/{targetType}/{targetID}", requireExtensionsWrite(rt.extensionValueAdmin.PutValues()))
	router.Handle("DELETE /api/v1/admin/extensions/values/{targetType}/{targetID}/{fieldCode}", requireExtensionsWrite(rt.extensionValueAdmin.DeleteValue()))
	router.Handle("GET /api/v1/admin/products/{id}/extensions", requireExtensionsRead(rt.extensionValueAdmin.ListProductExtensions()))
	router.Handle("PUT /api/v1/admin/products/{id}/extensions", requireExtensionsWrite(rt.extensionValueAdmin.PutProductExtensions()))
	router.Handle("GET /api/v1/admin/extensions/hooks", requireExtensionsRead(rt.extensionHookAdmin.ListHooks()))
	router.Handle("GET /api/v1/admin/extensions/slots", requireExtensionsRead(rt.extensionSlotAdmin.ListSlots()))
	router.Handle("GET /api/v1/admin/extensions/ports", requireExtensionsRead(rt.extensionPortAdmin.ListPorts()))
	router.Handle("GET /api/v1/admin/inventory", requireProductsRead(rt.inventoryAdmin.List()))
	router.Handle("PUT /api/v1/admin/inventory/{variantId}", requireProductsWrite(rt.inventoryAdmin.Adjust()))
	router.Handle("GET /api/v1/admin/stores", requireSettingsRead(rt.storeAdmin.List()))
	router.Handle("POST /api/v1/admin/stores", requireSettingsWrite(rt.storeAdmin.Create()))
	router.Handle("PUT /api/v1/admin/stores/{id}", requireSettingsWrite(rt.storeAdmin.Update()))

	// Plugin admin routes (permission-guarded; registered during plugin Init).
	// Use TryHandle so a conflicting or malformed pattern fails startup instead of panicking.
	for _, route := range rt.pluginApp.AdminRoutes() {
		if err := router.TryHandle(route.Pattern, shophttp.RequirePermission(route.Permission)(route.Handler)); err != nil {
			return nil, fmt.Errorf("register plugin admin route: %w", err)
		}
	}

	// Shipping zone admin routes.
	router.Handle("GET /api/v1/admin/shipping/zones", requireShippingRead(rt.shippingZoneAdmin.ListZones()))
	router.Handle("POST /api/v1/admin/shipping/zones", requireShippingWrite(rt.shippingZoneAdmin.CreateZone()))
	router.Handle("PUT /api/v1/admin/shipping/zones/{id}", requireShippingWrite(rt.shippingZoneAdmin.UpdateZone()))
	router.Handle("DELETE /api/v1/admin/shipping/zones/{id}", requireShippingWrite(rt.shippingZoneAdmin.DeleteZone()))
	router.Handle("GET /api/v1/admin/shipping/zones/{id}/rates", requireShippingRead(rt.shippingZoneAdmin.ListRates()))
	router.Handle("POST /api/v1/admin/shipping/zones/{id}/rates", requireShippingWrite(rt.shippingZoneAdmin.CreateRate()))
	router.Handle("PUT /api/v1/admin/shipping/zones/{zoneId}/rates/{rateId}", requireShippingWrite(rt.shippingZoneAdmin.UpdateRate()))
	router.Handle("DELETE /api/v1/admin/shipping/zones/{zoneId}/rates/{rateId}", requireShippingWrite(rt.shippingZoneAdmin.DeleteRate()))

	// Cart routes (guest-capable; ownership enforced in cart service).
	router.Handle("POST /api/v1/carts", rt.cartHandler.Create())
	router.Handle("GET /api/v1/carts/{cartId}", rt.cartHandler.Get())
	router.Handle("POST /api/v1/carts/{cartId}/items", rt.cartHandler.AddItem())
	router.Handle("PUT /api/v1/carts/{cartId}/items/{variantId}", rt.cartHandler.UpdateItem())
	router.Handle("DELETE /api/v1/carts/{cartId}/items/{variantId}", rt.cartHandler.RemoveItem())
	router.Handle("POST /api/v1/carts/{cartId}/coupon", rt.cartHandler.ApplyCoupon())
	router.Handle("DELETE /api/v1/carts/{cartId}/coupon", rt.cartHandler.RemoveCoupon())

	// Checkout route (guest-capable when contact_email is provided).
	router.Handle("POST /api/v1/checkout", rt.checkoutHandler.StartCheckout())

	// Order routes (behind RequireAuth).
	router.Handle("GET /api/v1/orders", requireAuth(rt.orderHandler.List()))
	router.Handle("GET /api/v1/orders/{orderId}", requireAuth(rt.orderHandler.Get()))
	router.Handle("GET /api/v1/orders/{orderId}/returns", requireAuth(rt.returnAccount.ListByOrder()))
	router.Handle("GET /api/v1/orders/{orderId}/returnable-lines", requireAuth(rt.returnAccount.ReturnableLines()))
	router.Handle("POST /api/v1/orders/{orderId}/returns", requireAuth(rt.returnAccount.Request()))

	// Account routes (behind RequireAuth).
	router.Handle("GET /api/v1/account/returns", requireAuth(rt.returnAccount.List()))
	router.Handle("GET /api/v1/account/returns/{returnId}", requireAuth(rt.returnAccount.Get()))
	router.Handle("POST /api/v1/account/returns/{returnId}/cancel", requireAuth(rt.returnAccount.Cancel()))
	router.Handle("GET /api/v1/account/consent", requireAuth(rt.accountHandler.GetConsent()))
	router.Handle("GET /api/v1/account/store-credit", requireAuth(rt.storeCreditAccount.GetBalance()))
	router.Handle("PUT /api/v1/account/consent", requireAuth(rt.accountHandler.UpdateConsent()))
	router.Handle("GET /api/v1/account/data", requireAuth(rt.accountHandler.GetData()))
	router.Handle("GET /api/v1/account/export", requireAuth(rt.accountHandler.Export()))
	router.Handle("DELETE /api/v1/account", requireAuth(rt.accountHandler.Delete()))

	// Shipping rates (behind RequireAuth — used during checkout).
	router.Handle("GET /api/v1/shipping/rates", requireAuth(rt.shippingRates.List()))

	// Payment webhook (public — called by external payment providers).
	// Stripe-specific route first (exact match takes priority over {provider}).
	if rt.stripeWebhook != nil {
		router.HandleFunc("POST /api/v1/payments/webhook/stripe", rt.stripeWebhook.Handle())
	}
	router.HandleFunc("POST /api/v1/payments/webhook/{provider}", rt.paymentWebhook.Handle())

	// Admin SPA — embedded static files served at /admin.
	adminHandler, adminErr := shophttp.NewAdminHandler()
	if adminErr != nil {
		return nil, fmt.Errorf("admin handler: %w", adminErr)
	}
	adminWithSetup := shophttp.SetupGate(rt.setupService, adminHandler)
	router.Handle("GET /admin", adminWithSetup)
	router.Handle("GET /admin/{path...}", adminWithSetup)

	// Storefront SSR routes (optional, gated by frontend.enabled).
	if cfg.Frontend.Enabled {
		themeEngine, thErr := themeapp.Load(cfg.Frontend.ThemePath, domtheme.WithSlotSource(slotRegistryThemeSource{reg: rt.slotRegistry}))
		if thErr != nil {
			return nil, fmt.Errorf("theme load: %w", thErr)
		}
		claimService := orderApp.NewClaimService(rt.repos.orderRepo)
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
		linkService := orderApp.NewLinkOrderService(rt.repos.orderRepo, rt.authService, rt.jwtIssuer)
		linkLinker := shophttp.NewStorefrontOrderLinkerAdapter(linkService)

		storefront := shophttp.NewStorefrontHandler(themeEngine, rt.repos.productRepo, rt.repos.categoryRepo, rt.pdp, rt.plp, rt.searchEngine).
			WithLegalConfig(rt.repos.configRepo).
			WithMenus(rt.repos.menuRepo, rt.menuResolver).
			WithContentBlocks(rt.repos.contentBlockRepo, rt.blockResolver, rt.repos.pageRepo).
			WithCart(rt.repos.variantRepo, rt.cartService).
			WithExtensions(rt.extensionValueService).
			WithCheckout(rt.shippingReg.Providers(), rt.payRegistry, rt.checkoutService).
			WithAccount(rt.authService, rt.repos.orderRepo, rt.accountService).
			WithTrustedProxies(cfg.RateLimit.TrustedProxies...).
			WithReturns(rt.returnService).
			WithAccountProfile(rt.repos.customerAddressRepo, rt.repos.consentRepo).
			WithOrderClaim(claimService).
			WithOrderClaimEmailer(claimEmailer).
			WithOrderLinker(linkLinker).
			WithAccountSecurity(rt.jwtSecretStr, 10*time.Minute).
			WithAccountSecurityEmailLinks(cfg.Server.PublicBaseURL, 45*time.Minute).
			WithAssets(rt.assetRegistry).
			WithLayeredNavAttributes(rt.attributeStore).
			WithAdvancedSearchAttributes(rt.attributeStore).
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

	// Probes skip the store/auth stack (hung DB must not block liveness).
	// Cheap middleware is applied to probe handlers only — not re-wrapped around the app.
	// /readyz keeps a dedicated per-IP limiter (probes sit outside RateLimitMiddleware).
	wrapProbe := func(h http.Handler) http.Handler {
		return shophttp.RecoveryMiddleware(log)(
			shophttp.SecurityHeadersMiddleware(cfg.RateLimit.TrustedProxies...)(
				shophttp.RequestIDMiddleware()(
					shophttp.LoggingMiddleware(log)(h),
				),
			),
		)
	}
	readyHandler := shophttp.ReadyProbeLimitMiddleware(
		cfg.RateLimit.TrustedProxies,
		cfg.RateLimit.Default.Rate,
		cfg.RateLimit.Default.Burst,
		log,
	)(shophttp.ReadyHandler(conn))

	return shophttp.MountProbes(
		wrapProbe(shophttp.HealthHandler()),
		wrapProbe(readyHandler),
		router.Handler(),
	), nil
}
