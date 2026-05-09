package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	orderApp "github.com/akarso/shopanda/internal/application/order"
	domainOrder "github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/id"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

type storefrontOrderClaimRepoStub struct {
	byID map[string]*domainOrder.Order
}

func newStorefrontOrderClaimRepoStub() *storefrontOrderClaimRepoStub {
	return &storefrontOrderClaimRepoStub{byID: map[string]*domainOrder.Order{}}
}

func (r *storefrontOrderClaimRepoStub) FindByID(_ context.Context, orderID string) (*domainOrder.Order, error) {
	if o := r.byID[orderID]; o != nil {
		clone := *o
		return &clone, nil
	}
	return nil, nil
}

func (r *storefrontOrderClaimRepoStub) FindByCustomerID(_ context.Context, _ string) ([]domainOrder.Order, error) {
	return nil, nil
}

func (r *storefrontOrderClaimRepoStub) FindByContactEmail(_ context.Context, _ string) ([]domainOrder.Order, error) {
	return nil, nil
}

func (r *storefrontOrderClaimRepoStub) List(_ context.Context, _, _ int) ([]domainOrder.Order, error) {
	return nil, nil
}

func (r *storefrontOrderClaimRepoStub) Save(_ context.Context, o *domainOrder.Order) error {
	clone := *o
	r.byID[o.ID] = &clone
	return nil
}

func (r *storefrontOrderClaimRepoStub) UpdateStatus(_ context.Context, o *domainOrder.Order) error {
	clone := *o
	r.byID[o.ID] = &clone
	return nil
}

type storefrontOrderClaimEmailerStub struct {
	lastEmail string
	lastToken string
}

func (e *storefrontOrderClaimEmailerStub) SendClaimEmail(contactEmail, claimToken string) error {
	e.lastEmail = contactEmail
	e.lastToken = claimToken
	return nil
}

type storefrontOrderLinkerStub struct {
	customerID string
	token      string
	err        error
	called     bool
}

func (s *storefrontOrderLinkerStub) RegisterAndLink(_ *http.Request, _, _, _, _, _ string) (string, string, error) {
	s.called = true
	if s.err != nil {
		return "", "", s.err
	}
	return s.customerID, s.token, nil
}

func TestStorefrontHandler_ClaimRegister_Success(t *testing.T) {
	repo := newStorefrontOrderClaimRepoStub()
	emailer := &storefrontOrderClaimEmailerStub{}
	linker := &storefrontOrderLinkerStub{customerID: "cust-123", token: "jwt-token"}
	claimSvc := orderApp.NewClaimService(repo)

	h := shophttp.NewStorefrontHandler(nil, nil, nil, nil, nil, nil).
		WithOrderClaim(claimSvc).
		WithOrderClaimEmailer(emailer).
		WithOrderLinker(linker).
		WithAccountSecurity("test-secret", time.Minute)

	o := mustNewStorefrontGuestOrder(t, "guest@example.com")
	if err := repo.Save(context.Background(), &o); err != nil {
		t.Fatalf("Save: %v", err)
	}

	searchReq := httptest.NewRequest(http.MethodPost, "/api/v1/orders/claim-search", strings.NewReader(url.Values{
		"contact_email": []string{"guest@example.com"},
	}.Encode()))
	searchReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	searchRec := httptest.NewRecorder()
	h.ClaimOrderSearch().ServeHTTP(searchRec, searchReq)

	if searchRec.Code != http.StatusOK {
		t.Fatalf("claim-search status = %d, want %d; body: %s", searchRec.Code, http.StatusOK, searchRec.Body.String())
	}
	if emailer.lastToken == "" {
		t.Fatal("expected claim token to be generated")
	}

	registerReq := httptest.NewRequest(http.MethodPost, "/api/v1/orders/claim-register", strings.NewReader(url.Values{
		"order_id":    []string{o.ID},
		"claim_token": []string{emailer.lastToken},
		"email":       []string{"guest@example.com"},
		"password":    []string{"SecurePass123"},
		"first_name":  []string{"Jane"},
		"last_name":   []string{"Doe"},
	}.Encode()))
	registerReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	registerRec := httptest.NewRecorder()
	h.ClaimLink().ServeHTTP(registerRec, registerReq)

	if registerRec.Code != http.StatusOK {
		t.Fatalf("claim-register status = %d, want %d; body: %s", registerRec.Code, http.StatusOK, registerRec.Body.String())
	}
	if !linker.called {
		t.Fatal("expected order linker to be called")
	}

	var body struct {
		Data struct {
			CustomerID string `json:"customer_id"`
			Email      string `json:"email"`
			Token      string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(registerRec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if body.Data.CustomerID != "cust-123" {
		t.Fatalf("customer_id = %q, want %q", body.Data.CustomerID, "cust-123")
	}
	if body.Data.Email != "guest@example.com" {
		t.Fatalf("email = %q, want %q", body.Data.Email, "guest@example.com")
	}
	if body.Data.Token != "jwt-token" {
		t.Fatalf("token = %q, want %q", body.Data.Token, "jwt-token")
	}
}

func TestStorefrontHandler_ClaimRegister_RejectsInvalidClaimToken(t *testing.T) {
	repo := newStorefrontOrderClaimRepoStub()
	linker := &storefrontOrderLinkerStub{customerID: "cust-123", token: "jwt-token"}
	claimSvc := orderApp.NewClaimService(repo)

	h := shophttp.NewStorefrontHandler(nil, nil, nil, nil, nil, nil).
		WithOrderClaim(claimSvc).
		WithOrderLinker(linker).
		WithAccountSecurity("test-secret", time.Minute)

	o := mustNewStorefrontGuestOrder(t, "guest@example.com")
	if err := repo.Save(context.Background(), &o); err != nil {
		t.Fatalf("Save: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/claim-register", strings.NewReader(url.Values{
		"order_id":    []string{o.ID},
		"claim_token": []string{"invalid"},
		"email":       []string{"guest@example.com"},
		"password":    []string{"SecurePass123"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ClaimLink().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if linker.called {
		t.Fatal("did not expect linker to be called for invalid token")
	}
}

func TestStorefrontHandler_ClaimRegister_ValidatesMissingFields(t *testing.T) {
	repo := newStorefrontOrderClaimRepoStub()
	linker := &storefrontOrderLinkerStub{}
	claimSvc := orderApp.NewClaimService(repo)

	h := shophttp.NewStorefrontHandler(nil, nil, nil, nil, nil, nil).
		WithOrderClaim(claimSvc).
		WithOrderLinker(linker).
		WithAccountSecurity("test-secret", time.Minute)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/claim-register", strings.NewReader(url.Values{}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ClaimLink().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if linker.called {
		t.Fatal("did not expect linker to be called for invalid payload")
	}
}

func mustNewStorefrontGuestOrder(t *testing.T, contactEmail string) domainOrder.Order {
	t.Helper()
	price := shared.MustNewMoney(1000, "EUR")
	item, err := domainOrder.NewItem("var-1", "SKU-001", "Test Product", 1, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := domainOrder.NewOrder(id.New(), "", contactEmail, "EUR", []domainOrder.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	return o
}
