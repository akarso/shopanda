package mfa

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/customer"
	mfadomain "github.com/akarso/shopanda/internal/domain/mfa"
	"github.com/akarso/shopanda/internal/domain/security"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/mfasec"
	"github.com/akarso/shopanda/internal/platform/password"
)

const pendingLoginTTL = 5 * time.Minute
const totpIssuer = "Shopanda Admin"

// Service manages admin TOTP enrollment and login verification.
type Service struct {
	repo         mfadomain.Repository
	customers    customer.CustomerRepository
	config       security.ConfigGetter
	jwtSecret    string
	deployEnabled bool
}

// NewService creates an MFA service.
func NewService(
	repo mfadomain.Repository,
	customers customer.CustomerRepository,
	config security.ConfigGetter,
	jwtSecret string,
	deployEnabled bool,
) *Service {
	if repo == nil {
		panic("mfa.NewService: nil repository")
	}
	if customers == nil {
		panic("mfa.NewService: nil customers repository")
	}
	if jwtSecret == "" {
		panic("mfa.NewService: empty jwt secret")
	}
	return &Service{
		repo:          repo,
		customers:     customers,
		config:        config,
		jwtSecret:     jwtSecret,
		deployEnabled: deployEnabled,
	}
}

// Status describes MFA enrollment for the current user.
type Status struct {
	Enrolled        bool `json:"enrolled"`
	PolicyRequired  bool `json:"policy_required"`
	DeployEnabled   bool `json:"deploy_enabled"`
}

// EnrollBeginResult holds enrollment setup data shown once.
type EnrollBeginResult struct {
	OTPAUTHURL string `json:"otpauth_url"`
	Secret     string `json:"secret"`
}

// EnrollConfirmResult returns recovery codes once after confirmation.
type EnrollConfirmResult struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

// RequiredForLogin reports whether MFA must complete before issuing a JWT.
func (s *Service) RequiredForLogin(ctx context.Context, c *customer.Customer) (bool, error) {
	if c == nil || !customer.IsAdminRole(c.Role) {
		return false, nil
	}
	if !s.deployEnabled {
		return false, nil
	}
	required, err := security.AdminMFARequired(ctx, s.config, "")
	if err != nil {
		return false, err
	}
	if !required {
		return false, nil
	}
	state, err := s.repo.GetState(ctx, c.ID)
	if err != nil {
		return false, fmt.Errorf("mfa: get state: %w", err)
	}
	return state.Enrolled(), nil
}

// IssuePendingLogin creates a short-lived token after password verification.
func (s *Service) IssuePendingLogin(c *customer.Customer) (string, time.Time, error) {
	token, expiresAt, err := mfasec.SignPendingLogin(s.jwtSecret, mfasec.PendingLoginClaims{
		CustomerID:      c.ID,
		TokenGeneration: c.TokenGeneration,
	}, pendingLoginTTL)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("mfa: pending login: %w", err)
	}
	return token, expiresAt, nil
}

// CompleteLogin validates MFA and returns the authenticated customer.
func (s *Service) CompleteLogin(ctx context.Context, pendingToken, code string) (*customer.Customer, error) {
	code = strings.TrimSpace(code)
	if pendingToken == "" || code == "" {
		return nil, apperror.Validation("pending token and code are required")
	}
	claims, err := mfasec.VerifyPendingLogin(s.jwtSecret, pendingToken)
	if err != nil {
		return nil, apperror.Unauthorized("invalid or expired login challenge")
	}
	c, err := s.customers.FindByID(ctx, claims.CustomerID)
	if err != nil {
		return nil, fmt.Errorf("mfa: find customer: %w", err)
	}
	if c == nil || c.Status != customer.StatusActive {
		return nil, apperror.Unauthorized("invalid or expired login challenge")
	}
	if c.TokenGeneration != claims.TokenGeneration {
		return nil, apperror.Unauthorized("invalid or expired login challenge")
	}
	if !customer.IsAdminRole(c.Role) {
		return nil, apperror.Unauthorized("invalid or expired login challenge")
	}
	if ok, err := s.validateCode(ctx, c.ID, code); err != nil {
		return nil, err
	} else if !ok {
		return nil, apperror.Unauthorized("invalid authentication code")
	}
	return c, nil
}

// GetStatus returns enrollment and policy state for a customer.
func (s *Service) GetStatus(ctx context.Context, customerID string) (Status, error) {
	state, err := s.repo.GetState(ctx, customerID)
	if err != nil {
		return Status{}, fmt.Errorf("mfa: status: %w", err)
	}
	required, err := security.AdminMFARequired(ctx, s.config, "")
	if err != nil {
		return Status{}, err
	}
	return Status{
		Enrolled:       state.Enrolled(),
		PolicyRequired: required,
		DeployEnabled:  s.deployEnabled,
	}, nil
}

// BeginEnrollment starts TOTP setup for an admin user.
func (s *Service) BeginEnrollment(ctx context.Context, customerID string) (*EnrollBeginResult, error) {
	c, err := s.loadAdmin(ctx, customerID)
	if err != nil {
		return nil, err
	}
	if !s.deployEnabled {
		return nil, apperror.Validation("admin mfa is disabled")
	}
	secret, err := mfasec.GenerateTOTPSecret()
	if err != nil {
		return nil, fmt.Errorf("mfa: generate secret: %w", err)
	}
	enc, err := mfasec.EncryptSecret(secret, s.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("mfa: encrypt secret: %w", err)
	}
	if err := s.repo.SavePendingSecret(ctx, customerID, enc); err != nil {
		return nil, fmt.Errorf("mfa: save pending secret: %w", err)
	}
	return &EnrollBeginResult{
		OTPAUTHURL: mfasec.OTPAuthURL(totpIssuer, c.Email, secret),
		Secret:     secret,
	}, nil
}

