package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	appAuth "github.com/akarso/shopanda/internal/application/auth"
	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/platform/auth"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

type emailChangeEvents struct {
	requested []customer.EmailChangeRequestedData
	notified  []customer.EmailChangeNotifiedData
}

func newEmailChangeHandler(t *testing.T) (http.Handler, *storefrontAccountCustomerRepoStub, string, *emailChangeEvents) {
	t.Helper()
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	bus := event.NewBus(logger.New("error"))
	captured := &emailChangeEvents{}
	bus.On(customer.EventEmailChangeRequested, func(_ context.Context, evt event.Event) error {
		captured.requested = append(captured.requested, evt.Data.(customer.EmailChangeRequestedData))
		return nil
	})
	bus.On(customer.EventEmailChangeNotified, func(_ context.Context, evt event.Event) error {
		captured.notified = append(captured.notified, evt.Data.(customer.EmailChangeNotifiedData))
		return nil
	})
	authSvc, repo := newStorefrontAuthServiceWithBus(t, bus)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	storefrontMarkCustomerEmailVerified(t, repo, out.CustomerID)
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).
		WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).
		WithAccountSecurity("test-secret", time.Minute).
		WithAccountSecurityEmailLinks("https://shop.test", 45*time.Minute)
	return newStorefrontRouter(h), repo, out.CustomerID, captured
}

func emailChangePost(t *testing.T, router http.Handler, customerID string, fresh bool, newEmail string) *httptest.ResponseRecorder {
	t.Helper()
	csrf := storefrontAccountCSRFCookie(t, router, "/account/login")
	form := url.Values{"csrf_token": {csrf.Value}, "new_email": {newEmail}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/account/security/email", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrf)
	if strings.TrimSpace(customerID) != "" {
		id, err := identity.NewIdentity(customerID, identity.RoleCustomer)
		if err != nil {
			t.Fatalf("NewIdentity: %v", err)
		}
		if fresh {
			id = id.WithAuthenticatedAt(time.Now().UTC())
		}
		req = req.WithContext(auth.WithIdentity(req.Context(), id))
	}
	router.ServeHTTP(rec, req)
	return rec
}

func emailChangeTokenFromEvent(t *testing.T, ev customer.EmailChangeRequestedData) string {
	t.Helper()
	parsed, err := url.Parse(ev.VerifyURL)
	if err != nil {
		t.Fatalf("parse verify url: %v", err)
	}
	if parsed.Scheme != "https" || parsed.Host != "shop.test" {
		t.Fatalf("verify url host = %s://%s, want https://shop.test", parsed.Scheme, parsed.Host)
	}
	if parsed.Path != "/account/security/email/confirm" {
		t.Fatalf("verify url path = %q, want /account/security/email/confirm", parsed.Path)
	}
	token := strings.TrimSpace(parsed.Query().Get("email_token"))
	if token == "" {
		t.Fatal("expected email_token in verify url")
	}
	return token
}

func TestStorefrontHandler_AccountEmailChange_RequestSendsLinkAndNotice(t *testing.T) {
	router, repo, customerID, events := newEmailChangeHandler(t)

	rec := emailChangePost(t, router, customerID, true, "New@Example.com")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/account/security?email_change=sent" {
		t.Fatalf("location = %q", loc)
	}
	if len(events.requested) != 1 || events.requested[0].NewEmail != "new@example.com" {
		t.Fatalf("requested = %+v", events.requested)
	}
	emailChangeTokenFromEvent(t, events.requested[0])
	if len(events.notified) != 1 || events.notified[0].OldEmail != "ada@example.com" || events.notified[0].NewEmail != "new@example.com" {
		t.Fatalf("notified = %+v", events.notified)
	}
	if repo.customers[customerID].PendingEmailNonce == "" {
		t.Fatal("expected pending nonce to be recorded")
	}
	if repo.customers[customerID].Email != "ada@example.com" {
		t.Fatal("email must not change before confirmation")
	}
}

func TestStorefrontHandler_AccountEmailChange_RequiresStepUp(t *testing.T) {
	router, _, customerID, events := newEmailChangeHandler(t)

	rec := emailChangePost(t, router, customerID, false, "new@example.com")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/account/security/verify") {
		t.Fatalf("location = %q, want step-up verify redirect", loc)
	}
	if len(events.requested) != 0 {
		t.Fatal("no email should be sent without step-up")
	}
}

