package postgres_test

import (
	"context"
	"testing"
	"time"

	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/migrate"
)

func setupExtensionValueRepo(t *testing.T) *postgres.ExtensionValueRepo {
	t.Helper()
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec("DELETE FROM extension_values"); err != nil {
			t.Errorf("cleanup DELETE FROM extension_values: %v", err)
		}
	})
	repo, err := postgres.NewExtensionValueRepo(db)
	if err != nil {
		t.Fatalf("NewExtensionValueRepo: %v", err)
	}
	return repo
}

func sampleExtensionValue(target domainext.Target, code, text string) domainext.Value {
	s := text
	return domainext.Value{
		FieldCode:  code,
		TargetType: target.Type,
		TargetID:   target.ID,
		Payload:    domainext.ValuePayload{StringValue: &s},
		UpdatedBy:  "admin-1",
		UpdatedAt:  time.Now().UTC(),
	}
}

func TestExtensionValueRepo_NilDB(t *testing.T) {
	_, err := postgres.NewExtensionValueRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestExtensionValueRepo_UpsertAndListByTarget(t *testing.T) {
	repo := setupExtensionValueRepo(t)
	ctx := context.Background()
	target := domainext.Target{Type: domainext.TargetProduct, ID: "prod-1"}

	if err := repo.Upsert(ctx, sampleExtensionValue(target, "acme.note", "hello")); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := repo.Upsert(ctx, sampleExtensionValue(target, "acme.note", "updated")); err != nil {
		t.Fatalf("Upsert update: %v", err)
	}

	values, err := repo.ListByTarget(ctx, target)
	if err != nil {
		t.Fatalf("ListByTarget: %v", err)
	}
	if len(values) != 1 {
		t.Fatalf("ListByTarget len = %d, want 1", len(values))
	}
	if values[0].Payload.StringValue == nil || *values[0].Payload.StringValue != "updated" {
		t.Fatalf("ListByTarget value = %+v", values[0].Payload)
	}
}

func TestExtensionValueRepo_Delete(t *testing.T) {
	repo := setupExtensionValueRepo(t)
	ctx := context.Background()
	target := domainext.Target{Type: domainext.TargetProduct, ID: "prod-1"}

	if err := repo.Upsert(ctx, sampleExtensionValue(target, "acme.note", "hello")); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := repo.Delete(ctx, target, "acme.note"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	values, err := repo.ListByTarget(ctx, target)
	if err != nil {
		t.Fatalf("ListByTarget: %v", err)
	}
	if len(values) != 0 {
		t.Fatalf("ListByTarget after delete = %+v", values)
	}

	err = repo.Delete(ctx, target, "acme.note")
	if err == nil || !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("Delete missing err = %v, want not found", err)
	}
}
