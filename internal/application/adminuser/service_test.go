package adminuser_test

import (
	"context"
	"testing"

	adminuserApp "github.com/akarso/shopanda/internal/application/adminuser"
	"github.com/akarso/shopanda/internal/domain/customer"
)

type stubCustomerRepo struct {
	customer.CustomerRepository
	byID      map[string]*customer.Customer
	byEmail   map[string]*customer.Customer
	created   *customer.Customer
	updated   *customer.Customer
	pwChanged bool
	admins    int
}

func (s *stubCustomerRepo) FindByID(_ context.Context, id string) (*customer.Customer, error) {
	if s.byID == nil {
		return nil, nil
	}
	return s.byID[id], nil
}

func (s *stubCustomerRepo) FindByEmail(_ context.Context, email string) (*customer.Customer, error) {
	if s.byEmail == nil {
		return nil, nil
	}
	return s.byEmail[email], nil
}

func (s *stubCustomerRepo) Create(_ context.Context, c *customer.Customer) error {
	cp := *c
	s.created = &cp
	return nil
}

func (s *stubCustomerRepo) Update(_ context.Context, c *customer.Customer) error {
	cp := *c
	s.updated = &cp
	return nil
}

func (s *stubCustomerRepo) ChangePasswordAndBumpTokenGeneration(_ context.Context, _ string, _ string) error {
	s.pwChanged = true
	return nil
}

func (s *stubCustomerRepo) CountActiveByRole(_ context.Context, role customer.Role) (int, error) {
	if role == customer.RoleAdmin {
		return s.admins, nil
	}
	return 0, nil
}

func TestService_Create_AdminUser(t *testing.T) {
	repo := &stubCustomerRepo{byEmail: map[string]*customer.Customer{}}
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
	repo := &stubCustomerRepo{
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
	repo := &stubCustomerRepo{byID: map[string]*customer.Customer{"u1": admin}}
	svc := adminuserApp.NewService(repo)

	err := svc.ResetPassword(context.Background(), "u1", "u1", "newpassword123")
	if err == nil {
		t.Fatal("expected self reset to fail")
	}
}

func TestService_Update_BlocksLastAdminRemoval(t *testing.T) {
	admin := &customer.Customer{
		ID:     "u1",
		Role:   customer.RoleAdmin,
		Status: customer.StatusActive,
	}
	repo := &stubCustomerRepo{
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
