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
	"github.com/akarso/shopanda/internal/platform/apperror"
	platformAuth "github.com/akarso/shopanda/internal/platform/auth"
	"github.com/akarso/shopanda/internal/platform/event"
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

func (r *authMockCustomerRepo) ListCustomers(_ context.Context, _, _ int) ([]customer.Customer, error) {
	return nil, nil
}

func (r *authMockCustomerRepo) BumpTokenGeneration(_ context.Context, customerID string) error {
	c := r.customers[customerID]
	if c == nil {
		return apperror.NotFound("customer not found")
	}
	c.BumpTokenGeneration()
	return nil
}

func (r *authMockCustomerRepo) WithTx(_ *sql.Tx) customer.CustomerRepository {
	return r
}

func (r *authMockCustomerRepo) Delete(_ context.Context, id string) error {
	delete(r.customers, id)
	return nil
}

// ── mock reset repo ──────────────────────────────────────────────────────

type authMockResetRepo struct {
	tokens map[string]*customer.PasswordResetToken
}

func newAuthMockResetRepo() *authMockResetRepo {
	return &authMockResetRepo{tokens: make(map[string]*customer.PasswordResetToken)}
}

func (r *authMockResetRepo) Create(_ context.Context, t *customer.PasswordResetToken) error {
	r.tokens[t.TokenHash] = t
	return nil
}

func (r *authMockResetRepo) FindByTokenHash(_ context.Context, hash string) (*customer.PasswordResetToken, error) {
	return r.tokens[hash], nil
}

func (r *authMockResetRepo) MarkUsed(_ context.Context, id string) error {
	for _, t := range r.tokens {
		if t.ID == id {
			now := time.Now().UTC()
			t.UsedAt = &now
			return nil
		}
	}
	return apperror.NotFound("reset token not found")
}

// ── setup ────────────────────────────────────────────────────────────────

func authSetup() (*shophttp.AuthHandler, *jwt.Issuer) {
	issuer, _ := jwt.NewIssuer("test-secret", time.Hour)
	repo := newAuthMockRepo()
	bus := event.NewBus(authTestLogger{})
	svc := appAuth.NewService(repo, newAuthMockResetRepo(), issuer, bus, authTestLogger{}, time.Hour)
	handle := shophttp.NewAuthHandler(svc)
	return handle, issuer
}

