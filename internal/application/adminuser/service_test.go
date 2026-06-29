package adminuser_test

import (
	"context"
	"testing"

	adminuserApp "github.com/akarso/shopanda/internal/application/adminuser"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type stubAdminUserRepo struct {
	byID      map[string]*customer.Customer
	byEmail   map[string]*customer.Customer
	created   *customer.Customer
	updated   *customer.Customer
	pwChanged bool
	admins    int
}

func (s *stubAdminUserRepo) FindByID(_ context.Context, id string) (*customer.Customer, error) {
	if s.byID == nil {
		return nil, nil
	}
	return s.byID[id], nil
}

func (s *stubAdminUserRepo) FindByEmail(_ context.Context, email string) (*customer.Customer, error) {
	if s.byEmail == nil {
		return nil, nil
	}
	return s.byEmail[email], nil
}

func (s *stubAdminUserRepo) Create(_ context.Context, c *customer.Customer) error {
	cp := *c
	s.created = &cp
	return nil
}

func (s *stubAdminUserRepo) ListAdminUsers(_ context.Context, _, _ int) ([]customer.Customer, error) {
	return nil, nil
}

func (s *stubAdminUserRepo) UpdateAdminUser(_ context.Context, c *customer.Customer, priorRole customer.Role, priorStatus customer.Status, revokeSessions bool) error {
	if priorRole == customer.RoleAdmin && priorStatus == customer.StatusActive &&
		(c.Role != customer.RoleAdmin || c.Status != customer.StatusActive) &&
		s.admins <= 1 {
		return apperror.Validation("cannot remove the last active admin user")
	}
	cp := *c
	if revokeSessions {
		cp.TokenGeneration++
	}
	s.updated = &cp
	return nil
}

func (s *stubAdminUserRepo) ChangePasswordAndBumpTokenGeneration(_ context.Context, _ string, _ string) error {
	s.pwChanged = true
	return nil
}

func TestService_Create_AdminUser(t *testing.T) {
	repo := &stubAdminUserRepo{byEmail: map[string]*customer.Customer{}}
	svc := adminuserApp.NewService(repo)

	user, err := svc.Create(context.Background(), adminuserApp.CreateInput{
		Email:     "manager@example.com",
		Password:  "password123",
		FirstName: "Morgan",
		LastName:  "Lee",
		Role:      customer.RoleManager,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if user.Role != customer.RoleManager {
		t.Errorf("role = %q, want manager", user.Role)
	}
	if repo.created == nil {
		t.Fatal("expected create to persist user")
	}
}

func TestService_Update_BlocksSelfDisable(t *testing.T) {
	admin := &customer.Customer{
		ID:     "u1",
		Email:  "admin@example.com",
		Role:   customer.RoleAdmin,
		Status: customer.StatusActive,
	}
	repo := &stubAdminUserRepo{
		byID:   map[string]*customer.Customer{"u1": admin},
		admins: 2,
	}
	svc := adminuserApp.NewService(repo)

	_, err := svc.Update(context.Background(), "u1", "u1", adminuserApp.UpdateInput{
		FirstName: "Admin",
		LastName:  "User",
		Role:      customer.RoleAdmin,
		Status:    customer.StatusDisabled,
	})
	if err == nil {
		t.Fatal("expected self-disable to fail")
	}
}

func TestService_ResetPassword_BlocksSelf(t *testing.T) {
	admin := &customer.Customer{
		ID:     "u1",
		Role:   customer.RoleAdmin,
		Status: customer.StatusActive,
	}
	repo := &stubAdminUserRepo{byID: map[string]*customer.Customer{"u1": admin}}
	svc := adminuserApp.NewService(repo)

	err := svc.ResetPassword(context.Background(), "u1", "u1", "newpassword123")
	if err == nil {
		t.Fatal("expected self reset to fail")
	}
}

func TestService_ResetPassword_Success(t *testing.T) {
	actor := &customer.Customer{
		ID:     "u1",
		Role:   customer.RoleAdmin,
		Status: customer.StatusActive,
	}
	target := &customer.Customer{
		ID:     "u2",
		Role:   customer.RoleManager,
		Status: customer.StatusActive,
	}
	repo := &stubAdminUserRepo{byID: map[string]*customer.Customer{
		"u1": actor,
		"u2": target,
	}}
	svc := adminuserApp.NewService(repo)

	err := svc.ResetPassword(context.Background(), "u1", "u2", "newpassword123")
	if err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if !repo.pwChanged {
		t.Fatal("expected password change to persist")
	}
}

func TestService_Update_BlocksLastAdminRemoval(t *testing.T) {
	admin := &customer.Customer{
		ID:     "u1",
		Role:   customer.RoleAdmin,
		Status: customer.StatusActive,
	}
	repo := &stubAdminUserRepo{
		byID:   map[string]*customer.Customer{"u1": admin},
		admins: 1,
	}
	svc := adminuserApp.NewService(repo)

	_, err := svc.Update(context.Background(), "u2", "u1", adminuserApp.UpdateInput{
		FirstName: "Admin",
		LastName:  "User",
		Role:      customer.RoleManager,
		Status:    customer.StatusActive,
	})
	if err == nil {
		t.Fatal("expected last admin demotion to fail")
	}
}

func TestService_Update_RevokesSessionsOnDisable(t *testing.T) {
	target := &customer.Customer{
		ID:              "u2",
		Role:            customer.RoleManager,
		Status:          customer.StatusActive,
		TokenGeneration: 3,
	}
	repo := &stubAdminUserRepo{byID: map[string]*customer.Customer{"u2": target}}
	svc := adminuserApp.NewService(repo)

	_, err := svc.Update(context.Background(), "u1", "u2", adminuserApp.UpdateInput{
		FirstName: "Morgan",
		LastName:  "Lee",
		Role:      customer.RoleManager,
		Status:    customer.StatusDisabled,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if repo.updated == nil {
		t.Fatal("expected update to persist")
	}
	if repo.updated.TokenGeneration != 4 {
		t.Errorf("token generation = %d, want 4", repo.updated.TokenGeneration)
	}
}
