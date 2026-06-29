package mfa_test

import (
	"context"
	"testing"
	"time"

	mfaApp "github.com/akarso/shopanda/internal/application/mfa"
	"github.com/akarso/shopanda/internal/domain/customer"
	mfadomain "github.com/akarso/shopanda/internal/domain/mfa"
	"github.com/akarso/shopanda/internal/platform/mfasec"
	"github.com/akarso/shopanda/internal/platform/password"
)

type stubMFARepo struct {
	states map[string]mfadomain.State
	codes  map[string][]string
}

func (s *stubMFARepo) GetState(_ context.Context, customerID string) (mfadomain.State, error) {
	return s.states[customerID], nil
}

func (s *stubMFARepo) SavePendingSecret(_ context.Context, customerID, secretEnc string) error {
	if s.states == nil {
		s.states = map[string]mfadomain.State{}
	}
	s.states[customerID] = mfadomain.State{SecretEnc: secretEnc}
	return nil
}

func (s *stubMFARepo) FinalizeEnrollment(_ context.Context, customerID string, confirmedAt time.Time, codeHashes []string) error {
	state := s.states[customerID]
	state.ConfirmedAt = &confirmedAt
	s.states[customerID] = state
	if s.codes == nil {
		s.codes = map[string][]string{}
	}
	s.codes[customerID] = append([]string(nil), codeHashes...)
	return nil
}

func (s *stubMFARepo) ClearEnrollment(_ context.Context, customerID string) error {
	delete(s.states, customerID)
	delete(s.codes, customerID)
	return nil
}

func (s *stubMFARepo) ReplaceRecoveryCodes(_ context.Context, customerID string, codeHashes []string) error {
	if s.codes == nil {
		s.codes = map[string][]string{}
	}
	s.codes[customerID] = append([]string(nil), codeHashes...)
	return nil
}

