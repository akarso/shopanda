package auth_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/auth"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/jwt"
	"github.com/akarso/shopanda/internal/platform/password"
)

// ── mock logger ──────────────────────────────────────────────────────────

type testLogger struct{}

func (l testLogger) Info(_ string, _ map[string]interface{})           {}
func (l testLogger) Warn(_ string, _ map[string]interface{})           {}
func (l testLogger) Error(_ string, _ error, _ map[string]interface{}) {}

// ── mock repo ────────────────────────────────────────────────────────────

type mockCustomerRepo struct {
	customers map[string]*customer.Customer
	byEmail   map[string]*customer.Customer
}

func newMockRepo() *mockCustomerRepo {
	return &mockCustomerRepo{
		customers: make(map[string]*customer.Customer),
		byEmail:   make(map[string]*customer.Customer),
	}
}

func (r *mockCustomerRepo) FindByID(_ context.Context, id string) (*customer.Customer, error) {
	c := r.customers[id]
	return c, nil
}

func (r *mockCustomerRepo) FindByEmail(_ context.Context, email string) (*customer.Customer, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	for e, c := range r.byEmail {
		if strings.ToLower(strings.TrimSpace(e)) == email {
			return c, nil
		}
	}
	return nil, nil
}

func (r *mockCustomerRepo) Create(_ context.Context, c *customer.Customer) error {
	r.customers[c.ID] = c
	r.byEmail[c.Email] = c
	return nil
}

func (r *mockCustomerRepo) Update(_ context.Context, c *customer.Customer) error {
	r.customers[c.ID] = c
	for email, existing := range r.byEmail {
		if existing.ID == c.ID && email != c.Email {
			delete(r.byEmail, email)
		}
	}
	r.byEmail[c.Email] = c
	return nil
}

func (r *mockCustomerRepo) ListCustomers(_ context.Context, _, _ int) ([]customer.Customer, error) {
	return nil, nil
}

func (r *mockCustomerRepo) BumpTokenGeneration(_ context.Context, customerID string) error {
	c := r.customers[customerID]
	if c == nil {
		return apperror.NotFound("customer not found")
	}
	c.BumpTokenGeneration()
	return nil
}

func (r *mockCustomerRepo) ChangePasswordAndBumpTokenGeneration(_ context.Context, customerID, passwordHash string) error {
	c := r.customers[customerID]
	if c == nil {
		return apperror.NotFound("customer not found")
	}
	c.PasswordHash = passwordHash
	c.BumpTokenGeneration()
	return nil
}

func (r *mockCustomerRepo) WithTx(_ *sql.Tx) customer.CustomerRepository {
	return r
}

func (r *mockCustomerRepo) Delete(_ context.Context, id string) error {
	c := r.customers[id]
	if c != nil {
		delete(r.byEmail, c.Email)
	}
	delete(r.customers, id)
	return nil
}

// ── mock reset repo ──────────────────────────────────────────────────────

type mockResetRepo struct {
	tokens map[string]*customer.PasswordResetToken // keyed by token_hash
}

func newMockResetRepo() *mockResetRepo {
	return &mockResetRepo{tokens: make(map[string]*customer.PasswordResetToken)}
}

func (r *mockResetRepo) Create(_ context.Context, t *customer.PasswordResetToken) error {
	r.tokens[t.TokenHash] = t
	return nil
}

func (r *mockResetRepo) FindByTokenHash(_ context.Context, hash string) (*customer.PasswordResetToken, error) {
	return r.tokens[hash], nil
}

func (r *mockResetRepo) MarkUsed(_ context.Context, id string) error {
	for _, t := range r.tokens {
		if t.ID == id {
			now := time.Now().UTC()
			t.UsedAt = &now
			return nil
		}
	}
	return apperror.NotFound("reset token not found")
}

// ── helpers ──────────────────────────────────────────────────────────────

func newTestService(repo *mockCustomerRepo) *auth.Service {
	issuer, _ := jwt.NewIssuer("test-secret", time.Hour)
	bus := event.NewBus(testLogger{})
	return auth.NewService(repo, newMockResetRepo(), issuer, bus, testLogger{}, time.Hour)
}

