package importer_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/importer"
	"github.com/akarso/shopanda/internal/domain/customer"
)

// --- customer test mocks ---

type mockCustomerRepoForImport struct {
	customers map[string]*customer.Customer // keyed by email
	createErr error
}

func newMockCustomerRepoForImport() *mockCustomerRepoForImport {
	return &mockCustomerRepoForImport{customers: make(map[string]*customer.Customer)}
}

func (m *mockCustomerRepoForImport) FindByID(_ context.Context, _ string) (*customer.Customer, error) {
	return nil, nil
}
func (m *mockCustomerRepoForImport) FindByEmail(_ context.Context, email string) (*customer.Customer, error) {
	return m.customers[email], nil
}
func (m *mockCustomerRepoForImport) Create(_ context.Context, c *customer.Customer) error {
	if m.createErr != nil {
		return m.createErr
	}
	if _, exists := m.customers[c.Email]; exists {
		return fmt.Errorf("duplicate email %q", c.Email)
	}
	m.customers[c.Email] = c
	return nil
}
func (m *mockCustomerRepoForImport) Update(_ context.Context, c *customer.Customer) error {
	m.customers[c.Email] = c
	return nil
}
func (m *mockCustomerRepoForImport) ListCustomers(_ context.Context, _, _ int) ([]customer.Customer, error) {
	return nil, nil
}
func (m *mockCustomerRepoForImport) BumpTokenGeneration(_ context.Context, _ string) error {
	return nil
}
func (m *mockCustomerRepoForImport) ChangePasswordAndBumpTokenGeneration(_ context.Context, _ string, _ string) error {
	return nil
}
func (m *mockCustomerRepoForImport) Delete(_ context.Context, _ string) error     { return nil }
func (m *mockCustomerRepoForImport) WithTx(_ *sql.Tx) customer.CustomerRepository { return m }

// --- tests ---

func TestCustomerImport_Basic(t *testing.T) {
	repo := newMockCustomerRepoForImport()
	imp := importer.NewCustomerImporter(repo)

	csv := "email,first_name,last_name,role,status\nalice@example.com,Alice,Smith,customer,active\nbob@example.com,Bob,Jones,admin,disabled\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Created != 2 {
		t.Errorf("Created = %d, want 2", result.Created)
	}
	if result.Skipped != 0 {
		t.Errorf("Skipped = %d, want 0", result.Skipped)
	}
	alice := repo.customers["alice@example.com"]
	if alice == nil {
		t.Fatal("alice not created")
	}
	if alice.FirstName != "Alice" || alice.LastName != "Smith" {
		t.Errorf("alice name = %q %q, want Alice Smith", alice.FirstName, alice.LastName)
	}
	if alice.Role != customer.RoleCustomer {
		t.Errorf("alice role = %q, want customer", alice.Role)
	}
	bob := repo.customers["bob@example.com"]
	if bob == nil {
		t.Fatal("bob not created")
	}
	if bob.Role != customer.RoleAdmin {
		t.Errorf("bob role = %q, want admin", bob.Role)
	}
	if bob.Status != customer.StatusDisabled {
		t.Errorf("bob status = %q, want disabled", bob.Status)
	}
}

func TestCustomerImport_MissingEmailColumn(t *testing.T) {
	repo := newMockCustomerRepoForImport()
	imp := importer.NewCustomerImporter(repo)

	csv := "first_name,last_name\nAlice,Smith\n"
	_, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err == nil {
		t.Fatal("expected error for missing email column")
	}
	if !strings.Contains(err.Error(), "email") {
		t.Errorf("error = %q, want containing 'email'", err.Error())
	}
}

func TestCustomerImport_EmptyEmail(t *testing.T) {
	repo := newMockCustomerRepoForImport()
	imp := importer.NewCustomerImporter(repo)

	csv := "email,first_name\n,Alice\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if !strings.Contains(result.Errors[0], "empty email") {
		t.Errorf("error = %q, want containing 'empty email'", result.Errors[0])
	}
}

func TestCustomerImport_InvalidRole(t *testing.T) {
	repo := newMockCustomerRepoForImport()
	imp := importer.NewCustomerImporter(repo)

	csv := "email,role\nalice@example.com,superuser\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if !strings.Contains(result.Errors[0], "invalid role") {
		t.Errorf("error = %q, want containing 'invalid role'", result.Errors[0])
	}
}

func TestCustomerImport_InvalidStatus(t *testing.T) {
	repo := newMockCustomerRepoForImport()
	imp := importer.NewCustomerImporter(repo)

	csv := "email,status\nalice@example.com,banned\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if !strings.Contains(result.Errors[0], "invalid status") {
		t.Errorf("error = %q, want containing 'invalid status'", result.Errors[0])
	}
}

func TestCustomerImport_DuplicateEmail(t *testing.T) {
	repo := newMockCustomerRepoForImport()
	imp := importer.NewCustomerImporter(repo)

	csv := "email\nalice@example.com\nalice@example.com\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Created != 1 {
		t.Errorf("Created = %d, want 1", result.Created)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
}

func TestCustomerImport_PasswordHashed(t *testing.T) {
	repo := newMockCustomerRepoForImport()
	imp := importer.NewCustomerImporter(repo)

	csv := "email,password\nalice@example.com,secret123\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Created != 1 {
		t.Errorf("Created = %d, want 1", result.Created)
	}
	alice := repo.customers["alice@example.com"]
	if alice == nil {
		t.Fatal("alice not created")
	}
	if alice.PasswordHash == "" {
		t.Error("PasswordHash is empty, expected bcrypt hash")
	}
	if alice.PasswordHash == "secret123" {
		t.Error("PasswordHash stores plaintext, expected bcrypt hash")
	}
	if !strings.HasPrefix(alice.PasswordHash, "$2a$") && !strings.HasPrefix(alice.PasswordHash, "$2b$") {
		t.Errorf("PasswordHash = %q, expected bcrypt prefix", alice.PasswordHash)
	}
}

func TestCustomerImport_EmailOnly(t *testing.T) {
	repo := newMockCustomerRepoForImport()
	imp := importer.NewCustomerImporter(repo)

	csv := "email\nalice@example.com\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Created != 1 {
		t.Errorf("Created = %d, want 1", result.Created)
	}
	alice := repo.customers["alice@example.com"]
	if alice == nil {
		t.Fatal("alice not created")
	}
	if alice.Role != customer.RoleCustomer {
		t.Errorf("default role = %q, want customer", alice.Role)
	}
	if alice.Status != customer.StatusActive {
		t.Errorf("default status = %q, want active", alice.Status)
	}
}

func TestCustomerImport_MixedValidAndInvalid(t *testing.T) {
	repo := newMockCustomerRepoForImport()
	imp := importer.NewCustomerImporter(repo)

	csv := "email,role,status\nalice@example.com,customer,active\nbob@example.com,badrole,active\n,customer,active\ncarol@example.com,admin,disabled\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Created != 2 {
		t.Errorf("Created = %d, want 2", result.Created)
	}
	if result.Skipped != 2 {
		t.Errorf("Skipped = %d, want 2", result.Skipped)
	}
	if len(result.Errors) != 2 {
		t.Errorf("Errors = %d, want 2", len(result.Errors))
	}
}
