package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

func mustNewResetToken(t *testing.T, customerID string) customer.PasswordResetToken {
	t.Helper()
	tok, _, err := customer.NewPasswordResetToken(id.New(), customerID, time.Hour)
	if err != nil {
		t.Fatalf("NewPasswordResetToken: %v", err)
	}
	return tok
}

func TestResetTokenRepo_NilDB(t *testing.T) {
	_, err := postgres.NewResetTokenRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestResetTokenRepo_CreateAndFindByHash(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM password_reset_tokens")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM password_reset_tokens") })

	repo, err := postgres.NewResetTokenRepo(db)
	if err != nil {
		t.Fatalf("NewResetTokenRepo: %v", err)
	}
	ctx := context.Background()

	tok := mustNewResetToken(t, id.New())
	if err := repo.Create(ctx, &tok); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindByTokenHash(ctx, tok.TokenHash)
	if err != nil {
		t.Fatalf("FindByTokenHash: %v", err)
	}
	if got == nil {
		t.Fatal("FindByTokenHash returned nil")
	}
	if got.ID != tok.ID {
		t.Errorf("ID: got %q, want %q", got.ID, tok.ID)
	}
	if got.CustomerID != tok.CustomerID {
		t.Errorf("CustomerID: got %q, want %q", got.CustomerID, tok.CustomerID)
	}
	if got.UsedAt != nil {
		t.Errorf("expected UsedAt nil, got %v", got.UsedAt)
	}
}

func TestResetTokenRepo_FindByTokenHash_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewResetTokenRepo(db)
	if err != nil {
		t.Fatalf("NewResetTokenRepo: %v", err)
	}

	got, err := repo.FindByTokenHash(context.Background(), "nonexistenthash")
	if err != nil {
		t.Fatalf("FindByTokenHash: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for non-existent hash")
	}
}

func TestResetTokenRepo_MarkUsed(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM password_reset_tokens")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM password_reset_tokens") })

	repo, err := postgres.NewResetTokenRepo(db)
	if err != nil {
		t.Fatalf("NewResetTokenRepo: %v", err)
	}
	ctx := context.Background()

	tok := mustNewResetToken(t, id.New())
	if err := repo.Create(ctx, &tok); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.MarkUsed(ctx, tok.ID); err != nil {
		t.Fatalf("MarkUsed: %v", err)
	}

	got, err := repo.FindByTokenHash(ctx, tok.TokenHash)
	if err != nil {
		t.Fatalf("FindByTokenHash: %v", err)
	}
	if got == nil {
		t.Fatal("FindByTokenHash returned nil after MarkUsed")
	}
	if got.UsedAt == nil {
		t.Fatal("expected UsedAt to be set after MarkUsed")
	}
}

func TestResetTokenRepo_MarkUsed_AlreadyUsed(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM password_reset_tokens")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM password_reset_tokens") })

	repo, err := postgres.NewResetTokenRepo(db)
	if err != nil {
		t.Fatalf("NewResetTokenRepo: %v", err)
	}
	ctx := context.Background()

	tok := mustNewResetToken(t, id.New())
	if err := repo.Create(ctx, &tok); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.MarkUsed(ctx, tok.ID); err != nil {
		t.Fatalf("first MarkUsed: %v", err)
	}

	// Second call: token already used → NotFound (TOCTOU-safe).
	err = repo.MarkUsed(ctx, tok.ID)
	if err == nil {
		t.Fatal("expected error marking already-used token")
	}
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Errorf("expected NotFound, got: %v", err)
	}
}

func TestResetTokenRepo_MarkUsed_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewResetTokenRepo(db)
	if err != nil {
		t.Fatalf("NewResetTokenRepo: %v", err)
	}

	err = repo.MarkUsed(context.Background(), id.New())
	if err == nil {
		t.Fatal("expected error marking non-existent token")
	}
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Errorf("expected NotFound, got: %v", err)
	}
}
