package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/shipping"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

// ── mock zone repository ────────────────────────────────────────────────

type mockZoneRepo struct {
	listZonesFn        func(ctx context.Context) ([]shipping.Zone, error)
	findZoneByIDFn     func(ctx context.Context, id string) (*shipping.Zone, error)
	createZoneFn       func(ctx context.Context, z *shipping.Zone) error
	updateZoneFn       func(ctx context.Context, z *shipping.Zone) error
	deleteZoneFn       func(ctx context.Context, id string) error
	listRateTiersFn    func(ctx context.Context, zoneID string) ([]shipping.RateTier, error)
	findRateTierByIDFn func(ctx context.Context, id string) (*shipping.RateTier, error)
	creatRateTierFn    func(ctx context.Context, rt *shipping.RateTier) error
	updateRateTierFn   func(ctx context.Context, rt *shipping.RateTier) error
	deleteRateTierFn   func(ctx context.Context, id string) error
}

func (m *mockZoneRepo) ListZones(ctx context.Context) ([]shipping.Zone, error) {
	if m.listZonesFn != nil {
		return m.listZonesFn(ctx)
	}
	return nil, nil
}

func (m *mockZoneRepo) FindZoneByID(ctx context.Context, id string) (*shipping.Zone, error) {
	if m.findZoneByIDFn != nil {
		return m.findZoneByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockZoneRepo) CreateZone(ctx context.Context, z *shipping.Zone) error {
	if m.createZoneFn != nil {
		return m.createZoneFn(ctx, z)
	}
	return nil
}

func (m *mockZoneRepo) UpdateZone(ctx context.Context, z *shipping.Zone) error {
	if m.updateZoneFn != nil {
		return m.updateZoneFn(ctx, z)
	}
	return nil
}

func (m *mockZoneRepo) DeleteZone(ctx context.Context, id string) error {
	if m.deleteZoneFn != nil {
		return m.deleteZoneFn(ctx, id)
	}
	return nil
}

func (m *mockZoneRepo) ListRateTiers(ctx context.Context, zoneID string) ([]shipping.RateTier, error) {
	if m.listRateTiersFn != nil {
		return m.listRateTiersFn(ctx, zoneID)
	}
	return nil, nil
}

func (m *mockZoneRepo) FindRateTierByID(ctx context.Context, id string) (*shipping.RateTier, error) {
	if m.findRateTierByIDFn != nil {
		return m.findRateTierByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockZoneRepo) CreateRateTier(ctx context.Context, rt *shipping.RateTier) error {
	if m.creatRateTierFn != nil {
		return m.creatRateTierFn(ctx, rt)
	}
	return nil
}

func (m *mockZoneRepo) UpdateRateTier(ctx context.Context, rt *shipping.RateTier) error {
	if m.updateRateTierFn != nil {
		return m.updateRateTierFn(ctx, rt)
	}
	return nil
}

func (m *mockZoneRepo) DeleteRateTier(ctx context.Context, id string) error {
	if m.deleteRateTierFn != nil {
		return m.deleteRateTierFn(ctx, id)
	}
	return nil
}

// ── helpers ─────────────────────────────────────────────────────────────

func zoneAdminSetup(repo shipping.ZoneRepository) *http.ServeMux {
	h := shophttp.NewShippingZoneAdminHandler(repo)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/shipping/zones", h.ListZones())
	mux.HandleFunc("POST /api/v1/admin/shipping/zones", h.CreateZone())
	mux.HandleFunc("PUT /api/v1/admin/shipping/zones/{id}", h.UpdateZone())
	mux.HandleFunc("DELETE /api/v1/admin/shipping/zones/{id}", h.DeleteZone())
	mux.HandleFunc("GET /api/v1/admin/shipping/zones/{id}/rates", h.ListRates())
	mux.HandleFunc("POST /api/v1/admin/shipping/zones/{id}/rates", h.CreateRate())
	mux.HandleFunc("PUT /api/v1/admin/shipping/zones/{zoneId}/rates/{rateId}", h.UpdateRate())
	mux.HandleFunc("DELETE /api/v1/admin/shipping/zones/{zoneId}/rates/{rateId}", h.DeleteRate())
	return mux
}

func seedZone() *shipping.Zone {
	now := time.Now().UTC()
	return &shipping.Zone{
		ID:        "zone-1",
		Name:      "Europe",
		Countries: []string{"DE", "FR", "NL"},
		Priority:  10,
		Active:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func seedRateTier() shipping.RateTier {
	return shipping.RateTier{
		ID:        "rt-1",
		ZoneID:    "zone-1",
		MinWeight: 0,
		MaxWeight: 5,
		Price:     shared.MustNewMoney(500, "EUR"),
	}
}

func zoneParseJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v; raw: %s", err, rec.Body.String())
	}
	return body
}

// ── ListZones ───────────────────────────────────────────────────────────

func TestShippingZoneAdmin_ListZones_OK(t *testing.T) {
	z := seedZone()
	repo := &mockZoneRepo{
		listZonesFn: func(_ context.Context) ([]shipping.Zone, error) {
			return []shipping.Zone{*z}, nil
		},
	}
	mux := zoneAdminSetup(repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/shipping/zones", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := zoneParseJSON(t, rec)
	data := body["data"].(map[string]interface{})
	zones := data["zones"].([]interface{})
	if len(zones) != 1 {
		t.Fatalf("zones len = %d, want 1", len(zones))
	}
	zone := zones[0].(map[string]interface{})
	if zone["name"] != "Europe" {
		t.Errorf("name = %v, want Europe", zone["name"])
	}
}

func TestShippingZoneAdmin_ListZones_Empty(t *testing.T) {
	repo := &mockZoneRepo{
		listZonesFn: func(_ context.Context) ([]shipping.Zone, error) {
			return nil, nil
		},
	}
	mux := zoneAdminSetup(repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/shipping/zones", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

// ── CreateZone ──────────────────────────────────────────────────────────

func TestShippingZoneAdmin_CreateZone_OK(t *testing.T) {
	repo := &mockZoneRepo{}
	mux := zoneAdminSetup(repo)

	body := `{"name":"Europe","countries":["DE","FR"],"priority":10}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/shipping/zones", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	resp := zoneParseJSON(t, rec)
	data := resp["data"].(map[string]interface{})
	zone := data["zone"].(map[string]interface{})
	if zone["name"] != "Europe" {
		t.Errorf("name = %v, want Europe", zone["name"])
	}
	if zone["active"] != true {
		t.Errorf("active = %v, want true", zone["active"])
	}
}

func TestShippingZoneAdmin_CreateZone_InvalidBody(t *testing.T) {
	mux := zoneAdminSetup(&mockZoneRepo{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/shipping/zones", strings.NewReader("not json"))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestShippingZoneAdmin_CreateZone_MissingName(t *testing.T) {
	mux := zoneAdminSetup(&mockZoneRepo{})

	body := `{"countries":["DE"],"priority":0}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/shipping/zones", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestShippingZoneAdmin_CreateZone_NoCountries(t *testing.T) {
	mux := zoneAdminSetup(&mockZoneRepo{})

	body := `{"name":"Empty","countries":[]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/shipping/zones", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

// ── UpdateZone ──────────────────────────────────────────────────────────

func TestShippingZoneAdmin_UpdateZone_OK(t *testing.T) {
	z := seedZone()
	repo := &mockZoneRepo{
		findZoneByIDFn: func(_ context.Context, id string) (*shipping.Zone, error) {
			if id == "zone-1" {
				return z, nil
			}
			return nil, nil
		},
	}
	mux := zoneAdminSetup(repo)

	body := `{"name":"EU Updated","priority":20}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/shipping/zones/zone-1", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	resp := zoneParseJSON(t, rec)
	data := resp["data"].(map[string]interface{})
	zone := data["zone"].(map[string]interface{})
	if zone["name"] != "EU Updated" {
		t.Errorf("name = %v, want EU Updated", zone["name"])
	}
	if int(zone["priority"].(float64)) != 20 {
		t.Errorf("priority = %v, want 20", zone["priority"])
	}
}

func TestShippingZoneAdmin_UpdateZone_NotFound(t *testing.T) {
	repo := &mockZoneRepo{} // FindZoneByID returns nil
	mux := zoneAdminSetup(repo)

	body := `{"name":"New"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/shipping/zones/missing", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestShippingZoneAdmin_UpdateZone_EmptyName(t *testing.T) {
	z := seedZone()
	repo := &mockZoneRepo{
		findZoneByIDFn: func(_ context.Context, _ string) (*shipping.Zone, error) {
			return z, nil
		},
	}
	mux := zoneAdminSetup(repo)

	body := `{"name":""}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/shipping/zones/zone-1", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestShippingZoneAdmin_UpdateZone_EmptyCountries(t *testing.T) {
	z := seedZone()
	repo := &mockZoneRepo{
		findZoneByIDFn: func(_ context.Context, _ string) (*shipping.Zone, error) {
			return z, nil
		},
	}
	mux := zoneAdminSetup(repo)

	body := `{"countries":[]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/shipping/zones/zone-1", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestShippingZoneAdmin_UpdateZone_InvalidCountryCode(t *testing.T) {
	z := seedZone()
	repo := &mockZoneRepo{
		findZoneByIDFn: func(_ context.Context, _ string) (*shipping.Zone, error) {
			return z, nil
		},
	}
	mux := zoneAdminSetup(repo)

	body := `{"countries":["DEU"]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/shipping/zones/zone-1", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestShippingZoneAdmin_UpdateZone_Deactivate(t *testing.T) {
	z := seedZone()
	repo := &mockZoneRepo{
		findZoneByIDFn: func(_ context.Context, _ string) (*shipping.Zone, error) {
			return z, nil
		},
	}
	mux := zoneAdminSetup(repo)

	body := `{"active":false}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/shipping/zones/zone-1", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := zoneParseJSON(t, rec)
	data := resp["data"].(map[string]interface{})
	zone := data["zone"].(map[string]interface{})
	if zone["active"] != false {
		t.Errorf("active = %v, want false", zone["active"])
	}
}

// ── DeleteZone ──────────────────────────────────────────────────────────

func TestShippingZoneAdmin_DeleteZone_OK(t *testing.T) {
	z := seedZone()
	repo := &mockZoneRepo{
		findZoneByIDFn: func(_ context.Context, id string) (*shipping.Zone, error) {
			if id == "zone-1" {
				return z, nil
			}
			return nil, nil
		},
	}
	mux := zoneAdminSetup(repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/admin/shipping/zones/zone-1", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestShippingZoneAdmin_DeleteZone_NotFound(t *testing.T) {
	repo := &mockZoneRepo{}
	mux := zoneAdminSetup(repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/admin/shipping/zones/missing", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// ── ListRates ───────────────────────────────────────────────────────────

func TestShippingZoneAdmin_ListRates_OK(t *testing.T) {
	z := seedZone()
	rt := seedRateTier()
	repo := &mockZoneRepo{
		findZoneByIDFn: func(_ context.Context, id string) (*shipping.Zone, error) {
			if id == "zone-1" {
				return z, nil
			}
			return nil, nil
		},
		listRateTiersFn: func(_ context.Context, zoneID string) ([]shipping.RateTier, error) {
			if zoneID == "zone-1" {
				return []shipping.RateTier{rt}, nil
			}
			return nil, nil
		},
	}
	mux := zoneAdminSetup(repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/shipping/zones/zone-1/rates", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := zoneParseJSON(t, rec)
	data := body["data"].(map[string]interface{})
	rates := data["rates"].([]interface{})
	if len(rates) != 1 {
		t.Fatalf("rates len = %d, want 1", len(rates))
	}
	rate := rates[0].(map[string]interface{})
	if int64(rate["price"].(float64)) != 500 {
		t.Errorf("price = %v, want 500", rate["price"])
	}
}

func TestShippingZoneAdmin_ListRates_ZoneNotFound(t *testing.T) {
	repo := &mockZoneRepo{}
	mux := zoneAdminSetup(repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/shipping/zones/missing/rates", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// ── CreateRate ──────────────────────────────────────────────────────────

func TestShippingZoneAdmin_CreateRate_OK(t *testing.T) {
	repo := &mockZoneRepo{}
	mux := zoneAdminSetup(repo)

	body := `{"min_weight":0,"max_weight":5,"price":500,"currency":"EUR"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/shipping/zones/zone-1/rates", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	resp := zoneParseJSON(t, rec)
	data := resp["data"].(map[string]interface{})
	rate := data["rate"].(map[string]interface{})
	if rate["currency"] != "EUR" {
		t.Errorf("currency = %v, want EUR", rate["currency"])
	}
}

func TestShippingZoneAdmin_CreateRate_DefaultCurrency(t *testing.T) {
	repo := &mockZoneRepo{}
	mux := zoneAdminSetup(repo)

	body := `{"min_weight":0,"max_weight":10,"price":750}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/shipping/zones/zone-1/rates", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	resp := zoneParseJSON(t, rec)
	data := resp["data"].(map[string]interface{})
	rate := data["rate"].(map[string]interface{})
	if rate["currency"] != "EUR" {
		t.Errorf("currency = %v, want EUR (default)", rate["currency"])
	}
}

func TestShippingZoneAdmin_CreateRate_InvalidBody(t *testing.T) {
	mux := zoneAdminSetup(&mockZoneRepo{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/shipping/zones/zone-1/rates", strings.NewReader("bad"))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

// ── UpdateRate ──────────────────────────────────────────────────────────

func TestShippingZoneAdmin_UpdateRate_OK(t *testing.T) {
	rt := seedRateTier()
	repo := &mockZoneRepo{
		findRateTierByIDFn: func(_ context.Context, id string) (*shipping.RateTier, error) {
			if id == "rt-1" {
				return &rt, nil
			}
			return nil, nil
		},
	}
	mux := zoneAdminSetup(repo)

	body := `{"price":999}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/shipping/zones/zone-1/rates/rt-1", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	resp := zoneParseJSON(t, rec)
	data := resp["data"].(map[string]interface{})
	rate := data["rate"].(map[string]interface{})
	if int64(rate["price"].(float64)) != 999 {
		t.Errorf("price = %v, want 999", rate["price"])
	}
}

func TestShippingZoneAdmin_UpdateRate_NotFound(t *testing.T) {
	repo := &mockZoneRepo{} // FindRateTierByID returns nil
	mux := zoneAdminSetup(repo)

	body := `{"price":100}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/shipping/zones/zone-1/rates/missing", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestShippingZoneAdmin_UpdateRate_InvalidInvariants(t *testing.T) {
	rt := seedRateTier()
	repo := &mockZoneRepo{
		findRateTierByIDFn: func(_ context.Context, id string) (*shipping.RateTier, error) {
			if id == "rt-1" {
				return &rt, nil
			}
			return nil, nil
		},
	}
	mux := zoneAdminSetup(repo)

	// min_weight > max_weight should fail domain validation.
	body := `{"min_weight":10,"max_weight":5}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/shipping/zones/zone-1/rates/rt-1", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

// ── DeleteRate ──────────────────────────────────────────────────────────

func TestShippingZoneAdmin_DeleteRate_OK(t *testing.T) {
	rt := seedRateTier()
	repo := &mockZoneRepo{
		findRateTierByIDFn: func(_ context.Context, id string) (*shipping.RateTier, error) {
			if id == "rt-1" {
				return &rt, nil
			}
			return nil, nil
		},
	}
	mux := zoneAdminSetup(repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/admin/shipping/zones/zone-1/rates/rt-1", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestShippingZoneAdmin_DeleteRate_RejectsMismatchedZone(t *testing.T) {
	rt := seedRateTier() // ZoneID = "zone-1"
	repo := &mockZoneRepo{
		findRateTierByIDFn: func(_ context.Context, id string) (*shipping.RateTier, error) {
			if id == "rt-1" {
				return &rt, nil
			}
			return nil, nil
		},
	}
	mux := zoneAdminSetup(repo)

	// Request zone "other-zone" but tier belongs to "zone-1".
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/admin/shipping/zones/other-zone/rates/rt-1", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// ── Free Shipping Threshold ─────────────────────────────────────────────

func TestShippingZoneAdmin_CreateZone_WithThreshold(t *testing.T) {
	repo := &mockZoneRepo{}
	mux := zoneAdminSetup(repo)

	body := `{"name":"EU","countries":["DE"],"priority":5,"free_shipping_threshold":5000,"free_shipping_currency":"EUR"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/shipping/zones", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	resp := zoneParseJSON(t, rec)
	data := resp["data"].(map[string]interface{})
	zone := data["zone"].(map[string]interface{})
	if int64(zone["free_shipping_threshold"].(float64)) != 5000 {
		t.Errorf("free_shipping_threshold = %v, want 5000", zone["free_shipping_threshold"])
	}
	if zone["free_shipping_currency"] != "EUR" {
		t.Errorf("free_shipping_currency = %v, want EUR", zone["free_shipping_currency"])
	}
}

func TestShippingZoneAdmin_CreateZone_ThresholdDefaultCurrency(t *testing.T) {
	repo := &mockZoneRepo{}
	mux := zoneAdminSetup(repo)

	body := `{"name":"EU","countries":["DE"],"priority":5,"free_shipping_threshold":3000}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/shipping/zones", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	resp := zoneParseJSON(t, rec)
	data := resp["data"].(map[string]interface{})
	zone := data["zone"].(map[string]interface{})
	if zone["free_shipping_currency"] != "EUR" {
		t.Errorf("free_shipping_currency = %v, want EUR (default)", zone["free_shipping_currency"])
	}
}

func TestShippingZoneAdmin_CreateZone_NoThreshold(t *testing.T) {
	repo := &mockZoneRepo{}
	mux := zoneAdminSetup(repo)

	body := `{"name":"EU","countries":["DE"],"priority":5}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/shipping/zones", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	resp := zoneParseJSON(t, rec)
	data := resp["data"].(map[string]interface{})
	zone := data["zone"].(map[string]interface{})
	if int64(zone["free_shipping_threshold"].(float64)) != 0 {
		t.Errorf("free_shipping_threshold = %v, want 0", zone["free_shipping_threshold"])
	}
}

func TestShippingZoneAdmin_UpdateZone_SetThreshold(t *testing.T) {
	z := seedZone()
	repo := &mockZoneRepo{
		findZoneByIDFn: func(_ context.Context, id string) (*shipping.Zone, error) {
			if id == "zone-1" {
				return z, nil
			}
			return nil, nil
		},
	}
	mux := zoneAdminSetup(repo)

	body := `{"free_shipping_threshold":5000,"free_shipping_currency":"EUR"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/shipping/zones/zone-1", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	resp := zoneParseJSON(t, rec)
	data := resp["data"].(map[string]interface{})
	zone := data["zone"].(map[string]interface{})
	if int64(zone["free_shipping_threshold"].(float64)) != 5000 {
		t.Errorf("free_shipping_threshold = %v, want 5000", zone["free_shipping_threshold"])
	}
}

func TestShippingZoneAdmin_UpdateZone_ClearThreshold(t *testing.T) {
	z := seedZone()
	z.FreeShippingThreshold = shared.MustNewMoney(5000, "EUR")
	repo := &mockZoneRepo{
		findZoneByIDFn: func(_ context.Context, id string) (*shipping.Zone, error) {
			if id == "zone-1" {
				return z, nil
			}
			return nil, nil
		},
	}
	mux := zoneAdminSetup(repo)

	body := `{"free_shipping_threshold":0}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/shipping/zones/zone-1", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	resp := zoneParseJSON(t, rec)
	data := resp["data"].(map[string]interface{})
	zone := data["zone"].(map[string]interface{})
	if int64(zone["free_shipping_threshold"].(float64)) != 0 {
		t.Errorf("free_shipping_threshold = %v, want 0", zone["free_shipping_threshold"])
	}
}

func TestShippingZoneAdmin_UpdateZone_SetThresholdDefaultCurrency(t *testing.T) {
	z := seedZone()
	repo := &mockZoneRepo{
		findZoneByIDFn: func(_ context.Context, id string) (*shipping.Zone, error) {
			if id == "zone-1" {
				return z, nil
			}
			return nil, nil
		},
	}
	mux := zoneAdminSetup(repo)

	body2 := `{"free_shipping_threshold":5000}`
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("PUT", "/api/v1/admin/shipping/zones/zone-1", strings.NewReader(body2))
	mux.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec2.Code, http.StatusOK, rec2.Body.String())
	}

	resp := zoneParseJSON(t, rec2)
	data := resp["data"].(map[string]interface{})
	zone2 := data["zone"].(map[string]interface{})
	if zone2["free_shipping_currency"] != "EUR" {
		t.Errorf("free_shipping_currency = %v, want EUR (default)", zone2["free_shipping_currency"])
	}
}

func TestShippingZoneAdmin_CreateZone_NegativeThreshold(t *testing.T) {
	mux := zoneAdminSetup(&mockZoneRepo{})

	body3 := `{"name":"EU","countries":["DE"],"priority":5,"free_shipping_threshold":-100}`
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest("POST", "/api/v1/admin/shipping/zones", strings.NewReader(body3))
	mux.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec3.Code, http.StatusUnprocessableEntity, rec3.Body.String())
	}
}

func TestShippingZoneAdmin_UpdateZone_NegativeThreshold(t *testing.T) {
	z := seedZone()
	repo := &mockZoneRepo{
		findZoneByIDFn: func(_ context.Context, id string) (*shipping.Zone, error) {
			if id == "zone-1" {
				return z, nil
			}
			return nil, nil
		},
	}
	mux := zoneAdminSetup(repo)

	body4 := `{"free_shipping_threshold":-100}`
	rec4 := httptest.NewRecorder()
	req4 := httptest.NewRequest("PUT", "/api/v1/admin/shipping/zones/zone-1", strings.NewReader(body4))
	mux.ServeHTTP(rec4, req4)

	if rec4.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec4.Code, http.StatusUnprocessableEntity, rec4.Body.String())
	}
}
