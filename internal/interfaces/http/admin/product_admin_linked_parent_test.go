package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
)

func TestProductAdminHandler_Create_RequiresLinkedParent(t *testing.T) {
	var created *catalog.Product
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, p *catalog.Product) error {
			cp := *p
			created = &cp
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/products", jsonBody(t, map[string]interface{}{
		"name": "Ticket",
		"slug": "ticket",
	}))
	newAdminRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if created == nil || created.RequiresLinkedParent {
		t.Fatalf("created = %+v, want flag false when omitted", created)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/products", jsonBody(t, map[string]interface{}{
		"name":                   "Ticket",
		"slug":                   "ticket-linked",
		"requires_linked_parent": true,
	}))
	newAdminRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if created == nil || !created.RequiresLinkedParent {
		t.Fatalf("created = %+v, want flag true", created)
	}
}

func TestProductAdminHandler_Update_RequiresLinkedParentOmitKeeps(t *testing.T) {
	existing := &catalog.Product{
		ID: "p1", Name: "Ticket", Slug: "ticket", Status: catalog.StatusDraft,
		Type: catalog.TypeSimple, PricingMode: catalog.PricingModeFixed, StockMode: catalog.StockModeFixed,
		RequiresLinkedParent: true,
		VisibilityModes:      catalog.DefaultVisibilityAxes(),
	}
	var updated *catalog.Product
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			cp := *existing
			return &cp, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			cp := *p
			updated = &cp
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).WithTxStarter(adminFakeTxDBConn())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/products/p1", jsonBody(t, map[string]interface{}{
		"name": "Ticket renamed",
	}))
	newAdminRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if updated == nil || !updated.RequiresLinkedParent || updated.Name != "Ticket renamed" {
		t.Fatalf("updated = %+v, want flag kept", updated)
	}
}

func TestProductAdminHandler_Update_RequiresLinkedParentOmitUsesLockedRow(t *testing.T) {
	existing := &catalog.Product{
		ID: "p1", Name: "Ticket", Slug: "ticket", Status: catalog.StatusDraft,
		Type: catalog.TypeSimple, PricingMode: catalog.PricingModeFixed, StockMode: catalog.StockModeFixed,
		RequiresLinkedParent: false,
		VisibilityModes:      catalog.DefaultVisibilityAxes(),
	}
	locked := true
	var updated *catalog.Product
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			cp := *existing
			return &cp, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			cp := *p
			updated = &cp
			return nil
		},
		lockedRequiresLinkedParent: &locked,
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).WithTxStarter(adminFakeTxDBConn())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/products/p1", jsonBody(t, map[string]interface{}{
		"name": "Ticket renamed",
	}))
	newAdminRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if updated == nil || !updated.RequiresLinkedParent || updated.Name != "Ticket renamed" {
		t.Fatalf("updated = %+v, want locked flag true", updated)
	}
}

func TestProductAdminHandler_Update_RequiresLinkedParentSetFalse(t *testing.T) {
	existing := &catalog.Product{
		ID: "p1", Name: "Ticket", Slug: "ticket", Status: catalog.StatusDraft,
		Type: catalog.TypeSimple, PricingMode: catalog.PricingModeFixed, StockMode: catalog.StockModeFixed,
		RequiresLinkedParent: true,
		VisibilityModes:      catalog.DefaultVisibilityAxes(),
	}
	var updated *catalog.Product
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			cp := *existing
			return &cp, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			cp := *p
			updated = &cp
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).WithTxStarter(adminFakeTxDBConn())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/products/p1", jsonBody(t, map[string]interface{}{
		"requires_linked_parent": false,
	}))
	newAdminRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if updated == nil || updated.RequiresLinkedParent {
		t.Fatalf("updated = %+v, want flag false", updated)
	}
}

func TestProductAdminHandler_Get_RequiresLinkedParentFieldAndExplain(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "Ticket", Slug: "ticket", Status: catalog.StatusActive,
				Type: catalog.TypeSimple, RequiresLinkedParent: true,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			}, nil
		},
		listCategoryIDsByProductFn: func(_ context.Context, _ string) ([]string, error) {
			return []string{}, nil
		},
	}
	modes := catalog.DefaultVisibilityAxes()
	modes.Purchasable = catalog.VisibilityModeVisible
	facts := &mockVisibilityFactsAdminSource{
		loadManyAnyStatusFn: func(_ context.Context, _ []string) (map[string]catalog.VisibilitySnapshot, error) {
			return map[string]catalog.VisibilitySnapshot{
				"p1": {
					Status: catalog.StatusActive,
					Modes:  modes,
					Inputs: catalog.VisibilityInputs{
						QuantityPositive: true, HasPrice: true, HasStoreAssignment: true,
					},
					RequiresLinkedParent: true,
				},
			}, nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).WithVisibilityFacts(facts, catalog.VisibilityOptions{}, true)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/products/p1", nil)
	newAdminRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := body["data"].(map[string]interface{})
	product := data["product"].(map[string]interface{})
	if product["RequiresLinkedParent"] != true {
		t.Fatalf("product.RequiresLinkedParent = %v, want true", product["RequiresLinkedParent"])
	}
	visibility := data["visibility"].(map[string]interface{})
	if visibility["requires_linked_parent"] != true {
		t.Fatalf("visibility.requires_linked_parent = %v, want true", visibility["requires_linked_parent"])
	}
	axes := visibility["axes"].(map[string]interface{})
	purchasable := axes["purchasable"].(map[string]interface{})
	if purchasable["resolved"] != false || purchasable["effective"] != false {
		t.Fatalf("purchasable = %+v, want resolved/effective false", purchasable)
	}
	codes, _ := purchasable["failure_codes"].([]interface{})
	if len(codes) != 1 || codes[0] != catalog.RequiresLinkedParentCode {
		t.Fatalf("purchasable failure_codes = %v, want [%q]", codes, catalog.RequiresLinkedParentCode)
	}
}
