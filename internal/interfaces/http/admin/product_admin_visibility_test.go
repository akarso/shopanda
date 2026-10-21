package admin_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/interfaces/http/admin"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	domainadmin "github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// mockVisibilityFactsAdminSource is a test double for
// catalog.VisibilityFactsAdminSource (PR-1059).
type mockVisibilityFactsAdminSource struct {
	loadManyAnyStatusFn func(ctx context.Context, productIDs []string) (map[string]catalog.VisibilitySnapshot, error)
}

func (m *mockVisibilityFactsAdminSource) LoadManyAnyStatus(ctx context.Context, productIDs []string) (map[string]catalog.VisibilitySnapshot, error) {
	if m.loadManyAnyStatusFn != nil {
		return m.loadManyAnyStatusFn(ctx, productIDs)
	}
	return nil, nil
}

func TestProductAdminHandler_Create_WithVisibilityModes(t *testing.T) {
	var created *catalog.Product
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, p *catalog.Product) error {
			created = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	body := jsonBody(t, map[string]interface{}{
		"name": "Widget",
		"slug": "widget",
		"visibility_modes": map[string]interface{}{
			"catalog":      "visible",
			"search":       "auto",
			"individually": "hidden",
			"purchasable":  "auto",
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if created == nil {
		t.Fatal("product was not created")
	}
	want := catalog.VisibilityAxes{
		Catalog:      catalog.VisibilityModeVisible,
		Search:       catalog.VisibilityModeAuto,
		Individually: catalog.VisibilityModeHidden,
		Purchasable:  catalog.VisibilityModeAuto,
	}
	if created.VisibilityModes != want {
		t.Fatalf("VisibilityModes = %#v, want %#v", created.VisibilityModes, want)
	}
}

func TestProductAdminHandler_Create_PartialVisibilityModesOnlyChangesGivenAxis(t *testing.T) {
	var created *catalog.Product
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, p *catalog.Product) error {
			created = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	body := jsonBody(t, map[string]interface{}{
		"name":             "Widget",
		"slug":             "widget",
		"visibility_modes": map[string]interface{}{"catalog": "hidden"},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	want := catalog.VisibilityAxes{
		Catalog:      catalog.VisibilityModeHidden,
		Search:       catalog.VisibilityModeAuto,
		Individually: catalog.VisibilityModeAuto,
		Purchasable:  catalog.VisibilityModeAuto,
	}
	if created == nil || created.VisibilityModes != want {
		t.Fatalf("VisibilityModes = %#v, want %#v (only catalog changed, rest default auto)", created, want)
	}
}

func TestProductAdminHandler_Create_OmittedVisibilityModesDefaultsAuto(t *testing.T) {
	var created *catalog.Product
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, p *catalog.Product) error {
			created = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	body := jsonBody(t, map[string]interface{}{
		"name": "Widget",
		"slug": "widget",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if created == nil || created.VisibilityModes != catalog.DefaultVisibilityAxes() {
		t.Fatalf("VisibilityModes = %#v, want DefaultVisibilityAxes()", created)
	}
}

func TestProductAdminHandler_Create_InvalidVisibilityMode(t *testing.T) {
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, _ *catalog.Product) error {
			t.Fatal("Create must not be called for an invalid visibility mode")
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	body := jsonBody(t, map[string]interface{}{
		"name": "Widget",
		"slug": "widget",
		"visibility_modes": map[string]interface{}{
			"catalog":      "sometimes",
			"search":       "auto",
			"individually": "auto",
			"purchasable":  "auto",
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `invalid visibility mode \"sometimes\"`) {
		t.Fatalf("body = %s, want invalid visibility mode error", rec.Body.String())
	}
}

func TestProductAdminHandler_Update_WithVisibilityModes(t *testing.T) {
	existing := &catalog.Product{
		ID:              "p1",
		Name:            "Widget",
		Slug:            "widget",
		Status:          catalog.StatusDraft,
		Type:            catalog.TypeSimple,
		VisibilityModes: catalog.DefaultVisibilityAxes(),
		Attributes:      map[string]interface{}{},
	}
	var updated *catalog.Product
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, _ string) (*catalog.Product, error) {
			cp := *existing
			return &cp, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			updated = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).WithTxStarter(adminFakeTxDBConn())

	body := jsonBody(t, map[string]interface{}{
		"visibility_modes": map[string]interface{}{
			"catalog":      "hidden",
			"search":       "hidden",
			"individually": "hidden",
			"purchasable":  "hidden",
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	want := catalog.VisibilityAxes{
		Catalog:      catalog.VisibilityModeHidden,
		Search:       catalog.VisibilityModeHidden,
		Individually: catalog.VisibilityModeHidden,
		Purchasable:  catalog.VisibilityModeHidden,
	}
	if updated == nil || updated.VisibilityModes != want {
		t.Fatalf("VisibilityModes = %#v, want %#v", updated, want)
	}
}

func TestProductAdminHandler_Update_PartialVisibilityModesOnlyChangesGivenAxis(t *testing.T) {
	existing := &catalog.Product{
		ID:     "p1",
		Name:   "Widget",
		Slug:   "widget",
		Status: catalog.StatusDraft,
		Type:   catalog.TypeSimple,
		VisibilityModes: catalog.VisibilityAxes{
			Catalog:      catalog.VisibilityModeVisible,
			Search:       catalog.VisibilityModeAuto,
			Individually: catalog.VisibilityModeAuto,
			Purchasable:  catalog.VisibilityModeHidden,
		},
	}
	var updated *catalog.Product
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, _ string) (*catalog.Product, error) {
			cp := *existing
			return &cp, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			updated = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).WithTxStarter(adminFakeTxDBConn())

	body := jsonBody(t, map[string]interface{}{
		"visibility_modes": map[string]interface{}{"catalog": "hidden"},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	want := catalog.VisibilityAxes{
		Catalog:      catalog.VisibilityModeHidden, // changed
		Search:       catalog.VisibilityModeAuto,   // unchanged
		Individually: catalog.VisibilityModeAuto,   // unchanged
		Purchasable:  catalog.VisibilityModeHidden, // unchanged
	}
	if updated == nil || updated.VisibilityModes != want {
		t.Fatalf("VisibilityModes = %#v, want %#v (only catalog changed)", updated, want)
	}
}

func TestProductAdminHandler_Update_OmittedVisibilityModesPreserves(t *testing.T) {
	forced := catalog.VisibilityAxes{
		Catalog:      catalog.VisibilityModeVisible,
		Search:       catalog.VisibilityModeAuto,
		Individually: catalog.VisibilityModeAuto,
		Purchasable:  catalog.VisibilityModeHidden,
	}
	existing := &catalog.Product{
		ID: "p1", Name: "Widget", Slug: "widget", Status: catalog.StatusDraft, Type: catalog.TypeSimple,
		VisibilityModes: forced,
	}
	var updated *catalog.Product
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, _ string) (*catalog.Product, error) {
			cp := *existing
			return &cp, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			updated = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).WithTxStarter(adminFakeTxDBConn())

	body := jsonBody(t, map[string]interface{}{"name": "Renamed"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if updated == nil || updated.VisibilityModes != forced {
		t.Fatalf("VisibilityModes = %#v, want preserved %#v", updated, forced)
	}
}

func TestProductAdminHandler_Update_InvalidVisibilityMode(t *testing.T) {
	existing := &catalog.Product{
		ID: "p1", Name: "Widget", Slug: "widget", Status: catalog.StatusDraft, Type: catalog.TypeSimple,
		VisibilityModes: catalog.DefaultVisibilityAxes(),
	}
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, _ string) (*catalog.Product, error) {
			cp := *existing
			return &cp, nil
		},
		updateFn: func(_ context.Context, _ *catalog.Product) error {
			t.Fatal("Update must not be called for an invalid visibility mode")
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).WithTxStarter(adminFakeTxDBConn())

	body := jsonBody(t, map[string]interface{}{
		"visibility_modes": map[string]interface{}{
			"catalog":      "nope",
			"search":       "auto",
			"individually": "auto",
			"purchasable":  "auto",
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestProductAdminHandler_Get_OmitsVisibilityWhenFactsNotWired(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "Widget", Slug: "widget", Status: catalog.StatusActive, Type: catalog.TypeSimple}, nil
		},
		listCategoryIDsByProductFn: func(_ context.Context, _ string) ([]string, error) {
			return []string{}, nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products/p1", nil)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := body["data"].(map[string]interface{})
	if _, ok := data["visibility"]; ok {
		t.Fatalf("visibility key should be omitted when facts are not wired, got: %v", data)
	}
	if _, ok := data["visibility_error"]; ok {
		t.Fatalf("visibility_error key should be omitted when facts are not wired, got: %v", data)
	}
}

func TestProductAdminHandler_Get_IncludesVisibilityPayload(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "Widget", Slug: "widget", Status: catalog.StatusDraft, Type: catalog.TypeSimple, VisibilityModes: catalog.DefaultVisibilityAxes()}, nil
		},
		listCategoryIDsByProductFn: func(_ context.Context, _ string) ([]string, error) {
			return []string{}, nil
		},
	}
	facts := &mockVisibilityFactsAdminSource{
		loadManyAnyStatusFn: func(_ context.Context, productIDs []string) (map[string]catalog.VisibilitySnapshot, error) {
			if len(productIDs) != 1 || productIDs[0] != "p1" {
				t.Fatalf("productIDs = %v, want [p1]", productIDs)
			}
			return map[string]catalog.VisibilitySnapshot{
				"p1": {
					Status: catalog.StatusDraft, // draft -> status is the only failing AutoBasis condition here
					Modes:  catalog.DefaultVisibilityAxes(),
					Inputs: catalog.VisibilityInputs{
						QuantityPositive:   true,
						HasPrice:           true,
						HasStoreAssignment: true,
						HasCategory:        true,
					},
				},
			}, nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).WithVisibilityFacts(facts, catalog.VisibilityOptions{}, true)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products/p1", nil)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := body["data"].(map[string]interface{})
	visibility, ok := data["visibility"].(map[string]interface{})
	if !ok {
		t.Fatalf("visibility is not an object: %v", data)
	}

	if visibility["enforced"] != true {
		t.Errorf("enforced = %v, want true", visibility["enforced"])
	}
	if visibility["status"] != "draft" {
		t.Errorf("status = %v, want draft", visibility["status"])
	}
	if visibility["auto_basis"] != false {
		t.Errorf("auto_basis = %v, want false (status is draft)", visibility["auto_basis"])
	}

	codes, ok := visibility["failure_codes"].([]interface{})
	if !ok {
		t.Fatalf("failure_codes is not an array: %v", visibility)
	}
	if len(codes) != 1 || codes[0] != catalog.AutoBasisCodeStatusInactive {
		t.Fatalf("failure_codes = %v, want [%q]", codes, catalog.AutoBasisCodeStatusInactive)
	}

	inputCodes, ok := visibility["input_failure_codes"].([]interface{})
	if !ok {
		t.Fatalf("input_failure_codes is not an array: %v", visibility)
	}
	if len(inputCodes) != 0 {
		t.Fatalf("input_failure_codes = %v, want none (all inputs ok; only status fails)", inputCodes)
	}

	axes, ok := visibility["axes"].(map[string]interface{})
	if !ok {
		t.Fatalf("axes is not an object: %v", visibility)
	}
	for _, axisKey := range []string{"catalog", "search", "individually", "purchasable"} {
		axis, ok := axes[axisKey].(map[string]interface{})
		if !ok {
			t.Fatalf("axes[%q] is not an object: %v", axisKey, axes)
		}
		if axis["mode"] != "auto" {
			t.Errorf("axes[%q].mode = %v, want auto", axisKey, axis["mode"])
		}
		if axis["override"] != false {
			t.Errorf("axes[%q].override = %v, want false", axisKey, axis["override"])
		}
		if axis["resolved"] != false {
			t.Errorf("axes[%q].resolved = %v, want false (auto_basis false)", axisKey, axis["resolved"])
		}
		if axis["effective"] != false {
			t.Errorf("axes[%q].effective = %v, want false (draft status)", axisKey, axis["effective"])
		}
	}

	if _, ok := visibility["require_category"]; ok {
		t.Fatalf("require_category must not be present in the payload, got: %v", visibility)
	}
}

// TestProductAdminHandler_Get_InputFailureCodesExcludesStatus: input_failure_codes
// must mirror AutoBasisInputFailureCodes (never status_inactive), even when
// failure_codes includes status_inactive for a draft product.
func TestProductAdminHandler_Get_InputFailureCodesExcludesStatus(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "Widget", Slug: "widget", Status: catalog.StatusDraft, Type: catalog.TypeSimple, VisibilityModes: catalog.DefaultVisibilityAxes()}, nil
		},
		listCategoryIDsByProductFn: func(_ context.Context, _ string) ([]string, error) {
			return []string{}, nil
		},
	}
	facts := &mockVisibilityFactsAdminSource{
		loadManyAnyStatusFn: func(_ context.Context, _ []string) (map[string]catalog.VisibilitySnapshot, error) {
			return map[string]catalog.VisibilitySnapshot{
				"p1": {
					Status: catalog.StatusDraft,
					Modes:  catalog.DefaultVisibilityAxes(),
					Inputs: catalog.VisibilityInputs{
						// Draft status AND no stock both fail; status must
						// never leak into input_failure_codes.
						QuantityPositive:   false,
						HasPrice:           true,
						HasStoreAssignment: true,
						HasCategory:        true,
					},
				},
			}, nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).WithVisibilityFacts(facts, catalog.VisibilityOptions{}, true)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products/p1", nil)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := body["data"].(map[string]interface{})
	visibility := data["visibility"].(map[string]interface{})

	failureCodes, _ := visibility["failure_codes"].([]interface{})
	wantFailure := []interface{}{catalog.AutoBasisCodeStatusInactive, catalog.AutoBasisCodeNoStock}
	if !reflect.DeepEqual(failureCodes, wantFailure) {
		t.Fatalf("failure_codes = %v, want %v", failureCodes, wantFailure)
	}

	inputCodes, _ := visibility["input_failure_codes"].([]interface{})
	wantInput := []interface{}{catalog.AutoBasisCodeNoStock}
	if !reflect.DeepEqual(inputCodes, wantInput) {
		t.Fatalf("input_failure_codes = %v, want %v (status must be excluded)", inputCodes, wantInput)
	}
}

func TestProductAdminHandler_Get_ForcedVisibleDraftIsNotEffective(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	modes.Catalog = catalog.VisibilityModeVisible
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "Widget", Slug: "widget", Status: catalog.StatusDraft, Type: catalog.TypeSimple, VisibilityModes: modes}, nil
		},
		listCategoryIDsByProductFn: func(_ context.Context, _ string) ([]string, error) {
			return []string{}, nil
		},
	}
	facts := &mockVisibilityFactsAdminSource{
		loadManyAnyStatusFn: func(_ context.Context, _ []string) (map[string]catalog.VisibilitySnapshot, error) {
			return map[string]catalog.VisibilitySnapshot{
				"p1": {
					Status: catalog.StatusDraft,
					Modes:  modes,
					Inputs: catalog.VisibilityInputs{QuantityPositive: true, HasPrice: true, HasStoreAssignment: true},
				},
			}, nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).WithVisibilityFacts(facts, catalog.VisibilityOptions{}, true)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products/p1", nil)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := body["data"].(map[string]interface{})
	visibility := data["visibility"].(map[string]interface{})
	axes := visibility["axes"].(map[string]interface{})
	catalogAxis := axes["catalog"].(map[string]interface{})

	if catalogAxis["mode"] != "visible" || catalogAxis["override"] != true {
		t.Fatalf("catalog axis = %v, want forced visible override", catalogAxis)
	}
	if catalogAxis["resolved"] != true {
		t.Error("resolved should be true (ResolveAxis ignores status)")
	}
	if catalogAxis["effective"] != false {
		t.Error("effective should be false (draft status always denies, even forced visible)")
	}
}

// TestProductAdminHandler_Get_MissingSnapshotSetsVisibilityError: missing snapshot
// after a successful LoadManyAnyStatus sets visibility_error (concurrent delete).
func TestProductAdminHandler_Get_MissingSnapshotSetsVisibilityError(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "Widget", Slug: "widget", Status: catalog.StatusActive, Type: catalog.TypeSimple}, nil
		},
		listCategoryIDsByProductFn: func(_ context.Context, _ string) ([]string, error) {
			return []string{}, nil
		},
	}
	facts := &mockVisibilityFactsAdminSource{
		loadManyAnyStatusFn: func(_ context.Context, _ []string) (map[string]catalog.VisibilitySnapshot, error) {
			return map[string]catalog.VisibilitySnapshot{}, nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).WithVisibilityFacts(facts, catalog.VisibilityOptions{}, true)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products/p1", nil)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := body["data"].(map[string]interface{})
	if _, ok := data["product"]; !ok {
		t.Fatal("product must still be returned when the visibility snapshot is missing")
	}
	if _, ok := data["visibility"]; ok {
		t.Fatalf("visibility key should be omitted when snapshot is missing, got: %v", data)
	}
	if data["visibility_error"] != "product not found in visibility facts" {
		t.Fatalf("visibility_error = %v, want %q", data["visibility_error"], "product not found in visibility facts")
	}
}

// TestProductAdminHandler_Get_VisibilityFactsErrorDegradesGracefully: facts-load
// error → 200 with visibility_error, product still returned.
func TestProductAdminHandler_Get_VisibilityFactsErrorDegradesGracefully(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "Widget", Slug: "widget", Status: catalog.StatusActive, Type: catalog.TypeSimple}, nil
		},
		listCategoryIDsByProductFn: func(_ context.Context, _ string) ([]string, error) {
			return []string{}, nil
		},
	}
	facts := &mockVisibilityFactsAdminSource{
		loadManyAnyStatusFn: func(_ context.Context, _ []string) (map[string]catalog.VisibilitySnapshot, error) {
			return nil, errors.New("database unavailable")
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).WithVisibilityFacts(facts, catalog.VisibilityOptions{}, true)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products/p1", nil)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (facts error must degrade, not 500); body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := body["data"].(map[string]interface{})
	if _, ok := data["product"]; !ok {
		t.Fatal("product must still be returned when visibility facts fail")
	}
	if _, ok := data["visibility"]; ok {
		t.Fatalf("visibility key should be omitted when facts load fails, got: %v", data)
	}
	if data["visibility_error"] != "visibility facts unavailable" {
		t.Fatalf("visibility_error = %v, want %q", data["visibility_error"], "visibility facts unavailable")
	}
}

// recordingAuditLogRepo records audit inserts for visibility_modes before/after tests.
type recordingAuditLogRepo struct {
	inserted []domainadmin.AuditLogRecord
}

func (m *recordingAuditLogRepo) Insert(_ context.Context, record domainadmin.AuditLogRecord) error {
	m.inserted = append(m.inserted, record)
	return nil
}

func (m *recordingAuditLogRepo) List(_ context.Context, _ domainadmin.AuditLogFilter) ([]domainadmin.AuditLogRecord, error) {
	return nil, nil
}

func (m *recordingAuditLogRepo) DeleteBefore(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func newRecordingAuditor() (*adminapp.Auditor, *recordingAuditLogRepo) {
	repo := &recordingAuditLogRepo{}
	auditor := adminapp.NewAuditor(logger.NewWithWriter(io.Discard, "error"))
	auditor.SetAuditLogRepository(repo)
	return auditor, repo
}

func TestProductAdminHandler_Update_AuditRecordsModesBeforeAfterWhenChanged(t *testing.T) {
	existing := &catalog.Product{
		ID:              "p1",
		Name:            "Widget",
		Slug:            "widget",
		Status:          catalog.StatusDraft,
		Type:            catalog.TypeSimple,
		VisibilityModes: catalog.DefaultVisibilityAxes(),
		Attributes:      map[string]interface{}{},
	}
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, _ string) (*catalog.Product, error) {
			cp := *existing
			return &cp, nil
		},
		updateFn: func(_ context.Context, _ *catalog.Product) error { return nil },
	}
	auditor, auditRepo := newRecordingAuditor()
	h := admin.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), auditor, logger.NewWithWriter(io.Discard, "error")).WithTxStarter(adminFakeTxDBConn())

	body := jsonBody(t, map[string]interface{}{
		"visibility_modes": map[string]interface{}{"catalog": "hidden"},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var success *domainadmin.AuditLogRecord
	for i := range auditRepo.inserted {
		if auditRepo.inserted[i].Result == "success" {
			success = &auditRepo.inserted[i]
		}
	}
	if success == nil {
		t.Fatal("expected a persisted success audit entry")
	}
	before, ok := success.Metadata["visibility_modes_before"].(map[string]string)
	if !ok {
		t.Fatalf("visibility_modes_before missing or wrong type: %#v", success.Metadata)
	}
	after, ok := success.Metadata["visibility_modes_after"].(map[string]string)
	if !ok {
		t.Fatalf("visibility_modes_after missing or wrong type: %#v", success.Metadata)
	}
	if before["catalog"] != "auto" {
		t.Errorf("visibility_modes_before[catalog] = %q, want %q", before["catalog"], "auto")
	}
	if after["catalog"] != "hidden" {
		t.Errorf("visibility_modes_after[catalog] = %q, want %q", after["catalog"], "hidden")
	}
}

func TestProductAdminHandler_Update_AuditOmitsModesBeforeAfterWhenResentIdentical(t *testing.T) {
	stored := catalog.VisibilityAxes{
		Catalog:      catalog.VisibilityModeVisible,
		Search:       catalog.VisibilityModeAuto,
		Individually: catalog.VisibilityModeHidden,
		Purchasable:  catalog.VisibilityModeAuto,
	}
	existing := &catalog.Product{
		ID:              "p1",
		Name:            "Widget",
		Slug:            "widget",
		Status:          catalog.StatusDraft,
		Type:            catalog.TypeSimple,
		VisibilityModes: stored,
		Attributes:      map[string]interface{}{},
	}
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, _ string) (*catalog.Product, error) {
			cp := *existing
			return &cp, nil
		},
		updateFn: func(_ context.Context, _ *catalog.Product) error { return nil },
	}
	auditor, auditRepo := newRecordingAuditor()
	h := admin.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), auditor, logger.NewWithWriter(io.Discard, "error")).WithTxStarter(adminFakeTxDBConn())

	// Resend all four axes identical to what is already stored — the admin
	// GUI's save always sends visibility_modes (collectModes reads every
	// select), so "no actual change" is the common case, not the exception.
	body := jsonBody(t, map[string]interface{}{
		"visibility_modes": map[string]interface{}{
			"catalog":      "visible",
			"search":       "auto",
			"individually": "hidden",
			"purchasable":  "auto",
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var success *domainadmin.AuditLogRecord
	for i := range auditRepo.inserted {
		if auditRepo.inserted[i].Result == "success" {
			success = &auditRepo.inserted[i]
		}
	}
	if success == nil {
		t.Fatal("expected a persisted success audit entry")
	}
	if _, ok := success.Metadata["visibility_modes_before"]; ok {
		t.Errorf("visibility_modes_before should be absent when resent modes match stored, got: %#v", success.Metadata)
	}
	if _, ok := success.Metadata["visibility_modes_after"]; ok {
		t.Errorf("visibility_modes_after should be absent when resent modes match stored, got: %#v", success.Metadata)
	}
}
