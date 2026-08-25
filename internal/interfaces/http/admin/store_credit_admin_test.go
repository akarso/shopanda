package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	storecreditApp "github.com/akarso/shopanda/internal/application/storecredit"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/domain/shared"
	credit "github.com/akarso/shopanda/internal/domain/storecredit"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type stubStoreCreditRepo struct {
	balance    int64
	entries    []credit.Entry
	issued     []shared.Money
	issuedKeys []string
	issueFn    func(ctx context.Context, customerID string, amount shared.Money, note string) error
}

func (s *stubStoreCreditRepo) GetBalance(_ context.Context, _, currency string) (shared.Money, error) {
	return shared.MustNewMoney(s.balance, currency), nil
}

func (s *stubStoreCreditRepo) Issue(ctx context.Context, customerID string, amount shared.Money, note, idempotencyKey string) error {
	if idempotencyKey != "" {
		for _, k := range s.issuedKeys {
			if k == idempotencyKey {
				return nil // idempotent replay: already issued, no-op
			}
		}
	}
	if s.issueFn != nil {
		if err := s.issueFn(ctx, customerID, amount, note); err != nil {
			return err
		}
	}
	s.issued = append(s.issued, amount)
	if idempotencyKey != "" {
		s.issuedKeys = append(s.issuedKeys, idempotencyKey)
	}
	s.balance += amount.Amount()
	return nil
}

func (s *stubStoreCreditRepo) Redeem(_ context.Context, _, _ string, amount shared.Money) error {
	return nil
}

func (s *stubStoreCreditRepo) ListLedger(_ context.Context, _, _ string, _, _ int) ([]credit.Entry, error) {
	return s.entries, nil
}

// newStoreCreditAdminRouter mirrors the real wiring in cmd/api/wire_routes.go:
// Issue is gated by rbac.StoreCreditWrite (not CustomersWrite), kept distinct
// so a role granted customer-profile editing doesn't implicitly gain the
// ability to mint store credit.
func newStoreCreditAdminRouter(h *admin.StoreCreditAdminHandler) *http.ServeMux {
	requireCustomersRead := admin.RequirePermission(rbac.CustomersRead)
	requireStoreCreditWrite := admin.RequirePermission(rbac.StoreCreditWrite)
	withAdminContext := admin.AdminContextMiddleware()
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/customers/{customerId}/store-credit", withAdminContext(requireCustomersRead(h.Get())))
	mux.Handle("POST /api/v1/admin/customers/{customerId}/store-credit/issue", withAdminContext(requireStoreCreditWrite(h.Issue())))
	return mux
}

func adminCustomerRepoFor(id string) *mockAdminCustomerRepo {
	return &mockAdminCustomerRepo{
		findByIDFn: func(_ context.Context, gotID string) (*customer.Customer, error) {
			if gotID != id {
				return nil, nil
			}
			c, err := customer.NewCustomer(id, "cust@example.com")
			if err != nil {
				return nil, err
			}
			return &c, nil
		},
	}
}

func issueRequest(t *testing.T, body map[string]interface{}) *http.Request {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/customers/cust-1/store-credit/issue", bytes.NewReader(raw))
	req.SetPathValue("customerId", "cust-1")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Store-ID", "store-1")
	req.Header.Set("X-Admin-Currency", "EUR")
	return req
}

