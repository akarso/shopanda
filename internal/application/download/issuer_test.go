package download_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"sync"
	"testing"
	"time"

	downloadapp "github.com/akarso/shopanda/internal/application/download"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domain "github.com/akarso/shopanda/internal/domain/download"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
)

type stubOrders struct {
	o   *order.Order
	err error
}

func (s stubOrders) FindByID(context.Context, string) (*order.Order, error) {
	return s.o, s.err
}

type stubPayments struct {
	byOrder map[string]*payment.Payment
}

func (s stubPayments) FindByOrderID(_ context.Context, orderID string) (*payment.Payment, error) {
	if s.byOrder == nil {
		return nil, nil
	}
	return s.byOrder[orderID], nil
}

type stubVariants map[string]*catalog.Variant

func (s stubVariants) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	return s[id], nil
}

type stubProducts map[string]*catalog.Product

func (s stubProducts) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	return s[id], nil
}

type stubFiles map[string][]catalog.DownloadableFile

func (s stubFiles) ListByProductID(_ context.Context, productID string) ([]catalog.DownloadableFile, error) {
	if rows, ok := s[productID]; ok {
		return rows, nil
	}
	return []catalog.DownloadableFile{}, nil
}

type stubSettings map[string]*domain.GrantSettings

func (s stubSettings) FindByProductID(_ context.Context, productID string) (*domain.GrantSettings, error) {
	return s[productID], nil
}

type stubComponents map[string][]catalog.BundleComponent

func (s stubComponents) ListByBundleID(_ context.Context, bundleID string) ([]catalog.BundleComponent, error) {
	if rows, ok := s[bundleID]; ok {
		return rows, nil
	}
	return []catalog.BundleComponent{}, nil
}

