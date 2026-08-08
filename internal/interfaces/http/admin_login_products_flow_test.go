package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	appAuth "github.com/akarso/shopanda/internal/application/auth"
	domainadmin "github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/rbac"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/jwt"
	"github.com/akarso/shopanda/internal/platform/jwt/jwttest"
	"github.com/akarso/shopanda/internal/platform/password"
)

func newAdminLoginProductsFlowRouter(t *testing.T, customers *authMockCustomerRepo, products *mockAdminProductRepo, issuer *jwt.Issuer) http.Handler {
	t.Helper()

	bus := event.NewBus(authTestLogger{})
	authHandler := shophttp.NewAuthHandler(appAuth.NewService(customers, newAuthMockResetRepo(), issuer, bus, authTestLogger{}, time.Hour))
	productHandler := shophttp.NewProductAdminHandler(products, testAdminBus())

	registry := domainadmin.NewRegistry()
	adminapp.RegisterProductSchemas(registry)
	if err := registry.SetFormPermission("product.form", rbac.ProductsWrite); err != nil {
		t.Fatalf("SetFormPermission: %v", err)
	}
	if err := registry.SetGridPermission("product.grid", rbac.ProductsRead); err != nil {
		t.Fatalf("SetGridPermission: %v", err)
	}
	schemaHandler := shophttp.NewSchemaHandler(registry, nil)

	requireAuth := shophttp.RequireAuth()
	requireProductsRead := shophttp.RequirePermission(rbac.ProductsRead)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/login", authHandler.Login())
	mux.Handle("GET /api/v1/auth/me", requireAuth(authHandler.Me()))
	mux.Handle("GET /api/v1/admin/grids/{name}", requireAuth(schemaHandler.GetGrid()))
	mux.Handle("GET /api/v1/admin/products", requireProductsRead(productHandler.List()))

	parser := appAuth.NewValidatingTokenParser(issuer, customers, 0)
	return shophttp.AuthMiddleware(parser)(shophttp.AdminContextMiddleware()(mux))
}

func TestAdminLoginToProductsFlow_EndToEnd(t *testing.T) {
	issuer, err := jwt.NewIssuer(jwttest.TestSecret, time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}

	customers := newAuthMockRepo()
	adminCustomer, err := customer.NewCustomer("admin-1", "admin@example.com")
	if err != nil {
		t.Fatalf("NewCustomer: %v", err)
	}
	passwordHash, err := password.Hash("password123")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if err := adminCustomer.SetPassword(passwordHash); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	adminCustomer.Role = customer.RoleAdmin
	adminCustomer.FirstName = "Admin"
	adminCustomer.LastName = "User"
	if err := customers.Create(context.Background(), &adminCustomer); err != nil {
		t.Fatalf("Create admin customer: %v", err)
	}

	products := &mockAdminProductRepo{
		listFn: func(_ context.Context, offset, limit int) ([]catalog.Product, error) {
			if offset != 0 {
				t.Errorf("offset = %d, want 0", offset)
			}
			if limit != 20 {
				t.Errorf("limit = %d, want 20", limit)
			}
			return []catalog.Product{{ID: "prod-1", Name: "Widget", Slug: "widget", Status: catalog.StatusActive}}, nil
		},
	}

	router := newAdminLoginProductsFlowRouter(t, customers, products, issuer)

	loginReq := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewBufferString(`{"email":"admin@example.com","password":"password123"}`))
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body: %s", loginRec.Code, http.StatusOK, loginRec.Body.String())
	}

	var loginEnv authEnvelope
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginEnv); err != nil {
		t.Fatalf("unmarshal login envelope: %v", err)
	}
	if loginEnv.Error != nil {
		t.Fatalf("login error = %+v, want nil", loginEnv.Error)
	}
	var loginData struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(loginEnv.Data, &loginData); err != nil {
		t.Fatalf("unmarshal login data: %v", err)
	}
	if loginData.Token == "" {
		t.Fatal("login token is empty")
	}

	meReq := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+loginData.Token)
	meRec := httptest.NewRecorder()
	router.ServeHTTP(meRec, meReq)

	if meRec.Code != http.StatusOK {
		t.Fatalf("me status = %d, want %d; body: %s", meRec.Code, http.StatusOK, meRec.Body.String())
	}

	var meEnv authEnvelope
	if err := json.Unmarshal(meRec.Body.Bytes(), &meEnv); err != nil {
		t.Fatalf("unmarshal me envelope: %v", err)
	}
	var meData struct {
		Role  string `json:"role"`
		Email string `json:"email"`
	}
	if err := json.Unmarshal(meEnv.Data, &meData); err != nil {
		t.Fatalf("unmarshal me data: %v", err)
	}
	if meData.Role != "admin" {
		t.Fatalf("role = %q, want %q", meData.Role, "admin")
	}
	if meData.Email != "admin@example.com" {
		t.Fatalf("email = %q, want %q", meData.Email, "admin@example.com")
	}

	gridReq := httptest.NewRequest("GET", "/api/v1/admin/grids/product.grid", nil)
	gridReq.Header.Set("Authorization", "Bearer "+loginData.Token)
	gridRec := httptest.NewRecorder()
	router.ServeHTTP(gridRec, gridReq)

	if gridRec.Code != http.StatusOK {
		t.Fatalf("grid status = %d, want %d; body: %s", gridRec.Code, http.StatusOK, gridRec.Body.String())
	}

	var gridEnv authEnvelope
	if err := json.Unmarshal(gridRec.Body.Bytes(), &gridEnv); err != nil {
		t.Fatalf("unmarshal grid envelope: %v", err)
	}
	var gridData struct {
		Grid struct {
			Name string `json:"name"`
		} `json:"grid"`
	}
	if err := json.Unmarshal(gridEnv.Data, &gridData); err != nil {
		t.Fatalf("unmarshal grid data: %v", err)
	}
	if gridData.Grid.Name != "product.grid" {
		t.Fatalf("grid name = %q, want %q", gridData.Grid.Name, "product.grid")
	}

	productsReq := httptest.NewRequest("GET", "/api/v1/admin/products?page=1&per_page=20&sort=created_at&order=desc", nil)
	productsReq.Header.Set("Authorization", "Bearer "+loginData.Token)
	productsRec := httptest.NewRecorder()
	router.ServeHTTP(productsRec, productsReq)

	if productsRec.Code != http.StatusOK {
		t.Fatalf("products status = %d, want %d; body: %s", productsRec.Code, http.StatusOK, productsRec.Body.String())
	}

	var productsBody map[string]interface{}
	if err := json.Unmarshal(productsRec.Body.Bytes(), &productsBody); err != nil {
		t.Fatalf("unmarshal products body: %v", err)
	}
	data, ok := productsBody["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("products data = %#v, want object", productsBody["data"])
	}
	items, ok := data["products"].([]interface{})
	if !ok {
		t.Fatalf("products = %#v, want array", data["products"])
	}
	if len(items) != 1 {
		t.Fatalf("products len = %d, want 1", len(items))
	}
}
