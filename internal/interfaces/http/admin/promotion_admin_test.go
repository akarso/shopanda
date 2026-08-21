package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
	"net/http"
	"net/http/httptest"
	"testing"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/promotion"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/id"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

type mockPromotionAdminRepo struct {
	findByIDFn func(ctx context.Context, id string) (*promotion.Promotion, error)
	listFn     func(ctx context.Context, offset, limit int) ([]promotion.Promotion, error)
	saveFn     func(ctx context.Context, p *promotion.Promotion) error
	deleteFn   func(ctx context.Context, id string) error
}

func (m *mockPromotionAdminRepo) FindByID(ctx context.Context, promoID string) (*promotion.Promotion, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, promoID)
	}
	return nil, nil
}

func (m *mockPromotionAdminRepo) ListActive(context.Context, promotion.PromotionType) ([]promotion.Promotion, error) {
	return nil, nil
}

func (m *mockPromotionAdminRepo) List(ctx context.Context, offset, limit int) ([]promotion.Promotion, error) {
	if m.listFn != nil {
		return m.listFn(ctx, offset, limit)
	}
	return nil, nil
}

func (m *mockPromotionAdminRepo) Save(ctx context.Context, p *promotion.Promotion) error {
	if m.saveFn != nil {
		return m.saveFn(ctx, p)
	}
	return nil
}

func (m *mockPromotionAdminRepo) Delete(ctx context.Context, promoID string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, promoID)
	}
	return nil
}

func newPromotionAdminRouter(h *admin.PromotionAdminHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/promotions", h.List())
	mux.HandleFunc("GET /api/v1/admin/promotions/{id}", h.Get())
	mux.HandleFunc("POST /api/v1/admin/promotions", h.Create())
	mux.HandleFunc("PUT /api/v1/admin/promotions/{id}", h.Update())
	mux.HandleFunc("DELETE /api/v1/admin/promotions/{id}", h.Delete())
	return mux
}

func promotionBody(t *testing.T, v interface{}) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return bytes.NewReader(b)
}

func catalogPromotionPayload(name string) map[string]interface{} {
	return map[string]interface{}{
		"name":              name,
		"type":              "catalog",
		"priority":          0,
		"active":            true,
		"coupon_bound":      false,
		"condition_type":    "always",
		"action_type":       "percentage",
		"action_percentage": 10,
	}
}

func TestPromotionAdminHandler_Create_CatalogPercentage(t *testing.T) {
	var saved *promotion.Promotion
	repo := &mockPromotionAdminRepo{
		saveFn: func(_ context.Context, p *promotion.Promotion) error {
			saved = p
			return nil
		},
	}
	sink := &auditSink{}
	h := admin.NewPromotionAdminHandlerWithAuditor(repo, adminapp.NewAuditor(sink))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/promotions", promotionBody(t, catalogPromotionPayload("Summer Sale")))
	req = withAdminFullScope(req, "admin-1", "store-eu", "en", "USD")
	newPromotionAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if saved == nil {
		t.Fatal("promotion was not saved")
	}
	if saved.Type != promotion.TypeCatalog {
		t.Fatalf("type = %q, want catalog", saved.Type)
	}
	body := parsePageBody(t, rec)
	promo := body["data"].(map[string]interface{})["promotion"].(map[string]interface{})
	if promo["action_percentage"].(float64) != 10 {
		t.Fatalf("action_percentage = %v, want 10", promo["action_percentage"])
	}
	last := sink.Last(t)
	if last.context["action"] != adminapp.AuditPromotionCreate {
		t.Errorf("audit action = %v, want %v", last.context["action"], adminapp.AuditPromotionCreate)
	}
}