func mustMoney(t *testing.T) shared.Money {
	t.Helper()
	m, err := shared.NewMoney(100, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	return m
}

func mustZeroMoney(t *testing.T) shared.Money {
	t.Helper()
	m, err := shared.NewMoney(0, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	return m
}

func markPaid(t *testing.T, o *order.Order) {
	t.Helper()
	if err := o.Confirm(); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if err := o.MarkPaid(); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
}

func TestIssuer_HandleOrderPaid_IssuesPerAsset(t *testing.T) {
	orderID := id.New()
	productID := id.New()
	variantID := id.New()
	price := mustMoney(t)
	item, err := order.NewItem(variantID, "SKU-1", "DL", 2, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(orderID, "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	markPaid(t, &o)
	product := &catalog.Product{ID: productID, Name: "DL", Slug: "dl", Type: catalog.TypeDownloadable, Status: catalog.StatusActive}
	repo := newMemGrantRepo()
	svc := downloadapp.NewService(repo, nil)
	issuer := downloadapp.NewIssuer(
		stubOrders{o: &o},
		stubVariants{variantID: {ID: variantID, ProductID: productID, SKU: "SKU-1", Name: "DL"}},
		stubProducts{productID: product},
		stubFiles{productID: {
			{ProductID: productID, AssetID: "asset-a"},
			{ProductID: productID, AssetID: "asset-b"},
		}},
		nil,
		svc,
		domain.Defaults{Expiry: 24 * time.Hour, DownloadLimit: 5},
		nil,
	)

	evt := event.New(order.EventOrderPaid, "core.order", order.OrderStatusChangedData{
		OrderID:   orderID,
		OldStatus: "pending",
		NewStatus: "paid",
	})
	if err := issuer.HandleOrderPaid(context.Background(), evt); err != nil {
		t.Fatalf("HandleOrderPaid: %v", err)
	}
	rows, err := svc.ListByOrderItem(context.Background(), domain.OrderItemID(orderID, variantID))
	if err != nil {
		t.Fatalf("ListByOrderItem: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("grants = %d, want 2", len(rows))
	}
	for _, g := range rows {
		if g.DownloadLimit != 10 { // 5 * qty 2
			t.Fatalf("limit = %d, want 10", g.DownloadLimit)
		}
		if g.CustomerID != "cust-1" {
			t.Fatalf("customer = %q", g.CustomerID)
		}
	}

	// Idempotent re-delivery.
	n, err := issuer.IssueForOrder(context.Background(), orderID)
	if err != nil {
		t.Fatalf("IssueForOrder: %v", err)
	}
	if n != 0 {
		t.Fatalf("re-issue created %d, want 0", n)
	}
}

func TestIssuer_QtyCap10000(t *testing.T) {
	orderID := id.New()
	productID := id.New()
	variantID := id.New()
	price := mustMoney(t)
	item, err := order.NewItem(variantID, "SKU-1", "DL", 3000, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(orderID, "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	markPaid(t, &o)
	svc := downloadapp.NewService(newMemGrantRepo(), nil)
	issuer := downloadapp.NewIssuer(
		stubOrders{o: &o},
		stubVariants{variantID: {ID: variantID, ProductID: productID, SKU: "SKU-1", Name: "DL"}},
		stubProducts{productID: {ID: productID, Name: "DL", Slug: "dl", Type: catalog.TypeDownloadable, Status: catalog.StatusActive}},
		stubFiles{productID: {{ProductID: productID, AssetID: "asset-a"}}},
		nil,
		svc,
		domain.Defaults{Expiry: time.Hour, DownloadLimit: 5},
		nil,
	)
	n, err := issuer.IssueForOrder(context.Background(), orderID)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	rows, _ := svc.ListByOrderItem(context.Background(), domain.OrderItemID(orderID, variantID))
	if len(rows) != 1 || rows[0].DownloadLimit != domain.MaxDownloadLimit {
		t.Fatalf("limit=%v want %d", rows, domain.MaxDownloadLimit)
	}
}

func TestIssuer_UnpaidRefused(t *testing.T) {
	orderID := id.New()
	variantID := id.New()
	price := mustMoney(t)
	item, err := order.NewItem(variantID, "SKU-1", "DL", 1, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(orderID, "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	// pending, payable > 0, no payment → refuse
	svc := downloadapp.NewService(newMemGrantRepo(), nil)
	issuer := downloadapp.NewIssuer(
		stubOrders{o: &o},
		stubVariants{variantID: {ID: variantID, ProductID: "p", SKU: "SKU-1", Name: "DL"}},
		stubProducts{"p": {ID: "p", Type: catalog.TypeDownloadable, Status: catalog.StatusActive}},
		stubFiles{"p": {{ProductID: "p", AssetID: "a"}}},
		nil,
		svc,
		domain.Defaults{Expiry: time.Hour, DownloadLimit: 1},
		nil,
	).WithPayments(stubPayments{})
	_, err = issuer.IssueForOrder(context.Background(), orderID)
	if err == nil || !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("err = %v, want unpaid validation", err)
	}
}

func TestIssuer_ZeroPayableAllowed(t *testing.T) {
	orderID := id.New()
	productID := id.New()
	variantID := id.New()
	price := mustZeroMoney(t)
	item, err := order.NewItem(variantID, "SKU-1", "Free", 1, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(orderID, "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	svc := downloadapp.NewService(newMemGrantRepo(), nil)
	issuer := downloadapp.NewIssuer(
		stubOrders{o: &o},
		stubVariants{variantID: {ID: variantID, ProductID: productID, SKU: "SKU-1", Name: "Free"}},
		stubProducts{productID: {ID: productID, Type: catalog.TypeDownloadable, Status: catalog.StatusActive}},
		stubFiles{productID: {{ProductID: productID, AssetID: "asset-a"}}},
		nil,
		svc,
		domain.Defaults{Expiry: time.Hour, DownloadLimit: 1},
		nil,
	).WithPayments(stubPayments{})
	n, err := issuer.IssueForOrder(context.Background(), orderID)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestIssuer_MissingFilesHardFail(t *testing.T) {
	orderID := id.New()
	productID := id.New()
	variantID := id.New()
	price := mustMoney(t)
	item, err := order.NewItem(variantID, "SKU-1", "DL", 1, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(orderID, "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	markPaid(t, &o)
	issuer := downloadapp.NewIssuer(
		stubOrders{o: &o},
		stubVariants{variantID: {ID: variantID, ProductID: productID, SKU: "SKU-1", Name: "DL"}},
		stubProducts{productID: {ID: productID, Type: catalog.TypeDownloadable, Status: catalog.StatusActive}},
		stubFiles{},
		nil,
		downloadapp.NewService(newMemGrantRepo(), nil),
		domain.Defaults{Expiry: time.Hour, DownloadLimit: 1},
		nil,
	)
	_, err = issuer.IssueForOrder(context.Background(), orderID)
	if err == nil || !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("err = %v, want missing-files validation", err)
	}
}

func TestIssuer_BundleLeafGetsGrants(t *testing.T) {
	orderID := id.New()
	bundleID := id.New()
	leafID := id.New()
	variantID := id.New()
	price := mustMoney(t)
	item, err := order.NewItem(variantID, "BUNDLE", "Kit", 1, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(orderID, "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	markPaid(t, &o)
	svc := downloadapp.NewService(newMemGrantRepo(), nil)
	issuer := downloadapp.NewIssuer(
		stubOrders{o: &o},
		stubVariants{variantID: {ID: variantID, ProductID: bundleID, SKU: "BUNDLE", Name: "Kit"}},
		stubProducts{
			bundleID: {ID: bundleID, Type: catalog.TypeBundle, Status: catalog.StatusActive},
			leafID:   {ID: leafID, Type: catalog.TypeDownloadable, Status: catalog.StatusActive},
		},
		stubFiles{leafID: {{ProductID: leafID, AssetID: "leaf-asset"}}},
		nil,
		svc,
		domain.Defaults{Expiry: time.Hour, DownloadLimit: 3},
		nil,
	).WithBundleComponents(stubComponents{
		bundleID: {{
			BundleProductID:    bundleID,
			ComponentProductID: leafID,
			Quantity:           1,
			Required:           true,
		}},
	})
	n, err := issuer.IssueForOrder(context.Background(), orderID)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	rows, _ := svc.ListByOrderItem(context.Background(), domain.OrderItemID(orderID, variantID))
	if len(rows) != 1 || rows[0].AssetID != "leaf-asset" || rows[0].DownloadLimit != 3 {
		t.Fatalf("rows=%+v", rows)
	}
}

func TestIssuer_SkipsNonDownloadable(t *testing.T) {
	orderID := id.New()
	productID := id.New()
	variantID := id.New()
	price := mustMoney(t)
	item, err := order.NewItem(variantID, "SKU-1", "Simple", 1, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(orderID, "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	markPaid(t, &o)
	repo := newMemGrantRepo()
	svc := downloadapp.NewService(repo, nil)
	issuer := downloadapp.NewIssuer(
		stubOrders{o: &o},
		stubVariants{variantID: {ID: variantID, ProductID: productID, SKU: "SKU-1", Name: "S"}},
		stubProducts{productID: {ID: productID, Name: "S", Slug: "s", Type: catalog.TypeSimple, Status: catalog.StatusActive}},
		stubFiles{},
		nil,
		svc,
		domain.Defaults{Expiry: time.Hour, DownloadLimit: 1},
		nil,
	)
	n, err := issuer.IssueForOrder(context.Background(), orderID)
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestIssuer_ProductOverride(t *testing.T) {
	orderID := id.New()
	productID := id.New()
	variantID := id.New()
	price := mustMoney(t)
	item, err := order.NewItem(variantID, "SKU-1", "DL", 1, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(orderID, "", "guest@example.com", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	markPaid(t, &o)
	days := 3
	limit := 2
	repo := newMemGrantRepo()
	svc := downloadapp.NewService(repo, nil)
	issuer := downloadapp.NewIssuer(
		stubOrders{o: &o},
		stubVariants{variantID: {ID: variantID, ProductID: productID, SKU: "SKU-1", Name: "DL"}},
		stubProducts{productID: {ID: productID, Name: "DL", Slug: "dl", Type: catalog.TypeDownloadable, Status: catalog.StatusActive}},
		stubFiles{productID: {{ProductID: productID, AssetID: "asset-a"}}},
		stubSettings{productID: {ProductID: productID, ExpiryDays: &days, DownloadLimit: &limit}},
		svc,
		domain.Defaults{Expiry: 24 * time.Hour, DownloadLimit: 5},
		nil,
	)
	n, err := issuer.IssueForOrder(context.Background(), orderID)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	rows, _ := svc.ListByOrderItem(context.Background(), domain.OrderItemID(orderID, variantID))
	if len(rows) != 1 || rows[0].DownloadLimit != 2 {
		t.Fatalf("rows=%+v", rows)
	}
	if rows[0].CustomerID != "" {
		t.Fatalf("guest customer should be empty, got %q", rows[0].CustomerID)
	}
	wantExpiry := time.Now().UTC().Add(3 * 24 * time.Hour)
	if rows[0].ExpiresAt.Before(wantExpiry.Add(-time.Minute)) || rows[0].ExpiresAt.After(wantExpiry.Add(time.Minute)) {
		t.Fatalf("ExpiresAt = %v, want ~%v", rows[0].ExpiresAt, wantExpiry)
	}
}

func TestIssuer_BadEventData(t *testing.T) {
	issuer := downloadapp.NewIssuer(
		stubOrders{},
		stubVariants{},
		stubProducts{},
		stubFiles{},
		nil,
		downloadapp.NewService(newMemGrantRepo(), nil),
		domain.Defaults{Expiry: time.Hour, DownloadLimit: 1},
		nil,
	)
	err := issuer.HandleOrderPaid(context.Background(), event.New(order.EventOrderPaid, "core.order", "nope"))
	if err == nil {
		t.Fatal("expected type error")
	}
}

func TestIssuer_NilComponentsFailsClosedOnBundle(t *testing.T) {
	orderID := id.New()
	bundleID := id.New()
	variantID := id.New()
	price := mustMoney(t)
	item, err := order.NewItem(variantID, "BUNDLE", "Kit", 1, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(orderID, "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	markPaid(t, &o)
	issuer := downloadapp.NewIssuer(
		stubOrders{o: &o},
		stubVariants{variantID: {ID: variantID, ProductID: bundleID, SKU: "BUNDLE", Name: "Kit"}},
		stubProducts{bundleID: {ID: bundleID, Type: catalog.TypeBundle, Status: catalog.StatusActive}},
		stubFiles{},
		nil,
		downloadapp.NewService(newMemGrantRepo(), nil),
		domain.Defaults{Expiry: time.Hour, DownloadLimit: 1},
		nil,
	)
	_, err = issuer.IssueForOrder(context.Background(), orderID)
	if err == nil || !apperror.Is(err, apperror.CodeInternal) {
		t.Fatalf("err = %v, want internal fail-closed for nil components", err)
	}
}

func TestIssuer_NilProductFailsClosed(t *testing.T) {
	orderID := id.New()
	productID := id.New()
	variantID := id.New()
	price := mustMoney(t)
	item, err := order.NewItem(variantID, "SKU-1", "DL", 1, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(orderID, "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	markPaid(t, &o)
	issuer := downloadapp.NewIssuer(
		stubOrders{o: &o},
		stubVariants{variantID: {ID: variantID, ProductID: productID, SKU: "SKU-1", Name: "DL"}},
		stubProducts{}, // product missing
		stubFiles{},
		nil,
		downloadapp.NewService(newMemGrantRepo(), nil),
		domain.Defaults{Expiry: time.Hour, DownloadLimit: 1},
		nil,
	)
	_, err = issuer.IssueForOrder(context.Background(), orderID)
	if err == nil || !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("err = %v, want validation for missing product", err)
	}
}

func TestIssuer_SecondPaidDoesNotRestoreRevoked(t *testing.T) {
	orderID := id.New()
	productID := id.New()
	variantID := id.New()
	price := mustMoney(t)
	item, err := order.NewItem(variantID, "SKU-1", "DL", 1, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(orderID, "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	markPaid(t, &o)
	svc := downloadapp.NewService(newMemGrantRepo(), nil)
	issuer := downloadapp.NewIssuer(
		stubOrders{o: &o},
		stubVariants{variantID: {ID: variantID, ProductID: productID, SKU: "SKU-1", Name: "DL"}},
		stubProducts{productID: {ID: productID, Type: catalog.TypeDownloadable, Status: catalog.StatusActive}},
		stubFiles{productID: {{ProductID: productID, AssetID: "asset-a"}}},
		nil,
		svc,
		domain.Defaults{Expiry: time.Hour, DownloadLimit: 3},
		nil,
	)
	n, err := issuer.IssueForOrder(context.Background(), orderID)
	if err != nil || n != 1 {
		t.Fatalf("first issue n=%d err=%v", n, err)
	}
	rows, err := svc.ListByOrderItem(context.Background(), domain.OrderItemID(orderID, variantID))
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	if _, _, err := svc.Revoke(context.Background(), rows[0].ID, "return"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	n, err = issuer.IssueForOrder(context.Background(), orderID)
	if err != nil || n != 0 {
		t.Fatalf("second paid n=%d err=%v, want 0 (no resurrect)", n, err)
	}
	rows, _ = svc.ListByOrderItem(context.Background(), domain.OrderItemID(orderID, variantID))
	if len(rows) != 1 || !rows[0].Expired(time.Now().UTC()) {
		t.Fatalf("grant must stay revoked: %+v", rows)
	}
}

type stubBlockingReturns map[string]struct{}

func (s stubBlockingReturns) UnitsOnBlockingReturn(_ context.Context, unitIDs []string) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	for _, id := range unitIDs {
		if _, ok := s[id]; ok {
			out[id] = struct{}{}
		}
	}
	return out, nil
}

func TestIssuer_SkipsLinesOnBlockingReturn(t *testing.T) {
	orderID := id.New()
	productID := id.New()
	variantID := id.New()
	price := mustMoney(t)
	item, err := order.NewItem(variantID, "SKU-1", "DL", 1, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(orderID, "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	markPaid(t, &o)
	svc := downloadapp.NewService(newMemGrantRepo(), nil)
	orderItemID := domain.OrderItemID(orderID, variantID)
	issuer := downloadapp.NewIssuer(
		stubOrders{o: &o},
		stubVariants{variantID: {ID: variantID, ProductID: productID, SKU: "SKU-1", Name: "DL"}},
		stubProducts{productID: {ID: productID, Type: catalog.TypeDownloadable, Status: catalog.StatusActive}},
		stubFiles{productID: {{ProductID: productID, AssetID: "asset-a"}}},
		nil,
		svc,
		domain.Defaults{Expiry: time.Hour, DownloadLimit: 3},
		nil,
	).WithBlockingReturnChecker(stubBlockingReturns{orderItemID: {}})

	n, err := issuer.IssueForOrder(context.Background(), orderID)
	if err != nil || n != 0 {
		t.Fatalf("IssueForOrder n=%d err=%v, want skip", n, err)
	}
	rows, _ := svc.ListByOrderItem(context.Background(), orderItemID)
	if len(rows) != 0 {
		t.Fatalf("expected no grants for returned line, got %d", len(rows))
	}
}

type scopeAwareProducts struct {
	byID map[string]*catalog.Product
}

func (s scopeAwareProducts) FindByID(ctx context.Context, id string) (*catalog.Product, error) {
	p := s.byID[id]
	if p == nil {
		return nil, nil
	}
	if !catalog.IncludeNonActiveProducts(ctx) && p.Status != catalog.StatusActive {
		return nil, nil
	}
	return p, nil
}

func TestIssuer_IssuesForDraftProductAfterPurchase(t *testing.T) {
	orderID := id.New()
	productID := id.New()
	variantID := id.New()
	price := mustMoney(t)
	item, err := order.NewItem(variantID, "SKU-1", "DL", 1, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(orderID, "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	markPaid(t, &o)
	svc := downloadapp.NewService(newMemGrantRepo(), nil)
	issuer := downloadapp.NewIssuer(
		stubOrders{o: &o},
		stubVariants{variantID: {ID: variantID, ProductID: productID, SKU: "SKU-1", Name: "DL"}},
		scopeAwareProducts{byID: map[string]*catalog.Product{
			productID: {ID: productID, Type: catalog.TypeDownloadable, Status: catalog.StatusDraft},
		}},
		stubFiles{productID: {{ProductID: productID, AssetID: "asset-a"}}},
		nil,
		svc,
		domain.Defaults{Expiry: time.Hour, DownloadLimit: 2},
		nil,
	)
	n, err := issuer.IssueForOrder(context.Background(), orderID)
	if err != nil || n != 1 {
		t.Fatalf("IssueForOrder n=%d err=%v, want 1 for draft product", n, err)
	}
}

// Fake TX driver so IssueForOrder takes the lock path without a real DB.
func init() {
	sql.Register("shopanda-download-issuer-tx", issuerFakeTxDriver{})
}

type issuerFakeTxDriver struct{}

func (issuerFakeTxDriver) Open(string) (driver.Conn, error) { return issuerFakeTxConn{}, nil }

type issuerFakeTxConn struct{}

func (issuerFakeTxConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (issuerFakeTxConn) Close() error                        { return nil }
func (issuerFakeTxConn) Begin() (driver.Tx, error)           { return issuerFakeTx{}, nil }

type issuerFakeTx struct{}

func (issuerFakeTx) Commit() error   { return nil }
func (issuerFakeTx) Rollback() error { return nil }

var (
	issuerFakeTxOnce sync.Once
	issuerFakeTxDB   *sql.DB
)

func issuerTxStarter() *sql.DB {
	issuerFakeTxOnce.Do(func() {
		db, err := sql.Open("shopanda-download-issuer-tx", "")
		if err != nil {
			panic(err)
		}
		issuerFakeTxDB = db
	})
	return issuerFakeTxDB
}

type lockingStubProducts struct {
	catalog.ProductRepository // embed for unused ProductRepository methods
	byID                      map[string]*catalog.Product
	mu                        sync.Mutex
	locked                    []string
}

func (s *lockingStubProducts) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	return s.byID[id], nil
}

func (s *lockingStubProducts) WithTx(*sql.Tx) catalog.ProductRepository { return s }

func (s *lockingStubProducts) LockForUpdate(_ context.Context, id string) (catalog.Type, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.locked = append(s.locked, id)
	p := s.byID[id]
	if p == nil {
		return "", apperror.NotFound("product not found")
	}
	return p.Type, nil
}

func (s *lockingStubProducts) lockedIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.locked))
	copy(out, s.locked)
	return out
}

// mutatingComponents returns before on the first ListByBundleID call, then
// after — simulating an admin optional→required flip between collect and
// the post-lock recollect.
type mutatingComponents struct {
	mu     sync.Mutex
	calls  int
	before []catalog.BundleComponent
	after  []catalog.BundleComponent
}

func (m *mutatingComponents) ListByBundleID(_ context.Context, _ string) ([]catalog.BundleComponent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.calls == 1 {
		out := make([]catalog.BundleComponent, len(m.before))
		copy(out, m.before)
		return out, nil
	}
	out := make([]catalog.BundleComponent, len(m.after))
	copy(out, m.after)
	return out, nil
}

func (m *mutatingComponents) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func TestIssuer_BundleLeafSetChangeRetriesAndLocksNewLeaf(t *testing.T) {
	orderID := id.New()
	bundleID := id.New()
	leaf1 := id.New()
	leaf2 := id.New()
	variantID := id.New()
	price := mustMoney(t)
	item, err := order.NewItem(variantID, "BUNDLE", "Kit", 1, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(orderID, "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	markPaid(t, &o)

	products := &lockingStubProducts{byID: map[string]*catalog.Product{
		bundleID: {ID: bundleID, Type: catalog.TypeBundle, Status: catalog.StatusActive},
		leaf1:    {ID: leaf1, Type: catalog.TypeDownloadable, Status: catalog.StatusActive},
		leaf2:    {ID: leaf2, Type: catalog.TypeDownloadable, Status: catalog.StatusActive},
	}}
	comps := &mutatingComponents{
		before: []catalog.BundleComponent{{
			BundleProductID: bundleID, ComponentProductID: leaf1, Quantity: 1, Required: true,
		}},
		after: []catalog.BundleComponent{
			{BundleProductID: bundleID, ComponentProductID: leaf1, Quantity: 1, Required: true},
			{BundleProductID: bundleID, ComponentProductID: leaf2, Quantity: 1, Required: true},
		},
	}
	repo := &withTxMemRepo{memGrantRepo: newMemGrantRepo()}
	svc := downloadapp.NewService(repo, nil)
	issuer := downloadapp.NewIssuer(
		stubOrders{o: &o},
		stubVariants{variantID: {ID: variantID, ProductID: bundleID, SKU: "BUNDLE", Name: "Kit"}},
		products,
		stubFiles{
			leaf1: {{ProductID: leaf1, AssetID: "asset-1"}},
			leaf2: {{ProductID: leaf2, AssetID: "asset-2"}},
		},
		nil,
		svc,
		domain.Defaults{Expiry: time.Hour, DownloadLimit: 2},
		nil,
	).WithBundleComponents(comps).WithTxStarter(issuerTxStarter())

	n, err := issuer.IssueForOrder(context.Background(), orderID)
	if err != nil || n != 2 {
		t.Fatalf("IssueForOrder n=%d err=%v, want 2 grants after retry", n, err)
	}
	if comps.callCount() < 3 {
		t.Fatalf("component list calls=%d, want >=3 (collect, post-lock miss, retry collect)", comps.callCount())
	}
	locked := products.lockedIDs()
	saw1, saw2 := false, false
	for _, id := range locked {
		if id == leaf1 {
			saw1 = true
		}
		if id == leaf2 {
			saw2 = true
		}
	}
	if !saw1 || !saw2 {
		t.Fatalf("locked=%v, want both leaf1 and leaf2 after retry", locked)
	}
	rows, _ := svc.ListByOrderItem(context.Background(), domain.OrderItemID(orderID, variantID))
	if len(rows) != 2 {
		t.Fatalf("grants=%d, want 2", len(rows))
	}
}

func TestIssuer_BundleLeafNoLongerRequiredSkipped(t *testing.T) {
	orderID := id.New()
	bundleID := id.New()
	leaf1 := id.New()
	leaf2 := id.New()
	variantID := id.New()
	price := mustMoney(t)
	item, err := order.NewItem(variantID, "BUNDLE", "Kit", 1, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(orderID, "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	markPaid(t, &o)

	products := &lockingStubProducts{byID: map[string]*catalog.Product{
		bundleID: {ID: bundleID, Type: catalog.TypeBundle, Status: catalog.StatusActive},
		leaf1:    {ID: leaf1, Type: catalog.TypeDownloadable, Status: catalog.StatusActive},
		leaf2:    {ID: leaf2, Type: catalog.TypeDownloadable, Status: catalog.StatusActive},
	}}
	// First collect sees both required; after lock only leaf1 remains required.
	comps := &mutatingComponents{
		before: []catalog.BundleComponent{
			{BundleProductID: bundleID, ComponentProductID: leaf1, Quantity: 1, Required: true},
			{BundleProductID: bundleID, ComponentProductID: leaf2, Quantity: 1, Required: true},
		},
		after: []catalog.BundleComponent{
			{BundleProductID: bundleID, ComponentProductID: leaf1, Quantity: 1, Required: true},
			{BundleProductID: bundleID, ComponentProductID: leaf2, Quantity: 1, Required: false},
		},
	}
	repo := &withTxMemRepo{memGrantRepo: newMemGrantRepo()}
	svc := downloadapp.NewService(repo, nil)
	issuer := downloadapp.NewIssuer(
		stubOrders{o: &o},
		stubVariants{variantID: {ID: variantID, ProductID: bundleID, SKU: "BUNDLE", Name: "Kit"}},
		products,
		stubFiles{
			leaf1: {{ProductID: leaf1, AssetID: "asset-1"}},
			leaf2: {{ProductID: leaf2, AssetID: "asset-2"}},
		},
		nil,
		svc,
		domain.Defaults{Expiry: time.Hour, DownloadLimit: 2},
		nil,
	).WithBundleComponents(comps).WithTxStarter(issuerTxStarter())

	n, err := issuer.IssueForOrder(context.Background(), orderID)
	if err != nil || n != 1 {
		t.Fatalf("IssueForOrder n=%d err=%v, want 1 (optional leaf skipped)", n, err)
	}
	rows, _ := svc.ListByOrderItem(context.Background(), domain.OrderItemID(orderID, variantID))
	if len(rows) != 1 || rows[0].AssetID != "asset-1" {
		t.Fatalf("rows=%+v, want only asset-1", rows)
	}
}
