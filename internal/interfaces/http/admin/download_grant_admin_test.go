package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	downloadapp "github.com/akarso/shopanda/internal/application/download"
	domain "github.com/akarso/shopanda/internal/domain/download"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type grantAdminOrderRepo struct {
	order.OrderRepository
	byID map[string]*order.Order
}

func (r *grantAdminOrderRepo) FindByID(_ context.Context, id string) (*order.Order, error) {
	if r.byID == nil {
		return nil, nil
	}
	return r.byID[id], nil
}

type grantAdminMemRepo struct {
	byID   map[string]*domain.DownloadGrant
	byCust map[string][]domain.DownloadGrant
	byItem map[string][]domain.DownloadGrant
}

func newGrantAdminMemRepo() *grantAdminMemRepo {
	return &grantAdminMemRepo{
		byID:   map[string]*domain.DownloadGrant{},
		byCust: map[string][]domain.DownloadGrant{},
		byItem: map[string][]domain.DownloadGrant{},
	}
}

func (m *grantAdminMemRepo) Insert(_ context.Context, g *domain.DownloadGrant) error {
	cp := *g
	m.byID[g.ID] = &cp
	m.byCust[g.CustomerID] = append(m.byCust[g.CustomerID], cp)
	m.byItem[g.OrderItemID] = append(m.byItem[g.OrderItemID], cp)
	return nil
}
func (m *grantAdminMemRepo) Update(_ context.Context, g *domain.DownloadGrant) error {
	existing := m.byID[g.ID]
	if existing == nil {
		return nil
	}
	cp := *g
	cp.DownloadCount = existing.DownloadCount
	m.byID[g.ID] = &cp
	return nil
}
func (m *grantAdminMemRepo) UpdateCount(_ context.Context, id string, newCount, expectedCount int) error {
	g := m.byID[id]
	if g == nil || g.DownloadCount != expectedCount || !g.ExpiresAt.After(time.Now().UTC()) {
		return domain.ErrCountConflict
	}
	g.DownloadCount = newCount
	return nil
}
func (m *grantAdminMemRepo) ClaimDownload(context.Context, string, string) (*domain.DownloadGrant, error) {
	return nil, domain.ErrCountConflict
}
func (m *grantAdminMemRepo) FindByID(_ context.Context, id string) (*domain.DownloadGrant, error) {
	g := m.byID[id]
	if g == nil {
		return nil, nil
	}
	cp := *g
	return &cp, nil
}
func (m *grantAdminMemRepo) FindByOrderItemAndAsset(context.Context, string, string) (*domain.DownloadGrant, error) {
	return nil, nil
}
func (m *grantAdminMemRepo) ListByCustomerID(_ context.Context, customerID string) ([]domain.DownloadGrant, error) {
	return append([]domain.DownloadGrant{}, m.byCust[customerID]...), nil
}
func (m *grantAdminMemRepo) ListByOrderItemID(_ context.Context, orderItemID string) ([]domain.DownloadGrant, error) {
	return append([]domain.DownloadGrant{}, m.byItem[orderItemID]...), nil
}
func (m *grantAdminMemRepo) CountByAssetID(context.Context, string) (int, error) { return 0, nil }
func (m *grantAdminMemRepo) DeleteExpiredOrRevokedByAssetID(context.Context, string) (int, error) {
	return 0, nil
}

func seedGrant(t *testing.T, repo *grantAdminMemRepo, customerID, orderItemID string) *domain.DownloadGrant {
	t.Helper()
	g, err := domain.NewDownloadGrant(id.New(), orderItemID, customerID, "asset-1", time.Now().UTC().Add(24*time.Hour), 5)
	if err != nil {
		t.Fatalf("NewDownloadGrant: %v", err)
	}
	if err := repo.Insert(context.Background(), g); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	return g
}

func TestDownloadGrantAdmin_ListRevokeReissue(t *testing.T) {
	repo := newGrantAdminMemRepo()
	svc := downloadapp.NewService(repo, nil)
	customerID := "cust-1"
	orderID := id.New()
	variantID := "var-1"
	unit := domain.OrderItemID(orderID, variantID)
	g := seedGrant(t, repo, customerID, unit)

	money, err := shared.NewMoney(1000, "EUR")
	if err != nil {
		t.Fatalf("money: %v", err)
	}
	item, err := order.NewItem(variantID, "SKU", "Digital", 1, money)
	if err != nil {
		t.Fatalf("item: %v", err)
	}
	ord, err := order.NewOrder(orderID, customerID, "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	orders := &grantAdminOrderRepo{byID: map[string]*order.Order{orderID: &ord}}
	h := admin.NewDownloadGrantAdminHandler(svc, orders, adminapp.NewAuditor(logger.New("error")))

	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/download-grants", admin.RequirePermission(rbac.DownloadsRead)(h.List()))
	mux.Handle("GET /api/v1/admin/orders/{orderId}/download-grants", admin.RequirePermission(rbac.DownloadsRead)(h.ListByOrder()))
	mux.Handle("POST /api/v1/admin/download-grants/{id}/revoke", admin.RequirePermission(rbac.DownloadsWrite)(h.Revoke()))
	mux.Handle("POST /api/v1/admin/download-grants/{id}/reissue", admin.RequirePermission(rbac.DownloadsWrite)(h.Reissue()))

	listReq := testhelper.AuthenticatedRequest(
		httptest.NewRequest(http.MethodGet, "/api/v1/admin/download-grants?customer_id="+customerID, nil),
		"support-1", identity.RoleSupport,
	)
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}

	orderReq := testhelper.AuthenticatedRequest(
		httptest.NewRequest(http.MethodGet, "/api/v1/admin/orders/"+orderID+"/download-grants", nil),
		"support-1", identity.RoleSupport,
	)
	orderRec := httptest.NewRecorder()
	mux.ServeHTTP(orderRec, orderReq)
	if orderRec.Code != http.StatusOK {
		t.Fatalf("order list status = %d body=%s", orderRec.Code, orderRec.Body.String())
	}
	if !strings.Contains(orderRec.Body.String(), g.ID) {
		t.Fatalf("order list missing grant id: %s", orderRec.Body.String())
	}

	revokeBody, _ := json.Marshal(map[string]string{"reason": "customer request"})
	revokeReq := testhelper.AuthenticatedRequest(
		httptest.NewRequest(http.MethodPost, "/api/v1/admin/download-grants/"+g.ID+"/revoke", bytes.NewReader(revokeBody)),
		"support-1", identity.RoleSupport,
	)
	revokeRec := httptest.NewRecorder()
	mux.ServeHTTP(revokeRec, revokeReq)
	if revokeRec.Code != http.StatusOK {
		t.Fatalf("revoke status = %d body=%s", revokeRec.Code, revokeRec.Body.String())
	}

	reissueBody, _ := json.Marshal(map[string]interface{}{
		"expiry_days": 14,
		"reason":      "support re-issue",
	})
	reissueReq := testhelper.AuthenticatedRequest(
		httptest.NewRequest(http.MethodPost, "/api/v1/admin/download-grants/"+g.ID+"/reissue", bytes.NewReader(reissueBody)),
		"support-1", identity.RoleSupport,
	)
	reissueRec := httptest.NewRecorder()
	mux.ServeHTTP(reissueRec, reissueReq)
	if reissueRec.Code != http.StatusOK {
		t.Fatalf("reissue status = %d body=%s", reissueRec.Code, reissueRec.Body.String())
	}
}
