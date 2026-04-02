package auth

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/jwt"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/password"
)

// Service orchestrates registration and login use cases.
type Service struct {
	customers customer.CustomerRepository
	jwt       *jwt.Issuer
	log       logger.Logger
}

// NewService creates an auth application service.
func NewService(
	customers customer.CustomerRepository,
	jwtIssuer *jwt.Issuer,
	log logger.Logger,
) *Service {
	return &Service{
		customers: customers,
		jwt:       jwtIssuer,
		log:       log,
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

	token, err := s.jwt.Create(c.ID, string(identity.RoleCustomer))
	if err != nil {
		return RegisterOutput{}, fmt.Errorf("auth service: create token: %w", err)
	}

	s.log.Info("auth.registered", map[string]interface{}{
		"customer_id": c.ID,
		"email":       c.Email,
	})

	return RegisterOutput{CustomerID: c.ID, Token: token}, nil
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
		return LoginOutput{}, apperror.Unauthorized("account is disabled")
	}

	if err := password.Compare(c.PasswordHash, in.Password); err != nil {
		return LoginOutput{}, apperror.Unauthorized("invalid email or password")
	}

	token, err := s.jwt.Create(c.ID, string(identity.RoleCustomer))
	if err != nil {
		return LoginOutput{}, fmt.Errorf("auth service: create token: %w", err)
	}

	s.log.Info("auth.login", map[string]interface{}{
		"customer_id": c.ID,
		"email":       c.Email,
	})

	return LoginOutput{CustomerID: c.ID, Token: token}, nil
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
