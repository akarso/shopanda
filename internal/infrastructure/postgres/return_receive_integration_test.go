package postgres_test

import (
	"context"
	"database/sql"
	"io"
	"math"
	"sync"
	"testing"
	"time"

	returnsapp "github.com/akarso/shopanda/internal/application/returns"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	domainReturns "github.com/akarso/shopanda/internal/domain/returns"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// Sorted so Receive restocks the low id before the high id.
const (
	receiveVariantLow  = "00000000-0000-4000-8000-000000000001"
	receiveVariantHigh = "ffffffff-ffff-4fff-bfff-ffffffffffff"
)

func TestReturnReceive_FailureAfterStockWriteRollsBack(t *testing.T) {
	db := testDB(t)
	ensureVariantsTable(t, db)
	ctx := context.Background()
	lowQty := 5

	returnID := saveReceivableReturn(t, db, []receiveLine{
		{variantID: receiveVariantLow, sku: "SKU-RECV-LOW", stock: lowQty},
		{variantID: receiveVariantHigh, sku: "SKU-RECV-HIGH", stock: math.MaxInt32},
	})
	svc := newReturnService(t, db)

	if _, err := svc.Receive(ctx, returnID); err == nil {
		t.Fatal("Receive: expected failure after the first stock write")
	}

	if got := stockQty(t, db, receiveVariantLow); got != lowQty {
		t.Fatalf("low stock = %d, want %d", got, lowQty)
	}
	if got := stockQty(t, db, receiveVariantHigh); got != math.MaxInt32 {
		t.Fatalf("high stock = %d, want %d", got, math.MaxInt32)
	}
	repo, err := postgres.NewReturnRepo(db)
	if err != nil {
		t.Fatalf("NewReturnRepo: %v", err)
	}
	got, err := repo.FindByID(ctx, returnID)
	if err != nil || got == nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status() != domainReturns.StatusApproved {
		t.Fatalf("status = %q, want approved", got.Status())
	}
	if got.RestockedAt != nil {
		t.Fatalf("restocked_at = %v, want nil", got.RestockedAt)
	}
}

func TestReturnReceive_OverlappingReceivesRestockOnce(t *testing.T) {
	db := testDB(t)
	ensureVariantsTable(t, db)
	ctx := context.Background()

	returnID := saveReceivableReturn(t, db, []receiveLine{
		{variantID: receiveVariantLow, sku: "SKU-RECV-LOW", stock: 5},
	})
	svc := newReturnService(t, db)

	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = svc.Receive(ctx, returnID)
		}(i)
	}
	close(start)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("overlapping Receive did not finish")
	}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Receive %d: %v", i, err)
		}
	}
	if got := stockQty(t, db, receiveVariantLow); got != 6 {
		t.Fatalf("stock = %d, want 6", got)
	}
	repo, err := postgres.NewReturnRepo(db)
	if err != nil {
		t.Fatalf("NewReturnRepo: %v", err)
	}
	got, err := repo.FindByID(ctx, returnID)
	if err != nil || got == nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status() != domainReturns.StatusReceived || got.RestockedAt == nil {
		t.Fatalf("status=%q restocked_at=%v, want received with restocked_at", got.Status(), got.RestockedAt)
	}
}

type receiveLine struct {
	variantID string
	sku       string
	stock     int
}

