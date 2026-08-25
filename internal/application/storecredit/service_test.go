package storecredit_test

import (
	"context"
	"testing"

	storecreditApp "github.com/akarso/shopanda/internal/application/storecredit"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/shared"
	credit "github.com/akarso/shopanda/internal/domain/storecredit"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

type fakeCustomerRepo struct {
	found *customer.Customer
}

func (f *fakeCustomerRepo) FindByID(_ context.Context, _ string) (*customer.Customer, error) {
	return f.found, nil
}
func (f *fakeCustomerRepo) FindByEmail(_ context.Context, _ string) (*customer.Customer, error) {
	return nil, nil
}
func (f *fakeCustomerRepo) Create(_ context.Context, _ *customer.Customer) error { return nil }
func (f *fakeCustomerRepo) Update(_ context.Context, _ *customer.Customer) error { return nil }
func (f *fakeCustomerRepo) ListCustomers(_ context.Context, _, _ int) ([]customer.Customer, error) {
	return nil, nil
}
func (f *fakeCustomerRepo) BumpTokenGeneration(_ context.Context, _ string) error { return nil }
func (f *fakeCustomerRepo) ChangePasswordAndBumpTokenGeneration(_ context.Context, _, _ string) error {
	return nil
}
func (f *fakeCustomerRepo) Delete(_ context.Context, _ string) error { return nil }

type fakeCreditRepo struct {
	issued []struct {
		amount shared.Money
		key    string
	}
}

func (f *fakeCreditRepo) GetBalance(_ context.Context, _, currency string) (shared.Money, error) {
	return shared.Zero(currency)
}
func (f *fakeCreditRepo) Issue(_ context.Context, _ string, amount shared.Money, _, idempotencyKey string) error {
	f.issued = append(f.issued, struct {
		amount shared.Money
		key    string
	}{amount, idempotencyKey})
	return nil
}
func (f *fakeCreditRepo) Redeem(_ context.Context, _, _ string, _ shared.Money) error { return nil }
func (f *fakeCreditRepo) ListLedger(_ context.Context, _, _ string, _, _ int) ([]credit.Entry, error) {
	return nil, nil
}

func newTestCustomer(t *testing.T) *customer.Customer {
	t.Helper()
	c, err := customer.NewCustomer(id.New(), "cust@example.com")
	if err != nil {
		t.Fatalf("NewCustomer: %v", err)
	}
	return &c
}

func TestService_Issue_PassesIdempotencyKeyThrough(t *testing.T) {
	repo := &fakeCreditRepo{}
	svc := storecreditApp.NewService(repo, &fakeCustomerRepo{found: newTestCustomer(t)})
	amount, _ := shared.NewMoney(500, "EUR")

	if err := svc.Issue(context.Background(), "cust-1", amount, "note", "key-123"); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if len(repo.issued) != 1 || repo.issued[0].key != "key-123" {
		t.Fatalf("issued = %+v, want key-123 passed through", repo.issued)
	}
}

// TestService_Issue_DefaultCapAppliesWithoutWithMaxIssueAmount pins the
// fix for a money-minting cap that used to default to unbounded until
// someone remembered to call WithMaxIssueAmount: unlike WithMetrics/
// WithTracer (where "never called" safely means "not observed"), a
// Service built directly — without going through cmd/api's wiring, which
// always calls WithMaxIssueAmount anyway — must not silently allow an
// arbitrary amount just because nothing configured a cap explicitly.
func TestService_Issue_DefaultCapAppliesWithoutWithMaxIssueAmount(t *testing.T) {
	repo := &fakeCreditRepo{}
	svc := storecreditApp.NewService(repo, &fakeCustomerRepo{found: newTestCustomer(t)})
	amount, _ := shared.NewMoney(1_000_000, "EUR") // above the 100000 default

	err := svc.Issue(context.Background(), "cust-1", amount, "note", "")
	if err == nil {
		t.Fatal("expected the default cap to reject an amount above it")
	}
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("err = %v, want a validation apperror", err)
	}
}

// TestService_Issue_ExplicitMaxOverridesDefault confirms
// WithMaxIssueAmount still overrides the default in both directions: a
// higher explicit max allows an amount the default would have rejected.
func TestService_Issue_ExplicitMaxOverridesDefault(t *testing.T) {
	repo := &fakeCreditRepo{}
	svc := storecreditApp.NewService(repo, &fakeCustomerRepo{found: newTestCustomer(t)}).WithMaxIssueAmount(1_000_000)
	amount, _ := shared.NewMoney(1_000_000, "EUR")

	if err := svc.Issue(context.Background(), "cust-1", amount, "note", ""); err != nil {
		t.Fatalf("Issue: %v, want the explicit higher max to allow this amount", err)
	}
}

// TestService_Issue_NegativeMaxIssueAmountIgnored is a defensive check
// mirroring config.validateStoreCredit's rejection of a negative
// max_issue_amount at config-load time — belt-and-suspenders for a
// Service built by hand (e.g. bypassing config.Load) rather than through
// cmd/api's normal wiring.
func TestService_Issue_NegativeMaxIssueAmountIgnored(t *testing.T) {
	repo := &fakeCreditRepo{}
	svc := storecreditApp.NewService(repo, &fakeCustomerRepo{found: newTestCustomer(t)}).WithMaxIssueAmount(-1)
	amount, _ := shared.NewMoney(1_000_000, "EUR") // above the default cap this should have left in place

	err := svc.Issue(context.Background(), "cust-1", amount, "note", "")
	if err == nil {
		t.Fatal("expected the default cap to still apply — a negative WithMaxIssueAmount call must be ignored, not treated as unbounded")
	}
}

func TestService_Issue_RejectsAboveMax(t *testing.T) {
	repo := &fakeCreditRepo{}
	svc := storecreditApp.NewService(repo, &fakeCustomerRepo{found: newTestCustomer(t)}).WithMaxIssueAmount(1000)
	amount, _ := shared.NewMoney(1001, "EUR")

	err := svc.Issue(context.Background(), "cust-1", amount, "note", "")
	if err == nil {
		t.Fatal("expected error for amount above configured max")
	}
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("err = %v, want a validation apperror", err)
	}
	if len(repo.issued) != 0 {
		t.Fatalf("issued = %+v, want none — cap must reject before touching the repo", repo.issued)
	}
}

func TestService_Issue_AllowsExactlyMax(t *testing.T) {
	repo := &fakeCreditRepo{}
	svc := storecreditApp.NewService(repo, &fakeCustomerRepo{found: newTestCustomer(t)}).WithMaxIssueAmount(1000)
	amount, _ := shared.NewMoney(1000, "EUR")

	if err := svc.Issue(context.Background(), "cust-1", amount, "note", ""); err != nil {
		t.Fatalf("Issue at exactly max: %v, want success (cap is inclusive)", err)
	}
}

func TestService_Issue_ZeroMaxDisablesCap(t *testing.T) {
	repo := &fakeCreditRepo{}
	svc := storecreditApp.NewService(repo, &fakeCustomerRepo{found: newTestCustomer(t)}).WithMaxIssueAmount(0)
	amount, _ := shared.NewMoney(1_000_000, "EUR")

	if err := svc.Issue(context.Background(), "cust-1", amount, "note", ""); err != nil {
		t.Fatalf("Issue: %v, want zero max to mean unbounded (explicit opt-out)", err)
	}
}