// ConfirmEnrollment verifies the first TOTP code and issues recovery codes.
func (s *Service) ConfirmEnrollment(ctx context.Context, customerID, code string) (*EnrollConfirmResult, error) {
	if strings.TrimSpace(code) == "" {
		return nil, apperror.Validation("code is required")
	}
	if _, err := s.loadAdmin(ctx, customerID); err != nil {
		return nil, err
	}
	state, err := s.repo.GetState(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("mfa: confirm get state: %w", err)
	}
	if state.Enrolled() {
		return nil, apperror.Validation("mfa is already enrolled")
	}
	if state.SecretEnc == "" {
		return nil, apperror.Validation("enrollment has not started")
	}
	secret, err := mfasec.DecryptSecret(state.SecretEnc, s.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("mfa: decrypt secret: %w", err)
	}
	if !mfasec.ValidateTOTP(code, secret) {
		return nil, apperror.Validation("invalid authentication code")
	}
	now := time.Now().UTC()
	if err := s.repo.ConfirmEnrollment(ctx, customerID, now); err != nil {
		return nil, fmt.Errorf("mfa: confirm enrollment: %w", err)
	}
	plaintext, hashes, err := mfadomain.NewRecoveryCodes(0)
	if err != nil {
		return nil, fmt.Errorf("mfa: recovery codes: %w", err)
	}
	if err := s.repo.ReplaceRecoveryCodes(ctx, customerID, hashes); err != nil {
		return nil, fmt.Errorf("mfa: save recovery codes: %w", err)
	}
	return &EnrollConfirmResult{RecoveryCodes: plaintext}, nil
}

// Disable removes MFA after password confirmation and revokes sessions.
func (s *Service) Disable(ctx context.Context, customerID, currentPassword string) error {
	c, err := s.loadAdmin(ctx, customerID)
	if err != nil {
		return err
	}
	if err := verifyPassword(c, currentPassword); err != nil {
		return err
	}
	if err := s.repo.ClearEnrollment(ctx, customerID); err != nil {
		return fmt.Errorf("mfa: disable: %w", err)
	}
	return s.customers.BumpTokenGeneration(ctx, customerID)
}

// RegenerateRecoveryCodes replaces recovery codes after password confirmation.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, customerID, currentPassword string) (*EnrollConfirmResult, error) {
	c, err := s.loadAdmin(ctx, customerID)
	if err != nil {
		return nil, err
	}
	if err := verifyPassword(c, currentPassword); err != nil {
		return nil, err
	}
	state, err := s.repo.GetState(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("mfa: regenerate get state: %w", err)
	}
	if !state.Enrolled() {
		return nil, apperror.Validation("mfa is not enrolled")
	}
	plaintext, hashes, err := mfadomain.NewRecoveryCodes(0)
	if err != nil {
		return nil, fmt.Errorf("mfa: recovery codes: %w", err)
	}
	if err := s.repo.ReplaceRecoveryCodes(ctx, customerID, hashes); err != nil {
		return nil, fmt.Errorf("mfa: save recovery codes: %w", err)
	}
	return &EnrollConfirmResult{RecoveryCodes: plaintext}, nil
}

func (s *Service) validateCode(ctx context.Context, customerID, code string) (bool, error) {
	state, err := s.repo.GetState(ctx, customerID)
	if err != nil {
		return false, fmt.Errorf("mfa: validate code state: %w", err)
	}
	if !state.Enrolled() {
		return false, nil
	}
	secret, err := mfasec.DecryptSecret(state.SecretEnc, s.jwtSecret)
	if err != nil {
		return false, fmt.Errorf("mfa: decrypt secret: %w", err)
	}
	if mfasec.ValidateTOTP(code, secret) {
		return true, nil
	}
	consumed, err := s.repo.ConsumeRecoveryCode(ctx, customerID, customer.HashToken(normalizeRecoveryInput(code)))
	if err != nil {
		return false, fmt.Errorf("mfa: consume recovery code: %w", err)
	}
	return consumed, nil
}

func (s *Service) loadAdmin(ctx context.Context, customerID string) (*customer.Customer, error) {
	if customerID == "" {
		return nil, apperror.Validation("customer id is required")
	}
	c, err := s.customers.FindByID(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("mfa: find customer: %w", err)
	}
	if c == nil || !customer.IsAdminRole(c.Role) {
		return nil, apperror.Forbidden("admin access required")
	}
	if c.Status != customer.StatusActive {
		return nil, apperror.Forbidden("account is disabled")
	}
	return c, nil
}

func verifyPassword(c *customer.Customer, raw string) error {
	if raw == "" {
		return apperror.Validation("password is required")
	}
	if err := password.Compare(c.PasswordHash, raw); err != nil {
		return apperror.Unauthorized("invalid password")
	}
	return nil
}

func normalizeRecoveryInput(code string) string {
	out := make([]byte, 0, len(code))
	for i := 0; i < len(code); i++ {
		switch c := code[i]; c {
		case '-', ' ':
			continue
		default:
			out = append(out, c)
		}
	}
	return string(out)
}
