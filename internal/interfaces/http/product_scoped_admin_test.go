package http_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/translation"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// --- stubs ---

type stubTranslationRepo struct {
	byEntityLang map[string][]translation.ContentTranslation
	upserts      []translation.ContentTranslation
	findErr      error
	upsertErr    error
}

func newStubTranslationRepo() *stubTranslationRepo {
	return &stubTranslationRepo{byEntityLang: make(map[string][]translation.ContentTranslation)}
}

func (s *stubTranslationRepo) FindByEntityAndLanguage(_ context.Context, entityID, language string) ([]translation.ContentTranslation, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}
	out := append([]translation.ContentTranslation{}, s.byEntityLang[entityID+"|"+language]...)
	return out, nil
}

func (s *stubTranslationRepo) FindFieldValue(_ context.Context, _, _, _ string) (*translation.ContentTranslation, error) {
	return nil, nil
}

func (s *stubTranslationRepo) Upsert(_ context.Context, ct *translation.ContentTranslation) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	s.upserts = append(s.upserts, *ct)
	key := ct.EntityID + "|" + ct.Language
	s.byEntityLang[key] = append(s.byEntityLang[key], *ct)
	return nil
}

func (s *stubTranslationRepo) DeleteByEntity(_ context.Context, _ string) error { return nil }

type scopedPriceRepo struct {
	prices    map[string]pricing.Price
	upserts   []pricing.Price
	findErr   error
	upsertErr error
}

func newScopedPriceRepo() *scopedPriceRepo {
	return &scopedPriceRepo{prices: make(map[string]pricing.Price)}
}

func priceStubKey(variantID, currency, storeID string) string {
	return variantID + "|" + currency + "|" + storeID
}

func (s *scopedPriceRepo) FindByVariantCurrencyAndStore(_ context.Context, variantID, currency, storeID string) (*pricing.Price, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}
	if p, ok := s.prices[priceStubKey(variantID, currency, storeID)]; ok {
		clone := p
		return &clone, nil
	}
	return nil, nil
}

func (s *scopedPriceRepo) ListByVariantID(_ context.Context, _ string) ([]pricing.Price, error) {
	return nil, nil
}

func (s *scopedPriceRepo) List(_ context.Context, _, _ int) ([]pricing.Price, error) {
	return nil, nil
}

func (s *scopedPriceRepo) Upsert(_ context.Context, p *pricing.Price) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	s.upserts = append(s.upserts, *p)
	s.prices[priceStubKey(p.VariantID, p.Amount.Currency(), p.StoreID)] = *p
	return nil
}

type stubVariantRepo struct {
	variants map[string]catalog.Variant
	findErr  error
}

func newStubVariantRepo() *stubVariantRepo {
	return &stubVariantRepo{variants: make(map[string]catalog.Variant)}
}

func (s *stubVariantRepo) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}
	if v, ok := s.variants[id]; ok {
		clone := v
		return &clone, nil
	}
	return nil, nil
}

func (s *stubVariantRepo) FindBySKU(_ context.Context, _ string) (*catalog.Variant, error) {
	return nil, nil
}

func (s *stubVariantRepo) ListByProductID(_ context.Context, _ string, _, _ int) ([]catalog.Variant, error) {
	return nil, nil
}

func (s *stubVariantRepo) Create(_ context.Context, _ *catalog.Variant) error { return nil }
func (s *stubVariantRepo) Update(_ context.Context, _ *catalog.Variant) error { return nil }

// --- helpers ---

func withAdminFullScope(req *http.Request, adminID, storeID, language, currency string) *http.Request {
	ac := &admin.AdminContext{AdminID: adminID, StoreID: storeID, Language: language, Currency: currency}
	return req.WithContext(ac.WithContext(req.Context()))
}

func newTranslationAdminMux(h *shophttp.ProductTranslationAdminHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/products/{id}/translations", h.Get())
	mux.HandleFunc("PUT /api/v1/admin/products/{id}/translations", h.Update())
	return mux
}

func newPriceAdminMux(h *shophttp.ProductPriceAdminHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/products/{id}/variants/{variantId}/price", h.Get())
	mux.HandleFunc("PUT /api/v1/admin/products/{id}/variants/{variantId}/price", h.Update())
	return mux
}

func productExistsRepo(id string) *mockAdminProductRepo {
	return &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, pid string) (*catalog.Product, error) {
			if pid != id {
				return nil, nil
			}
			return &catalog.Product{ID: id, Name: "Widget", Slug: "widget", Status: catalog.StatusActive}, nil
		},
	}
}