func TestPromotionAdminHandler_Create_InvalidAction(t *testing.T) {
	repo := &mockPromotionAdminRepo{}
	h := admin.NewPromotionAdminHandler(repo)

	payload := catalogPromotionPayload("Bad Promo")
	payload["action_type"] = "bogus"

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/promotions", promotionBody(t, payload))
	newPromotionAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestPromotionAdminHandler_Create_Tiered(t *testing.T) {
	var saved *promotion.Promotion
	repo := &mockPromotionAdminRepo{
		saveFn: func(_ context.Context, p *promotion.Promotion) error {
			saved = p
			return nil
		},
	}
	h := admin.NewPromotionAdminHandler(repo)

	payload := catalogPromotionPayload("Tiered Promo")
	payload["action_type"] = "tiered"
	payload["action_tiers"] = []map[string]interface{}{
		{"min_qty": 2, "percentage": 5},
		{"min_qty": 5, "percentage": 15},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/promotions", promotionBody(t, payload))
	newPromotionAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if saved == nil {
		t.Fatal("promotion was not saved")
	}
	body := parsePageBody(t, rec)
	promo := body["data"].(map[string]interface{})["promotion"].(map[string]interface{})
	if promo["action_type"] != "tiered" {
		t.Fatalf("action_type = %v, want tiered", promo["action_type"])
	}
	tiers := promo["action_tiers"].([]interface{})
	if len(tiers) != 2 {
		t.Fatalf("action_tiers len = %d, want 2", len(tiers))
	}
	t0 := tiers[0].(map[string]interface{})
	if t0["min_qty"].(float64) != 2 || t0["percentage"].(float64) != 5 {
		t.Fatalf("tier[0] = %v, want min_qty=2 percentage=5", t0)
	}
	t1 := tiers[1].(map[string]interface{})
	if t1["min_qty"].(float64) != 5 || t1["percentage"].(float64) != 15 {
		t.Fatalf("tier[1] = %v, want min_qty=5 percentage=15", t1)
	}
}

func TestPromotionAdminHandler_Create_BuyXGetY(t *testing.T) {
	var saved *promotion.Promotion
	repo := &mockPromotionAdminRepo{
		saveFn: func(_ context.Context, p *promotion.Promotion) error {
			saved = p
			return nil
		},
	}
	h := admin.NewPromotionAdminHandler(repo)

	payload := catalogPromotionPayload("B2G1")
	payload["action_type"] = "buy_x_get_y"
	payload["action_buy_qty"] = 2
	payload["action_get_qty"] = 1

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/promotions", promotionBody(t, payload))
	newPromotionAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if saved == nil {
		t.Fatal("promotion was not saved")
	}
	body := parsePageBody(t, rec)
	promo := body["data"].(map[string]interface{})["promotion"].(map[string]interface{})
	if promo["action_type"] != "buy_x_get_y" {
		t.Fatalf("action_type = %v, want buy_x_get_y", promo["action_type"])
	}
	if promo["action_buy_qty"].(float64) != 2 {
		t.Fatalf("action_buy_qty = %v, want 2", promo["action_buy_qty"])
	}
	if promo["action_get_qty"].(float64) != 1 {
		t.Fatalf("action_get_qty = %v, want 1", promo["action_get_qty"])
	}
}

func TestPromotionAdminHandler_Create_CartFixed(t *testing.T) {
	var saved *promotion.Promotion
	repo := &mockPromotionAdminRepo{
		saveFn: func(_ context.Context, p *promotion.Promotion) error {
			saved = p
			return nil
		},
	}
	h := admin.NewPromotionAdminHandler(repo)

	payload := map[string]interface{}{
		"name":            "Cart $5 off",
		"type":            "cart",
		"condition_type":  "min_cart_total",
		"condition_value": 5000,
		"action_type":     "fixed",
		"action_amount":   500,
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/promotions", promotionBody(t, payload))
	newPromotionAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if saved == nil || saved.Type != promotion.TypeCart {
		t.Fatalf("saved type = %v, want cart", saved)
	}
}

func TestPromotionAdminHandler_Delete_NotFound(t *testing.T) {
	promotionID := id.New()
	repo := &mockPromotionAdminRepo{
		findByIDFn: func(_ context.Context, _ string) (*promotion.Promotion, error) {
			return nil, nil
		},
	}
	h := admin.NewPromotionAdminHandler(repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/admin/promotions/"+promotionID, nil)
	newPromotionAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestPromotionAdminHandler_Create_ForbiddenWithoutWritePermission(t *testing.T) {
	h := admin.NewPromotionAdminHandler(&mockPromotionAdminRepo{})
	requireProductsWrite := shophttp.RequirePermission(rbac.ProductsWrite)
	withAdminContext := shophttp.AdminContextMiddleware()

	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/admin/promotions", withAdminContext(requireProductsWrite(h.Create())))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/promotions", promotionBody(t, catalogPromotionPayload("Blocked")))
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestPromotionAdminHandler_List_OK(t *testing.T) {
	promoID := id.New()
	conditions := []byte(`{"type":"always"}`)
	actions := []byte(`{"type":"percentage","percentage":10}`)
	repo := &mockPromotionAdminRepo{
		listFn: func(_ context.Context, offset, limit int) ([]promotion.Promotion, error) {
			return []promotion.Promotion{{
				ID:         promoID,
				Name:       "Sale",
				Type:       promotion.TypeCatalog,
				Active:     true,
				Conditions: conditions,
				Actions:    actions,
				CreatedAt:  fixedTime,
				UpdatedAt:  fixedTime,
			}}, nil
		},
	}
	h := admin.NewPromotionAdminHandler(repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/promotions?offset=0&limit=50", nil)
	newPromotionAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestPromotionAdminHandler_Update_PartialNameOnly(t *testing.T) {
	promotionID := id.New()
	conditions := []byte(`{"type":"always"}`)
	actions := []byte(`{"type":"percentage","percentage":10}`)
	existing := &promotion.Promotion{
		ID:          promotionID,
		Name:        "Original",
		Type:        promotion.TypeCatalog,
		Active:      true,
		Conditions:  conditions,
		Actions:     actions,
		CouponBound: false,
	}
	var saved *promotion.Promotion
	repo := &mockPromotionAdminRepo{
		findByIDFn: func(_ context.Context, id string) (*promotion.Promotion, error) {
			if id == promotionID {
				copy := *existing
				return &copy, nil
			}
			return nil, nil
		},
		saveFn: func(_ context.Context, p *promotion.Promotion) error {
			saved = p
			return nil
		},
	}
	h := admin.NewPromotionAdminHandler(repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/promotions/"+promotionID, promotionBody(t, map[string]interface{}{
		"name": "Renamed Sale",
	}))
	newPromotionAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if saved == nil {
		t.Fatal("promotion was not saved")
	}
	if saved.Name != "Renamed Sale" {
		t.Fatalf("name = %q, want Renamed Sale", saved.Name)
	}
	if string(saved.Conditions) != string(conditions) {
		t.Fatalf("conditions changed: %s", saved.Conditions)
	}
	if string(saved.Actions) != string(actions) {
		t.Fatalf("actions changed: %s", saved.Actions)
	}
}

func TestPromotionAdminHandler_Create_WhitespaceNameRejected(t *testing.T) {
	repo := &mockPromotionAdminRepo{}
	h := admin.NewPromotionAdminHandler(repo)

	payload := catalogPromotionPayload("   ")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/promotions", promotionBody(t, payload))
	newPromotionAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestPromotionAdminHandler_Create_AdvancedRulesJSON(t *testing.T) {
	var saved *promotion.Promotion
	repo := &mockPromotionAdminRepo{
		saveFn: func(_ context.Context, p *promotion.Promotion) error {
			saved = p
			return nil
		},
	}
	h := admin.NewPromotionAdminHandler(repo)

	payload := catalogPromotionPayload("Advanced Promo")
	payload["rules_mode"] = "advanced"
	payload["conditions"] = map[string]interface{}{"type": "always"}
	payload["actions"] = map[string]interface{}{
		"type": "tiered",
		"tiers": []map[string]interface{}{
			{"min_qty": 2, "percentage": 5},
			{"min_qty": 5, "percentage": 15},
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/promotions", promotionBody(t, payload))
	newPromotionAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if saved == nil {
		t.Fatal("promotion was not saved")
	}
	if _, err := adminapp.DecodePromotionRules(saved.Type, saved.Conditions, saved.Actions); err != nil {
		t.Fatalf("saved rules not decode-compatible: %v", err)
	}
	body := parsePageBody(t, rec)
	promo := body["data"].(map[string]interface{})["promotion"].(map[string]interface{})
	if promo["action_type"] != "tiered" {
		t.Fatalf("action_type = %v, want tiered", promo["action_type"])
	}
	if promo["conditions"] == nil || promo["actions"] == nil {
		t.Fatalf("expected raw conditions/actions in response, got %+v", promo)
	}
}

func TestPromotionAdminHandler_Create_AdvancedRulesJSON_RejectsUnsupported(t *testing.T) {
	repo := &mockPromotionAdminRepo{
		saveFn: func(_ context.Context, p *promotion.Promotion) error {
			t.Fatal("save should not be called for unsupported advanced rules")
			return nil
		},
	}
	h := admin.NewPromotionAdminHandler(repo)

	payload := catalogPromotionPayload("Bad Advanced Promo")
	payload["rules_mode"] = "advanced"
	payload["conditions"] = map[string]interface{}{"type": "always"}
	payload["actions"] = map[string]interface{}{"type": "custom_engine_v2", "factor": 0.5}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/promotions", promotionBody(t, payload))
	newPromotionAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestPromotionAdminHandler_Create_AdvancedRulesJSON_RejectsNullActions(t *testing.T) {
	repo := &mockPromotionAdminRepo{}
	h := admin.NewPromotionAdminHandler(repo)

	payload := catalogPromotionPayload("Null Actions Promo")
	payload["rules_mode"] = "advanced"
	payload["conditions"] = map[string]interface{}{"type": "always"}
	payload["actions"] = nil

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/promotions", promotionBody(t, payload))
	newPromotionAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}
