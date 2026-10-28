package returns_test

import (
	"context"
	"database/sql"
	"io"
	"testing"
	"time"

	downloadapp "github.com/akarso/shopanda/internal/application/download"
	returnsApp "github.com/akarso/shopanda/internal/application/returns"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domain "github.com/akarso/shopanda/internal/domain/download"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// memGrantRepoTX is an in-memory DownloadGrant repository with WithTx for
// returns revoke tests (PR-1075).
type memGrantRepoTX struct {
	byID   map[string]*domain.DownloadGrant
	byPair map[string]*domain.DownloadGrant
}

func newMemGrantRepoTX() *memGrantRepoTX {
	return &memGrantRepoTX{
		byID:   map[string]*domain.DownloadGrant{},
		byPair: map[string]*domain.DownloadGrant{},
	}
}

func grantPairKey(orderItemID, assetID string) string {
	return orderItemID + "\x00" + assetID
}

func (m *memGrantRepoTX) WithTx(*sql.Tx) domain.Repository { return m }

func (m *memGrantRepoTX) Insert(_ context.Context, g *domain.DownloadGrant) error {
	cp := *g
	m.byID[g.ID] = &cp
	m.byPair[grantPairKey(g.OrderItemID, g.AssetID)] = &cp
	return nil
}

func (m *memGrantRepoTX) Update(_ context.Context, g *domain.DownloadGrant) error {
	existing, ok := m.byID[g.ID]
	if !ok {
		return apperror.NotFound("download grant not found")
	}
	cp := *g
	cp.DownloadCount = existing.DownloadCount
	m.byID[g.ID] = &cp
	m.byPair[grantPairKey(g.OrderItemID, g.AssetID)] = &cp
	return nil
}

func (m *memGrantRepoTX) UpdateCount(_ context.Context, id string, newCount, expectedCount int) error {
	g, ok := m.byID[id]
	if !ok {
		return apperror.NotFound("download grant not found")
	}
	if g.DownloadCount != expectedCount || g.Expired(time.Now().UTC()) {
		return domain.ErrCountConflict
	}
	g.DownloadCount = newCount
	return nil
}

func (m *memGrantRepoTX) FindByID(_ context.Context, id string) (*domain.DownloadGrant, error) {
	g, ok := m.byID[id]
	if !ok {
		return nil, nil
	}
	cp := *g
	return &cp, nil
}

func (m *memGrantRepoTX) FindByOrderItemAndAsset(_ context.Context, orderItemID, assetID string) (*domain.DownloadGrant, error) {
	g, ok := m.byPair[grantPairKey(orderItemID, assetID)]
	if !ok {
		return nil, nil
	}
	cp := *g
	return &cp, nil
}

func (m *memGrantRepoTX) ListByCustomerID(context.Context, string) ([]domain.DownloadGrant, error) {
	return []domain.DownloadGrant{}, nil
}

func (m *memGrantRepoTX) ListByOrderItemID(_ context.Context, orderItemID string) ([]domain.DownloadGrant, error) {
	out := []domain.DownloadGrant{}
	for _, g := range m.byID {
		if g.OrderItemID == orderItemID {
			out = append(out, *g)
		}
	}
	return out, nil
}

func (m *memGrantRepoTX) CountByAssetID(_ context.Context, assetID string) (int, error) {
	now := time.Now().UTC()
	n := 0
	for _, g := range m.byID {
		if g.AssetID == assetID && !g.Expired(now) {
			n++
		}
	}
	return n, nil
}

func (m *memGrantRepoTX) DeleteExpiredOrRevokedByAssetID(_ context.Context, assetID string) (int, error) {
	now := time.Now().UTC()
	n := 0
	for id, g := range m.byID {
		if g.AssetID == assetID && g.Expired(now) {
			delete(m.byID, id)
			delete(m.byPair, grantPairKey(g.OrderItemID, g.AssetID))
			n++
		}
	}
	return n, nil
}

func paidDownloadableOrder(t *testing.T, qty int) *order.Order {
	t.Helper()
	item, err := order.NewItem("dl-var", "SKU-DL", "Download", qty, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	item.Restockable = false
	ord, err := order.NewOrder(id.New(), "c1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	if err := ord.Confirm(); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if err := ord.MarkPaid(); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	return &ord
}

func TestService_Approve_RevokesGrant_SecondPaidDoesNotRestore(t *testing.T) {
	ord := paidDownloadableOrder(t, 1)
	grantRepo := newMemGrantRepoTX()
	grantSvc := downloadapp.NewService(grantRepo, nil)
	orderItemID := domain.OrderItemID(ord.ID, "dl-var")
	g, err := grantSvc.Issue(context.Background(), orderItemID, "c1", "asset-dl", time.Now().UTC().Add(24*time.Hour), 5)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	bus := event.NewBus(logger.NewWithWriter(io.Discard, "info"))
	pay := completedPayment(t, ord.ID)
	svc := returnsApp.NewService(
		newMemReturnRepo(),
		&memOrderRepo{order: ord},
		&memStockRepo{qty: map[string]int{}},
		memVariantRepo{},
		&memPaymentRepo{payment: pay},
		&stubRefunder{},
		bus,
		logger.NewWithWriter(io.Discard, "info"),
	).WithTxStarter(returnsFakeTxDB()).WithDownloadGrants(grantSvc)

	ret, err := svc.RequestReturn(context.Background(), ord.ID, "c1", "unused", []returnsApp.RequestLine{
		{VariantID: "dl-var", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}
	if _, err := svc.Approve(context.Background(), ret.ID); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	got, err := grantSvc.Get(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Expired(time.Now().UTC()) {
		t.Fatalf("grant must be revoked after approve: %+v", got)
	}

	// Second order.paid (IssueIdempotent) must not resurrect the revoked grant.
	again, existed, err := grantSvc.IssueIdempotent(context.Background(), orderItemID, "c1", "asset-dl", time.Now().UTC().Add(48*time.Hour), 9)
	if err != nil || !existed {
		t.Fatalf("IssueIdempotent existed=%v err=%v", existed, err)
	}
	if !again.Expired(time.Now().UTC()) || again.DownloadLimit != 5 {
		t.Fatalf("second paid must leave revoked grant untouched: %+v", again)
	}
}

func TestService_RequestReturn_RejectsPartialDownloadable(t *testing.T) {
	ord := paidDownloadableOrder(t, 3)
	grantRepo := newMemGrantRepoTX()
	grantSvc := downloadapp.NewService(grantRepo, nil)
	orderItemID := domain.OrderItemID(ord.ID, "dl-var")
	if _, err := grantSvc.Issue(context.Background(), orderItemID, "c1", "asset-dl", time.Now().UTC().Add(24*time.Hour), 5); err != nil {
		t.Fatalf("Issue: %v", err)
	}

	bus := event.NewBus(logger.NewWithWriter(io.Discard, "info"))
	pay := completedPayment(t, ord.ID)
	svc := returnsApp.NewService(
		newMemReturnRepo(),
		&memOrderRepo{order: ord},
		&memStockRepo{qty: map[string]int{}},
		memVariantRepo{},
		&memPaymentRepo{payment: pay},
		&stubRefunder{},
		bus,
		logger.NewWithWriter(io.Discard, "info"),
	).WithTxStarter(returnsFakeTxDB()).WithDownloadGrants(grantSvc)

	_, err := svc.RequestReturn(context.Background(), ord.ID, "c1", "partial", []returnsApp.RequestLine{
		{VariantID: "dl-var", Quantity: 1},
	})
	if err == nil || !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("partial downloadable return = %v, want validation", err)
	}
}

type grantIssueVariants map[string]*catalog.Variant

func (m grantIssueVariants) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	return m[id], nil
}

type grantIssueProducts map[string]*catalog.Product

func (m grantIssueProducts) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	return m[id], nil
}

type grantIssueFiles map[string][]catalog.DownloadableFile

func (m grantIssueFiles) ListByProductID(_ context.Context, productID string) ([]catalog.DownloadableFile, error) {
	if rows, ok := m[productID]; ok {
		return rows, nil
	}
	return []catalog.DownloadableFile{}, nil
}

func newDownloadIssuerForReturn(t *testing.T, ord *order.Order, grantSvc *downloadapp.Service, returns *returnsApp.Service) *downloadapp.Issuer {
	t.Helper()
	productID := "dl-prod"
	return downloadapp.NewIssuer(
		&memOrderRepo{order: ord},
		grantIssueVariants{"dl-var": {ID: "dl-var", ProductID: productID, SKU: "SKU-DL", Name: "Download"}},
		grantIssueProducts{productID: {ID: productID, Type: catalog.TypeDownloadable, Status: catalog.StatusActive}},
		grantIssueFiles{productID: {{ProductID: productID, AssetID: "asset-dl"}}},
		nil,
		grantSvc,
		domain.Defaults{Expiry: time.Hour, DownloadLimit: 3},
		logger.NewWithWriter(io.Discard, "info"),
	).WithTxStarter(returnsFakeTxDB()).WithBlockingReturnChecker(returns)
}

func TestService_Reject_RetriesSkippedGrantIssuance(t *testing.T) {
	ord := paidDownloadableOrder(t, 1)
	grantRepo := newMemGrantRepoTX()
	grantSvc := downloadapp.NewService(grantRepo, nil)
	bus := event.NewBus(logger.NewWithWriter(io.Discard, "info"))
	pay := completedPayment(t, ord.ID)
	svc := returnsApp.NewService(
		newMemReturnRepo(),
		&memOrderRepo{order: ord},
		&memStockRepo{qty: map[string]int{}},
		memVariantRepo{},
		&memPaymentRepo{payment: pay},
		&stubRefunder{},
		bus,
		logger.NewWithWriter(io.Discard, "info"),
	).WithTxStarter(returnsFakeTxDB())
	issuer := newDownloadIssuerForReturn(t, ord, grantSvc, svc)
	svc.WithDownloadGrantIssuer(issuer)

	orderItemID := domain.OrderItemID(ord.ID, "dl-var")
	ret, err := svc.RequestReturn(context.Background(), ord.ID, "c1", "change of mind", []returnsApp.RequestLine{
		{VariantID: "dl-var", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}
	n, err := issuer.IssueForOrder(context.Background(), ord.ID)
	if err != nil || n != 0 {
		t.Fatalf("IssueForOrder during requested return n=%d err=%v, want skip", n, err)
	}

	if _, err := svc.Reject(context.Background(), ret.ID); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	rows, err := grantSvc.ListByOrderItem(context.Background(), orderItemID)
	if err != nil {
		t.Fatalf("ListByOrderItem: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("after Reject want 1 grant, got %d", len(rows))
	}
}

func TestService_Cancel_RetriesSkippedGrantIssuance(t *testing.T) {
	ord := paidDownloadableOrder(t, 1)
	grantRepo := newMemGrantRepoTX()
	grantSvc := downloadapp.NewService(grantRepo, nil)
	bus := event.NewBus(logger.NewWithWriter(io.Discard, "info"))
	pay := completedPayment(t, ord.ID)
	svc := returnsApp.NewService(
		newMemReturnRepo(),
		&memOrderRepo{order: ord},
		&memStockRepo{qty: map[string]int{}},
		memVariantRepo{},
		&memPaymentRepo{payment: pay},
		&stubRefunder{},
		bus,
		logger.NewWithWriter(io.Discard, "info"),
	).WithTxStarter(returnsFakeTxDB())
	issuer := newDownloadIssuerForReturn(t, ord, grantSvc, svc)
	svc.WithDownloadGrantIssuer(issuer)

	orderItemID := domain.OrderItemID(ord.ID, "dl-var")
	ret, err := svc.RequestReturn(context.Background(), ord.ID, "c1", "cancel me", []returnsApp.RequestLine{
		{VariantID: "dl-var", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}
	n, err := issuer.IssueForOrder(context.Background(), ord.ID)
	if err != nil || n != 0 {
		t.Fatalf("IssueForOrder during requested return n=%d err=%v, want skip", n, err)
	}

	if _, err := svc.Cancel(context.Background(), ret.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	rows, err := grantSvc.ListByOrderItem(context.Background(), orderItemID)
	if err != nil {
		t.Fatalf("ListByOrderItem: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("after Cancel want 1 grant, got %d", len(rows))
	}
}

func TestService_Refunded_StillBlocksGrantIssuance(t *testing.T) {
	ord := paidDownloadableOrder(t, 1)
	grantRepo := newMemGrantRepoTX()
	grantSvc := downloadapp.NewService(grantRepo, nil)
	orderItemID := domain.OrderItemID(ord.ID, "dl-var")
	if _, err := grantSvc.Issue(context.Background(), orderItemID, "c1", "asset-dl", time.Now().UTC().Add(24*time.Hour), 5); err != nil {
		t.Fatalf("Issue: %v", err)
	}

	bus := event.NewBus(logger.NewWithWriter(io.Discard, "info"))
	pay := completedPayment(t, ord.ID)
	svc := returnsApp.NewService(
		newMemReturnRepo(),
		&memOrderRepo{order: ord},
		&memStockRepo{qty: map[string]int{}},
		memVariantRepo{},
		&memPaymentRepo{payment: pay},
		&stubRefunder{},
		bus,
		logger.NewWithWriter(io.Discard, "info"),
	).WithTxStarter(returnsFakeTxDB()).WithDownloadGrants(grantSvc)
	issuer := newDownloadIssuerForReturn(t, ord, grantSvc, svc)

	ret, err := svc.RequestReturn(context.Background(), ord.ID, "c1", "refund", []returnsApp.RequestLine{
		{VariantID: "dl-var", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}
	if _, err := svc.Approve(context.Background(), ret.ID); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if _, err := svc.Receive(context.Background(), ret.ID); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if _, err := svc.Refund(context.Background(), ret.ID); err != nil {
		t.Fatalf("Refund: %v", err)
	}

	n, err := issuer.IssueForOrder(context.Background(), ord.ID)
	if err != nil || n != 0 {
		t.Fatalf("IssueForOrder after refund n=%d err=%v, want skip", n, err)
	}
	rows, err := grantSvc.ListByOrderItem(context.Background(), orderItemID)
	if err != nil {
		t.Fatalf("ListByOrderItem: %v", err)
	}
	if len(rows) != 1 || !rows[0].Expired(time.Now().UTC()) {
		t.Fatalf("refunded line must keep revoked grant only: %+v", rows)
	}
}
