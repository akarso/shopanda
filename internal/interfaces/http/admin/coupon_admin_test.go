package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/interfaces/http/admin"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/promotion"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/id"
)

type mockCouponRepo struct {
	findByCodeFn      func(ctx context.Context, code string) (*promotion.Coupon, error)
	findByIDFn        func(ctx context.Context, id string) (*promotion.Coupon, error)
	listByPromotionFn func(ctx context.Context, promotionID string) ([]promotion.Coupon, error)
	listFn            func(ctx context.Context, offset, limit int) ([]promotion.Coupon, error)
	saveFn            func(ctx context.Context, c *promotion.Coupon) error
	deleteFn          func(ctx context.Context, id string) error
}

func (m *mockCouponRepo) FindByCode(ctx context.Context, code string) (*promotion.Coupon, error) {
	if m.findByCodeFn != nil {
		return m.findByCodeFn(ctx, code)
	}
	return nil, nil
}

func (m *mockCouponRepo) FindByID(ctx context.Context, couponID string) (*promotion.Coupon, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, couponID)
	}
	return nil, nil
}

func (m *mockCouponRepo) ListByPromotion(ctx context.Context, promotionID string) ([]promotion.Coupon, error) {
	if m.listByPromotionFn != nil {
		return m.listByPromotionFn(ctx, promotionID)
	}
	return nil, nil
}

func (m *mockCouponRepo) List(ctx context.Context, offset, limit int) ([]promotion.Coupon, error) {
	if m.listFn != nil {
		return m.listFn(ctx, offset, limit)
	}
	return nil, nil
}

func (m *mockCouponRepo) Save(ctx context.Context, c *promotion.Coupon) error {
	if m.saveFn != nil {
		return m.saveFn(ctx, c)
	}
	return nil
}

func (m *mockCouponRepo) Delete(ctx context.Context, couponID string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, couponID)
	}
	return nil
}

type mockPromotionRepo struct {
	findByIDFn func(ctx context.Context, id string) (*promotion.Promotion, error)
}

func (m *mockPromotionRepo) FindByID(ctx context.Context, promoID string) (*promotion.Promotion, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, promoID)
	}
	return nil, nil
}

func (m *mockPromotionRepo) ListActive(context.Context, promotion.PromotionType) ([]promotion.Promotion, error) {
	return nil, nil
}

func (m *mockPromotionRepo) List(context.Context, int, int) ([]promotion.Promotion, error) {
	return nil, nil
}

func (m *mockPromotionRepo) Save(context.Context, *promotion.Promotion) error { return nil }
func (m *mockPromotionRepo) Delete(context.Context, string) error             { return nil }

func newCouponAdminRouter(h *admin.CouponAdminHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/coupons", h.List())
	mux.HandleFunc("GET /api/v1/admin/coupons/{id}", h.Get())
	mux.HandleFunc("POST /api/v1/admin/coupons", h.Create())
	mux.HandleFunc("PUT /api/v1/admin/coupons/{id}", h.Update())
	mux.HandleFunc("DELETE /api/v1/admin/coupons/{id}", h.Delete())
	return mux
}

func couponBody(t *testing.T, v interface{}) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return bytes.NewReader(b)
}

func testPromotion(promoID string) *promotion.Promotion {
	return &promotion.Promotion{
		ID:   promoID,
		Name: "Summer Sale",
		Type: promotion.TypeCart,
	}
}

