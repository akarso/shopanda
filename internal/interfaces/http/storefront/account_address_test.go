package storefront_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/identity"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/internal/platform/auth"
)

func testCustomerIdentity(t *testing.T, userID string) identity.Identity {
	t.Helper()
	id, err := identity.NewIdentity(userID, identity.RoleCustomer)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAddressHandler_CreateAndList(t *testing.T) {
	repo := newStorefrontAddressRepoStub()
	h := storefront.NewAddressHandler(repo)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/account/addresses", h.Create())
	mux.HandleFunc("GET /api/v1/account/addresses", h.List())

	body := []byte(`{"recipient":"Ada","street":"1 Main","city":"Paris","postcode":"75001","country":"FR","label":"Home"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/addresses", bytes.NewReader(body))
	req = req.WithContext(auth.WithIdentity(context.Background(), testCustomerIdentity(t, "cust-1")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/account/addresses", nil)
	req = req.WithContext(auth.WithIdentity(context.Background(), testCustomerIdentity(t, "cust-1")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data struct {
			Addresses []struct {
				Recipient string `json:"recipient"`
				City      string `json:"city"`
			} `json:"addresses"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data.Addresses) != 1 || env.Data.Addresses[0].Recipient != "Ada" {
		t.Fatalf("addresses = %#v", env.Data.Addresses)
	}
}

func TestAddressHandler_RejectsForeignUpdate(t *testing.T) {
	repo := newStorefrontAddressRepoStub()
	addr, err := customer.NewAddress("a1", "cust-1", "Home", "Ada", "1 Main", "Paris", "75001", "FR")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(context.Background(), &addr); err != nil {
		t.Fatal(err)
	}
	h := storefront.NewAddressHandler(repo)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/account/addresses/a1", bytes.NewReader([]byte(`{"recipient":"Eve","street":"2","city":"Lyon","postcode":"69001","country":"FR"}`)))
	req.SetPathValue("addressId", "a1")
	req = req.WithContext(auth.WithIdentity(context.Background(), testCustomerIdentity(t, "cust-2")))
	rec := httptest.NewRecorder()
	h.Update().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAddressHandler_Update_ClearsDefault(t *testing.T) {
	repo := newStorefrontAddressRepoStub()
	addr, err := customer.NewAddress("a1", "cust-1", "Home", "Ada", "1 Main", "Paris", "75001", "FR")
	if err != nil {
		t.Fatal(err)
	}
	addr.IsDefault = true
	if err := repo.Create(context.Background(), &addr); err != nil {
		t.Fatal(err)
	}
	h := storefront.NewAddressHandler(repo)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/account/addresses/a1", bytes.NewReader([]byte(`{"recipient":"Ada","street":"1 Main","city":"Paris","postcode":"75001","country":"FR","is_default":false}`)))
	req.SetPathValue("addressId", "a1")
	req = req.WithContext(auth.WithIdentity(context.Background(), testCustomerIdentity(t, "cust-1")))
	rec := httptest.NewRecorder()
	h.Update().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	got, err := repo.FindByID(context.Background(), "a1")
	if err != nil || got == nil {
		t.Fatalf("FindByID: %v %#v", err, got)
	}
	if got.IsDefault {
		t.Fatal("is_default:false must persist on PUT")
	}
}

func TestAddressHandler_Update_OmitsIsDefault(t *testing.T) {
	repo := newStorefrontAddressRepoStub()
	addr, err := customer.NewAddress("a1", "cust-1", "Home", "Ada", "1 Main", "Paris", "75001", "FR")
	if err != nil {
		t.Fatal(err)
	}
	addr.IsDefault = true
	if err := repo.Create(context.Background(), &addr); err != nil {
		t.Fatal(err)
	}
	h := storefront.NewAddressHandler(repo)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/account/addresses/a1", bytes.NewReader([]byte(`{"recipient":"Ada","street":"9 Side","city":"Paris","postcode":"75001","country":"FR"}`)))
	req.SetPathValue("addressId", "a1")
	req = req.WithContext(auth.WithIdentity(context.Background(), testCustomerIdentity(t, "cust-1")))
	rec := httptest.NewRecorder()
	h.Update().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	got, err := repo.FindByID(context.Background(), "a1")
	if err != nil || got == nil {
		t.Fatalf("FindByID: %v %#v", err, got)
	}
	if !got.IsDefault {
		t.Fatal("omitted is_default must leave the default flag unchanged")
	}
	if got.Street != "9 Side" {
		t.Fatalf("street = %q, want 9 Side", got.Street)
	}
}

func TestAddressHandler_SetDefaultAndDelete(t *testing.T) {
	repo := newStorefrontAddressRepoStub()
	a1, err := customer.NewAddress("a1", "cust-1", "Home", "Ada", "1 Main", "Paris", "75001", "FR")
	if err != nil {
		t.Fatal(err)
	}
	a2, err := customer.NewAddress("a2", "cust-1", "Work", "Ada", "2 Side", "Lyon", "69001", "FR")
	if err != nil {
		t.Fatal(err)
	}
	a1.IsDefault = true
	if err := repo.Create(context.Background(), &a1); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(context.Background(), &a2); err != nil {
		t.Fatal(err)
	}
	h := storefront.NewAddressHandler(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/addresses/a2/default", nil)
	req.SetPathValue("addressId", "a2")
	req = req.WithContext(auth.WithIdentity(context.Background(), testCustomerIdentity(t, "cust-1")))
	rec := httptest.NewRecorder()
	h.SetDefault().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("set default status = %d body=%s", rec.Code, rec.Body.String())
	}
	def, err := repo.FindDefault(context.Background(), "cust-1")
	if err != nil || def == nil || def.ID != "a2" {
		t.Fatalf("default = %#v err=%v", def, err)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/account/addresses/a1", nil)
	req.SetPathValue("addressId", "a1")
	req = req.WithContext(auth.WithIdentity(context.Background(), testCustomerIdentity(t, "cust-1")))
	rec = httptest.NewRecorder()
	h.Delete().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}
	gone, err := repo.FindByID(context.Background(), "a1")
	if err != nil || gone != nil {
		t.Fatalf("deleted address still present: %#v err=%v", gone, err)
	}
}

func TestAddressHandler_Update_InvalidJSON(t *testing.T) {
	repo := newStorefrontAddressRepoStub()
	addr, err := customer.NewAddress("a1", "cust-1", "Home", "Ada", "1 Main", "Paris", "75001", "FR")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(context.Background(), &addr); err != nil {
		t.Fatal(err)
	}
	h := storefront.NewAddressHandler(repo)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/account/addresses/a1", bytes.NewReader([]byte("not-json")))
	req.SetPathValue("addressId", "a1")
	req = req.WithContext(auth.WithIdentity(context.Background(), testCustomerIdentity(t, "cust-1")))
	rec := httptest.NewRecorder()
	h.Update().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestAddressHandler_Update_EmptyAddressID(t *testing.T) {
	h := storefront.NewAddressHandler(newStorefrontAddressRepoStub())
	req := httptest.NewRequest(http.MethodPut, "/api/v1/account/addresses/", bytes.NewReader([]byte(`{"recipient":"Ada","street":"1","city":"Paris","postcode":"75001","country":"FR"}`)))
	req.SetPathValue("addressId", "")
	req = req.WithContext(auth.WithIdentity(context.Background(), testCustomerIdentity(t, "cust-1")))
	rec := httptest.NewRecorder()
	h.Update().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}
