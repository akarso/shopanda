package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

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

func TestReturnRepo_LockForRestock_SecondUpdateAlreadyRestocked(t *testing.T) {
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
	if err := ret.Approve(); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := repo.Save(ctx, &ret); err != nil {
		t.Fatalf("Save: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	bound, ok := repo.WithTx(tx).(*postgres.ReturnRepo)
	if !ok {
		t.Fatal("WithTx did not return *ReturnRepo")
	}
	locked, err := bound.LockForRestock(ctx, ret.ID)
	if err != nil {
		t.Fatalf("LockForRestock: %v", err)
	}
	if locked == nil || locked.RestockedAt != nil {
		t.Fatalf("locked = %+v, want unrestocked return", locked)
	}
	if err := locked.MarkReceived(); err != nil {
		t.Fatalf("MarkReceived: %v", err)
	}
	if err := bound.Update(ctx, locked); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := locked.RecordRestocked(time.Now().UTC()); err != nil {
		t.Fatalf("RecordRestocked: %v", err)
	}
	if err := bound.UpdateIfUnrestocked(ctx, locked); err != nil {
		t.Fatalf("UpdateIfUnrestocked: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	tx2, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx 2: %v", err)
	}
	defer tx2.Rollback()
	bound2, ok := repo.WithTx(tx2).(*postgres.ReturnRepo)
	if !ok {
		t.Fatal("WithTx did not return *ReturnRepo")
	}
	locked2, err := bound2.LockForRestock(ctx, ret.ID)
	if err != nil {
		t.Fatalf("LockForRestock 2: %v", err)
	}
	if locked2 == nil || locked2.RestockedAt == nil {
		t.Fatal("second lock should see restocked_at")
	}
	if err := bound2.UpdateIfUnrestocked(ctx, locked2); !errors.Is(err, domainReturns.ErrAlreadyRestocked) {
		t.Fatalf("UpdateIfUnrestocked = %v, want ErrAlreadyRestocked", err)
	}
}

func TestReturnRepo_Update_StaleApproveKeepsRestock(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo, returnID := saveRequestedReturn(t, db)
	ctx := context.Background()

	first, err := repo.FindByID(ctx, returnID)
	if err != nil || first == nil {
		t.Fatalf("FindByID first: %v", err)
	}
	second, err := repo.FindByID(ctx, returnID)
	if err != nil || second == nil {
		t.Fatalf("FindByID second: %v", err)
	}

	if err := first.Approve(); err != nil {
		t.Fatalf("Approve first: %v", err)
	}
	if err := repo.Update(ctx, first); err != nil {
		t.Fatalf("Update approve: %v", err)
	}
	if first.StoredStatus() != domainReturns.StatusApproved {
		t.Fatalf("stored after approve = %q", first.StoredStatus())
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	bound, ok := repo.WithTx(tx).(*postgres.ReturnRepo)
	if !ok {
		t.Fatal("WithTx did not return *ReturnRepo")
	}
	locked, err := bound.LockForRestock(ctx, returnID)
	if err != nil {
		t.Fatalf("LockForRestock: %v", err)
	}
	if err := locked.MarkReceived(); err != nil {
		t.Fatalf("MarkReceived: %v", err)
	}
	if err := bound.Update(ctx, locked); err != nil {
		t.Fatalf("Update received: %v", err)
	}
	restockedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := locked.RecordRestocked(restockedAt); err != nil {
		t.Fatalf("RecordRestocked: %v", err)
	}
	if err := bound.UpdateIfUnrestocked(ctx, locked); err != nil {
		t.Fatalf("UpdateIfUnrestocked: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := second.Approve(); err != nil {
		t.Fatalf("Approve stale: %v", err)
	}
	if err := repo.Update(ctx, second); !errors.Is(err, domainReturns.ErrStatusConflict) {
		t.Fatalf("stale Update = %v, want ErrStatusConflict", err)
	}

	got, err := repo.FindByID(ctx, returnID)
	if err != nil || got == nil {
		t.Fatalf("FindByID after stale approve: %v", err)
	}
	if got.Status() != domainReturns.StatusReceived {
		t.Fatalf("status = %q, want received", got.Status())
	}
	if got.RestockedAt == nil || !got.RestockedAt.Equal(restockedAt) {
		t.Fatalf("restocked_at = %v, want %v", got.RestockedAt, restockedAt)
	}
}

func TestReturnRepo_Update_NilTimestampDoesNotClear(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo, returnID := saveRequestedReturn(t, db)
	ctx := context.Background()

	loaded, err := repo.FindByID(ctx, returnID)
	if err != nil || loaded == nil {
		t.Fatalf("FindByID: %v", err)
	}
	if err := loaded.Approve(); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := repo.Update(ctx, loaded); err != nil {
		t.Fatalf("Update approve: %v", err)
	}
	if err := loaded.MarkReceived(); err != nil {
		t.Fatalf("MarkReceived: %v", err)
	}
	if err := repo.Update(ctx, loaded); err != nil {
		t.Fatalf("Update received: %v", err)
	}
	restockedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := loaded.RecordRestocked(restockedAt); err != nil {
		t.Fatalf("RecordRestocked: %v", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	bound, ok := repo.WithTx(tx).(*postgres.ReturnRepo)
	if !ok {
		t.Fatal("WithTx did not return *ReturnRepo")
	}
	if err := bound.UpdateIfUnrestocked(ctx, loaded); err != nil {
		t.Fatalf("UpdateIfUnrestocked: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	refunded, err := repo.FindByID(ctx, returnID)
	if err != nil || refunded == nil {
		t.Fatalf("FindByID refund: %v", err)
	}
	refundedAt := restockedAt.Add(time.Second)
	if err := refunded.MarkRefunded(refundedAt); err != nil {
		t.Fatalf("MarkRefunded: %v", err)
	}
	if err := repo.Update(ctx, refunded); err != nil {
		t.Fatalf("Update refund: %v", err)
	}

	refunded.RestockedAt = nil
	refunded.RefundedAt = nil
	if err := repo.Update(ctx, refunded); err != nil {
		t.Fatalf("Update nil timestamps: %v", err)
	}
	got, err := repo.FindByID(ctx, returnID)
	if err != nil || got == nil {
		t.Fatalf("FindByID final: %v", err)
	}
	if got.Status() != domainReturns.StatusRefunded {
		t.Fatalf("status = %q, want refunded", got.Status())
	}
	if got.RestockedAt == nil || !got.RestockedAt.Equal(restockedAt) {
		t.Fatalf("restocked_at = %v, want %v", got.RestockedAt, restockedAt)
	}
	if got.RefundedAt == nil || !got.RefundedAt.Equal(refundedAt) {
		t.Fatalf("refunded_at = %v, want %v", got.RefundedAt, refundedAt)
	}
}

func TestReturnRepo_Update_Missing(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo, err := postgres.NewReturnRepo(db)
	if err != nil {
		t.Fatalf("NewReturnRepo: %v", err)
	}
	item, err := domainReturns.NewItem("var-1", "SKU-001", "Test Product", 1, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	ret, err := domainReturns.NewReturn(id.New(), id.New(), "cust-1", "damaged", "EUR", []domainReturns.Item{item})
	if err != nil {
		t.Fatalf("NewReturn: %v", err)
	}
	if err := ret.Approve(); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	err = repo.Update(context.Background(), &ret)
	if err == nil || errors.Is(err, domainReturns.ErrStatusConflict) {
		t.Fatalf("Update missing = %v, want not found", err)
	}
}

func saveRequestedReturn(t *testing.T, db *sql.DB) (*postgres.ReturnRepo, string) {
	t.Helper()
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
		t.Fatalf("Save return: %v", err)
	}
	return repo, ret.ID
}