func saveReceivableReturn(t *testing.T, db *sql.DB, lines []receiveLine) string {
	t.Helper()
	cleanupReceiveRows(t, db)
	t.Cleanup(func() { cleanupReceiveRows(t, db) })

	ctx := context.Background()
	product := createTestProduct(t, db)
	variantRepo, err := postgres.NewVariantRepo(db)
	if err != nil {
		t.Fatalf("NewVariantRepo: %v", err)
	}
	stockRepo, err := postgres.NewStockRepo(db)
	if err != nil {
		t.Fatalf("NewStockRepo: %v", err)
	}
	orderRepo, err := postgres.NewOrderRepo(db)
	if err != nil {
		t.Fatalf("NewOrderRepo: %v", err)
	}
	returnRepo, err := postgres.NewReturnRepo(db)
	if err != nil {
		t.Fatalf("NewReturnRepo: %v", err)
	}

	price := shared.MustNewMoney(1000, "EUR")
	orderItems := make([]order.Item, 0, len(lines))
	returnItems := make([]domainReturns.Item, 0, len(lines))
	for _, line := range lines {
		variant, err := catalog.NewVariant(line.variantID, product.ID, line.sku)
		if err != nil {
			t.Fatalf("NewVariant: %v", err)
		}
		variant.Name = line.sku
		if err := variantRepo.Create(ctx, &variant); err != nil {
			t.Fatalf("Create variant: %v", err)
		}
		entry, err := inventory.NewStockEntry(line.variantID, line.stock)
		if err != nil {
			t.Fatalf("NewStockEntry: %v", err)
		}
		if err := stockRepo.SetStock(ctx, &entry); err != nil {
			t.Fatalf("SetStock: %v", err)
		}
		orderItem, err := order.NewItem(line.variantID, line.sku, line.sku, 1, price)
		if err != nil {
			t.Fatalf("order.NewItem: %v", err)
		}
		orderItems = append(orderItems, orderItem)
		returnItem, err := domainReturns.NewItem(line.variantID, line.sku, line.sku, 1, price)
		if err != nil {
			t.Fatalf("returns.NewItem: %v", err)
		}
		returnItems = append(returnItems, returnItem)
	}

	ord, err := order.NewOrder(id.New(), "cust-1", "", "EUR", orderItems)
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	if err := ord.Confirm(); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if err := ord.MarkPaid(); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	if err := orderRepo.Save(ctx, &ord); err != nil {
		t.Fatalf("Save order: %v", err)
	}
	ret, err := domainReturns.NewReturn(id.New(), ord.ID, ord.CustomerID, "damaged", "EUR", returnItems)
	if err != nil {
		t.Fatalf("NewReturn: %v", err)
	}
	if err := ret.Approve(); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := returnRepo.Save(ctx, &ret); err != nil {
		t.Fatalf("Save return: %v", err)
	}
	return ret.ID
}

func newReturnService(t *testing.T, db *sql.DB) *returnsapp.Service {
	t.Helper()
	returns, err := postgres.NewReturnRepo(db)
	if err != nil {
		t.Fatalf("NewReturnRepo: %v", err)
	}
	orders, err := postgres.NewOrderRepo(db)
	if err != nil {
		t.Fatalf("NewOrderRepo: %v", err)
	}
	stock, err := postgres.NewStockRepo(db)
	if err != nil {
		t.Fatalf("NewStockRepo: %v", err)
	}
	variants, err := postgres.NewVariantRepo(db)
	if err != nil {
		t.Fatalf("NewVariantRepo: %v", err)
	}
	log := logger.NewWithWriter(io.Discard, "info")
	return returnsapp.NewService(returns, orders, stock, variants, noopPayments{}, nil, event.NewBus(log), log).WithTxStarter(db)
}

func stockQty(t *testing.T, db *sql.DB, variantID string) int {
	t.Helper()
	repo, err := postgres.NewStockRepo(db)
	if err != nil {
		t.Fatalf("NewStockRepo: %v", err)
	}
	entry, err := repo.GetStock(context.Background(), variantID)
	if err != nil {
		t.Fatalf("GetStock: %v", err)
	}
	return entry.Quantity
}

func cleanupReceiveRows(t *testing.T, db *sql.DB) {
	t.Helper()
	mustExec(t, db, "DELETE FROM order_return_items")
	mustExec(t, db, "DELETE FROM order_returns")
	mustExec(t, db, "DELETE FROM order_items")
	mustExec(t, db, "DELETE FROM orders")
	mustExec(t, db, "DELETE FROM stock WHERE variant_id IN ($1, $2)", receiveVariantLow, receiveVariantHigh)
	mustExec(t, db, "DELETE FROM variants WHERE id IN ($1, $2)", receiveVariantLow, receiveVariantHigh)
}

type noopPayments struct{}

func (noopPayments) FindByID(context.Context, string) (*payment.Payment, error) {
	return nil, nil
}
func (noopPayments) FindByOrderID(context.Context, string) (*payment.Payment, error) {
	return nil, nil
}
func (noopPayments) Create(context.Context, *payment.Payment) error { return nil }
func (noopPayments) UpdateStatus(context.Context, *payment.Payment, time.Time) error {
	return nil
}
func (noopPayments) List(context.Context, payment.ListFilter) ([]payment.Payment, error) {
	return nil, nil
}

var _ payment.PaymentRepository = noopPayments{}
