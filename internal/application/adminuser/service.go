package adminuser

import (
	"context"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/adminuser"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/password"
)

const minPasswordLen = 8

// Service manages admin-panel user accounts stored as customers with admin roles.
type Service struct {
	users adminuser.Repository
}

// NewService creates an admin user service.
func NewService(users adminuser.Repository) *Service {
	if users == nil {
		panic("adminuser.NewService: nil admin user repository")
	}
	return &Service{users: users}
}

// CreateInput is the data required to create an admin user.
type CreateInput struct {
	Email     string
	Password  string
	FirstName string
	LastName  string
	Role      customer.Role
}

// UpdateInput updates mutable admin user fields.
type UpdateInput struct {
	FirstName string
	LastName  string
	Role      customer.Role
	Status    customer.Status
}

// Create provisions a new admin-capable user account.
func (s *Service) Create(ctx context.Context, in CreateInput) (*customer.Customer, error) {
	email, err := normalizeEmail(in.Email)
	if err != nil {
		return nil, err
	}
	if err := validatePassword(in.Password); err != nil {
		return nil, err
	}
	if err := validateAssignableRole(in.Role); err != nil {
		return nil, err
	}

	existing, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("adminuser: find email: %w", err)
	}
	if existing != nil {
		return nil, apperror.Conflict("email already registered")
	}

	hash, err := password.Hash(in.Password)
	if err != nil {
		return nil, fmt.Errorf("adminuser: hash password: %w", err)
	}

	c, err := customer.NewCustomer(id.New(), email)
	if err != nil {
		return nil, apperror.Validation(err.Error())
	}
	c.Role = in.Role
	c.FirstName = strings.TrimSpace(in.FirstName)
	c.LastName = strings.TrimSpace(in.LastName)
	c.MarkEmailVerified()
	if err := c.SetPassword(hash); err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := s.users.Create(ctx, &c); err != nil {
		return nil, fmt.Errorf("adminuser: create: %w", err)
	}
	return &c, nil
}

// Get returns an admin user by ID.
func (s *Service) Get(ctx context.Context, userID string) (*customer.Customer, error) {
	return s.loadAdminUser(ctx, userID)
}

// List returns admin-capable users.
func (s *Service) List(ctx context.Context, offset, limit int) ([]customer.Customer, error) {
	if offset < 0 {
		return nil, apperror.Validation("offset must be >= 0")
	}
	if limit <= 0 {
		limit = 20
	}
	return s.users.ListAdminUsers(ctx, offset, limit)
}

// Update changes role, name, or status for an admin user.
func (s *Service) Update(ctx context.Context, actorID, userID string, in UpdateInput) (*customer.Customer, error) {
	if err := validateAssignableRole(in.Role); err != nil {
		return nil, err
	}
	if !in.Status.IsValid() {
		return nil, apperror.Validation("invalid status")
	}

	c, err := s.loadAdminUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	if actorID == userID {
		if in.Role != c.Role {
			return nil, apperror.Forbidden("cannot change your own role")
		}
		if in.Status != customer.StatusActive {
			return nil, apperror.Forbidden("cannot disable your own account")
		}
	}

	priorRole := c.Role
	priorStatus := c.Status
	revokeSessions := false

	c.FirstName = strings.TrimSpace(in.FirstName)
	c.LastName = strings.TrimSpace(in.LastName)
	c.Role = in.Role
	switch in.Status {
	case customer.StatusActive:
		if c.Status == customer.StatusDisabled {
			if err := c.Enable(); err != nil {
				return nil, apperror.Validation(err.Error())
			}
		}
	case customer.StatusDisabled:
		if c.Status == customer.StatusActive {
			if err := c.Disable(); err != nil {
				return nil, apperror.Validation(err.Error())
			}
			revokeSessions = true
		}
	}

	if err := s.users.UpdateAdminUser(ctx, c, priorRole, priorStatus, revokeSessions); err != nil {
		return nil, fmt.Errorf("adminuser: update: %w", err)
	}
	return c, nil
}

// ResetPassword sets a new password and invalidates existing sessions.
func (s *Service) ResetPassword(ctx context.Context, actorID, userID, newPassword string) error {
	if actorID == userID {
		return apperror.Forbidden("use account password change for your own password")
	}
	if _, err := s.loadAdminUser(ctx, userID); err != nil {
		return err
	}
	if err := validatePassword(newPassword); err != nil {
		return err
	}

	hash, err := password.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("adminuser: hash password: %w", err)
	}
	if err := s.users.ChangePasswordAndBumpTokenGeneration(ctx, userID, hash); err != nil {
		return fmt.Errorf("adminuser: reset password: %w", err)
	}
	return nil
}

func (s *Service) loadAdminUser(ctx context.Context, userID string) (*customer.Customer, error) {
	if userID == "" {
		return nil, apperror.Validation("user id must not be empty")
	}
	c, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("adminuser: find: %w", err)
	}
	if c == nil || !customer.IsAdminRole(c.Role) {
		return nil, apperror.NotFound("admin user not found")
	}
	return c, nil
}

func validateAssignableRole(role customer.Role) error {
	if !customer.IsAdminRole(role) {
		return apperror.Validation("invalid admin role")
	}
	return nil
}

func validatePassword(pw string) error {
	if strings.TrimSpace(pw) == "" {
		return apperror.Validation("password is required")
	}
	if len(pw) < minPasswordLen {
		return apperror.Validation("password must be at least 8 characters")
	}
	return nil
}

func normalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" {
		return "", apperror.Validation("email is required")
	}
	if !strings.Contains(email, "@") {
		return "", apperror.Validation("invalid email format")
	}
	return email, nil
}