func TestStoreCreditAdminHandler_Issue(t *testing.T) {
	repo := &stubStoreCreditRepo{}
	customers := &mockAdminCustomerRepo{
		findByIDFn: func(_ context.Context, id string) (*customer.Customer, error) {
			if id != "cust-1" {
				return nil, nil
			}
			c, err := customer.NewCustomer("cust-1", "cust@example.com")
			if err != nil {
				return nil, err
			}
			return &c, nil
		},
	}
	svc := storecreditApp.NewService(repo, customers)
	h := admin.NewStoreCreditAdminHandler(svc, adminapp.NewAuditor(logger.New("error")))
	mux := newStoreCreditAdminRouter(h)

	body, _ := json.Marshal(map[string]interface{}{
		"amount": 2500,
		"note":   "goodwill",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/customers/cust-1/store-credit/issue", bytes.NewReader(body))
	req.SetPathValue("customerId", "cust-1")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Store-ID", "store-1")
	req.Header.Set("X-Admin-Currency", "EUR")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	balance := resp.Data["balance"].(map[string]interface{})
	if balance["amount"].(float64) != 2500 {
		t.Errorf("balance amount = %v, want 2500", balance["amount"])
	}
	if len(repo.issued) != 1 || repo.issued[0].Amount() != 2500 {
		t.Errorf("issued = %+v, want 2500", repo.issued)
	}
}

func TestStoreCreditAdminHandler_Get(t *testing.T) {
	repo := &stubStoreCreditRepo{
		balance: 1800,
		entries: []credit.Entry{
			{
				ID:         "entry-1",
				CustomerID: "cust-1",
				Currency:   "EUR",
				Amount:     shared.MustNewMoney(500, "EUR"),
				Kind:       credit.KindIssue,
				Note:       "goodwill",
			},
		},
	}
	svc := storecreditApp.NewService(repo, &mockAdminCustomerRepo{})
	h := admin.NewStoreCreditAdminHandler(svc, adminapp.NewAuditor(logger.New("error")))
	mux := newStoreCreditAdminRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/customers/cust-1/store-credit?offset=0&limit=10", nil)
	req.SetPathValue("customerId", "cust-1")
	req.Header.Set("X-Admin-Store-ID", "store-1")
	req.Header.Set("X-Admin-Currency", "EUR")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	balance := resp.Data["balance"].(map[string]interface{})
	if balance["amount"].(float64) != 1800 {
		t.Errorf("balance amount = %v, want 1800", balance["amount"])
	}
	if balance["currency"] != "EUR" {
		t.Errorf("balance currency = %v, want EUR", balance["currency"])
	}
	ledger, ok := resp.Data["ledger"].([]interface{})
	if !ok || len(ledger) != 1 {
		t.Fatalf("ledger = %#v, want one entry", resp.Data["ledger"])
	}
	entry := ledger[0].(map[string]interface{})
	if entry["id"] != "entry-1" {
		t.Errorf("ledger id = %v, want entry-1", entry["id"])
	}
	if entry["kind"] != "issue" {
		t.Errorf("ledger kind = %v, want issue", entry["kind"])
	}
	if entry["amount"].(float64) != 500 {
		t.Errorf("ledger amount = %v, want 500", entry["amount"])
	}
	if entry["currency"] != "EUR" {
		t.Errorf("ledger currency = %v, want EUR", entry["currency"])
	}
	if entry["note"] != "goodwill" {
		t.Errorf("ledger note = %v, want goodwill", entry["note"])
	}
}

func TestStoreCreditAdminHandler_Issue_Forbidden(t *testing.T) {
	repo := &stubStoreCreditRepo{}
	svc := storecreditApp.NewService(repo, adminCustomerRepoFor("cust-1"))
	h := admin.NewStoreCreditAdminHandler(svc, adminapp.NewAuditor(logger.New("error")))
	mux := newStoreCreditAdminRouter(h)

	// Support has neither customers.write nor customers.store_credit.write.
	req := issueRequest(t, map[string]interface{}{"amount": 2500})
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if len(repo.issued) != 0 {
		t.Errorf("issued = %+v, want none — store credit must not be minted without permission", repo.issued)
	}
}

func TestStoreCreditAdminHandler_Issue_NegativeAmount(t *testing.T) {
	repo := &stubStoreCreditRepo{}
	svc := storecreditApp.NewService(repo, adminCustomerRepoFor("cust-1"))
	h := admin.NewStoreCreditAdminHandler(svc, adminapp.NewAuditor(logger.New("error")))
	mux := newStoreCreditAdminRouter(h)

	req := issueRequest(t, map[string]interface{}{"amount": -100})
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if len(repo.issued) != 0 {
		t.Errorf("issued = %+v, want none", repo.issued)
	}
}

func TestStoreCreditAdminHandler_Issue_CustomerNotFound(t *testing.T) {
	repo := &stubStoreCreditRepo{}
	svc := storecreditApp.NewService(repo, &mockAdminCustomerRepo{}) // FindByID returns nil, nil
	h := admin.NewStoreCreditAdminHandler(svc, adminapp.NewAuditor(logger.New("error")))
	mux := newStoreCreditAdminRouter(h)

	req := issueRequest(t, map[string]interface{}{"amount": 2500})
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// TestStoreCreditAdminHandler_Issue_DomainValidationErrorNotInternal pins the
// fix mapping storecredit.Service.Issue validation failures to 422, not a
// blanket 500 — JSONError must unwrap the apperror code through the
// "storecredit: issue: %w" wrapping instead of the handler forcing Internal
// for everything except NotFound.
func TestStoreCreditAdminHandler_Issue_DomainValidationErrorNotInternal(t *testing.T) {
	repo := &stubStoreCreditRepo{
		issueFn: func(context.Context, string, shared.Money, string) error {
			return apperror.Validation("store credit: amount exceeds issuance limit")
		},
	}
	svc := storecreditApp.NewService(repo, adminCustomerRepoFor("cust-1"))
	h := admin.NewStoreCreditAdminHandler(svc, adminapp.NewAuditor(logger.New("error")))
	mux := newStoreCreditAdminRouter(h)

	req := issueRequest(t, map[string]interface{}{"amount": 2500})
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d (validation, not internal); body=%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

// TestStoreCreditAdminHandler_Issue_IdempotencyKeyPreventsDoubleIssue pins the
// fix for a retried POST minting credit twice: the same Idempotency-Key
// header on two requests must only credit the account once.
func TestStoreCreditAdminHandler_Issue_IdempotencyKeyPreventsDoubleIssue(t *testing.T) {
	repo := &stubStoreCreditRepo{}
	svc := storecreditApp.NewService(repo, adminCustomerRepoFor("cust-1"))
	h := admin.NewStoreCreditAdminHandler(svc, adminapp.NewAuditor(logger.New("error")))
	mux := newStoreCreditAdminRouter(h)

	for i := 0; i < 2; i++ {
		req := issueRequest(t, map[string]interface{}{"amount": 2500})
		req.Header.Set("Idempotency-Key", "retry-key-1")
		req = testhelper.AdminRequest(req, "admin-1")

		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("attempt %d: status = %d, want %d; body=%s", i, rec.Code, http.StatusCreated, rec.Body.String())
		}
	}

	if len(repo.issued) != 1 {
		t.Fatalf("issued = %+v, want exactly 1 credit despite 2 requests with the same idempotency key", repo.issued)
	}
	if repo.balance != 2500 {
		t.Fatalf("balance = %d, want 2500 (not double-credited)", repo.balance)
	}
}

// TestStoreCreditAdminHandler_Issue_DifferentIdempotencyKeysBothIssue confirms
// the fix above only dedupes matching keys — two distinct legitimate
// issuances (different keys) must both go through.
func TestStoreCreditAdminHandler_Issue_DifferentIdempotencyKeysBothIssue(t *testing.T) {
	repo := &stubStoreCreditRepo{}
	svc := storecreditApp.NewService(repo, adminCustomerRepoFor("cust-1"))
	h := admin.NewStoreCreditAdminHandler(svc, adminapp.NewAuditor(logger.New("error")))
	mux := newStoreCreditAdminRouter(h)

	for _, key := range []string{"key-a", "key-b"} {
		req := issueRequest(t, map[string]interface{}{"amount": 1000})
		req.Header.Set("Idempotency-Key", key)
		req = testhelper.AdminRequest(req, "admin-1")

		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("key %q: status = %d, want %d; body=%s", key, rec.Code, http.StatusCreated, rec.Body.String())
		}
	}

	if len(repo.issued) != 2 {
		t.Fatalf("issued = %+v, want 2 (distinct keys must not be deduped against each other)", repo.issued)
	}
}

// TestStoreCreditAdminHandler_Issue_ExceedsMaxAmount pins the fix for
// unbounded single-issue amounts: a service configured with a max must
// reject an amount above it instead of minting an arbitrary amount.
func TestStoreCreditAdminHandler_Issue_ExceedsMaxAmount(t *testing.T) {
	repo := &stubStoreCreditRepo{}
	svc := storecreditApp.NewService(repo, adminCustomerRepoFor("cust-1")).WithMaxIssueAmount(1000)
	h := admin.NewStoreCreditAdminHandler(svc, adminapp.NewAuditor(logger.New("error")))
	mux := newStoreCreditAdminRouter(h)

	req := issueRequest(t, map[string]interface{}{"amount": 1001})
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if len(repo.issued) != 0 {
		t.Errorf("issued = %+v, want none — amount exceeds configured max", repo.issued)
	}
}

// TestStoreCreditAdminHandler_Issue_AtMaxAmountAllowed confirms the max-amount
// check is inclusive: exactly the configured max must still succeed.
func TestStoreCreditAdminHandler_Issue_AtMaxAmountAllowed(t *testing.T) {
	repo := &stubStoreCreditRepo{}
	svc := storecreditApp.NewService(repo, adminCustomerRepoFor("cust-1")).WithMaxIssueAmount(1000)
	h := admin.NewStoreCreditAdminHandler(svc, adminapp.NewAuditor(logger.New("error")))
	mux := newStoreCreditAdminRouter(h)

	req := issueRequest(t, map[string]interface{}{"amount": 1000})
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

// TestStoreCreditAdminHandler_Issue_OversizedIdempotencyKeyRejected pins a
// clear 422 for an Idempotency-Key over store_credit_ledger.idempotency_key's
// VARCHAR(255) bound (migration 064), instead of letting it reach the DB
// and surface as a generic constraint-violation 500.
func TestStoreCreditAdminHandler_Issue_OversizedIdempotencyKeyRejected(t *testing.T) {
	repo := &stubStoreCreditRepo{}
	svc := storecreditApp.NewService(repo, adminCustomerRepoFor("cust-1"))
	h := admin.NewStoreCreditAdminHandler(svc, adminapp.NewAuditor(logger.New("error")))
	mux := newStoreCreditAdminRouter(h)

	req := issueRequest(t, map[string]interface{}{"amount": 1000})
	req.Header.Set("Idempotency-Key", strings.Repeat("a", 256))
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if len(repo.issued) != 0 {
		t.Errorf("issued = %+v, want none", repo.issued)
	}
}
