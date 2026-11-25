package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/voucher"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

func TestVoucherRepo_InsertIdempotentAndCodeConflict(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM voucher_redemptions")
		db.Exec("DELETE FROM vouchers")
		db.Exec("DELETE FROM order_items")
		db.Exec("DELETE FROM orders")
	})

	orderRepo, err := postgres.NewOrderRepo(db)
	if err != nil {
		t.Fatalf("NewOrderRepo: %v", err)
	}
	o := mustNewOrder(t, "cust-v", "EUR")
	if err := orderRepo.Save(context.Background(), &o); err != nil {
		t.Fatalf("Save order: %v", err)
	}
	prodID := id.New()
	mustExec(t, db, `INSERT INTO products (id, name, slug) VALUES ($1, $2, $3)`, prodID, "Pass", "pass-"+prodID)

	repo, err := postgres.NewVoucherRepo(db)
	if err != nil {
		t.Fatalf("NewVoucherRepo: %v", err)
	}
	ctx := context.Background()
	code, err := voucher.GenerateCode()
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	v, err := voucher.NewVoucher(id.New(), code, o.ID, voucher.OrderItemKey(o.ID, "var-1"), prodID, "var-1", "cust-v", 0, 1, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewVoucher: %v", err)
	}
	if err := repo.Insert(ctx, v); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	dupSeq, err := voucher.NewVoucher(id.New(), mustNewCode(t), o.ID, v.OrderItemID, prodID, "var-1", "cust-v", 0, 1, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewVoucher dup seq: %v", err)
	}
	err = repo.Insert(ctx, dupSeq)
	if err == nil || !apperror.Is(err, apperror.CodeConflict) {
		t.Fatalf("dup seq Insert = %v, want conflict", err)
	}

	codeTaken, err := voucher.NewVoucher(id.New(), code, o.ID, v.OrderItemID, prodID, "var-1", "cust-v", 1, 1, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewVoucher code taken: %v", err)
	}
	err = repo.Insert(ctx, codeTaken)
	if !errors.Is(err, voucher.ErrCodeTaken) {
		t.Fatalf("dup code Insert = %v, want ErrCodeTaken", err)
	}
}

func TestVoucherRepo_ListStatusBeforeLimit(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM voucher_redemptions")
		db.Exec("DELETE FROM vouchers")
		db.Exec("DELETE FROM order_items")
		db.Exec("DELETE FROM orders")
	})

	orderRepo, err := postgres.NewOrderRepo(db)
	if err != nil {
		t.Fatalf("NewOrderRepo: %v", err)
	}
	o := mustNewOrder(t, "cust-v2", "EUR")
	if err := orderRepo.Save(context.Background(), &o); err != nil {
		t.Fatalf("Save order: %v", err)
	}
	prodID := id.New()
	mustExec(t, db, `INSERT INTO products (id, name, slug) VALUES ($1, $2, $3)`, prodID, "Pass2", "pass2-"+prodID)

	repo, err := postgres.NewVoucherRepo(db)
	if err != nil {
		t.Fatalf("NewVoucherRepo: %v", err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		v, err := voucher.NewVoucher(id.New(), mustNewCode(t), o.ID, voucher.OrderItemKey(o.ID, "var-1"), prodID, "var-1", "cust-v2", i, 1, now.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatalf("NewVoucher %d: %v", i, err)
		}
		if err := repo.Insert(ctx, v); err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
		if i == 0 {
			if err := repo.Void(ctx, v.ID, now); err != nil {
				t.Fatalf("Void: %v", err)
			}
		}
	}
	issued, err := repo.List(ctx, voucher.ListFilter{OrderID: o.ID, Status: voucher.StatusIssued, Limit: 1})
	if err != nil {
		t.Fatalf("List issued: %v", err)
	}
	if len(issued) != 1 || issued[0].Status() != voucher.StatusIssued {
		t.Fatalf("issued page = %+v, want 1 issued row (status filter before LIMIT)", issued)
	}
	voided, err := repo.List(ctx, voucher.ListFilter{OrderID: o.ID, Status: voucher.StatusVoid, Limit: 10})
	if err != nil {
		t.Fatalf("List void: %v", err)
	}
	if len(voided) != 1 || voided[0].Status() != voucher.StatusVoid {
		t.Fatalf("voided = %+v, want 1", voided)
	}
}

func TestVoucherRepo_UpdateCountsNotFoundVsConflict(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM voucher_redemptions")
		db.Exec("DELETE FROM vouchers")
		db.Exec("DELETE FROM order_items")
		db.Exec("DELETE FROM orders")
	})

	orderRepo, err := postgres.NewOrderRepo(db)
	if err != nil {
		t.Fatalf("NewOrderRepo: %v", err)
	}
	o := mustNewOrder(t, "cust-v3", "EUR")
	if err := orderRepo.Save(context.Background(), &o); err != nil {
		t.Fatalf("Save order: %v", err)
	}
	prodID := id.New()
	mustExec(t, db, `INSERT INTO products (id, name, slug) VALUES ($1, $2, $3)`, prodID, "Pass3", "pass3-"+prodID)

	repo, err := postgres.NewVoucherRepo(db)
	if err != nil {
		t.Fatalf("NewVoucherRepo: %v", err)
	}
	ctx := context.Background()
	v, err := voucher.NewVoucher(id.New(), mustNewCode(t), o.ID, voucher.OrderItemKey(o.ID, "var-1"), prodID, "var-1", "", 0, 2, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewVoucher: %v", err)
	}
	if err := repo.Insert(ctx, v); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := repo.UpdateCounts(ctx, v.ID, 1, 0); err != nil {
		t.Fatalf("UpdateCounts: %v", err)
	}
	err = repo.UpdateCounts(ctx, v.ID, 2, 0) // stale expected
	if err == nil || !apperror.Is(err, apperror.CodeConflict) {
		t.Fatalf("stale CAS = %v, want conflict", err)
	}
	if err := repo.Void(ctx, v.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Void: %v", err)
	}
	err = repo.UpdateCounts(ctx, v.ID, 2, 1)
	if err == nil || !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("void UpdateCounts = %v, want not found", err)
	}
	if err := repo.LinkCustomer(ctx, o.ID, "linked-cust"); err != nil {
		t.Fatalf("LinkCustomer: %v", err)
	}
	got, err := repo.FindByID(ctx, v.ID)
	if err != nil || got == nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.CustomerID != "linked-cust" {
		t.Fatalf("customer_id = %q, want linked-cust", got.CustomerID)
	}
}

func mustNewCode(t *testing.T) string {
	t.Helper()
	code, err := voucher.GenerateCode()
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	return code
}