func TestStorefrontHandler_AccountEmailChange_RequiresAuthentication(t *testing.T) {
	router, _, _, events := newEmailChangeHandler(t)

	rec := emailChangePost(t, router, "", false, "new@example.com")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/account/login") {
		t.Fatalf("location = %q, want login redirect", loc)
	}
	if len(events.requested) != 0 {
		t.Fatal("no email should be sent for anonymous request")
	}
}

func TestStorefrontHandler_AccountEmailChange_RejectsTakenEmail(t *testing.T) {
	router, repo, customerID, events := newEmailChangeHandler(t)
	other, err := customer.NewCustomer("other-cust", "taken@example.com")
	if err != nil {
		t.Fatalf("NewCustomer: %v", err)
	}
	repo.customers[other.ID] = &other
	repo.byEmail[other.Email] = &other

	rec := emailChangePost(t, router, customerID, true, "taken@example.com")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "already registered") {
		t.Fatalf("body = %q, want conflict message", rec.Body.String())
	}
	if len(events.requested) != 0 {
		t.Fatal("no email should be sent when address is taken")
	}
}

func TestStorefrontHandler_AccountEmailChangeConfirm_AppliesChange(t *testing.T) {
	router, repo, customerID, events := newEmailChangeHandler(t)

	if rec := emailChangePost(t, router, customerID, true, "new@example.com"); rec.Code != http.StatusSeeOther {
		t.Fatalf("request status = %d; body: %s", rec.Code, rec.Body.String())
	}
	token := emailChangeTokenFromEvent(t, events.requested[0])

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/account/security/email/confirm?email_token="+url.QueryEscape(token), nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("confirm status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "updated") {
		t.Fatalf("body = %q, want success message", rec.Body.String())
	}
	if repo.customers[customerID].Email != "new@example.com" {
		t.Fatalf("email = %q, want new@example.com", repo.customers[customerID].Email)
	}
	if repo.customers[customerID].PendingEmailNonce != "" {
		t.Fatal("nonce should be cleared after confirm")
	}
}

func TestStorefrontHandler_AccountEmailChangeConfirm_RejectsInvalidToken(t *testing.T) {
	router, _, _, _ := newEmailChangeHandler(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/account/security/email/confirm?email_token=not-a-real-token", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid or has expired") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestStorefrontHandler_AccountEmailChangeConfirm_RejectsSupersededNonce(t *testing.T) {
	router, repo, customerID, events := newEmailChangeHandler(t)

	if rec := emailChangePost(t, router, customerID, true, "new@example.com"); rec.Code != http.StatusSeeOther {
		t.Fatalf("request status = %d; body: %s", rec.Code, rec.Body.String())
	}
	token := emailChangeTokenFromEvent(t, events.requested[0])
	// Simulate a newer request superseding the first link.
	repo.customers[customerID].SetPendingEmailNonce("a-newer-nonce")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/account/security/email/confirm?email_token="+url.QueryEscape(token), nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if repo.customers[customerID].Email != "ada@example.com" {
		t.Fatal("email must be unchanged for a superseded link")
	}
}

func TestStorefrontHandler_AccountEmailChangeConfirm_RejectsRaceConflict(t *testing.T) {
	router, repo, customerID, events := newEmailChangeHandler(t)

	if rec := emailChangePost(t, router, customerID, true, "new@example.com"); rec.Code != http.StatusSeeOther {
		t.Fatalf("request status = %d; body: %s", rec.Code, rec.Body.String())
	}
	token := emailChangeTokenFromEvent(t, events.requested[0])
	// Another account registers the target address between request and confirm.
	other, err := customer.NewCustomer("other-cust", "new@example.com")
	if err != nil {
		t.Fatalf("NewCustomer: %v", err)
	}
	repo.customers[other.ID] = &other
	repo.byEmail[other.Email] = &other

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/account/security/email/confirm?email_token="+url.QueryEscape(token), nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if repo.customers[customerID].Email != "ada@example.com" {
		t.Fatal("email must be unchanged on conflict")
	}
}
