package postgres_test

import (
	"context"
	"testing"

	domainReturns "github.com/akarso/shopanda/internal/domain/returns"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

func TestReturnRepo_NilDB(t *testing.T) {
	_, err := postgres.NewReturnRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestReturnRepo_SaveFindUpdate(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM order_return_items")
	mustExec(t, db, "DELETE FROM order_returns")
	t.Cleanup(func() {
		mustExec(t, db, "DELETE FROM order_return_items")
		mustExec(t, db, "DELETE FROM order_returns")
		mustExec(t, db, "DELETE FROM order_items")
		mustExec(t, db, "DELETE FROM orders")
	})

	orderRepo, err := postgres.NewOrderRepo(db)
	if err != nil {
		t.Fatalf("NewOrderRepo: %v", err)
	}
	repo, err := postgres.NewReturnRepo(db)
	if err != nil {
		t.Fatalf("NewReturnRepo: %v", err)
	}
	ctx := context.Background()

	ord := mustNewOrder(t, "cust-1", "EUR")
	if err := ord.Confirm(); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if err := ord.MarkPaid(); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	if err := orderRepo.Save(ctx, &ord); err != nil {
		t.Fatalf("Save order: %v", err)
	}

	item, err := domainReturns.NewItem("var-1", "SKU-001", "Test Product", 1, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	ret, err := domainReturns.NewReturn(id.New(), ord.ID, ord.CustomerID, "damaged", "EUR", []domainReturns.Item{item})
	if err != nil {
		t.Fatalf("NewReturn: %v", err)
	}
	if err := repo.Save(ctx, &ret); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByID(ctx, ret.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil || got.Status() != domainReturns.StatusRequested {
		t.Fatalf("got = %+v", got)
	}

	if err := got.Approve(); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}

	list, err := repo.FindByOrderID(ctx, ord.ID)
	if err != nil {
		t.Fatalf("FindByOrderID: %v", err)
	}
	if len(list) != 1 || list[0].Status() != domainReturns.StatusApproved {
		t.Fatalf("list = %+v", list)
	}
}

func TestReturnRepo_FindByID_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo, err := postgres.NewReturnRepo(db)
	if err != nil {
		t.Fatalf("NewReturnRepo: %v", err)
	}
	got, err := repo.FindByID(context.Background(), id.New())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil")
	}
}
