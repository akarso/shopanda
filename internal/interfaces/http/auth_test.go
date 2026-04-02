package http_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	appAuth "github.com/akarso/shopanda/internal/application/auth"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/identity"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	platformAuth "github.com/akarso/shopanda/internal/platform/auth"
	"github.com/akarso/shopanda/internal/platform/jwt"
)

// ── mock logger + repo ───────────────────────────────────────────────────

type authTestLogger struct{}

func (l authTestLogger) Info(_ string, _ map[string]interface{})           {}
func (l authTestLogger) Warn(_ string, _ map[string]interface{})           {}
func (l authTestLogger) Error(_ string, _ error, _ map[string]interface{}) {}

type authMockCustomerRepo struct {
	customers map[string]*customer.Customer
	byEmail   map[string]*customer.Customer
}

func newAuthMockRepo() *authMockCustomerRepo {
	return &authMockCustomerRepo{
		customers: make(map[string]*customer.Customer),
		byEmail:   make(map[string]*customer.Customer),
	}
}

func (r *authMockCustomerRepo) FindByID(_ context.Context, id string) (*customer.Customer, error) {
	return r.customers[id], nil
}

func (r *authMockCustomerRepo) FindByEmail(_ context.Context, email string) (*customer.Customer, error) {
	return r.byEmail[email], nil
}

func (r *authMockCustomerRepo) Create(_ context.Context, c *customer.Customer) error {
	r.customers[c.ID] = c
	r.byEmail[c.Email] = c
	return nil
}

func (r *authMockCustomerRepo) Update(_ context.Context, c *customer.Customer) error {
	r.customers[c.ID] = c
	r.byEmail[c.Email] = c
	return nil
}

func (r *authMockCustomerRepo) WithTx(_ *sql.Tx) customer.CustomerRepository {
	return r
}

// ── setup ────────────────────────────────────────────────────────────────

func authSetup() (*shophttp.AuthHandler, *jwt.Issuer) {
	issuer, _ := jwt.NewIssuer("test-secret", time.Hour)
	repo := newAuthMockRepo()
	svc := appAuth.NewService(repo, issuer, authTestLogger{})
	handle := shophttp.NewAuthHandler(svc)
	return handle, issuer
}

// ── envelope ─────────────────────────────────────────────────────────────

type authEnvelope struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// ── Register tests ───────────────────────────────────────────────────────

func TestAuthHandler_Register_Success(t *testing.T) {
	h, _ := authSetup()
	body := `{"email":"alice@example.com","password":"password123","first_name":"Alice","last_name":"Smith"}`
	req := httptest.NewRequest("POST", "/auth/register", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	h.Register().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}

	var env authEnvelope
	json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error != nil {
		t.Fatalf("unexpected error: %+v", env.Error)
	}

	var data struct {
		CustomerID string `json:"customer_id"`
		Token      string `json:"token"`
	}
	json.Unmarshal(env.Data, &data)
	if data.CustomerID == "" {
		t.Error("expected non-empty customer_id")
	}
	if data.Token == "" {
		t.Error("expected non-empty token")
	}
}

func TestAuthHandler_Register_InvalidBody(t *testing.T) {
	h, _ := authSetup()
	req := httptest.NewRequest("POST", "/auth/register", bytes.NewBufferString("not json"))
	rec := httptest.NewRecorder()

	h.Register().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
}

func TestAuthHandler_Register_MissingEmail(t *testing.T) {
	h, _ := authSetup()
	body := `{"password":"password123"}`
	req := httptest.NewRequest("POST", "/auth/register", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	h.Register().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
}

// ── Login tests ──────────────────────────────────────────────────────────

func TestAuthHandler_Login_Success(t *testing.T) {
	h, _ := authSetup()

	// Register first.
	regBody := `{"email":"bob@example.com","password":"password123"}`
	regReq := httptest.NewRequest("POST", "/auth/register", bytes.NewBufferString(regBody))
	regRec := httptest.NewRecorder()
	h.Register().ServeHTTP(regRec, regReq)

	// Login.
	loginBody := `{"email":"bob@example.com","password":"password123"}`
	loginReq := httptest.NewRequest("POST", "/auth/login", bytes.NewBufferString(loginBody))
	loginRec := httptest.NewRecorder()
	h.Login().ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", loginRec.Code)
	}

	var env authEnvelope
	json.Unmarshal(loginRec.Body.Bytes(), &env)
	if env.Error != nil {
		t.Fatalf("unexpected error: %+v", env.Error)
	}
}

func TestAuthHandler_Login_WrongPassword(t *testing.T) {
	h, _ := authSetup()

	regBody := `{"email":"bob@example.com","password":"password123"}`
	regReq := httptest.NewRequest("POST", "/auth/register", bytes.NewBufferString(regBody))
	regRec := httptest.NewRecorder()
	h.Register().ServeHTTP(regRec, regReq)

	loginBody := `{"email":"bob@example.com","password":"wrongpass"}`
	loginReq := httptest.NewRequest("POST", "/auth/login", bytes.NewBufferString(loginBody))
	loginRec := httptest.NewRecorder()
	h.Login().ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", loginRec.Code)
	}
}

// ── Me tests ─────────────────────────────────────────────────────────────

func TestAuthHandler_Me_Success(t *testing.T) {
	h, issuer := authSetup()

	// Register to create a customer.
	regBody := `{"email":"me@example.com","password":"password123","first_name":"Me","last_name":"User"}`
	regReq := httptest.NewRequest("POST", "/auth/register", bytes.NewBufferString(regBody))
	regRec := httptest.NewRecorder()
	h.Register().ServeHTTP(regRec, regReq)

	var regEnv authEnvelope
	json.Unmarshal(regRec.Body.Bytes(), &regEnv)
	var regData struct {
		CustomerID string `json:"customer_id"`
	}
	json.Unmarshal(regEnv.Data, &regData)

	// Build authenticated request using JWT.
	token, _ := issuer.Create(regData.CustomerID, "customer")
	id, _ := identity.NewIdentity(regData.CustomerID, identity.RoleCustomer)
	meReq := httptest.NewRequest("GET", "/auth/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+token)
	meReq = meReq.WithContext(platformAuth.WithIdentity(meReq.Context(), id))
	meRec := httptest.NewRecorder()

	h.Me().ServeHTTP(meRec, meReq)

	if meRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", meRec.Code)
	}

	var env authEnvelope
	json.Unmarshal(meRec.Body.Bytes(), &env)
	var data struct {
		Email     string `json:"email"`
		FirstName string `json:"first_name"`
	}
	json.Unmarshal(env.Data, &data)
	if data.Email != "me@example.com" {
		t.Errorf("email = %q, want me@example.com", data.Email)
	}
	if data.FirstName != "Me" {
		t.Errorf("first_name = %q, want Me", data.FirstName)
	}
}

func TestAuthHandler_Me_Unauthenticated(t *testing.T) {
	h, _ := authSetup()
	req := httptest.NewRequest("GET", "/auth/me", nil)
	rec := httptest.NewRecorder()

	h.Me().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}