func authSetupWithRepo() (*shophttp.AuthHandler, *jwt.Issuer, *authMockCustomerRepo) {
	issuer, _ := jwt.NewIssuer("test-secret", time.Hour)
	repo := newAuthMockRepo()
	bus := event.NewBus(authTestLogger{})
	svc := appAuth.NewService(repo, newAuthMockResetRepo(), issuer, bus, authTestLogger{}, time.Hour)
	handle := shophttp.NewAuthHandler(svc)
	return handle, issuer, repo
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
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env.Error != nil {
		t.Fatalf("unexpected error: %+v", env.Error)
	}

	var data struct {
		CustomerID string `json:"customer_id"`
		Token      string `json:"token"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
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
	if err := json.Unmarshal(loginRec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
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
	if err := json.Unmarshal(regRec.Body.Bytes(), &regEnv); err != nil {
		t.Fatalf("unmarshal reg envelope: %v", err)
	}
	var regData struct {
		CustomerID string `json:"customer_id"`
	}
	if err := json.Unmarshal(regEnv.Data, &regData); err != nil {
		t.Fatalf("unmarshal reg data: %v", err)
	}

	// Build authenticated request using JWT.
	token, _, err := issuer.Create(regData.CustomerID, "customer", 0)
	if err != nil {
		t.Fatalf("Create token: %v", err)
	}
	id, err := identity.NewIdentity(regData.CustomerID, identity.RoleCustomer)
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	meReq := httptest.NewRequest("GET", "/auth/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+token)
	meReq = meReq.WithContext(platformAuth.WithIdentity(meReq.Context(), id))
	meRec := httptest.NewRecorder()

	h.Me().ServeHTTP(meRec, meReq)

	if meRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", meRec.Code)
	}

	var env authEnvelope
	if err := json.Unmarshal(meRec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	var data struct {
		Email     string `json:"email"`
		FirstName string `json:"first_name"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
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

// ── Logout tests ─────────────────────────────────────────────────────────

func TestAuthHandler_Logout_Success(t *testing.T) {
	h, issuer, repo := authSetupWithRepo()

	// Register a customer.
	regBody := `{"email":"logout@example.com","password":"password123"}`
	regReq := httptest.NewRequest("POST", "/auth/register", bytes.NewBufferString(regBody))
	regRec := httptest.NewRecorder()
	h.Register().ServeHTTP(regRec, regReq)

	var regEnv authEnvelope
	if err := json.Unmarshal(regRec.Body.Bytes(), &regEnv); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var regData struct {
		CustomerID string `json:"customer_id"`
	}
	if err := json.Unmarshal(regEnv.Data, &regData); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}

	token, _, err := issuer.Create(regData.CustomerID, "customer", 0)
	if err != nil {
		t.Fatalf("Create token: %v", err)
	}
	id, err := identity.NewIdentity(regData.CustomerID, identity.RoleCustomer)
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}

	logoutReq := httptest.NewRequest("POST", "/auth/logout", nil)
	logoutReq.Header.Set("Authorization", "Bearer "+token)
	logoutReq = logoutReq.WithContext(platformAuth.WithIdentity(logoutReq.Context(), id))
	logoutRec := httptest.NewRecorder()

	h.Logout().ServeHTTP(logoutRec, logoutReq)

	if logoutRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", logoutRec.Code)
	}

	// Verify token generation was bumped.
	c := repo.customers[regData.CustomerID]
	if c == nil {
		t.Fatal("customer not found in repo after logout")
	}
	if c.TokenGeneration != 1 {
		t.Errorf("TokenGeneration = %d, want 1", c.TokenGeneration)
	}

	// Verify old token is rejected by ValidatingTokenParser.
	parser := appAuth.NewValidatingTokenParser(issuer, repo, 0)
	_, err = parser.Parse(context.Background(), token)
	if err == nil {
		t.Error("expected old token to be rejected after logout")
	}
}

func TestAuthHandler_Logout_Unauthenticated(t *testing.T) {
	h, _ := authSetup()
	req := httptest.NewRequest("POST", "/auth/logout", nil)
	rec := httptest.NewRecorder()

	h.Logout().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// ── PasswordReset tests ──────────────────────────────────────────────────

func TestAuthHandler_RequestPasswordReset_Success(t *testing.T) {
	h, _ := authSetup()
	body := `{"email":"reset@example.com"}`
	req := httptest.NewRequest("POST", "/auth/password-reset/request", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	h.RequestPasswordReset().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestAuthHandler_RequestPasswordReset_InvalidBody(t *testing.T) {
	h, _ := authSetup()
	req := httptest.NewRequest("POST", "/auth/password-reset/request", bytes.NewBufferString("not json"))
	rec := httptest.NewRecorder()

	h.RequestPasswordReset().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
}

func TestAuthHandler_ConfirmPasswordReset_InvalidBody(t *testing.T) {
	h, _ := authSetup()
	req := httptest.NewRequest("POST", "/auth/password-reset/confirm", bytes.NewBufferString("not json"))
	rec := httptest.NewRecorder()

	h.ConfirmPasswordReset().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
}

// ── Register: expires_at ─────────────────────────────────────────────────

func TestAuthHandler_Register_ExpiresAt(t *testing.T) {
	h, _ := authSetup()
	body := `{"email":"expiry@example.com","password":"password123"}`
	req := httptest.NewRequest("POST", "/auth/register", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	h.Register().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}

	var env authEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var data struct {
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if data.ExpiresAt == "" {
		t.Error("expected non-empty expires_at")
	}
}
