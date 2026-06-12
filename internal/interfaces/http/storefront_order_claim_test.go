package http_test

import (
	"context"
	"encoding/json"
	"errors"
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

func (r *storefrontOrderClaimRepoStub) FindByContactEmail(_ context.Context, contactEmail string) ([]domainOrder.Order, error) {
	norm := strings.ToLower(strings.TrimSpace(contactEmail))
	var orders []domainOrder.Order
	for _, o := range r.byID {
		if o.CustomerID == "" && strings.ToLower(strings.TrimSpace(o.ContactEmail)) == norm {
			orders = append(orders, *o)
		}
	}
	return orders, nil
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

func (r *storefrontOrderClaimRepoStub) LinkToCustomer(_ context.Context, o *domainOrder.Order) error {
	stored := r.byID[o.ID]
	if stored == nil {
		return errors.New("stub: link to customer: order not found")
	}
	if stored.CustomerID != "" {
		return errors.New("stub: link to customer: already linked")
	}
	clone := *o
	r.byID[o.ID] = &clone
	return nil
}

func (r *storefrontOrderClaimRepoStub) LinkToCustomerByContactEmail(_ context.Context, contactEmail, customerID string, updatedAt time.Time) (int64, error) {
	norm := strings.ToLower(strings.TrimSpace(contactEmail))
	var linked int64
	for id, o := range r.byID {
		if o.CustomerID == "" && strings.ToLower(strings.TrimSpace(o.ContactEmail)) == norm {
			clone := *o
			clone.CustomerID = customerID
			clone.UpdatedAt = updatedAt
			r.byID[id] = &clone
			linked++
		}
	}
	return linked, nil
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
	customerID  string
	token       string
	err         error
	called      bool
	claimCalled bool
	claimEmail  string
}

func (s *storefrontOrderLinkerStub) RegisterAndLink(_ *http.Request, _, _, _, _, _ string) (string, string, error) {
	s.called = true
	if s.err != nil {
		return "", "", s.err
	}
	return s.customerID, s.token, nil
}

func (s *storefrontOrderLinkerStub) RegisterAndClaimByEmail(_ *http.Request, contactEmail, _, _, _ string) (string, string, time.Time, error) {
	s.claimCalled = true
	s.claimEmail = contactEmail
	if s.err != nil {
		return "", "", time.Time{}, s.err
	}
	return s.customerID, s.token, time.Now().Add(time.Hour), nil
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

func TestStorefrontHandler_ClaimOrderSearch_NoMatches_SkipsEmail(t *testing.T) {
	repo := newStorefrontOrderClaimRepoStub()
	emailer := &storefrontOrderClaimEmailerStub{}
	claimSvc := orderApp.NewClaimService(repo)

	h := shophttp.NewStorefrontHandler(nil, nil, nil, nil, nil, nil).
		WithOrderClaim(claimSvc).
		WithOrderClaimEmailer(emailer).
		WithAccountSecurity("test-secret", time.Minute)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/claim-search", strings.NewReader(url.Values{
		"contact_email": []string{"nobody@example.com"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ClaimOrderSearch().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if emailer.lastToken != "" || emailer.lastEmail != "" {
		t.Fatalf("expected no claim email for unknown address, got email to %q", emailer.lastEmail)
	}
	if !strings.Contains(rec.Body.String(), "If any orders exist for this email") {
		t.Fatalf("expected generic message, got body: %s", rec.Body.String())
	}
}

func TestStorefrontHandler_ClaimOrderSearch_Matches_SendsEmailWithSameResponse(t *testing.T) {
	repo := newStorefrontOrderClaimRepoStub()
	emailer := &storefrontOrderClaimEmailerStub{}
	claimSvc := orderApp.NewClaimService(repo)

	h := shophttp.NewStorefrontHandler(nil, nil, nil, nil, nil, nil).
		WithOrderClaim(claimSvc).
		WithOrderClaimEmailer(emailer).
		WithAccountSecurity("test-secret", time.Minute)
	handler := h.ClaimOrderSearch()

	o := mustNewStorefrontGuestOrder(t, "guest@example.com")
	if err := repo.Save(context.Background(), &o); err != nil {
		t.Fatalf("Save: %v", err)
	}

	search := func(email string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/claim-search", strings.NewReader(url.Values{
			"contact_email": []string{email},
		}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	noMatchRec := search("nobody@example.com")
	if noMatchRec.Code != http.StatusOK {
		t.Fatalf("no-match status = %d, want %d; body: %s", noMatchRec.Code, http.StatusOK, noMatchRec.Body.String())
	}
	if emailer.lastToken != "" {
		t.Fatal("did not expect claim email for unknown address")
	}

	matchRec := search("guest@example.com")
	if matchRec.Code != http.StatusOK {
		t.Fatalf("match status = %d, want %d; body: %s", matchRec.Code, http.StatusOK, matchRec.Body.String())
	}
	if emailer.lastToken == "" || emailer.lastEmail != "guest@example.com" {
		t.Fatalf("expected claim email to guest@example.com, got %q (token %q)", emailer.lastEmail, emailer.lastToken)
	}

	// The full payload must be identical in both branches so the endpoint
	// cannot be used to enumerate which emails have orders.
	if matchRec.Body.String() != noMatchRec.Body.String() {
		t.Fatalf("response bodies differ:\nno match: %s\nmatch:    %s", noMatchRec.Body.String(), matchRec.Body.String())
	}
}

func newStorefrontClaimPageHandler(t *testing.T, repo *storefrontOrderClaimRepoStub, linker *storefrontOrderLinkerStub) *shophttp.StorefrontHandler {
	t.Helper()
	return shophttp.NewStorefrontHandler(createTestTheme(t), nil, newStorefrontCategoryMock(), nil, nil, nil).
		WithOrderClaim(orderApp.NewClaimService(repo)).
		WithOrderLinker(linker).
		WithAccountSecurity("test-secret", time.Minute)
}

func storefrontClaimTokenForTest(t *testing.T, h *shophttp.StorefrontHandler, repo *storefrontOrderClaimRepoStub, contactEmail string) string {
	t.Helper()
	emailer := &storefrontOrderClaimEmailerStub{}
	searchHandler := h.WithOrderClaimEmailer(emailer).ClaimOrderSearch()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/claim-search", strings.NewReader(url.Values{
		"contact_email": []string{contactEmail},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	searchHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("claim-search status = %d; body: %s", rec.Code, rec.Body.String())
	}
	if emailer.lastToken == "" {
		t.Fatal("expected claim token to be generated")
	}
	return emailer.lastToken
}

func TestStorefrontHandler_AccountOrdersClaim_RendersClaimableOrders(t *testing.T) {
	repo := newStorefrontOrderClaimRepoStub()
	linker := &storefrontOrderLinkerStub{customerID: "cust-1", token: "jwt"}
	h := newStorefrontClaimPageHandler(t, repo, linker)

	o := mustNewStorefrontGuestOrder(t, "guest@example.com")
	if err := repo.Save(context.Background(), &o); err != nil {
		t.Fatalf("Save: %v", err)
	}
	token := storefrontClaimTokenForTest(t, h, repo, "guest@example.com")

	req := httptest.NewRequest(http.MethodGet, "/account/orders/claim?claim_token="+url.QueryEscape(token), nil)
	rec := httptest.NewRecorder()
	h.AccountOrdersClaim().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, o.ID) {
		t.Fatalf("expected order %s on claim page; body: %s", o.ID, body)
	}
	if !strings.Contains(body, "guest@example.com") {
		t.Fatalf("expected contact email on claim page; body: %s", body)
	}
	if !strings.Contains(body, `name="claim_token"`) {
		t.Fatalf("expected registration form with claim token; body: %s", body)
	}
}

func TestStorefrontHandler_AccountOrdersClaim_InvalidToken(t *testing.T) {
	repo := newStorefrontOrderClaimRepoStub()
	linker := &storefrontOrderLinkerStub{}
	h := newStorefrontClaimPageHandler(t, repo, linker)

	req := httptest.NewRequest(http.MethodGet, "/account/orders/claim?claim_token=bogus", nil)
	rec := httptest.NewRecorder()
	h.AccountOrdersClaim().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid or has expired") {
		t.Fatalf("expected invalid-token message; body: %s", rec.Body.String())
	}
}

func TestStorefrontHandler_AccountOrdersClaim_RegistersLinksAndSignsIn(t *testing.T) {
	repo := newStorefrontOrderClaimRepoStub()
	linker := &storefrontOrderLinkerStub{customerID: "cust-9", token: "session-jwt"}
	h := newStorefrontClaimPageHandler(t, repo, linker)

	o := mustNewStorefrontGuestOrder(t, "guest@example.com")
	if err := repo.Save(context.Background(), &o); err != nil {
		t.Fatalf("Save: %v", err)
	}
	token := storefrontClaimTokenForTest(t, h, repo, "guest@example.com")

	req := httptest.NewRequest(http.MethodPost, "/account/orders/claim", strings.NewReader(url.Values{
		"claim_token": []string{token},
		"password":    []string{"SecurePass123"},
		"first_name":  []string{"Jane"},
		"last_name":   []string{"Doe"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.AccountOrdersClaim().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/account/orders" {
		t.Fatalf("redirect = %q, want /account/orders", got)
	}
	if !linker.claimCalled {
		t.Fatal("expected RegisterAndClaimByEmail to be called")
	}
	if linker.claimEmail != "guest@example.com" {
		t.Fatalf("claim email = %q, want guest@example.com", linker.claimEmail)
	}
	sessionCookie := ""
	for _, c := range rec.Result().Cookies() {
		if c.Name == "shopanda_storefront_session" {
			sessionCookie = c.Value
		}
	}
	if sessionCookie != "session-jwt" {
		t.Fatalf("session cookie = %q, want session-jwt", sessionCookie)
	}
}

func TestStorefrontHandler_AccountOrdersClaim_PostInvalidToken(t *testing.T) {
	repo := newStorefrontOrderClaimRepoStub()
	linker := &storefrontOrderLinkerStub{}
	h := newStorefrontClaimPageHandler(t, repo, linker)

	req := httptest.NewRequest(http.MethodPost, "/account/orders/claim", strings.NewReader(url.Values{
		"claim_token": []string{"bogus"},
		"password":    []string{"SecurePass123"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.AccountOrdersClaim().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if linker.claimCalled {
		t.Fatal("did not expect registration for invalid token")
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
