package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/jwt"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/password"
)

// Service orchestrates registration and login use cases.
type Service struct {
	customers customer.CustomerRepository
	resets    customer.PasswordResetRepository
	jwt       *jwt.Issuer
	bus       *event.Bus
	log       logger.Logger
	resetTTL  time.Duration
}

// NewService creates an auth application service.
func NewService(
	customers customer.CustomerRepository,
	resets customer.PasswordResetRepository,
	jwtIssuer *jwt.Issuer,
	bus *event.Bus,
	log logger.Logger,
	resetTTL time.Duration,
) *Service {
	return &Service{
		customers: customers,
		resets:    resets,
		jwt:       jwtIssuer,
		bus:       bus,
		log:       log,
		resetTTL:  resetTTL,
	}
}

// RegisterInput contains the fields for customer registration.
type RegisterInput struct {
	Email     string
	Password  string
	FirstName string
	LastName  string
}

// RegisterOutput is the result of a successful registration.
type RegisterOutput struct {
	CustomerID string
	Token      string
	ExpiresAt  time.Time
}

// Register creates a new customer account and returns a JWT.
func (s *Service) Register(ctx context.Context, in RegisterInput) (RegisterOutput, error) {
	if in.Email == "" {
		return RegisterOutput{}, apperror.Validation("email is required")
	}
	if in.Password == "" {
		return RegisterOutput{}, apperror.Validation("password is required")
	}
	if len(in.Password) < 8 {
		return RegisterOutput{}, apperror.Validation("password must be at least 8 characters")
	}

	// Check uniqueness.
	existing, err := s.customers.FindByEmail(ctx, in.Email)
	if err != nil {
		return RegisterOutput{}, fmt.Errorf("auth service: register: %w", err)
	}
	if existing != nil {
		return RegisterOutput{}, apperror.Conflict("email already registered")
	}

	hash, err := password.Hash(in.Password)
	if err != nil {
		return RegisterOutput{}, fmt.Errorf("auth service: hash password: %w", err)
	}

	c, err := customer.NewCustomer(id.New(), in.Email)
	if err != nil {
		return RegisterOutput{}, fmt.Errorf("auth service: new customer: %w", err)
	}
	c.FirstName = in.FirstName
	c.LastName = in.LastName
	if err := c.SetPassword(hash); err != nil {
		return RegisterOutput{}, fmt.Errorf("auth service: set password: %w", err)
	}

	if err := s.customers.Create(ctx, &c); err != nil {
		return RegisterOutput{}, fmt.Errorf("auth service: create customer: %w", err)
	}

	expiresAt := time.Now().UTC().Add(s.jwt.TTL())
	token, err := s.jwt.Create(c.ID, string(identity.RoleCustomer), c.TokenGeneration)
	if err != nil {
		return RegisterOutput{}, fmt.Errorf("auth service: create token: %w", err)
	}

	if err := s.bus.Publish(ctx, event.New(customer.EventCustomerCreated, "auth.service", customer.CustomerCreatedData{
		CustomerID: c.ID,
	})); err != nil {
		s.log.Warn("auth.event.publish_failed", map[string]interface{}{
			"event": customer.EventCustomerCreated,
			"error": err.Error(),
		})
	}

	s.log.Info("auth.registered", map[string]interface{}{
		"customer_id": c.ID,
	})

	return RegisterOutput{
		CustomerID: c.ID,
		Token:      token,
		ExpiresAt:  expiresAt,
	}, nil
}

// LoginInput contains the fields for customer login.
type LoginInput struct {
	Email    string
	Password string
}

// LoginOutput is the result of a successful login.
type LoginOutput struct {
	CustomerID string
	Token      string
	ExpiresAt  time.Time
}

// Login authenticates a customer and returns a JWT.
func (s *Service) Login(ctx context.Context, in LoginInput) (LoginOutput, error) {
	if in.Email == "" {
		return LoginOutput{}, apperror.Validation("email is required")
	}
	if in.Password == "" {
		return LoginOutput{}, apperror.Validation("password is required")
	}

	c, err := s.customers.FindByEmail(ctx, in.Email)
	if err != nil {
		return LoginOutput{}, fmt.Errorf("auth service: login: %w", err)
	}
	if c == nil {
		return LoginOutput{}, apperror.Unauthorized("invalid email or password")
	}

	if c.Status != customer.StatusActive {
		return LoginOutput{}, apperror.Unauthorized("invalid email or password")
	}

	if err := password.Compare(c.PasswordHash, in.Password); err != nil {
		return LoginOutput{}, apperror.Unauthorized("invalid email or password")
	}

	expiresAt := time.Now().UTC().Add(s.jwt.TTL())
	token, err := s.jwt.Create(c.ID, string(identity.RoleCustomer), c.TokenGeneration)
	if err != nil {
		return LoginOutput{}, fmt.Errorf("auth service: create token: %w", err)
	}

	s.log.Info("auth.login", map[string]interface{}{
		"customer_id": c.ID,
	})

	return LoginOutput{
		CustomerID: c.ID,
		Token:      token,
		ExpiresAt:  expiresAt,
	}, nil
}

