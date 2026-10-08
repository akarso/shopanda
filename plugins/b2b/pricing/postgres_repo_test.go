package pricing_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/akarso/shopanda/internal/domain/customergroup"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/migrate"
	"github.com/akarso/shopanda/plugins/b2b"
	"github.com/akarso/shopanda/plugins/b2b/groups"
	b2bpricing "github.com/akarso/shopanda/plugins/b2b/pricing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("SHOPANDA_TEST_DSN")
	if dsn == "" {
		t.Skip("SHOPANDA_TEST_DSN not set; skipping integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func ensureTables(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run core migrations: %v", err)
	}
	if err := b2b.RunMigrationsForTest(db); err != nil {
		t.Fatalf("run b2b migrations: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM customer_group_prices`)
		_, _ = db.Exec(`DELETE FROM customer_group_members`)
		_, _ = db.Exec(`DELETE FROM customer_groups`)
	})
}

func seedVariant(t *testing.T, db *sql.DB) string {
	t.Helper()
	productID := id.New()
	variantID := id.New()
	if _, err := db.Exec(`INSERT INTO products (id, name, slug, status) VALUES ($1, 'p', $2, 'active')
		ON CONFLICT (id) DO NOTHING`, productID, "p-"+productID[:8]); err != nil {
		t.Fatalf("insert product: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO variants (id, product_id, sku, name) VALUES ($1, $2, $3, 'v')
		ON CONFLICT (id) DO NOTHING`, variantID, productID, "sku-"+variantID[:8]); err != nil {
		t.Fatalf("insert variant: %v", err)
	}
	return variantID
}

func TestPostgresGroupPriceRepo_UpsertZeroAmount(t *testing.T) {
	db := testDB(t)
	ensureTables(t, db)

	groupRepo, err := groups.NewPostgresRepo(db)
	if err != nil {
		t.Fatalf("NewPostgresRepo: %v", err)
	}
	g, err := customergroup.NewGroup(id.New(), "zero-price-group", "Zero price group", "")
	if err != nil {
		t.Fatalf("NewGroup: %v", err)
	}
	if err := groupRepo.Save(context.Background(), &g); err != nil {
		t.Fatalf("Save group: %v", err)
	}

	priceRepo, err := b2bpricing.NewPostgresGroupPriceRepo(db)
	if err != nil {
		t.Fatalf("NewPostgresGroupPriceRepo: %v", err)
	}
	vid := seedVariant(t, db)
	amount := shared.MustNewMoney(0, "EUR")
	gp, err := customergroup.NewGroupPrice(id.New(), g.ID, vid, "", amount)
	if err != nil {
		t.Fatalf("NewGroupPrice: %v", err)
	}
	if err := priceRepo.Upsert(context.Background(), &gp); err != nil {
		t.Fatalf("Upsert zero amount: %v", err)
	}

	got, err := priceRepo.FindByVariantsGroupCurrencyAndStore(context.Background(), []string{vid}, g.ID, "EUR", "")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	p := got[vid]
	if p == nil || !p.Amount.IsZero() {
		t.Fatalf("got = %#v, want zero amount", p)
	}
}
