package voucher_test

import (
	"context"
	"testing"
	"time"

	voucherapp "github.com/akarso/shopanda/internal/application/voucher"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
	domain "github.com/akarso/shopanda/internal/domain/voucher"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type memVoucherRepo struct {
	byKey map[string]*domain.Voucher
	byID  map[string]*domain.Voucher
}

func newMemVoucherRepo() *memVoucherRepo {
	return &memVoucherRepo{byKey: map[string]*domain.Voucher{}, byID: map[string]*domain.Voucher{}}
}

func key(orderID, variantID string, seq int) string {
	return orderID + "|" + variantID + "|" + string(rune('0'+seq))
}

func (m *memVoucherRepo) Insert(_ context.Context, v *domain.Voucher) error {
	k := v.OrderID + "|" + v.VariantID + "|" + string(rune('0'+v.Seq))
	if _, ok := m.byKey[k]; ok {
		return apperror.Wrap(apperror.CodeConflict, "voucher already exists for order line sequence", domain.ErrAlreadyIssued)
	}
	for _, existing := range m.byID {
		if existing.Code == v.Code {
			return domain.ErrCodeTaken
		}
	}
	cp := *v
	m.byKey[k] = &cp
	m.byID[v.ID] = &cp
	return nil
}
func (m *memVoucherRepo) UpdateCounts(context.Context, string, int, int) error { return nil }
func (m *memVoucherRepo) Void(context.Context, string, time.Time) error        { return nil }
func (m *memVoucherRepo) VoidForOrderItem(_ context.Context, orderItemID string, at time.Time) ([]domain.Voucher, error) {
	var out []domain.Voucher
	for _, v := range m.byID {
		if v.OrderItemID == orderItemID && v.VoidedAt == nil {
			t := at.UTC()
			v.VoidedAt = &t
			out = append(out, *v)
		}
	}
	return out, nil
}
func (m *memVoucherRepo) LinkCustomer(_ context.Context, orderID, customerID string) error {
	for _, v := range m.byID {
		if v.OrderID == orderID && v.CustomerID == "" {
			v.CustomerID = customerID
		}
	}
	return nil
}
func (m *memVoucherRepo) FindByID(_ context.Context, id string) (*domain.Voucher, error) {
	return m.byID[id], nil
}
func (m *memVoucherRepo) FindByCode(context.Context, string) (*domain.Voucher, error) {
	return nil, nil
}
func (m *memVoucherRepo) List(context.Context, domain.ListFilter) ([]domain.Voucher, error) {
	return nil, nil
}
func (m *memVoucherRepo) ListByOrderID(context.Context, string) ([]domain.Voucher, error) {
	return nil, nil
}

type memSettings struct {
	row *domain.Settings
}

func (m *memSettings) FindByProductID(context.Context, string) (*domain.Settings, error) {
	return m.row, nil
}
func (m *memSettings) Upsert(context.Context, *domain.Settings) error { return nil }
func (m *memSettings) Remove(context.Context, string) error           { return nil }

type memOrders struct{ o *order.Order }

func (m *memOrders) FindByID(context.Context, string) (*order.Order, error) { return m.o, nil }

type memVariants struct{ v *catalog.Variant }

func (m *memVariants) FindByID(context.Context, string) (*catalog.Variant, error) { return m.v, nil }

type memProducts struct{ p *catalog.Product }

func (m *memProducts) FindByID(context.Context, string) (*catalog.Product, error) { return m.p, nil }

func TestIssuer_IssuesForEnabledVirtual(t *testing.T) {
	item, err := order.NewItem("var-1", "SKU-V", "Pass", 2, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(id.New(), "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	if err := o.SetStatusFromDB(string(order.OrderStatusPaid)); err != nil {
		t.Fatalf("SetStatusFromDB paid: %v", err)
	}
	prodID := id.New()
	p, err := catalog.NewProduct(prodID, "Pass", "pass")
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	p.Type = catalog.TypeVirtual
	variant, err := catalog.NewVariant("var-1", prodID, "SKU-V")
	if err != nil {
		t.Fatalf("NewVariant: %v", err)
	}
	settings := &domain.Settings{ProductID: prodID, Enabled: true, CodesPerUnit: 1, MaxRedemptions: 1}
	repo := newMemVoucherRepo()
	svc := voucherapp.NewService(repo, event.NewBus(logger.New("error")), nil).WithSettings(&memSettings{row: settings})
	issuer := voucherapp.NewIssuer(&memOrders{o: &o}, &memVariants{v: &variant}, &memProducts{p: &p}, &memSettings{row: settings}, svc, nil)
	n, err := issuer.IssueForOrder(context.Background(), o.ID)
	if err != nil {
		t.Fatalf("IssueForOrder: %v", err)
	}
	if n != 2 {
		t.Fatalf("issued = %d, want 2", n)
	}
	n2, err := issuer.IssueForOrder(context.Background(), o.ID)
	if err != nil {
		t.Fatalf("IssueForOrder idempotent: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("reissue = %d, want 0", n2)
	}
}

func TestIssuer_UsesCheckoutSnapshotWhenLiveSettingsOff(t *testing.T) {
	item, err := order.NewItem("var-1", "SKU-V", "Pass", 1, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	cpu, maxR := 2, 3
	item.VoucherCodesPerUnit = &cpu
	item.VoucherMaxRedemptions = &maxR
	o, err := order.NewOrder(id.New(), "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	_ = o.SetStatusFromDB(string(order.OrderStatusPaid))
	prodID := id.New()
	p, err := catalog.NewProduct(prodID, "Pass", "pass-snap")
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	p.Type = catalog.TypeVirtual
	variant, err := catalog.NewVariant("var-1", prodID, "SKU-V")
	if err != nil {
		t.Fatalf("NewVariant: %v", err)
	}
	// Live settings disabled — snapshot must still issue.
	live := &domain.Settings{ProductID: prodID, Enabled: false, CodesPerUnit: 1, MaxRedemptions: 1}
	repo := newMemVoucherRepo()
	svc := voucherapp.NewService(repo, event.NewBus(logger.New("error")), nil).WithSettings(&memSettings{row: live})
	issuer := voucherapp.NewIssuer(&memOrders{o: &o}, &memVariants{v: &variant}, &memProducts{p: &p}, &memSettings{row: live}, svc, nil)
	n, err := issuer.IssueForOrder(context.Background(), o.ID)
	if err != nil {
		t.Fatalf("IssueForOrder: %v", err)
	}
	if n != 2 {
		t.Fatalf("issued = %d, want 2 from snapshot", n)
	}
	for _, v := range repo.byID {
		if v.MaxRedemptions != 3 {
			t.Fatalf("max_redemptions = %d, want 3 from snapshot", v.MaxRedemptions)
		}
	}
}

func TestIssuer_SkipsNonVoucherVirtual(t *testing.T) {
	item, err := order.NewItem("var-1", "SKU-V", "Virtual", 1, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(id.New(), "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	_ = o.SetStatusFromDB(string(order.OrderStatusPaid))
	prodID := id.New()
	p, err := catalog.NewProduct(prodID, "Virtual", "virtual")
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	p.Type = catalog.TypeVirtual
	variant, err := catalog.NewVariant("var-1", prodID, "SKU-V")
	if err != nil {
		t.Fatalf("NewVariant: %v", err)
	}
	repo := newMemVoucherRepo()
	svc := voucherapp.NewService(repo, event.NewBus(logger.New("error")), nil).WithSettings(&memSettings{})
	issuer := voucherapp.NewIssuer(&memOrders{o: &o}, &memVariants{v: &variant}, &memProducts{p: &p}, &memSettings{}, svc, nil)
	n, err := issuer.IssueForOrder(context.Background(), o.ID)
	if err != nil {
		t.Fatalf("IssueForOrder: %v", err)
	}
	if n != 0 {
		t.Fatalf("issued = %d, want 0", n)
	}
}