// Me returns the customer for the given authenticated customer ID.
func (s *Service) Me(ctx context.Context, customerID string) (*customer.Customer, error) {
	c, err := s.customers.FindByID(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("auth service: me: %w", err)
	}
	if c == nil {
		return nil, apperror.NotFound("customer not found")
	}
	return c, nil
}

// Logout invalidates all tokens for the given customer by bumping
// their token generation counter.
func (s *Service) Logout(ctx context.Context, customerID string) error {
	if err := s.customers.BumpTokenGeneration(ctx, customerID); err != nil {
		return fmt.Errorf("auth service: logout: %w", err)
	}

	s.log.Info("auth.logout", map[string]interface{}{
		"customer_id": customerID,
	})
	return nil
}

// RequestPasswordReset generates a reset token and emits an event
// for downstream delivery (email plugin). Always returns success to
// prevent email enumeration.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	if email == "" {
		return apperror.Validation("email is required")
	}

	c, err := s.customers.FindByEmail(ctx, email)
	if err != nil {
		return fmt.Errorf("auth service: request reset: %w", err)
	}
	if c == nil {
		// Do not reveal whether the account exists.
		return nil
	}

	rt, plaintext, err := customer.NewPasswordResetToken(id.New(), c.ID, s.resetTTL)
	if err != nil {
		return fmt.Errorf("auth service: request reset: %w", err)
	}

	if err := s.resets.Create(ctx, &rt); err != nil {
		return fmt.Errorf("auth service: request reset: %w", err)
	}

	if err := s.bus.Publish(ctx, event.New(customer.EventPasswordResetRequested, "auth.service", customer.PasswordResetRequestedData{
		CustomerID: c.ID,
		Token:      plaintext,
	})); err != nil {
		s.log.Warn("auth.event.publish_failed", map[string]interface{}{
			"event": customer.EventPasswordResetRequested,
			"error": err.Error(),
		})
	}

	s.log.Info("auth.password_reset.requested", map[string]interface{}{
		"customer_id": c.ID,
	})
	return nil
}

// ConfirmPasswordResetInput contains the fields for password reset confirmation.
type ConfirmPasswordResetInput struct {
	Token       string
	NewPassword string
}

// ConfirmPasswordReset validates the reset token and sets the new password.
func (s *Service) ConfirmPasswordReset(ctx context.Context, in ConfirmPasswordResetInput) error {
	if in.Token == "" {
		return apperror.Validation("token is required")
	}
	if in.NewPassword == "" {
		return apperror.Validation("new password is required")
	}
	if len(in.NewPassword) < 8 {
		return apperror.Validation("password must be at least 8 characters")
	}

	hash := customer.HashToken(in.Token)
	rt, err := s.resets.FindByTokenHash(ctx, hash)
	if err != nil {
		return fmt.Errorf("auth service: confirm reset: %w", err)
	}
	if rt == nil {
		return apperror.Unauthorized("invalid or expired token")
	}
	if rt.IsExpired() || rt.IsUsed() {
		return apperror.Unauthorized("invalid or expired token")
	}

	c, err := s.customers.FindByID(ctx, rt.CustomerID)
	if err != nil {
		return fmt.Errorf("auth service: confirm reset: %w", err)
	}
	if c == nil {
		return apperror.Unauthorized("invalid or expired token")
	}

	pwHash, err := password.Hash(in.NewPassword)
	if err != nil {
		return fmt.Errorf("auth service: confirm reset: %w", err)
	}
	if err := c.SetPassword(pwHash); err != nil {
		return fmt.Errorf("auth service: confirm reset: %w", err)
	}

	// Mark token used before updating customer — if customer update fails,
	// the token is consumed but the password is unchanged (user can request a new token).
	// This prevents the worse scenario of password changed but token still reusable.
	if err := rt.MarkUsed(); err != nil {
		return fmt.Errorf("auth service: confirm reset: %w", err)
	}
	if err := s.resets.MarkUsed(ctx, rt.ID); err != nil {
		return fmt.Errorf("auth service: confirm reset: %w", err)
	}

	if err := s.customers.Update(ctx, c); err != nil {
		return fmt.Errorf("auth service: confirm reset: %w", err)
	}

	// Atomic DB-level increment, consistent with Logout.
	if err := s.customers.BumpTokenGeneration(ctx, c.ID); err != nil {
		return fmt.Errorf("auth service: confirm reset: %w", err)
	}

	s.log.Info("auth.password_reset.confirmed", map[string]interface{}{
		"customer_id": c.ID,
	})
	return nil
}