// ── Register tests ───────────────────────────────────────────────────────

func TestRegister_Success(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	out, err := svc.Register(context.Background(), auth.RegisterInput{
		Email:     "alice@example.com",
		Password:  "password123",
		FirstName: "Alice",
		LastName:  "Smith",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if out.CustomerID == "" {
		t.Error("expected non-empty customer ID")
	}
	if out.Token == "" {
		t.Error("expected non-empty token")
	}

	// Verify customer was persisted.
	c := repo.customers[out.CustomerID]
	if c == nil {
		t.Fatal("customer not in repo")
	}
	if c.Email != "alice@example.com" {
		t.Errorf("Email = %q, want alice@example.com", c.Email)
	}
	if c.FirstName != "Alice" {
		t.Errorf("FirstName = %q, want Alice", c.FirstName)
	}
	if c.PasswordHash == "" {
		t.Error("expected non-empty password hash")
	}
}

func TestRegister_EmptyEmail(t *testing.T) {
	svc := newTestService(newMockRepo())
	_, err := svc.Register(context.Background(), auth.RegisterInput{
		Email: "", Password: "password123",
	})
	if err == nil {
		t.Fatal("expected error for empty email")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeValidation {
		t.Errorf("expected validation error, got %v", err)
	}
}

func TestRegister_TokenIncludesDisplayName(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)
	issuer, _ := jwt.NewIssuer("test-secret", time.Hour)

	out, err := svc.Register(context.Background(), auth.RegisterInput{
		Email:     "alice@example.com",
		Password:  "password123",
		FirstName: "Alice",
		LastName:  "Smith",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	claims, err := issuer.Parse(out.Token)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.DisplayName != "Alice Smith" {
		t.Fatalf("DisplayName = %q, want %q", claims.DisplayName, "Alice Smith")
	}
}

func TestRegister_ShortPassword(t *testing.T) {
	svc := newTestService(newMockRepo())
	_, err := svc.Register(context.Background(), auth.RegisterInput{
		Email: "alice@example.com", Password: "short",
	})
	if err == nil {
		t.Fatal("expected error for short password")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeValidation {
		t.Errorf("expected validation error, got %v", err)
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	_, _ = svc.Register(context.Background(), auth.RegisterInput{
		Email: "dup@example.com", Password: "password123",
	})
	_, err := svc.Register(context.Background(), auth.RegisterInput{
		Email: "dup@example.com", Password: "password123",
	})
	if err == nil {
		t.Fatal("expected error for duplicate email")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeConflict {
		t.Errorf("expected conflict error, got %v", err)
	}
}

func TestRegister_MixedCaseEmailStoredNormalized(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	out, err := svc.Register(context.Background(), auth.RegisterInput{
		Email: "Foo@Bar.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if repo.customers[out.CustomerID].Email != "foo@bar.com" {
		t.Fatalf("stored email = %q, want foo@bar.com", repo.customers[out.CustomerID].Email)
	}
}

func TestRegister_PaddedEmailStoredNormalized(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	out, err := svc.Register(context.Background(), auth.RegisterInput{
		Email: "  foo@bar.com  ", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if repo.customers[out.CustomerID].Email != "foo@bar.com" {
		t.Fatalf("stored email = %q, want foo@bar.com", repo.customers[out.CustomerID].Email)
	}
}

func TestRegister_DuplicateEmailCaseInsensitive(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	_, err := svc.Register(context.Background(), auth.RegisterInput{
		Email: "Foo@Bar.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	_, err = svc.Register(context.Background(), auth.RegisterInput{
		Email: "foo@bar.com", Password: "password123",
	})
	if err == nil {
		t.Fatal("expected conflict for case-variant duplicate")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeConflict {
		t.Fatalf("expected conflict error, got %v", err)
	}
}

func TestLogin_DifferentCaseSucceeds(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	_, err := svc.Register(context.Background(), auth.RegisterInput{
		Email: "Foo@Bar.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err = svc.Login(context.Background(), auth.LoginInput{
		Email: "foo@bar.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Login with different case: %v", err)
	}
}

func TestLogin_PaddedEmailSucceeds(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	_, err := svc.Register(context.Background(), auth.RegisterInput{
		Email: "foo@bar.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err = svc.Login(context.Background(), auth.LoginInput{
		Email: "  foo@bar.com  ", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Login with padded email: %v", err)
	}
}

func TestRequestPasswordReset_DifferentCaseSucceeds(t *testing.T) {
	repo := newMockRepo()
	resetRepo := newMockResetRepo()
	issuer, _ := jwt.NewIssuer("test-secret", time.Hour)
	bus := event.NewBus(testLogger{})
	svc := auth.NewService(repo, resetRepo, issuer, bus, testLogger{}, time.Hour)

	_, err := svc.Register(context.Background(), auth.RegisterInput{
		Email: "Reset@Example.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	err = svc.RequestPasswordReset(context.Background(), "reset@example.com")
	if err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	if len(resetRepo.tokens) != 1 {
		t.Fatalf("reset tokens = %d, want 1", len(resetRepo.tokens))
	}
}

func TestRequestPasswordReset_PaddedEmailSucceeds(t *testing.T) {
	repo := newMockRepo()
	resetRepo := newMockResetRepo()
	issuer, _ := jwt.NewIssuer("test-secret", time.Hour)
	bus := event.NewBus(testLogger{})
	svc := auth.NewService(repo, resetRepo, issuer, bus, testLogger{}, time.Hour)

	_, err := svc.Register(context.Background(), auth.RegisterInput{
		Email: "reset@example.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	err = svc.RequestPasswordReset(context.Background(), "  reset@example.com  ")
	if err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	if len(resetRepo.tokens) != 1 {
		t.Fatalf("reset tokens = %d, want 1", len(resetRepo.tokens))
	}
}

// ── Login tests ──────────────────────────────────────────────────────────

func TestLogin_Success(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	_, _ = svc.Register(context.Background(), auth.RegisterInput{
		Email: "bob@example.com", Password: "password123",
	})

	out, err := svc.Login(context.Background(), auth.LoginInput{
		Email: "bob@example.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if out.Token == "" {
		t.Error("expected non-empty token")
	}
	if out.CustomerID == "" {
		t.Error("expected non-empty customer ID")
	}
}

func TestLogin_TokenIncludesDisplayNameFallbackEmail(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)
	issuer, _ := jwt.NewIssuer("test-secret", time.Hour)

	_, _ = svc.Register(context.Background(), auth.RegisterInput{
		Email: "bob@example.com", Password: "password123",
	})

	out, err := svc.Login(context.Background(), auth.LoginInput{
		Email: "bob@example.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	claims, err := issuer.Parse(out.Token)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.DisplayName != "bob@example.com" {
		t.Fatalf("DisplayName = %q, want %q", claims.DisplayName, "bob@example.com")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	_, _ = svc.Register(context.Background(), auth.RegisterInput{
		Email: "bob@example.com", Password: "password123",
	})

	_, err := svc.Login(context.Background(), auth.LoginInput{
		Email: "bob@example.com", Password: "wrongpass",
	})
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeUnauthorized {
		t.Errorf("expected unauthorized error, got %v", err)
	}
}

func TestLogin_NonExistent(t *testing.T) {
	svc := newTestService(newMockRepo())

	_, err := svc.Login(context.Background(), auth.LoginInput{
		Email: "nobody@example.com", Password: "password123",
	})
	if err == nil {
		t.Fatal("expected error for non-existent user")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeUnauthorized {
		t.Errorf("expected unauthorized error, got %v", err)
	}
}

func TestUpdateProfile_Success(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	out, err := svc.Register(context.Background(), auth.RegisterInput{
		Email:     "alice@example.com",
		Password:  "password123",
		FirstName: "Alice",
		LastName:  "Smith",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	c, err := svc.UpdateProfile(context.Background(), auth.UpdateProfileInput{
		CustomerID: out.CustomerID,
		FirstName:  "Ada",
		LastName:   "Lovelace",
	})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if c.FirstName != "Ada" || c.LastName != "Lovelace" {
		t.Fatalf("profile = %q %q, want Ada Lovelace", c.FirstName, c.LastName)
	}
}

func TestChangePassword_Success(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	out, err := svc.Register(context.Background(), auth.RegisterInput{
		Email:    "bob@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	before := repo.customers[out.CustomerID].TokenGeneration
	if err := svc.ChangePassword(context.Background(), auth.ChangePasswordInput{
		CustomerID:      out.CustomerID,
		CurrentPassword: "password123",
		NewPassword:     "newpassword123",
	}); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if repo.customers[out.CustomerID].TokenGeneration != before+1 {
		t.Fatalf("token generation = %d, want %d", repo.customers[out.CustomerID].TokenGeneration, before+1)
	}
	if err := password.Compare(repo.customers[out.CustomerID].PasswordHash, "newpassword123"); err != nil {
		t.Fatalf("new password not persisted: %v", err)
	}
	if _, err := svc.Login(context.Background(), auth.LoginInput{Email: "bob@example.com", Password: "password123"}); err == nil {
		t.Fatal("expected old password to be rejected")
	}
	if _, err := svc.Login(context.Background(), auth.LoginInput{Email: "bob@example.com", Password: "newpassword123"}); err != nil {
		t.Fatalf("expected new password to succeed: %v", err)
	}
}

func TestChangePassword_WrongCurrentPassword(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	out, err := svc.Register(context.Background(), auth.RegisterInput{
		Email:    "carol@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	err = svc.ChangePassword(context.Background(), auth.ChangePasswordInput{
		CustomerID:      out.CustomerID,
		CurrentPassword: "wrong-password",
		NewPassword:     "newpassword123",
	})
	if err == nil {
		t.Fatal("expected error for wrong current password")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeUnauthorized {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
}

func TestVerifyPassword_Success(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	out, err := svc.Register(context.Background(), auth.RegisterInput{
		Email:    "verify@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := svc.VerifyPassword(context.Background(), out.CustomerID, "password123"); err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
}

func TestVerifyPassword_WrongPassword(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	out, err := svc.Register(context.Background(), auth.RegisterInput{
		Email:    "verify-wrong@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	err = svc.VerifyPassword(context.Background(), out.CustomerID, "wrong-password")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeUnauthorized {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
}

func TestVerifyPassword_DisabledAccount(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	out, err := svc.Register(context.Background(), auth.RegisterInput{
		Email:    "verify-disabled@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := repo.customers[out.CustomerID].Disable(); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	err = svc.VerifyPassword(context.Background(), out.CustomerID, "password123")
	if err == nil {
		t.Fatal("expected error for disabled account")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeUnauthorized {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
	if appErr.Message != "account is not active" {
		t.Fatalf("message = %q, want %q", appErr.Message, "account is not active")
	}
}

func TestLogin_DisabledAccount(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	out, _ := svc.Register(context.Background(), auth.RegisterInput{
		Email: "disabled@example.com", Password: "password123",
	})
	c := repo.customers[out.CustomerID]
	_ = c.Disable()

	_, err := svc.Login(context.Background(), auth.LoginInput{
		Email: "disabled@example.com", Password: "password123",
	})
	if err == nil {
		t.Fatal("expected error for disabled account")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeUnauthorized {
		t.Errorf("expected unauthorized error, got %v", err)
	}
}

func TestLogin_EmptyEmail(t *testing.T) {
	svc := newTestService(newMockRepo())
	_, err := svc.Login(context.Background(), auth.LoginInput{
		Email: "", Password: "password123",
	})
	if err == nil {
		t.Fatal("expected error for empty email")
	}
}

func TestLogin_EmptyPassword(t *testing.T) {
	svc := newTestService(newMockRepo())
	_, err := svc.Login(context.Background(), auth.LoginInput{
		Email: "bob@example.com", Password: "",
	})
	if err == nil {
		t.Fatal("expected error for empty password")
	}
}

// ── Me tests ─────────────────────────────────────────────────────────────

func TestMe_Success(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	out, _ := svc.Register(context.Background(), auth.RegisterInput{
		Email: "me@example.com", Password: "password123",
	})

	c, err := svc.Me(context.Background(), out.CustomerID)
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if c.Email != "me@example.com" {
		t.Errorf("Email = %q, want me@example.com", c.Email)
	}
}

func TestMe_NotFound(t *testing.T) {
	svc := newTestService(newMockRepo())
	_, err := svc.Me(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent customer")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeNotFound {
		t.Errorf("expected not_found error, got %v", err)
	}
}

// ── Verify password is hashed ────────────────────────────────────────────

func TestRegister_PasswordIsHashed(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	out, _ := svc.Register(context.Background(), auth.RegisterInput{
		Email: "hash@example.com", Password: "password123",
	})

	c := repo.customers[out.CustomerID]
	if c.PasswordHash == "password123" {
		t.Error("password hash should not equal plaintext")
	}
	if err := password.Compare(c.PasswordHash, "password123"); err != nil {
		t.Errorf("password hash verification failed: %v", err)
	}
}

// ── Register: ExpiresAt ──────────────────────────────────────────────────

func TestRegister_ExpiresAt(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	out, err := svc.Register(context.Background(), auth.RegisterInput{
		Email: "exp@example.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if out.ExpiresAt.IsZero() {
		t.Error("expected non-zero ExpiresAt")
	}
	if !out.ExpiresAt.After(time.Now()) {
		t.Error("ExpiresAt should be in the future")
	}
}

// ── Login: ExpiresAt ─────────────────────────────────────────────────────

func TestLogin_ExpiresAt(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	_, _ = svc.Register(context.Background(), auth.RegisterInput{
		Email: "exp@example.com", Password: "password123",
	})
	out, err := svc.Login(context.Background(), auth.LoginInput{
		Email: "exp@example.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if out.ExpiresAt.IsZero() {
		t.Error("expected non-zero ExpiresAt")
	}
}

// ── Logout tests ─────────────────────────────────────────────────────────

func TestLogout_Success(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	out, _ := svc.Register(context.Background(), auth.RegisterInput{
		Email: "logout@example.com", Password: "password123",
	})

	err := svc.Logout(context.Background(), out.CustomerID)
	if err != nil {
		t.Fatalf("Logout: %v", err)
	}

	// TokenGeneration should be bumped.
	c := repo.customers[out.CustomerID]
	if c.TokenGeneration != 1 {
		t.Errorf("TokenGeneration = %d, want 1", c.TokenGeneration)
	}
}

func TestLogout_NotFound(t *testing.T) {
	svc := newTestService(newMockRepo())
	err := svc.Logout(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent customer")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeNotFound {
		t.Errorf("expected not_found error, got %v", err)
	}
}

// ── RequestPasswordReset tests ───────────────────────────────────────────

func TestRequestPasswordReset_Success(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	_, _ = svc.Register(context.Background(), auth.RegisterInput{
		Email: "reset@example.com", Password: "password123",
	})

	err := svc.RequestPasswordReset(context.Background(), "reset@example.com")
	if err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
}

func TestRequestPasswordReset_NonExistent_NoError(t *testing.T) {
	svc := newTestService(newMockRepo())
	err := svc.RequestPasswordReset(context.Background(), "nobody@example.com")
	if err != nil {
		t.Fatalf("expected no error for non-existent email, got %v", err)
	}
}

func TestRequestPasswordReset_EmptyEmail(t *testing.T) {
	svc := newTestService(newMockRepo())
	err := svc.RequestPasswordReset(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty email")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeValidation {
		t.Errorf("expected validation error, got %v", err)
	}
}

func TestRequestSecurityVerificationLink_Success(t *testing.T) {
	repo := newMockRepo()
	issuer, _ := jwt.NewIssuer("test-secret", time.Hour)
	bus := event.NewBus(testLogger{})
	var published customer.SecurityVerificationRequestedData
	bus.On(customer.EventSecurityVerificationRequested, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(customer.SecurityVerificationRequestedData)
		if !ok {
			t.Fatalf("event data type = %T", evt.Data)
		}
		published = data
		return nil
	})
	svc := auth.NewService(repo, newMockResetRepo(), issuer, bus, testLogger{}, time.Hour)

	out, err := svc.Register(context.Background(), auth.RegisterInput{
		Email:    "verify@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	err = svc.RequestSecurityVerificationLink(context.Background(), out.CustomerID, "https://shop.test/account/security/verify?email_token=abc")
	if err != nil {
		t.Fatalf("RequestSecurityVerificationLink: %v", err)
	}
	if published.CustomerID != out.CustomerID {
		t.Fatalf("published.CustomerID = %q, want %q", published.CustomerID, out.CustomerID)
	}
	if published.VerifyURL != "https://shop.test/account/security/verify?email_token=abc" {
		t.Fatalf("published.VerifyURL = %q", published.VerifyURL)
	}
}

func TestRequestSecurityVerificationLink_EmptyCustomerID(t *testing.T) {
	svc := newTestService(newMockRepo())
	err := svc.RequestSecurityVerificationLink(context.Background(), "", "https://shop.test/account/security/verify?email_token=abc")
	if err == nil {
		t.Fatal("expected error for empty customer id")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeValidation {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestRequestSecurityVerificationLink_CustomerNotFound(t *testing.T) {
	svc := newTestService(newMockRepo())
	err := svc.RequestSecurityVerificationLink(context.Background(), "missing", "https://shop.test/account/security/verify?email_token=abc")
	if err == nil {
		t.Fatal("expected error for missing customer")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeNotFound {
		t.Fatalf("expected not_found error, got %v", err)
	}
}

func TestRequestSecurityVerificationLink_DisabledAccount(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)
	out, err := svc.Register(context.Background(), auth.RegisterInput{
		Email:    "disabled-verify@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := repo.customers[out.CustomerID].Disable(); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	err = svc.RequestSecurityVerificationLink(context.Background(), out.CustomerID, "https://shop.test/account/security/verify?email_token=abc")
	if err == nil {
		t.Fatal("expected error for disabled account")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeUnauthorized {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
}

func TestRequestEmailVerificationLink_Success(t *testing.T) {
	repo := newMockRepo()
	issuer, _ := jwt.NewIssuer("test-secret", time.Hour)
	bus := event.NewBus(testLogger{})
	var published customer.EmailVerificationRequestedData
	bus.On(customer.EventEmailVerificationRequested, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(customer.EmailVerificationRequestedData)
		if !ok {
			t.Fatalf("event data type = %T", evt.Data)
		}
		published = data
		return nil
	})
	svc := auth.NewService(repo, newMockResetRepo(), issuer, bus, testLogger{}, time.Hour)

	out, err := svc.Register(context.Background(), auth.RegisterInput{
		Email:    "verify-email@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	err = svc.RequestEmailVerificationLink(context.Background(), out.CustomerID, "https://shop.test/account/verify-email?email_token=abc")
	if err != nil {
		t.Fatalf("RequestEmailVerificationLink: %v", err)
	}
	if published.CustomerID != out.CustomerID {
		t.Fatalf("published.CustomerID = %q, want %q", published.CustomerID, out.CustomerID)
	}
	if published.VerifyURL != "https://shop.test/account/verify-email?email_token=abc" {
		t.Fatalf("published.VerifyURL = %q", published.VerifyURL)
	}
}

func TestRequestEmailVerificationLink_AlreadyVerified_NoEvent(t *testing.T) {
	repo := newMockRepo()
	issuer, _ := jwt.NewIssuer("test-secret", time.Hour)
	bus := event.NewBus(testLogger{})
	published := false
	bus.On(customer.EventEmailVerificationRequested, func(_ context.Context, evt event.Event) error {
		published = true
		return nil
	})
	svc := auth.NewService(repo, newMockResetRepo(), issuer, bus, testLogger{}, time.Hour)

	out, err := svc.Register(context.Background(), auth.RegisterInput{
		Email:    "already-verified@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	repo.customers[out.CustomerID].MarkEmailVerified()

	err = svc.RequestEmailVerificationLink(context.Background(), out.CustomerID, "https://shop.test/account/verify-email?email_token=abc")
	if err != nil {
		t.Fatalf("RequestEmailVerificationLink: %v", err)
	}
	if published {
		t.Fatal("expected already verified customer to skip publishing")
	}
}

func TestMarkEmailVerified_Success(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)
	out, err := svc.Register(context.Background(), auth.RegisterInput{
		Email:    "mark-verified@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	err = svc.MarkEmailVerified(context.Background(), out.CustomerID)
	if err != nil {
		t.Fatalf("MarkEmailVerified: %v", err)
	}
	if repo.customers[out.CustomerID].EmailVerifiedAt == nil {
		t.Fatal("expected EmailVerifiedAt to be set")
	}
}

// ── ConfirmPasswordReset tests ───────────────────────────────────────────

func TestConfirmPasswordReset_EmptyToken(t *testing.T) {
	svc := newTestService(newMockRepo())
	err := svc.ConfirmPasswordReset(context.Background(), auth.ConfirmPasswordResetInput{
		Token: "", NewPassword: "newpassword123",
	})
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestConfirmPasswordReset_ShortPassword(t *testing.T) {
	svc := newTestService(newMockRepo())
	err := svc.ConfirmPasswordReset(context.Background(), auth.ConfirmPasswordResetInput{
		Token: "some-token", NewPassword: "short",
	})
	if err == nil {
		t.Fatal("expected error for short password")
	}
}

func TestConfirmPasswordReset_InvalidToken(t *testing.T) {
	svc := newTestService(newMockRepo())
	err := svc.ConfirmPasswordReset(context.Background(), auth.ConfirmPasswordResetInput{
		Token: "invalid-token", NewPassword: "newpassword123",
	})
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeUnauthorized {
		t.Errorf("expected unauthorized error, got %v", err)
	}
}

func TestConfirmPasswordReset_Success(t *testing.T) {
	repo := newMockRepo()
	resetRepo := newMockResetRepo()
	issuer, _ := jwt.NewIssuer("test-secret", time.Hour)
	bus := event.NewBus(testLogger{})
	svc := auth.NewService(repo, resetRepo, issuer, bus, testLogger{}, time.Hour)

	// Register a customer.
	out, err := svc.Register(context.Background(), auth.RegisterInput{
		Email: "reset@example.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	c := repo.customers[out.CustomerID]
	oldHash := c.PasswordHash

	// Seed a valid reset token directly into the mock repo.
	plaintext := "test-reset-token"
	hash := customer.HashToken(plaintext)
	rt := &customer.PasswordResetToken{
		ID:         "rt-1",
		CustomerID: out.CustomerID,
		TokenHash:  hash,
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		CreatedAt:  time.Now().UTC(),
	}
	resetRepo.tokens[hash] = rt

	// Confirm the reset.
	err = svc.ConfirmPasswordReset(context.Background(), auth.ConfirmPasswordResetInput{
		Token:       plaintext,
		NewPassword: "new-password-123",
	})
	if err != nil {
		t.Fatalf("ConfirmPasswordReset: %v", err)
	}

	// Verify password was changed.
	c = repo.customers[out.CustomerID]
	if c.PasswordHash == oldHash {
		t.Error("expected password hash to change")
	}
	if err := password.Compare(c.PasswordHash, "new-password-123"); err != nil {
		t.Error("new password should verify")
	}

	// Verify token was marked used.
	if rt.UsedAt == nil {
		t.Error("expected reset token to be marked used")
	}

	// Verify token generation was bumped.
	if c.TokenGeneration < 1 {
		t.Errorf("TokenGeneration = %d, want >= 1", c.TokenGeneration)
	}
}
