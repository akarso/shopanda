package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

// ensureMigrations runs all migrations. Delegates to ensureProductsTable which
// applies the full migration set.
func ensureMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	ensureProductsTable(t, db)
}

func mustNewCustomer(t *testing.T, email string) customer.Customer {
	t.Helper()
	c, err := customer.NewCustomer(id.New(), email)
	if err != nil {
		t.Fatalf("NewCustomer: %v", err)
	}
	return c
}

func TestCustomerRepo_CreateAndFindByID(t *testing.T) {
	db := testDB(t)
	ensureMigrations(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM customers") })

	repo, err := postgres.NewCustomerRepo(db)
	if err != nil {
		t.Fatalf("NewCustomerRepo: %v", err)
	}
	ctx := context.Background()

	c := mustNewCustomer(t, "alice@example.com")
	c.FirstName = "Alice"
	c.LastName = "Smith"

	if err := repo.Create(ctx, &c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByID returned nil")
	}
	if got.ID != c.ID {
		t.Errorf("ID = %q, want %q", got.ID, c.ID)
	}
	if got.Email != "alice@example.com" {
		t.Errorf("Email = %q, want alice@example.com", got.Email)
	}
	if got.FirstName != "Alice" {
		t.Errorf("FirstName = %q, want Alice", got.FirstName)
	}
	if got.LastName != "Smith" {
		t.Errorf("LastName = %q, want Smith", got.LastName)
	}
	if got.Status != customer.StatusActive {
		t.Errorf("Status = %q, want active", got.Status)
	}
}

func TestCustomerRepo_FindByID_NotFound(t *testing.T) {
	db := testDB(t)
	ensureMigrations(t, db)

	repo, err := postgres.NewCustomerRepo(db)
	if err != nil {
		t.Fatalf("NewCustomerRepo: %v", err)
	}
	got, err := repo.FindByID(context.Background(), id.New())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != nil {
		t.Error("expected nil for non-existent ID")
	}
}

func TestCustomerRepo_FindByEmail(t *testing.T) {
	db := testDB(t)
	ensureMigrations(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM customers") })

	repo, err := postgres.NewCustomerRepo(db)
	if err != nil {
		t.Fatalf("NewCustomerRepo: %v", err)
	}
	ctx := context.Background()

	c := mustNewCustomer(t, "bob@example.com")
	if err := repo.Create(ctx, &c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindByEmail(ctx, "bob@example.com")
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	if got == nil {
		t.Fatal("FindByEmail returned nil")
	}
	if got.Email != "bob@example.com" {
		t.Errorf("Email = %q, want bob@example.com", got.Email)
	}
}

func TestCustomerRepo_FindByEmail_NotFound(t *testing.T) {
	db := testDB(t)
	ensureMigrations(t, db)

	repo, err := postgres.NewCustomerRepo(db)
	if err != nil {
		t.Fatalf("NewCustomerRepo: %v", err)
	}
	got, err := repo.FindByEmail(context.Background(), "nobody@example.com")
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	if got != nil {
		t.Error("expected nil for non-existent email")
	}
}

func TestCustomerRepo_Create_DuplicateEmail(t *testing.T) {
	db := testDB(t)
	ensureMigrations(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM customers") })

	repo, err := postgres.NewCustomerRepo(db)
	if err != nil {
		t.Fatalf("NewCustomerRepo: %v", err)
	}
	ctx := context.Background()

	c1 := mustNewCustomer(t, "dup@example.com")
	if err := repo.Create(ctx, &c1); err != nil {
		t.Fatalf("Create first: %v", err)
	}

	c2 := mustNewCustomer(t, "dup@example.com")
	err = repo.Create(ctx, &c2)
	if err == nil {
		t.Fatal("expected error for duplicate email")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if appErr.Code != apperror.CodeConflict {
		t.Errorf("Code = %q, want conflict", appErr.Code)
	}
}

func TestCustomerRepo_Update(t *testing.T) {
	db := testDB(t)
	ensureMigrations(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM customers") })

	repo, err := postgres.NewCustomerRepo(db)
	if err != nil {
		t.Fatalf("NewCustomerRepo: %v", err)
	}
	ctx := context.Background()

	c := mustNewCustomer(t, "update@example.com")
	if err := repo.Create(ctx, &c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	c.FirstName = "Updated"
	c.LastName = "Name"
	if err := repo.Update(ctx, &c); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.FindByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.FirstName != "Updated" {
		t.Errorf("FirstName = %q, want Updated", got.FirstName)
	}
	if got.LastName != "Name" {
		t.Errorf("LastName = %q, want Name", got.LastName)
	}
}

func TestCustomerRepo_Update_NotFound(t *testing.T) {
	db := testDB(t)
	ensureMigrations(t, db)

	repo, err := postgres.NewCustomerRepo(db)
	if err != nil {
		t.Fatalf("NewCustomerRepo: %v", err)
	}
	ctx := context.Background()

	c := mustNewCustomer(t, "ghost@example.com")
	err = repo.Update(ctx, &c)
	if err == nil {
		t.Fatal("expected error for non-existent customer")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if appErr.Code != apperror.CodeNotFound {
		t.Errorf("Code = %q, want not_found", appErr.Code)
	}
}

func TestCustomerRepo_ChangePasswordAndBumpTokenGeneration(t *testing.T) {
	db := testDB(t)
	ensureMigrations(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM customers") })

	repo, err := postgres.NewCustomerRepo(db)
	if err != nil {
		t.Fatalf("NewCustomerRepo: %v", err)
	}
	ctx := context.Background()

	c := mustNewCustomer(t, "password@example.com")
	c.PasswordHash = "old-hash"
	if err := repo.Create(ctx, &c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.ChangePasswordAndBumpTokenGeneration(ctx, c.ID, "new-hash"); err != nil {
		t.Fatalf("ChangePasswordAndBumpTokenGeneration: %v", err)
	}

	got, err := repo.FindByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.PasswordHash != "new-hash" {
		t.Fatalf("PasswordHash = %q, want %q", got.PasswordHash, "new-hash")
	}
	if got.TokenGeneration != 1 {
		t.Fatalf("TokenGeneration = %d, want %d", got.TokenGeneration, 1)
	}
}

func TestCustomerRepo_WithTx_CommitVisible(t *testing.T) {
	db := testDB(t)
	ensureMigrations(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM customers") })

	repo, err := postgres.NewCustomerRepo(db)
	if err != nil {
		t.Fatalf("NewCustomerRepo: %v", err)
	}
	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer tx.Rollback()

	txRepo := repo.WithTx(tx)

	c := mustNewCustomer(t, "tx-commit@example.com")
	if err := txRepo.Create(ctx, &c); err != nil {
		t.Fatalf("txRepo.Create: %v", err)
	}

	// Visible within the transaction.
	got, err := txRepo.FindByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("txRepo.FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected customer visible within tx")
	}

	// Not visible outside the transaction before commit.
	outside, err := repo.FindByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("repo.FindByID before commit: %v", err)
	}
	if outside != nil {
		t.Error("expected customer not visible outside tx before commit")
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Visible after commit.
	afterCommit, err := repo.FindByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("repo.FindByID after commit: %v", err)
	}
	if afterCommit == nil {
		t.Fatal("expected customer visible after commit")
	}
	if afterCommit.Email != "tx-commit@example.com" {
		t.Errorf("Email = %q, want tx-commit@example.com", afterCommit.Email)
	}
}

func TestCustomerRepo_WithTx_RollbackInvisible(t *testing.T) {
	db := testDB(t)
	ensureMigrations(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM customers") })

	repo, err := postgres.NewCustomerRepo(db)
	if err != nil {
		t.Fatalf("NewCustomerRepo: %v", err)
	}
	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}

	txRepo := repo.WithTx(tx)

	c := mustNewCustomer(t, "tx-rollback@example.com")
	if err := txRepo.Create(ctx, &c); err != nil {
		t.Fatalf("txRepo.Create: %v", err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	// Not visible after rollback.
	got, err := repo.FindByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("repo.FindByID after rollback: %v", err)
	}
	if got != nil {
		t.Error("expected customer not visible after rollback")
	}
}
