package postgres_test

import (
	"context"
	"testing"
	"time"

	domainintegration "github.com/akarso/shopanda/internal/domain/integration"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/pkg/integrationhttp"
)

func TestIntegrationIdempotencyRepo_AdminListAndGet(t *testing.T) {
	db := testDB(t)
	t.Cleanup(func() { _, _ = db.Exec("DELETE FROM integration_idempotency") })

	repo, err := postgres.NewIntegrationIdempotencyRepo(db)
	if err != nil {
		t.Fatalf("NewIntegrationIdempotencyRepo: %v", err)
	}
	ctx := context.Background()
	expires := time.Now().UTC().Add(24 * time.Hour)

	_, claimed, err := repo.Begin(ctx, "integrationdemo", "key-a", "POST", "/hook", "hash-a", expires)
	if err != nil || !claimed {
		t.Fatalf("Begin first: claimed=%v err=%v", claimed, err)
	}
	if err := repo.Complete(ctx, "integrationdemo", "key-a", 200, []byte(`{"ok":true}`)); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	_, inProgress, err := repo.Begin(ctx, "integrationdemo", "key-b", "POST", "/hook", "hash-b", expires)
	if err != nil || !inProgress {
		t.Fatalf("Begin second: claimed=%v err=%v", inProgress, err)
	}

	completed := true
	items, err := repo.List(ctx, domainintegration.IdempotencyListFilter{
		PluginSlug: "integrationdemo",
		Completed:  &completed,
		Offset:     0,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].IdempotencyKey != "key-a" || !items[0].Completed {
		t.Fatalf("items = %+v", items)
	}

	got, err := repo.Get(ctx, "integrationdemo", "key-a")
	if err != nil || got == nil || got.StatusCode != 200 || string(got.ResponseBody) != `{"ok":true}` {
		t.Fatalf("Get = %+v err=%v", got, err)
	}

	missing, err := repo.Get(ctx, "integrationdemo", "missing")
	if err != nil || missing != nil {
		t.Fatalf("Get missing = %+v err=%v", missing, err)
	}
}

func TestIntegrationIdempotencyRepo_ImplementsStore(t *testing.T) {
	var _ integrationhttp.IdempotencyStore = (*postgres.IntegrationIdempotencyRepo)(nil)
}

func TestIntegrationIdempotencyRepo_ImplementsAdminRepository(t *testing.T) {
	var _ domainintegration.IdempotencyAdminRepository = (*postgres.IntegrationIdempotencyRepo)(nil)
}