func testCoupon(couponID, code, promoID string) *promotion.Coupon {
	now := fixedTime
	return &promotion.Coupon{
		ID:          couponID,
		Code:        code,
		PromotionID: promoID,
		UsageLimit:  10,
		UsageCount:  2,
		Active:      true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func couponAdminWithSink(coupons *mockCouponRepo, promotions *mockPromotionRepo, sink *auditSink) *admin.CouponAdminHandler {
	return admin.NewCouponAdminHandlerWithAuditor(coupons, promotions, adminapp.NewAuditor(sink))
}

func TestCouponAdminHandler_List_OK(t *testing.T) {
	promoID := id.New()
	coupons := &mockCouponRepo{
		listFn: func(_ context.Context, offset, limit int) ([]promotion.Coupon, error) {
			if offset != 0 || limit != 50 {
				t.Errorf("offset/limit = %d/%d, want 0/50", offset, limit)
			}
			return []promotion.Coupon{*testCoupon(id.New(), "SAVE10", promoID)}, nil
		},
	}
	promotions := &mockPromotionRepo{
		findByIDFn: func(_ context.Context, pid string) (*promotion.Promotion, error) {
			if pid != promoID {
				return nil, nil
			}
			return testPromotion(promoID), nil
		},
	}
	h := admin.NewCouponAdminHandler(coupons, promotions)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/coupons?offset=0&limit=50", nil)
	newCouponAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := parsePageBody(t, rec)
	data := body["data"].(map[string]interface{})
	items := data["coupons"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("coupons len = %d, want 1", len(items))
	}
}

func TestCouponAdminHandler_Create_OK(t *testing.T) {
	promoID := id.New()
	var saved *promotion.Coupon
	coupons := &mockCouponRepo{
		findByCodeFn: func(_ context.Context, code string) (*promotion.Coupon, error) {
			return nil, nil
		},
		saveFn: func(_ context.Context, c *promotion.Coupon) error {
			saved = c
			return nil
		},
	}
	promotions := &mockPromotionRepo{
		findByIDFn: func(_ context.Context, pid string) (*promotion.Promotion, error) {
			return testPromotion(promoID), nil
		},
	}
	sink := &auditSink{}
	h := couponAdminWithSink(coupons, promotions, sink)

	body := couponBody(t, map[string]interface{}{
		"code":         "save10",
		"promotion_id": promoID,
		"usage_limit":  5,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/coupons", body)
	req = withAdminFullScope(req, "admin-1", "store-eu", "en", "USD")
	newCouponAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if saved == nil {
		t.Fatal("coupon was not saved")
	}
	if saved.Code != "SAVE10" {
		t.Errorf("code = %q, want SAVE10", saved.Code)
	}
	if saved.UsageLimit != 5 {
		t.Errorf("usage_limit = %d, want 5", saved.UsageLimit)
	}
	last := sink.Last(t)
	if last.context["action"] != adminapp.AuditCouponCreate {
		t.Errorf("audit action = %v, want %v", last.context["action"], adminapp.AuditCouponCreate)
	}
	assertScopeTriad(t, last.context, "store-eu", "en", "USD")
}

func TestCouponAdminHandler_Create_InvalidCode(t *testing.T) {
	promoID := id.New()
	coupons := &mockCouponRepo{}
	promotions := &mockPromotionRepo{
		findByIDFn: func(_ context.Context, _ string) (*promotion.Promotion, error) {
			return testPromotion(promoID), nil
		},
	}
	h := admin.NewCouponAdminHandler(coupons, promotions)

	body := couponBody(t, map[string]interface{}{
		"code":         "x",
		"promotion_id": promoID,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/coupons", body)
	newCouponAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestCouponAdminHandler_Create_PromotionNotFound(t *testing.T) {
	coupons := &mockCouponRepo{
		findByCodeFn: func(_ context.Context, _ string) (*promotion.Coupon, error) {
			return nil, nil
		},
	}
	promotions := &mockPromotionRepo{
		findByIDFn: func(_ context.Context, _ string) (*promotion.Promotion, error) {
			return nil, nil
		},
	}
	h := admin.NewCouponAdminHandler(coupons, promotions)

	body := couponBody(t, map[string]interface{}{
		"code":         "SAVE10",
		"promotion_id": id.New(),
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/coupons", body)
	newCouponAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCouponAdminHandler_Delete_OK(t *testing.T) {
	couponID := id.New()
	promoID := id.New()
	existing := testCoupon(couponID, "SAVE10", promoID)
	coupons := &mockCouponRepo{
		findByIDFn: func(_ context.Context, id string) (*promotion.Coupon, error) {
			if id == couponID {
				return existing, nil
			}
			return nil, nil
		},
		deleteFn: func(_ context.Context, id string) error {
			if id != couponID {
				t.Errorf("delete id = %q, want %q", id, couponID)
			}
			return nil
		},
	}
	sink := &auditSink{}
	h := couponAdminWithSink(coupons, &mockPromotionRepo{}, sink)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/admin/coupons/"+couponID, nil)
	req = withAdminFullScope(req, "admin-1", "store-eu", "en", "USD")
	newCouponAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	last := sink.Last(t)
	if last.context["action"] != adminapp.AuditCouponDelete {
		t.Errorf("audit action = %v, want %v", last.context["action"], adminapp.AuditCouponDelete)
	}
}

func TestCouponAdminHandler_Delete_NotFound(t *testing.T) {
	couponID := id.New()
	coupons := &mockCouponRepo{
		findByIDFn: func(_ context.Context, _ string) (*promotion.Coupon, error) {
			return nil, nil
		},
	}
	h := admin.NewCouponAdminHandler(coupons, &mockPromotionRepo{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/admin/coupons/"+couponID, nil)
	newCouponAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCouponAdminHandler_Create_ForbiddenWithoutWritePermission(t *testing.T) {
	h := admin.NewCouponAdminHandler(&mockCouponRepo{}, &mockPromotionRepo{})
	requireProductsWrite := admin.RequirePermission(rbac.ProductsWrite)
	withAdminContext := admin.AdminContextMiddleware()

	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/admin/coupons", withAdminContext(requireProductsWrite(h.Create())))

	body := couponBody(t, map[string]interface{}{
		"code":         "SAVE10",
		"promotion_id": id.New(),
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/coupons", body)
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestCouponAdminHandler_Update_UsageLimit(t *testing.T) {
	couponID := id.New()
	promoID := id.New()
	existing := testCoupon(couponID, "SAVE10", promoID)
	var saved *promotion.Coupon
	coupons := &mockCouponRepo{
		findByIDFn: func(_ context.Context, id string) (*promotion.Coupon, error) {
			if id == couponID {
				return existing, nil
			}
			return nil, nil
		},
		saveFn: func(_ context.Context, c *promotion.Coupon) error {
			saved = c
			return nil
		},
	}
	h := admin.NewCouponAdminHandler(coupons, &mockPromotionRepo{})

	body := couponBody(t, map[string]interface{}{"usage_limit": 20})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/coupons/"+couponID, body)
	newCouponAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if saved == nil || saved.UsageLimit != 20 {
		t.Fatalf("saved usage_limit = %v, want 20", saved)
	}
	if saved.UsageCount != 2 {
		t.Errorf("usage_count changed to %d, want 2", saved.UsageCount)
	}
}

func TestCouponAdminHandler_Get_NotFound(t *testing.T) {
	couponID := id.New()
	coupons := &mockCouponRepo{
		findByIDFn: func(_ context.Context, _ string) (*promotion.Coupon, error) {
			return nil, nil
		},
	}
	h := admin.NewCouponAdminHandler(coupons, &mockPromotionRepo{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/coupons/"+couponID, nil)
	newCouponAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCouponAdminHandler_Create_DuplicateCode(t *testing.T) {
	promoID := id.New()
	existingID := id.New()
	coupons := &mockCouponRepo{
		findByCodeFn: func(_ context.Context, code string) (*promotion.Coupon, error) {
			if code == "SAVE10" {
				return testCoupon(existingID, "SAVE10", promoID), nil
			}
			return nil, nil
		},
	}
	promotions := &mockPromotionRepo{
		findByIDFn: func(_ context.Context, _ string) (*promotion.Promotion, error) {
			return testPromotion(promoID), nil
		},
	}
	h := admin.NewCouponAdminHandler(coupons, promotions)

	body := couponBody(t, map[string]interface{}{
		"code":         "SAVE10",
		"promotion_id": promoID,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/coupons", body)
	newCouponAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestCouponAdminHandler_Create_NegativeUsageLimit(t *testing.T) {
	promoID := id.New()
	coupons := &mockCouponRepo{
		findByCodeFn: func(_ context.Context, _ string) (*promotion.Coupon, error) {
			return nil, nil
		},
	}
	promotions := &mockPromotionRepo{
		findByIDFn: func(_ context.Context, _ string) (*promotion.Promotion, error) {
			return testPromotion(promoID), nil
		},
	}
	h := admin.NewCouponAdminHandler(coupons, promotions)

	body := couponBody(t, map[string]interface{}{
		"code":         "SAVE10",
		"promotion_id": promoID,
		"usage_limit":  -1,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/coupons", body)
	newCouponAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}