func (s *stubMFARepo) ConsumeRecoveryCode(_ context.Context, customerID, codeHash string) (bool, error) {
	for i, hash := range s.codes[customerID] {
		if hash == codeHash {
			s.codes[customerID] = append(s.codes[customerID][:i], s.codes[customerID][i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

type stubConfigRepo struct {
	values map[string]interface{}
}

func (s *stubConfigRepo) Get(_ context.Context, key string) (interface{}, error) {
	if s.values == nil {
		return nil, nil
	}
	return s.values[key], nil
}

type stubCustomerRepo struct {
	byID map[string]*customer.Customer
}

func (s *stubCustomerRepo) FindByID(_ context.Context, id string) (*customer.Customer, error) {
	return s.byID[id], nil
}

func (s *stubCustomerRepo) FindByEmail(context.Context, string) (*customer.Customer, error) {
	return nil, nil
}
func (s *stubCustomerRepo) Create(context.Context, *customer.Customer) error { return nil }
func (s *stubCustomerRepo) Update(context.Context, *customer.Customer) error { return nil }
func (s *stubCustomerRepo) ListCustomers(context.Context, int, int) ([]customer.Customer, error) {
	return nil, nil
}
func (s *stubCustomerRepo) BumpTokenGeneration(_ context.Context, id string) error {
	if c := s.byID[id]; c != nil {
		c.TokenGeneration++
	}
	return nil
}
func (s *stubCustomerRepo) ChangePasswordAndBumpTokenGeneration(context.Context, string, string) error {
	return nil
}
func (s *stubCustomerRepo) Delete(context.Context, string) error { return nil }

func TestService_EnrollAndLoginWithTOTP(t *testing.T) {
	hash, err := password.Hash("password123")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	admin := &customer.Customer{
		ID:              "u1",
		Email:           "admin@example.com",
		Role:            customer.RoleAdmin,
		Status:          customer.StatusActive,
		PasswordHash:    hash,
		TokenGeneration: 1,
	}
	repo := &stubMFARepo{states: map[string]mfadomain.State{}}
	customers := &stubCustomerRepo{byID: map[string]*customer.Customer{"u1": admin}}
	configRepo := &stubConfigRepo{values: map[string]interface{}{
		"security.admin_mfa_required": true,
	}}
	svc := mfaApp.NewService(repo, customers, configRepo, "test-secret", true)

	begin, err := svc.BeginEnrollment(context.Background(), "u1")
	if err != nil {
		t.Fatalf("BeginEnrollment: %v", err)
	}
	code, err := mfasec.GenerateTOTPCode(begin.Secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	if _, err := svc.ConfirmEnrollment(context.Background(), "u1", code); err != nil {
		t.Fatalf("ConfirmEnrollment: %v", err)
	}

	required, err := svc.RequiredForLogin(context.Background(), admin)
	if err != nil {
		t.Fatalf("RequiredForLogin: %v", err)
	}
	if !required {
		t.Fatal("expected mfa required")
	}

	pending, _, err := svc.IssuePendingLogin(admin)
	if err != nil {
		t.Fatalf("IssuePendingLogin: %v", err)
	}
	code, err = mfasec.GenerateTOTPCode(begin.Secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	out, err := svc.CompleteLogin(context.Background(), pending, code)
	if err != nil {
		t.Fatalf("CompleteLogin: %v", err)
	}
	if out.ID != "u1" {
		t.Fatalf("customer id = %q, want u1", out.ID)
	}
}

func TestService_RequiredForLogin_PolicyWithoutEnrollment(t *testing.T) {
	admin := &customer.Customer{
		ID:     "u1",
		Role:   customer.RoleAdmin,
		Status: customer.StatusActive,
	}
	svc := mfaApp.NewService(
		&stubMFARepo{states: map[string]mfadomain.State{}},
		&stubCustomerRepo{byID: map[string]*customer.Customer{"u1": admin}},
		&stubConfigRepo{values: map[string]interface{}{"security.admin_mfa_required": true}},
		"test-secret",
		true,
	)

	required, err := svc.RequiredForLogin(context.Background(), admin)
	if err != nil {
		t.Fatalf("RequiredForLogin: %v", err)
	}
	if !required {
		t.Fatal("expected policy-required admin to require mfa challenge")
	}
}

func TestService_CompleteLogin_LocksOutAfterFailures(t *testing.T) {
	hash, err := password.Hash("password123")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	admin := &customer.Customer{
		ID:              "u1",
		Email:           "admin@example.com",
		Role:            customer.RoleAdmin,
		Status:          customer.StatusActive,
		PasswordHash:    hash,
		TokenGeneration: 1,
	}
	repo := &stubMFARepo{states: map[string]mfadomain.State{}}
	customers := &stubCustomerRepo{byID: map[string]*customer.Customer{"u1": admin}}
	svc := mfaApp.NewService(repo, customers, &stubConfigRepo{}, "test-secret", true)

	begin, err := svc.BeginEnrollment(context.Background(), "u1")
	if err != nil {
		t.Fatalf("BeginEnrollment: %v", err)
	}
	code, err := mfasec.GenerateTOTPCode(begin.Secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateTOTPCode: %v", err)
	}
	if _, err := svc.ConfirmEnrollment(context.Background(), "u1", code); err != nil {
		t.Fatalf("ConfirmEnrollment: %v", err)
	}

	pending, _, err := svc.IssuePendingLogin(admin)
	if err != nil {
		t.Fatalf("IssuePendingLogin: %v", err)
	}
	for i := 0; i < 5; i++ {
		_, _ = svc.CompleteLogin(context.Background(), pending, "000000")
	}
	_, err = svc.CompleteLogin(context.Background(), pending, "000000")
	if err == nil {
		t.Fatal("expected lockout after repeated failures")
	}
}

func TestPendingLoginToken_RoundTrip(t *testing.T) {
	token, _, err := mfasec.SignPendingLogin("secret", mfasec.PendingLoginClaims{
		CustomerID:      "u1",
		TokenGeneration: 2,
	}, time.Minute)
	if err != nil {
		t.Fatalf("SignPendingLogin: %v", err)
	}
	claims, err := mfasec.VerifyPendingLogin("secret", token)
	if err != nil {
		t.Fatalf("VerifyPendingLogin: %v", err)
	}
	if claims.CustomerID != "u1" || claims.TokenGeneration != 2 {
		t.Fatalf("claims = %+v", claims)
	}
}
