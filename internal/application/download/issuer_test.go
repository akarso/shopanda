package download_test

import (
	"context"
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
