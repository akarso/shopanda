package postgres_test

import (
	"context"
	"testing"

	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/migrate"
)

func setupExtensionFieldRepo(t *testing.T) *postgres.ExtensionFieldRepo {
	t.Helper()
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec("DELETE FROM extension_fields"); err != nil {
			t.Errorf("cleanup DELETE FROM extension_fields: %v", err)
		}
	})
	repo, err := postgres.NewExtensionFieldRepo(db)
	if err != nil {
		t.Fatalf("NewExtensionFieldRepo: %v", err)
	}
	return repo
}

func sampleExtensionField(code string, scope domainext.TargetType) domainext.ExtensionField {
	field, err := domainext.NewExtensionField(domainext.FieldDef{
		Code:        code,
		Label:       "Sample",
		Type:        domainext.FieldTypeString,
		Scope:       scope,
		StorageMode: domainext.StorageStored,
	})
	if err != nil {
		panic(err)
	}
	return field
}

func TestExtensionFieldRepo_NilDB(t *testing.T) {
	_, err := postgres.NewExtensionFieldRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestExtensionFieldRepo_CreateDuplicateConflict(t *testing.T) {
	repo := setupExtensionFieldRepo(t)
	ctx := context.Background()
	field := sampleExtensionField("acme.dup.field", domainext.TargetProduct)

	if err := repo.Create(ctx, field); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	err := repo.Create(ctx, field)
	if err == nil || !apperror.Is(err, apperror.CodeConflict) {
		t.Fatalf("duplicate Create err = %v, want conflict", err)
	}
}

func TestExtensionFieldRepo_SaveAndFindByCode(t *testing.T) {
	repo := setupExtensionFieldRepo(t)
	ctx := context.Background()
	field := sampleExtensionField("acme.sample.field", domainext.TargetProduct)

	if err := repo.Save(ctx, field); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByCode(ctx, field.Code)
	if err != nil {
		t.Fatalf("FindByCode: %v", err)
	}
	if got.Label != field.Label || got.Scope != field.Scope {
		t.Fatalf("FindByCode = %+v, want %+v", got, field)
	}
}

func TestExtensionFieldRepo_ListActiveByScope(t *testing.T) {
	repo := setupExtensionFieldRepo(t)
	ctx := context.Background()

	productField := sampleExtensionField("acme.product.field", domainext.TargetProduct)
	cartField := sampleExtensionField("acme.cart.field", domainext.TargetCartItem)
	for _, field := range []domainext.ExtensionField{productField, cartField} {
		if err := repo.Save(ctx, field); err != nil {
			t.Fatalf("Save %s: %v", field.Code, err)
		}
	}

	fields, err := repo.ListActive(ctx, domainext.TargetProduct)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(fields) != 1 || fields[0].Code != productField.Code {
		t.Fatalf("ListActive(product) = %+v", fields)
	}
}

func TestExtensionFieldRepo_SoftDeleteHidesFromDefaultList(t *testing.T) {
	repo := setupExtensionFieldRepo(t)
	ctx := context.Background()
	field := sampleExtensionField("acme.delete.field", domainext.TargetProduct)

	if err := repo.Save(ctx, field); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := repo.SoftDelete(ctx, field.Code); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	fields, err := repo.ListActive(ctx, "")
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(fields) != 0 {
		t.Fatalf("ListActive after soft delete = %+v, want empty", fields)
	}

	_, err = repo.FindByCode(ctx, field.Code)
	if err == nil {
		t.Fatal("expected not found after soft delete")
	}
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("FindByCode error = %v, want not_found", err)
	}
}

func TestExtensionFieldRepo_SaveRestoresSoftDeletedField(t *testing.T) {
	repo := setupExtensionFieldRepo(t)
	ctx := context.Background()
	field := sampleExtensionField("acme.restore.field", domainext.TargetProduct)

	if err := repo.Save(ctx, field); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := repo.SoftDelete(ctx, field.Code); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	field.Label = "Restored"
	if err := repo.Save(ctx, field); err != nil {
		t.Fatalf("Save restore: %v", err)
	}

	got, err := repo.FindByCode(ctx, field.Code)
	if err != nil {
		t.Fatalf("FindByCode: %v", err)
	}
	if got.Label != "Restored" {
		t.Errorf("Label = %q, want Restored", got.Label)
	}
}

func TestExtensionFieldRepo_SoftDeleteMissing(t *testing.T) {
	repo := setupExtensionFieldRepo(t)
	ctx := context.Background()

	err := repo.SoftDelete(ctx, "acme.missing.field")
	if err == nil {
		t.Fatal("expected not found")
	}
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("SoftDelete error = %v, want not_found", err)
	}
}
