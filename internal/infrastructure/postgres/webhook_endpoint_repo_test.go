package postgres_test

import (
	"context"
	"sort"
	"testing"

	domainwebhook "github.com/akarso/shopanda/internal/domain/webhook"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

func TestWebhookEndpointRepo_NilDB(t *testing.T) {
	_, err := postgres.NewWebhookEndpointRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

// TestWebhookEndpointRepo_CreateFindListRoundTripsEventsArray exercises the
// exact scan path that changed the most in the pgx migration: events is a
// Postgres TEXT[] column, and pgx's stdlib driver (unlike lib/pq) requires
// pgTypeMap.SQLScanner(&ep.Events) to decode it into a []string instead of
// silently failing to assign the driver's raw text-literal wire form. This
// was previously untested — the only way to catch a wrong pgtype.Map OID
// resolution here is a real round trip through Postgres.
func TestWebhookEndpointRepo_CreateFindListRoundTripsEventsArray(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db) // runs migrations; webhook_endpoints ships in the same migration set
	t.Cleanup(func() {
		db.Exec("DELETE FROM webhook_endpoints")
	})

	repo, err := postgres.NewWebhookEndpointRepo(db)
	if err != nil {
		t.Fatalf("NewWebhookEndpointRepo: %v", err)
	}
	ctx := context.Background()

	ep := &domainwebhook.Endpoint{
		URL:         "https://example.com/hooks",
		Secret:      "whsec_test",
		Events:      []string{"order.created", "order.paid", "order.refunded"},
		Active:      true,
		Description: "test endpoint",
	}
	if err := repo.Create(ctx, ep); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ep.ID == "" {
		t.Fatal("Create did not assign an ID")
	}

	got, err := repo.FindByID(ctx, ep.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByID returned nil")
	}
	assertSameEvents(t, got.Events, ep.Events)

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, e := range list {
		if e.ID == ep.ID {
			found = true
			assertSameEvents(t, e.Events, ep.Events)
		}
	}
	if !found {
		t.Fatal("List did not include the created endpoint")
	}
}

func TestWebhookEndpointRepo_UpdateRoundTripsEventsArray(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM webhook_endpoints")
	})

	repo, err := postgres.NewWebhookEndpointRepo(db)
	if err != nil {
		t.Fatalf("NewWebhookEndpointRepo: %v", err)
	}
	ctx := context.Background()

	ep := &domainwebhook.Endpoint{
		URL:    "https://example.com/hooks",
		Secret: "whsec_test",
		Events: []string{"order.created"},
		Active: true,
	}
	if err := repo.Create(ctx, ep); err != nil {
		t.Fatalf("Create: %v", err)
	}

	ep.Events = []string{"order.created", "order.cancelled"}
	ep.Active = false
	if err := repo.Update(ctx, ep); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.FindByID(ctx, ep.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Active {
		t.Error("expected Active to be false after update")
	}
	assertSameEvents(t, got.Events, ep.Events)
}

func TestWebhookEndpointRepo_ListActive(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM webhook_endpoints")
	})

	repo, err := postgres.NewWebhookEndpointRepo(db)
	if err != nil {
		t.Fatalf("NewWebhookEndpointRepo: %v", err)
	}
	ctx := context.Background()

	active := &domainwebhook.Endpoint{URL: "https://a.example.com", Secret: "s", Events: []string{"order.created"}, Active: true}
	inactive := &domainwebhook.Endpoint{URL: "https://b.example.com", Secret: "s", Events: []string{"order.created"}, Active: false}
	for _, e := range []*domainwebhook.Endpoint{active, inactive} {
		if err := repo.Create(ctx, e); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	list, err := repo.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	for _, e := range list {
		if e.ID == inactive.ID {
			t.Fatal("ListActive returned an inactive endpoint")
		}
	}
}

func TestWebhookEndpointRepo_Delete(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM webhook_endpoints")
	})

	repo, err := postgres.NewWebhookEndpointRepo(db)
	if err != nil {
		t.Fatalf("NewWebhookEndpointRepo: %v", err)
	}
	ctx := context.Background()

	ep := &domainwebhook.Endpoint{URL: "https://example.com", Secret: "s", Events: []string{"order.created"}}
	if err := repo.Create(ctx, ep); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.Delete(ctx, ep.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := repo.FindByID(ctx, ep.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestWebhookEndpointRepo_Delete_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewWebhookEndpointRepo(db)
	if err != nil {
		t.Fatalf("NewWebhookEndpointRepo: %v", err)
	}
	ctx := context.Background()

	err = repo.Delete(ctx, "00000000-0000-0000-0000-000000000000")
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Errorf("expected NotFound error, got: %v", err)
	}
}

func assertSameEvents(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("Events: got %v, want %v", got, want)
	}
	gotSorted := append([]string(nil), got...)
	wantSorted := append([]string(nil), want...)
	sort.Strings(gotSorted)
	sort.Strings(wantSorted)
	for i := range gotSorted {
		if gotSorted[i] != wantSorted[i] {
			t.Fatalf("Events: got %v, want %v", got, want)
		}
	}
}