func assertScopeTriad(t *testing.T, ctx map[string]interface{}, storeID, language, currency string) {
	t.Helper()
	if got := ctx["detail_store_id"]; got != storeID {
		t.Errorf("detail_store_id = %v, want %q", got, storeID)
	}
	if got := ctx["detail_language"]; got != language {
		t.Errorf("detail_language = %v, want %q", got, language)
	}
	if got := ctx["detail_currency"]; got != currency {
		t.Errorf("detail_currency = %v, want %q", got, currency)
	}
}

// --- translation tests ---

func TestProductTranslationAdmin_Get_ReturnsActiveLanguageEntries(t *testing.T) {
	transRepo := newStubTranslationRepo()
	ct, _ := translation.NewContentTranslation("p1", "fr", "name", "Bonbon")
	transRepo.byEntityLang["p1|fr"] = []translation.ContentTranslation{ct}
	sink := &auditSink{}
	h := shophttp.NewProductTranslationAdminHandler(productExistsRepo("p1"), transRepo, admin.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products/p1/translations", nil)
	req = withAdminFullScope(req, "admin-1", "store-eu", "fr", "EUR")
	newTranslationAdminMux(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var env struct {
		Data struct {
			Entries     map[string]string `json:"entries"`
			Language    string            `json:"language"`
			FieldScopes map[string]string `json:"field_scopes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Data.Entries["name"] != "Bonbon" {
		t.Errorf("entries.name = %q, want %q", env.Data.Entries["name"], "Bonbon")
	}
	if env.Data.Entries["description"] != "" {
		t.Errorf("entries.description = %q, want empty", env.Data.Entries["description"])
	}
	if env.Data.Language != "fr" {
		t.Errorf("language = %q, want fr", env.Data.Language)
	}
	if env.Data.FieldScopes["name"] != "translatable" {
		t.Errorf("field_scopes.name = %q, want translatable", env.Data.FieldScopes["name"])
	}
	assertScopeTriad(t, sink.Last(t).context, "store-eu", "fr", "EUR")
}

func TestProductTranslationAdmin_Get_LanguageRequired(t *testing.T) {
	h := shophttp.NewProductTranslationAdminHandler(productExistsRepo("p1"), newStubTranslationRepo(), admin.NewAuditor(&auditSink{}), logger.NewWithWriter(io.Discard, "info"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products/p1/translations", nil)
	req = withAdminFullScope(req, "admin-1", "store-eu", "", "EUR")
	newTranslationAdminMux(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestProductTranslationAdmin_Get_ProductNotFound(t *testing.T) {
	repo := &mockAdminProductRepo{findByIDFn: func(_ context.Context, _ string) (*catalog.Product, error) { return nil, nil }}
	h := shophttp.NewProductTranslationAdminHandler(repo, newStubTranslationRepo(), admin.NewAuditor(&auditSink{}), logger.NewWithWriter(io.Discard, "info"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products/missing/translations", nil)
	req = withAdminFullScope(req, "admin-1", "store-eu", "fr", "EUR")
	newTranslationAdminMux(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestProductTranslationAdmin_Update_UpsertsActiveLanguage(t *testing.T) {
	transRepo := newStubTranslationRepo()
	sink := &auditSink{}
	h := shophttp.NewProductTranslationAdminHandler(productExistsRepo("p1"), transRepo, admin.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	body := map[string]interface{}{"entries": map[string]string{"name": "Bonbon", "description": ""}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1/translations", jsonBody(t, body))
	req = withAdminFullScope(req, "admin-1", "store-eu", "fr", "EUR")
	newTranslationAdminMux(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(transRepo.upserts) != 1 {
		t.Fatalf("upserts = %d, want 1 (empty description skipped)", len(transRepo.upserts))
	}
	up := transRepo.upserts[0]
	if up.EntityID != "p1" || up.Language != "fr" || up.Field != "name" || up.Value != "Bonbon" {
		t.Fatalf("upsert = %+v, want p1/fr/name/Bonbon", up)
	}
	assertScopeTriad(t, sink.Last(t).context, "store-eu", "fr", "EUR")
}

func TestProductTranslationAdmin_Update_InvalidField(t *testing.T) {
	h := shophttp.NewProductTranslationAdminHandler(productExistsRepo("p1"), newStubTranslationRepo(), admin.NewAuditor(&auditSink{}), logger.NewWithWriter(io.Discard, "info"))

	body := map[string]interface{}{"entries": map[string]string{"slug": "x"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1/translations", jsonBody(t, body))
	req = withAdminFullScope(req, "admin-1", "store-eu", "fr", "EUR")
	newTranslationAdminMux(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

// --- price tests ---

func seededVariantRepo() *stubVariantRepo {
	repo := newStubVariantRepo()
	repo.variants["v1"] = catalog.Variant{ID: "v1", ProductID: "p1", SKU: "SKU-1"}
	return repo
}

func TestProductPriceAdmin_Get_ReturnsStoreScopedPrice(t *testing.T) {
	priceRepo := newScopedPriceRepo()
	money := shared.MustNewMoney(1999, "EUR")
	p, _ := pricing.NewPrice("price-1", "v1", "store-eu", money)
	priceRepo.prices[priceStubKey("v1", "EUR", "store-eu")] = p
	sink := &auditSink{}
	h := shophttp.NewProductPriceAdminHandler(productExistsRepo("p1"), seededVariantRepo(), priceRepo, admin.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products/p1/variants/v1/price", nil)
	req = withAdminFullScope(req, "admin-1", "store-eu", "en", "EUR")
	newPriceAdminMux(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var env struct {
		Data struct {
			Price struct {
				Amount   int64  `json:"amount"`
				Currency string `json:"currency"`
				StoreID  string `json:"store_id"`
			} `json:"price"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Data.Price.Amount != 1999 || env.Data.Price.Currency != "EUR" || env.Data.Price.StoreID != "store-eu" {
		t.Fatalf("price = %+v, want 1999/EUR/store-eu", env.Data.Price)
	}
	assertScopeTriad(t, sink.Last(t).context, "store-eu", "en", "EUR")
}

func TestProductPriceAdmin_Get_CurrencyRequired(t *testing.T) {
	h := shophttp.NewProductPriceAdminHandler(productExistsRepo("p1"), seededVariantRepo(), newScopedPriceRepo(), admin.NewAuditor(&auditSink{}), logger.NewWithWriter(io.Discard, "info"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products/p1/variants/v1/price", nil)
	req = withAdminFullScope(req, "admin-1", "store-eu", "en", "")
	newPriceAdminMux(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestProductPriceAdmin_Get_VariantNotFoundForProduct(t *testing.T) {
	variantRepo := newStubVariantRepo()
	variantRepo.variants["v1"] = catalog.Variant{ID: "v1", ProductID: "other", SKU: "SKU-1"}
	h := shophttp.NewProductPriceAdminHandler(productExistsRepo("p1"), variantRepo, newScopedPriceRepo(), admin.NewAuditor(&auditSink{}), logger.NewWithWriter(io.Discard, "info"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products/p1/variants/v1/price", nil)
	req = withAdminFullScope(req, "admin-1", "store-eu", "en", "EUR")
	newPriceAdminMux(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestProductPriceAdmin_Update_WritesOnlyActiveStoreScope(t *testing.T) {
	priceRepo := newScopedPriceRepo()
	// Pre-existing global price must remain untouched after a store-scoped write.
	globalMoney := shared.MustNewMoney(1000, "EUR")
	globalPrice, _ := pricing.NewPrice("price-global", "v1", "", globalMoney)
	priceRepo.prices[priceStubKey("v1", "EUR", "")] = globalPrice
	sink := &auditSink{}
	h := shophttp.NewProductPriceAdminHandler(productExistsRepo("p1"), seededVariantRepo(), priceRepo, admin.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1/variants/v1/price", jsonBody(t, map[string]interface{}{"amount": 2500}))
	req = withAdminFullScope(req, "admin-1", "store-eu", "en", "EUR")
	newPriceAdminMux(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(priceRepo.upserts) != 1 {
		t.Fatalf("upserts = %d, want 1", len(priceRepo.upserts))
	}
	up := priceRepo.upserts[0]
	if up.StoreID != "store-eu" || up.Amount.Amount() != 2500 || up.Amount.Currency() != "EUR" {
		t.Fatalf("upsert = store=%q amount=%d cur=%q, want store-eu/2500/EUR", up.StoreID, up.Amount.Amount(), up.Amount.Currency())
	}
	if got := priceRepo.prices[priceStubKey("v1", "EUR", "")]; got.Amount.Amount() != 1000 {
		t.Fatalf("global price amount = %d, want 1000 (untouched)", got.Amount.Amount())
	}
	assertScopeTriad(t, sink.Last(t).context, "store-eu", "en", "EUR")
}

func TestProductPriceAdmin_Update_AmountRequired(t *testing.T) {
	h := shophttp.NewProductPriceAdminHandler(productExistsRepo("p1"), seededVariantRepo(), newScopedPriceRepo(), admin.NewAuditor(&auditSink{}), logger.NewWithWriter(io.Discard, "info"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1/variants/v1/price", jsonBody(t, map[string]interface{}{}))
	req = withAdminFullScope(req, "admin-1", "store-eu", "en", "EUR")
	newPriceAdminMux(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}
