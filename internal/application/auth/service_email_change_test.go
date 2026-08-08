package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/auth"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/jwt"
	"github.com/akarso/shopanda/internal/platform/jwt/jwttest"
)

func newEmailChangeService(repo *mockCustomerRepo, bus *event.Bus) *auth.Service {
	issuer, _ := jwt.NewIssuer(jwttest.TestSecret, time.Hour)
	return auth.NewService(repo, newMockResetRepo(), issuer, bus, testLogger{}, time.Hour)
}

func seedActiveCustomer(t *testing.T, repo *mockCustomerRepo, id, email string) *customer.Customer {
	t.Helper()
	c, err := customer.NewCustomer(id, email)
	if err != nil {
		t.Fatalf("NewCustomer: %v", err)
	}
	c.MarkEmailVerified()
	repo.customers[c.ID] = &c
	repo.byEmail[c.Email] = &c
	return &c
}

func assertAppCode(t *testing.T, err error, want apperror.Code) {
	t.Helper()
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != want {
		t.Fatalf("expected %s error, got %v", want, err)
	}
}

func TestRequestEmailChange_Success(t *testing.T) {
	repo := newMockRepo()
	seedActiveCustomer(t, repo, "cust-1", "old@example.com")
	bus := event.NewBus(testLogger{})

	var requested []customer.EmailChangeRequestedData
	var notified []customer.EmailChangeNotifiedData
	bus.On(customer.EventEmailChangeRequested, func(_ context.Context, evt event.Event) error {
		requested = append(requested, evt.Data.(customer.EmailChangeRequestedData))
		return nil
	})
	bus.On(customer.EventEmailChangeNotified, func(_ context.Context, evt event.Event) error {
		notified = append(notified, evt.Data.(customer.EmailChangeNotifiedData))
		return nil
	})

	svc := newEmailChangeService(repo, bus)
	err := svc.RequestEmailChange(context.Background(), auth.RequestEmailChangeInput{
		CustomerID: "cust-1",
		NewEmail:   "New@Example.com",
		Nonce:      "nonce-1",
		VerifyURL:  "https://shop.test/account/security/email/confirm?email_token=tok",
	})
	if err != nil {
		t.Fatalf("RequestEmailChange: %v", err)
	}

	c := repo.customers["cust-1"]
	if c.PendingEmailNonce != "nonce-1" {
		t.Fatalf("PendingEmailNonce = %q, want nonce-1", c.PendingEmailNonce)
	}
	if c.Email != "old@example.com" {
		t.Fatalf("email changed prematurely: %q", c.Email)
	}
	if len(requested) != 1 || requested[0].NewEmail != "new@example.com" {
		t.Fatalf("requested event = %+v", requested)
	}
	if requested[0].VerifyURL == "" {
		t.Fatal("expected verify url in requested event")
	}
	if len(notified) != 1 || notified[0].OldEmail != "old@example.com" || notified[0].NewEmail != "new@example.com" {
		t.Fatalf("notified event = %+v", notified)
	}
}

func TestRequestEmailChange_SameEmailRejected(t *testing.T) {
	repo := newMockRepo()
	seedActiveCustomer(t, repo, "cust-1", "user@example.com")
	bus := event.NewBus(testLogger{})
	var requested int
	bus.On(customer.EventEmailChangeRequested, func(_ context.Context, _ event.Event) error {
		requested++
		return nil
	})
	svc := newEmailChangeService(repo, bus)

	err := svc.RequestEmailChange(context.Background(), auth.RequestEmailChangeInput{
		CustomerID: "cust-1", NewEmail: "USER@example.com", Nonce: "n", VerifyURL: "https://shop.test/x",
	})
	assertAppCode(t, err, apperror.CodeValidation)
	if requested != 0 {
		t.Fatal("no event should be published on validation failure")
	}
	if repo.customers["cust-1"].PendingEmailNonce != "" {
		t.Fatal("nonce should not be recorded on validation failure")
	}
}

func TestRequestEmailChange_TakenEmailRejected(t *testing.T) {
	repo := newMockRepo()
	seedActiveCustomer(t, repo, "cust-1", "user@example.com")
	seedActiveCustomer(t, repo, "cust-2", "taken@example.com")
	bus := event.NewBus(testLogger{})
	var requested int
	bus.On(customer.EventEmailChangeRequested, func(_ context.Context, _ event.Event) error {
		requested++
		return nil
	})
	svc := newEmailChangeService(repo, bus)

	err := svc.RequestEmailChange(context.Background(), auth.RequestEmailChangeInput{
		CustomerID: "cust-1", NewEmail: "taken@example.com", Nonce: "n", VerifyURL: "https://shop.test/x",
	})
	assertAppCode(t, err, apperror.CodeConflict)
	if requested != 0 {
		t.Fatal("no event should be published when address is taken")
	}
}

func TestRequestEmailChange_InvalidEmailRejected(t *testing.T) {
	repo := newMockRepo()
	seedActiveCustomer(t, repo, "cust-1", "user@example.com")
	svc := newEmailChangeService(repo, event.NewBus(testLogger{}))

	err := svc.RequestEmailChange(context.Background(), auth.RequestEmailChangeInput{
		CustomerID: "cust-1", NewEmail: "not-an-email", Nonce: "n", VerifyURL: "https://shop.test/x",
	})
	assertAppCode(t, err, apperror.CodeValidation)
}

func TestConfirmEmailChange_Success(t *testing.T) {
	repo := newMockRepo()
	c := seedActiveCustomer(t, repo, "cust-1", "old@example.com")
	c.SetPendingEmailNonce("nonce-1")
	startGen := c.TokenGeneration
	svc := newEmailChangeService(repo, event.NewBus(testLogger{}))

	updated, err := svc.ConfirmEmailChange(context.Background(), auth.ConfirmEmailChangeInput{
		CustomerID: "cust-1", NewEmail: "new@example.com", Nonce: "nonce-1",
	})
	if err != nil {
		t.Fatalf("ConfirmEmailChange: %v", err)
	}
	if updated.Email != "new@example.com" {
		t.Fatalf("email = %q, want new@example.com", updated.Email)
	}
	if updated.EmailVerifiedAt == nil {
		t.Fatal("expected new email to be marked verified")
	}
	if updated.PendingEmailNonce != "" {
		t.Fatal("nonce should be cleared after confirm")
	}
	if updated.TokenGeneration != startGen {
		t.Fatalf("token generation changed (%d -> %d); existing sessions must stay valid", startGen, updated.TokenGeneration)
	}
	if repo.byEmail["old@example.com"] != nil {
		t.Fatal("old email should no longer resolve")
	}
}

func TestConfirmEmailChange_SupersededNonceRejected(t *testing.T) {
	repo := newMockRepo()
	c := seedActiveCustomer(t, repo, "cust-1", "old@example.com")
	c.SetPendingEmailNonce("nonce-2") // a newer request replaced nonce-1
	svc := newEmailChangeService(repo, event.NewBus(testLogger{}))

	_, err := svc.ConfirmEmailChange(context.Background(), auth.ConfirmEmailChangeInput{
		CustomerID: "cust-1", NewEmail: "new@example.com", Nonce: "nonce-1",
	})
	assertAppCode(t, err, apperror.CodeUnauthorized)
	if repo.customers["cust-1"].Email != "old@example.com" {
		t.Fatal("email must be unchanged after rejected confirm")
	}
}

func TestConfirmEmailChange_NoPendingRejected(t *testing.T) {
	repo := newMockRepo()
	seedActiveCustomer(t, repo, "cust-1", "old@example.com")
	svc := newEmailChangeService(repo, event.NewBus(testLogger{}))

	_, err := svc.ConfirmEmailChange(context.Background(), auth.ConfirmEmailChangeInput{
		CustomerID: "cust-1", NewEmail: "new@example.com", Nonce: "nonce-1",
	})
	assertAppCode(t, err, apperror.CodeUnauthorized)
}

func TestConfirmEmailChange_RaceConflictRejected(t *testing.T) {
	repo := newMockRepo()
	c := seedActiveCustomer(t, repo, "cust-1", "old@example.com")
	c.SetPendingEmailNonce("nonce-1")
	// Another account registered the target address after the request.
	seedActiveCustomer(t, repo, "cust-2", "new@example.com")
	svc := newEmailChangeService(repo, event.NewBus(testLogger{}))

	_, err := svc.ConfirmEmailChange(context.Background(), auth.ConfirmEmailChangeInput{
		CustomerID: "cust-1", NewEmail: "new@example.com", Nonce: "nonce-1",
	})
	assertAppCode(t, err, apperror.CodeConflict)
	if repo.customers["cust-1"].Email != "old@example.com" {
		t.Fatal("email must be unchanged after conflict")
	}
}
