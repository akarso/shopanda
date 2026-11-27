package notification_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/application/hooks"
	"github.com/akarso/shopanda/internal/application/notification"
	"github.com/akarso/shopanda/internal/domain/customer"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	domainInvoice "github.com/akarso/shopanda/internal/domain/invoice"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/pkg/extapi"
)

// --- mocks ---

type mockCustomerRepo struct {
	customer.CustomerRepository
	findByID func(ctx context.Context, id string) (*customer.Customer, error)
}

func (m *mockCustomerRepo) FindByID(ctx context.Context, id string) (*customer.Customer, error) {
	return m.findByID(ctx, id)
}

type mockOrderRepo struct {
	order.OrderRepository
	findByID func(ctx context.Context, id string) (*order.Order, error)
}

func (m *mockOrderRepo) FindByID(ctx context.Context, id string) (*order.Order, error) {
	return m.findByID(ctx, id)
}

type mockQueue struct {
	jobs.Queue
	enqueued  []jobs.Job
	byID      map[string]struct{}
	errOnce   error
	ignoreID  bool // when true, do not conflict on duplicate job ids
	enqueueFn func(ctx context.Context, job jobs.Job) error
	details   map[string]*jobs.Detail
	replaced  []jobs.Job
}

func (m *mockQueue) Get(_ context.Context, id string) (*jobs.Detail, error) {
	if m.details == nil {
		return nil, nil
	}
	return m.details[id], nil
}

func (m *mockQueue) Requeue(_ context.Context, id string, _ int, _ ...jobs.RequeueOption) error {
	return nil
}

func (m *mockQueue) ReplaceFailed(_ context.Context, job jobs.Job) error {
	m.replaced = append(m.replaced, job)
	if m.details != nil {
		m.details[job.ID] = &jobs.Detail{Summary: jobs.Summary{ID: job.ID, Status: jobs.StatusPending, Attempts: 0}}
	}
	return nil
}

func (m *mockQueue) Enqueue(ctx context.Context, job jobs.Job) error {
	if m.enqueueFn != nil {
		return m.enqueueFn(ctx, job)
	}
	if m.errOnce != nil {
		err := m.errOnce
		m.errOnce = nil
		return err
	}
	if m.byID == nil {
		m.byID = map[string]struct{}{}
	}
	if !m.ignoreID {
		if _, ok := m.byID[job.ID]; ok {
			return apperror.Conflict("job already enqueued")
		}
	}
	m.byID[job.ID] = struct{}{}
	m.enqueued = append(m.enqueued, job)
	return nil
}

type mockDedupe struct {
	keys              map[string]struct{}
	failDeleteIfDone  bool
	lastDeleteCtxDone bool
}

func (m *mockDedupe) key(subject, kind string) string { return subject + "|" + kind }

func (m *mockDedupe) TryInsert(_ context.Context, subjectKey, kind string) (bool, error) {
	if m.keys == nil {
		m.keys = map[string]struct{}{}
	}
	k := m.key(subjectKey, kind)
	if _, ok := m.keys[k]; ok {
		return false, nil
	}
	m.keys[k] = struct{}{}
	return true, nil
}

func (m *mockDedupe) MarkCompleted(_ context.Context, subjectKey, kind string) error {
	// Successful claim stays in the map (permanent for this mock).
	if m.keys == nil {
		m.keys = map[string]struct{}{}
	}
	m.keys[m.key(subjectKey, kind)] = struct{}{}
	return nil
}

func (m *mockDedupe) Delete(ctx context.Context, subjectKey, kind string) error {
	m.lastDeleteCtxDone = ctx.Err() != nil
	if m.failDeleteIfDone && ctx.Err() != nil {
		return ctx.Err()
	}
	if m.keys != nil {
		delete(m.keys, m.key(subjectKey, kind))
	}
	return nil
}

type mockInvoiceRepo struct {
	domainInvoice.InvoiceRepository
	findByID func(ctx context.Context, id string) (*domainInvoice.Invoice, error)
}

func (m *mockInvoiceRepo) FindByID(ctx context.Context, id string) (*domainInvoice.Invoice, error) {
	if m.findByID != nil {
		return m.findByID(ctx, id)
	}
	return nil, nil
}

type mockPDFRenderer struct {
	render func(inv domainInvoice.Invoice) ([]byte, error)
}

func (m *mockPDFRenderer) Render(inv domainInvoice.Invoice, _ domainInvoice.SellerBlock) ([]byte, error) {
	if m.render != nil {
		return m.render(inv)
	}
	return []byte("%PDF-mock"), nil
}

type mockMailer struct {
	sent []mail.Message
}

func (m *mockMailer) Send(_ context.Context, msg mail.Message) error {
	m.sent = append(m.sent, msg)
	return nil
}

type mockLogger struct{}

func (mockLogger) Info(string, map[string]interface{})         {}
func (mockLogger) Warn(string, map[string]interface{})         {}
func (mockLogger) Error(string, error, map[string]interface{}) {}

// newTestService creates a Service with test dependencies.
func newTestService(t *testing.T, tmpl *mail.Templates, custRepo *mockCustomerRepo, ordRepo *mockOrderRepo, q *mockQueue, opts ...notification.Option) *notification.Service {
	t.Helper()
	return notification.New(tmpl, custRepo, ordRepo, q, mockLogger{}, opts...)
}

// --- HandleOrderPaid tests ---

func TestHandleOrderPaid(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			if id == "cust-1" {
				c, _ := customer.NewCustomer("cust-1", "alice@example.com")
				c.FirstName = "Alice"
				return &c, nil
			}
			return nil, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			if id == "ord-1" {
				return &order.Order{ID: "ord-1", CustomerID: "cust-1"}, nil
			}
			return nil, nil
		},
	}

	// ignoreID: prove notification_dedupe (not job-id collision) suppresses the second send.
	q.ignoreID = true
	dedupe := &mockDedupe{}
	svc := newTestService(t, tmpl, custRepo, ordRepo, q, notification.WithEmailDedupe(dedupe))
	evt := event.New(order.EventOrderPaid, "core.order", order.OrderStatusChangedData{
		OrderID:   "ord-1",
		OldStatus: "pending",
		NewStatus: "paid",
	})

	if err := svc.HandleOrderPaid(context.Background(), evt); err != nil {
		t.Fatalf("HandleOrderPaid: %v", err)
	}

	if len(q.enqueued) != 1 {
		t.Fatalf("expected 1 enqueued job, got %d", len(q.enqueued))
	}
	job := q.enqueued[0]
	if job.Type != notification.JobTypeEmailSend {
		t.Errorf("job type = %q, want %q", job.Type, notification.JobTypeEmailSend)
	}
	if to, _ := job.Payload["to"].(string); to != "alice@example.com" {
		t.Errorf("payload.to = %q, want alice@example.com", to)
	}
	if subj, _ := job.Payload["subject"].(string); subj == "" {
		t.Error("payload.subject is empty")
	}
	if body, _ := job.Payload["body"].(string); body == "" {
		t.Error("payload.body is empty")
	}

	// Republish order.paid (grant heal) must not enqueue a second confirmation.
	if err := svc.HandleOrderPaid(context.Background(), evt); err != nil {
		t.Fatalf("second HandleOrderPaid: %v", err)
	}
	if len(q.enqueued) != 1 {
		t.Fatalf("expected still 1 enqueued job after republish, got %d", len(q.enqueued))
	}
}

type stubLayout struct {
	wrapped bool
}

func (s *stubLayout) Wrap(body string, ed mail.EmailData) (string, error) {
	s.wrapped = true
	return "<layout>" + body + "</layout>:" + ed.StoreName, nil
}

func TestHandleOrderPaid_WrapsLayout(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	layout := &stubLayout{}
	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			c, _ := customer.NewCustomer("cust-1", "alice@example.com")
			return &c, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			return &order.Order{ID: "ord-1", CustomerID: "cust-1"}, nil
		},
	}
	svc := newTestService(t, tmpl, custRepo, ordRepo, q, notification.WithLayout(layout))
	evt := event.New(order.EventOrderPaid, "core.order", order.OrderStatusChangedData{
		OrderID: "ord-1", OldStatus: "pending", NewStatus: "paid",
	})
	if err := svc.HandleOrderPaid(context.Background(), evt); err != nil {
		t.Fatalf("HandleOrderPaid: %v", err)
	}
	if !layout.wrapped {
		t.Fatal("expected layout.Wrap")
	}
	body, _ := q.enqueued[0].Payload["body"].(string)
	if !strings.HasPrefix(body, "<layout>") || !strings.Contains(body, "</layout>") {
		t.Fatalf("body not wrapped: %q", body)
	}
}

type errLayout struct{ err error }

func (e errLayout) Wrap(string, mail.EmailData) (string, error) { return "", e.err }

func TestHandleOrderPaid_WrapErrorSkipsEnqueue(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			c, _ := customer.NewCustomer("cust-1", "alice@example.com")
			return &c, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			return &order.Order{ID: "ord-1", CustomerID: "cust-1"}, nil
		},
	}
	svc := newTestService(t, tmpl, custRepo, ordRepo, q, notification.WithLayout(errLayout{err: errors.New("wrap boom")}))
	evt := event.New(order.EventOrderPaid, "core.order", order.OrderStatusChangedData{
		OrderID: "ord-1", OldStatus: "pending", NewStatus: "paid",
	})
	if err := svc.HandleOrderPaid(context.Background(), evt); err == nil {
		t.Fatal("expected wrap error")
	}
	if len(q.enqueued) != 0 {
		t.Fatalf("wrap error must skip enqueue, got %d", len(q.enqueued))
	}
}

func TestHandleOrderPaid_TrimsTrailingSlashStoreURL(t *testing.T) {
	tmpl := mail.NewTemplates()
	tmpl.Register("order_confirmed", "Order {{.Data.OrderID}}",
		`<a href="{{.StoreURL}}/account/orders/{{.Data.OrderID}}">View</a>`)
	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			c, _ := customer.NewCustomer("cust-1", "alice@example.com")
			return &c, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			return &order.Order{ID: "ord-1", CustomerID: "cust-1"}, nil
		},
	}
	svc := newTestService(t, tmpl, custRepo, ordRepo, q,
		notification.WithStoreURL("https://shop.example/"),
	)
	evt := event.New(order.EventOrderPaid, "core.order", order.OrderStatusChangedData{
		OrderID: "ord-1", OldStatus: "pending", NewStatus: "paid",
	})
	if err := svc.HandleOrderPaid(context.Background(), evt); err != nil {
		t.Fatalf("HandleOrderPaid: %v", err)
	}
	body, _ := q.enqueued[0].Payload["body"].(string)
	want := "https://shop.example/account/orders/ord-1"
	if !strings.Contains(body, want) {
		t.Fatalf("body missing %q\n%s", want, body)
	}
	if strings.Contains(body, "https://shop.example//account") {
		t.Fatalf("double slash in StoreURL link: %s", body)
	}
}

func TestHandleOrderPaid_EnqueueFailureAllowsRetry(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	q := &mockQueue{errOnce: errors.New("queue down"), ignoreID: true}
	dedupe := &mockDedupe{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			if id == "cust-1" {
				c, _ := customer.NewCustomer("cust-1", "alice@example.com")
				c.FirstName = "Alice"
				return &c, nil
			}
			return nil, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			if id == "ord-1" {
				return &order.Order{ID: "ord-1", CustomerID: "cust-1"}, nil
			}
			return nil, nil
		},
	}

	svc := newTestService(t, tmpl, custRepo, ordRepo, q, notification.WithEmailDedupe(dedupe))
	evt := event.New(order.EventOrderPaid, "core.order", order.OrderStatusChangedData{
		OrderID:   "ord-1",
		OldStatus: "pending",
		NewStatus: "paid",
	})

	if err := svc.HandleOrderPaid(context.Background(), evt); err == nil {
		t.Fatal("expected enqueue failure")
	}
	if len(q.enqueued) != 0 {
		t.Fatalf("expected no enqueue after failure, got %d", len(q.enqueued))
	}

	// Claim released → retry can send.
	if err := svc.HandleOrderPaid(context.Background(), evt); err != nil {
		t.Fatalf("retry HandleOrderPaid: %v", err)
	}
	if len(q.enqueued) != 1 {
		t.Fatalf("expected 1 enqueued job after retry, got %d", len(q.enqueued))
	}
}

func TestHandleOrderPaid_EnqueueFailureCancelledCtxStillReleasesClaim(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	q := &mockQueue{ignoreID: true}
	q.enqueueFn = func(c context.Context, job jobs.Job) error {
		cancel()          // simulate shutdown cancelling the request ctx while Enqueue waits
		q.enqueueFn = nil // subsequent retries use normal enqueue
		return context.Canceled
	}
	dedupe := &mockDedupe{failDeleteIfDone: true}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			if id == "cust-1" {
				c, _ := customer.NewCustomer("cust-1", "alice@example.com")
				c.FirstName = "Alice"
				return &c, nil
			}
			return nil, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			if id == "ord-1" {
				return &order.Order{ID: "ord-1", CustomerID: "cust-1"}, nil
			}
			return nil, nil
		},
	}

	svc := newTestService(t, tmpl, custRepo, ordRepo, q, notification.WithEmailDedupe(dedupe))
	evt := event.New(order.EventOrderPaid, "core.order", order.OrderStatusChangedData{
		OrderID:   "ord-1",
		OldStatus: "pending",
		NewStatus: "paid",
	})

	if err := svc.HandleOrderPaid(ctx, evt); err == nil {
		t.Fatal("expected enqueue failure")
	}
	if dedupe.lastDeleteCtxDone {
		t.Fatal("dedupe Delete must use a live background ctx, not the cancelled request ctx")
	}
	if _, stuck := dedupe.keys["ord-1|"+notification.KindOrderConfirmed]; stuck {
		t.Fatal("claim must be released so a later order.paid can retry")
	}

	// Fresh request ctx can claim and enqueue.
	if err := svc.HandleOrderPaid(context.Background(), evt); err != nil {
		t.Fatalf("retry HandleOrderPaid: %v", err)
	}
	if len(q.enqueued) != 1 {
		t.Fatalf("expected 1 enqueued job after retry, got %d", len(q.enqueued))
	}
}

func TestHandleOrderPaid_GuestOrder(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			t.Fatalf("unexpected customer lookup for guest order: %q", id)
			return nil, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			if id == "ord-1" {
				return &order.Order{ID: "ord-1", CustomerID: "", ContactEmail: "guest@example.com"}, nil
			}
			return nil, nil
		},
	}

	svc := newTestService(t, tmpl, custRepo, ordRepo, q)
	evt := event.New(order.EventOrderPaid, "core.order", order.OrderStatusChangedData{
		OrderID:   "ord-1",
		OldStatus: "pending",
		NewStatus: "paid",
	})

	if err := svc.HandleOrderPaid(context.Background(), evt); err != nil {
		t.Fatalf("HandleOrderPaid guest: %v", err)
	}

	if len(q.enqueued) != 1 {
		t.Fatalf("expected 1 enqueued job, got %d", len(q.enqueued))
	}
	job := q.enqueued[0]
	if to, _ := job.Payload["to"].(string); to != "guest@example.com" {
		t.Errorf("payload.to = %q, want guest@example.com", to)
	}
	body, _ := job.Payload["body"].(string)
	if strings.Contains(body, "Thank you, !") {
		t.Errorf("guest body has broken empty-name salutation: %q", body)
	}
	if body == "" {
		t.Error("payload.body is empty")
	}
}

func TestHandleOrderPaid_GuestOrderMissingContactEmail(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	q := &mockQueue{}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, _ string) (*order.Order, error) {
			return &order.Order{ID: "ord-1", CustomerID: "", ContactEmail: ""}, nil
		},
	}

	svc := newTestService(t, tmpl, &mockCustomerRepo{}, ordRepo, q)
	evt := event.New(order.EventOrderPaid, "core.order", order.OrderStatusChangedData{OrderID: "ord-1"})

	if err := svc.HandleOrderPaid(context.Background(), evt); err == nil {
		t.Fatal("expected error for guest order without contact email")
	}
	if len(q.enqueued) != 0 {
		t.Fatalf("expected 0 enqueued jobs, got %d", len(q.enqueued))
	}
}

func TestHandleOrderPaid_OrderNotFound(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, _ string) (*customer.Customer, error) {
			return nil, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, _ string) (*order.Order, error) {
			return nil, nil
		},
	}

	svc := newTestService(t, tmpl, custRepo, ordRepo, q)
	evt := event.New(order.EventOrderPaid, "core.order", order.OrderStatusChangedData{
		OrderID: "missing",
	})

	err := svc.HandleOrderPaid(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for missing order")
	}
	if len(q.enqueued) != 0 {
		t.Fatalf("expected 0 enqueued jobs, got %d", len(q.enqueued))
	}
}

func TestHandleOrderPaid_CustomerNotFound(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, _ string) (*customer.Customer, error) {
			return nil, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			if id == "ord-1" {
				return &order.Order{ID: "ord-1", CustomerID: "cust-missing"}, nil
			}
			return nil, nil
		},
	}

	svc := newTestService(t, tmpl, custRepo, ordRepo, q)
	evt := event.New(order.EventOrderPaid, "core.order", order.OrderStatusChangedData{
		OrderID: "ord-1",
	})

	err := svc.HandleOrderPaid(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for missing customer")
	}
	if len(q.enqueued) != 0 {
		t.Fatalf("expected 0 enqueued jobs, got %d", len(q.enqueued))
	}
}

func TestHandleOrderPaid_IncludesLineExtensionsAndVariantID(t *testing.T) {
	tmpl := mail.NewTemplates()
	tmpl.Register("order_confirmed",
		"Order {{.Data.OrderID}} — Confirmation",
		`{{range .Data.Items}}{{.VariantID}}|{{.SKU}}|{{index .Extensions "acme.gift.message"}}|{{range .ExtensionsList}}{{.Label}}:{{.Value}}{{end}}{{end}}`,
	)

	item, err := order.NewItem("var-1", "SKU-1", "Widget", 1, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder("ord-1", "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}

	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code:        "acme.gift.message",
		Label:       "Gift",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetOrderItem,
		StorageMode: domainext.StorageSnapshot,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := reg.Register(domainext.FieldDef{
		Code:        "acme.secret",
		Label:       "Secret",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetOrderItem,
		StorageMode: domainext.StorageSnapshot,
		Visibility:  domainext.VisibilityPrivate,
	}); err != nil {
		t.Fatalf("register private: %v", err)
	}
	extRepo := &notifExtMemRepo{values: map[string]domainext.Value{}}
	msg := "Happy birthday"
	secret := "nope"
	target := domainext.OrderItemTarget("ord-1", "var-1")
	extRepo.values[string(target.Type)+":"+target.ID+":acme.gift.message"] = domainext.Value{
		FieldCode:  "acme.gift.message",
		TargetType: target.Type,
		TargetID:   target.ID,
		Payload:    domainext.ValuePayload{StringValue: &msg},
	}
	extRepo.values[string(target.Type)+":"+target.ID+":acme.secret"] = domainext.Value{
		FieldCode:  "acme.secret",
		TargetType: target.Type,
		TargetID:   target.ID,
		Payload:    domainext.ValuePayload{StringValue: &secret},
	}
	values := extensionapp.NewValueService(reg, extRepo)

	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			if id == "cust-1" {
				c, _ := customer.NewCustomer("cust-1", "alice@example.com")
				c.FirstName = "Alice"
				return &c, nil
			}
			return nil, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			if id == "ord-1" {
				return &o, nil
			}
			return nil, nil
		},
	}
	svc := newTestService(t, tmpl, custRepo, ordRepo, q, notification.WithOrderExtensions(values))
	evt := event.New(order.EventOrderPaid, "core.order", order.OrderStatusChangedData{
		OrderID:   "ord-1",
		OldStatus: "pending",
		NewStatus: "paid",
	})
	if err := svc.HandleOrderPaid(context.Background(), evt); err != nil {
		t.Fatalf("HandleOrderPaid: %v", err)
	}
	if len(q.enqueued) != 1 {
		t.Fatalf("enqueued = %d, want 1", len(q.enqueued))
	}
	body, _ := q.enqueued[0].Payload["body"].(string)
	if !strings.Contains(body, "var-1|SKU-1|Happy birthday|Gift:Happy birthday") {
		t.Fatalf("body = %q, want variant/sku/extension + labeled list", body)
	}
	if strings.Contains(body, "nope") || strings.Contains(body, "acme.secret") {
		t.Fatalf("private extension leaked into email body: %q", body)
	}
}

func TestHandleOrderPaid_ExtensionListFailureStillEnqueues(t *testing.T) {
	tmpl := mail.NewTemplates()
	tmpl.Register("order_confirmed",
		"Order {{.Data.OrderID}} — Confirmation",
		`{{range .Data.Items}}{{.VariantID}}|{{.SKU}}|hasExt={{if .Extensions}}yes{{else}}no{{end}}{{end}}`,
	)
	item, err := order.NewItem("var-1", "SKU-1", "Widget", 1, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder("ord-1", "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	reg := extensionapp.NewRegistry()
	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			c, _ := customer.NewCustomer("cust-1", "alice@example.com")
			return &c, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			return &o, nil
		},
	}
	svc := newTestService(t, tmpl, custRepo, ordRepo, q, notification.WithOrderExtensions(boomOrderExtAPI{reg: reg}))
	evt := event.New(order.EventOrderPaid, "core.order", order.OrderStatusChangedData{
		OrderID: "ord-1", OldStatus: "pending", NewStatus: "paid",
	})
	if err := svc.HandleOrderPaid(context.Background(), evt); err != nil {
		t.Fatalf("HandleOrderPaid: %v", err)
	}
	if len(q.enqueued) != 1 {
		t.Fatalf("enqueued = %d, want 1 despite extension failure", len(q.enqueued))
	}
	body, _ := q.enqueued[0].Payload["body"].(string)
	if !strings.Contains(body, "var-1|SKU-1|hasExt=no") {
		t.Fatalf("body = %q, want core line without Extensions", body)
	}
}

func TestHandleOrderPaid_OrderLineDisplayEnrichImage(t *testing.T) {
	extensionapp.SetImageHostLookupForTest(t, func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})
	bodyHTML, err := os.ReadFile("../../../templates/emails/order_confirmed.html")
	if err != nil {
		t.Fatalf("read real email template: %v", err)
	}
	tmpl := mail.NewTemplates()
	tmpl.Register("order_confirmed",
		"Order {{.Data.OrderID}} — Confirmation",
		string(bodyHTML),
	)
	item, err := order.NewItem("var-1", "SKU-1", "Widget", 1, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder("ord-1", "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	o.Language = "de"
	hookReg := hooks.NewRegistry(nil)
	var sawLang string
	_ = hookReg.Register(hooks.HookOrderLineDisplay, 100, "metaldogs", func(ctx *hooks.Context) error {
		if raw, ok := ctx.Get("language"); ok {
			sawLang, _ = raw.(string)
		}
		rawExt, _ := ctx.Get("extensions")
		if ext, _ := rawExt.(map[string]interface{}); ext != nil {
			ext["acme.gift.message"] = "tampered-by-hook"
		}
		raw, _ := ctx.Get("fields")
		fields := raw.(*[]extapi.OrderLineDisplayField)
		*fields = append(*fields,
			extapi.OrderLineDisplayField{
				Code:  "plugin.thumb",
				Label: "Design",
				Kind:  extapi.DisplayKindImage,
				Value: "https://cdn.example/ord-1/var-1.png?x=1&y=2",
				Alt:   `preview "quoted"`,
			},
			extapi.OrderLineDisplayField{
				Code:  "plugin.http",
				Kind:  extapi.DisplayKindImage,
				Value: "http://cdn.example/nope.png",
			},
			extapi.OrderLineDisplayField{
				Code:  "plugin.note",
				Label: "Note",
				Kind:  extapi.DisplayKindText,
				Value: `say <b>hi</b> & "bye"`,
			},
		)
		return nil
	})
	_ = hookReg.Register(hooks.HookOrderLineDisplay, 50, "boom", func(ctx *hooks.Context) error {
		raw, _ := ctx.Get("fields")
		fields := raw.(*[]extapi.OrderLineDisplayField)
		*fields = append(*fields, extapi.OrderLineDisplayField{
			Code: "plugin.partial", Kind: extapi.DisplayKindText, Value: "should-drop",
		})
		return errors.New("enrich boom")
	})

	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code: "acme.gift.message", Label: "Gift", Type: domainext.FieldTypeString,
		Scope: domainext.TargetOrderItem, StorageMode: domainext.StorageSnapshot,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	extRepo := &notifExtMemRepo{values: map[string]domainext.Value{}}
	msg := "Happy birthday"
	target := domainext.OrderItemTarget("ord-1", "var-1")
	extRepo.values[string(target.Type)+":"+target.ID+":acme.gift.message"] = domainext.Value{
		FieldCode: "acme.gift.message", TargetType: target.Type, TargetID: target.ID,
		Payload: domainext.ValuePayload{StringValue: &msg},
	}
	values := extensionapp.NewValueService(reg, extRepo)

	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			c, _ := customer.NewCustomer("cust-1", "alice@example.com")
			return &c, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			return &o, nil
		},
	}
	svc := newTestService(t, tmpl, custRepo, ordRepo, q,
		notification.WithOrderExtensions(values),
		notification.WithOrderLineHooks(hookReg),
	)
	evt := event.New(order.EventOrderPaid, "core.order", order.OrderStatusChangedData{
		OrderID: "ord-1", OldStatus: "pending", NewStatus: "paid",
	})
	if err := svc.HandleOrderPaid(context.Background(), evt); err != nil {
		t.Fatalf("HandleOrderPaid: %v", err)
	}
	if sawLang != "de" {
		t.Fatalf("hook language = %q, want de from order.Language", sawLang)
	}
	if len(q.enqueued) != 1 {
		t.Fatalf("enqueued = %d, want 1", len(q.enqueued))
	}
	body, _ := q.enqueued[0].Payload["body"].(string)
	if !strings.Contains(body, `src="https://cdn.example/ord-1/var-1.png?x=1&amp;y=2"`) {
		t.Fatalf("body missing escaped https img: %q", body)
	}
	if !strings.Contains(body, `alt="preview &#34;quoted&#34;"`) && !strings.Contains(body, `alt="preview &#34;quoted&#34;`) {
		// html/template may encode quotes as &#34;
		if !strings.Contains(body, "preview") || !strings.Contains(body, "alt=") {
			t.Fatalf("body missing alt: %q", body)
		}
	}
	if strings.Contains(body, "http://cdn.example/nope.png") || strings.Contains(body, "should-drop") {
		t.Fatalf("http/partial must be absent: %q", body)
	}
	if strings.Contains(body, "tampered-by-hook") {
		t.Fatalf("hook must not mutate stored Extensions map: %q", body)
	}
	if !strings.Contains(body, "Gift: Happy birthday") {
		t.Fatalf("stored text row missing: %q", body)
	}
	if !strings.Contains(body, "say &lt;b&gt;hi&lt;/b&gt;") {
		t.Fatalf("text with < must be escaped: %q", body)
	}
}

func TestHandleOrderPaid_OrderLineDisplayLanguageFallsBackToStatic(t *testing.T) {
	tmpl := mail.NewTemplates()
	tmpl.Register("order_confirmed", "Order {{.Data.OrderID}}", "ok")
	item, err := order.NewItem("var-1", "SKU-1", "Widget", 1, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder("ord-legacy", "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	// Language unset — legacy row; enrich must use static fallback.
	var sawLang string
	hookReg := hooks.NewRegistry(nil)
	_ = hookReg.Register(hooks.HookOrderLineDisplay, 100, "metaldogs", func(ctx *hooks.Context) error {
		if raw, ok := ctx.Get("language"); ok {
			sawLang, _ = raw.(string)
		}
		return nil
	})
	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			c, _ := customer.NewCustomer("cust-1", "alice@example.com")
			return &c, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			return &o, nil
		},
	}
	svc := newTestService(t, tmpl, custRepo, ordRepo, q,
		notification.WithOrderLineHooks(hookReg),
		notification.WithFallbackLanguage("fr"),
	)
	evt := event.New(order.EventOrderPaid, "core.order", order.OrderStatusChangedData{
		OrderID: "ord-legacy", OldStatus: "pending", NewStatus: "paid",
	})
	if err := svc.HandleOrderPaid(context.Background(), evt); err != nil {
		t.Fatalf("HandleOrderPaid: %v", err)
	}
	if sawLang != "fr" {
		t.Fatalf("hook language = %q, want fr static fallback", sawLang)
	}
}

func TestHandleOrderPaid_OrderLineDisplayLanguageLookupEmptyOrder(t *testing.T) {
	tmpl := mail.NewTemplates()
	tmpl.Register("order_confirmed", "Order {{.Data.OrderID}}", "ok")
	item1, err := order.NewItem("var-1", "SKU-1", "Widget", 1, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	item2, err := order.NewItem("var-2", "SKU-2", "Gadget", 1, shared.MustNewMoney(2000, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder("ord-legacy", "cust-1", "", "EUR", []order.Item{item1, item2})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	var sawLang string
	var lookupCalls int
	hookReg := hooks.NewRegistry(nil)
	_ = hookReg.Register(hooks.HookOrderLineDisplay, 100, "metaldogs", func(ctx *hooks.Context) error {
		if raw, ok := ctx.Get("language"); ok {
			sawLang, _ = raw.(string)
		}
		return nil
	})
	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			c, _ := customer.NewCustomer("cust-1", "alice@example.com")
			return &c, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			return &o, nil
		},
	}
	svc := newTestService(t, tmpl, custRepo, ordRepo, q,
		notification.WithOrderLineHooks(hookReg),
		notification.WithStoreLanguageLookup(func(context.Context) (string, error) {
			lookupCalls++
			return "pl", nil
		}),
	)
	evt := event.New(order.EventOrderPaid, "core.order", order.OrderStatusChangedData{
		OrderID: "ord-legacy", OldStatus: "pending", NewStatus: "paid",
	})
	if err := svc.HandleOrderPaid(context.Background(), evt); err != nil {
		t.Fatalf("HandleOrderPaid: %v", err)
	}
	if sawLang != "pl" {
		t.Fatalf("hook language = %q, want pl from store lookup", sawLang)
	}
	if lookupCalls != 1 {
		t.Fatalf("store lookup calls = %d, want 1 (once per email, not per line)", lookupCalls)
	}
}

func TestHandleOrderPaid_OrderLineDisplayLanguageOrderBeatsLookup(t *testing.T) {
	tmpl := mail.NewTemplates()
	tmpl.Register("order_confirmed", "Order {{.Data.OrderID}}", "ok")
	item, err := order.NewItem("var-1", "SKU-1", "Widget", 1, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder("ord-de", "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	o.Language = "de"
	var sawLang string
	var lookupCalls int
	hookReg := hooks.NewRegistry(nil)
	_ = hookReg.Register(hooks.HookOrderLineDisplay, 100, "metaldogs", func(ctx *hooks.Context) error {
		if raw, ok := ctx.Get("language"); ok {
			sawLang, _ = raw.(string)
		}
		return nil
	})
	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			c, _ := customer.NewCustomer("cust-1", "alice@example.com")
			return &c, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			return &o, nil
		},
	}
	svc := newTestService(t, tmpl, custRepo, ordRepo, q,
		notification.WithOrderLineHooks(hookReg),
		notification.WithStoreLanguageLookup(func(context.Context) (string, error) {
			lookupCalls++
			return "fr", nil
		}),
	)
	evt := event.New(order.EventOrderPaid, "core.order", order.OrderStatusChangedData{
		OrderID: "ord-de", OldStatus: "pending", NewStatus: "paid",
	})
	if err := svc.HandleOrderPaid(context.Background(), evt); err != nil {
		t.Fatalf("HandleOrderPaid: %v", err)
	}
	if sawLang != "de" {
		t.Fatalf("hook language = %q, want de (order beats lookup fr)", sawLang)
	}
	if lookupCalls != 0 {
		t.Fatalf("store lookup calls = %d, want 0 when order language is set", lookupCalls)
	}
}

func TestHandleOrderPaid_OrderLineDisplayLanguageLookupErrorUsesStatic(t *testing.T) {
	tmpl := mail.NewTemplates()
	tmpl.Register("order_confirmed", "Order {{.Data.OrderID}}", "ok")
	item, err := order.NewItem("var-1", "SKU-1", "Widget", 1, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder("ord-legacy", "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	var sawLang string
	hookReg := hooks.NewRegistry(nil)
	_ = hookReg.Register(hooks.HookOrderLineDisplay, 100, "metaldogs", func(ctx *hooks.Context) error {
		if raw, ok := ctx.Get("language"); ok {
			sawLang, _ = raw.(string)
		}
		return nil
	})
	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			c, _ := customer.NewCustomer("cust-1", "alice@example.com")
			return &c, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			return &o, nil
		},
	}
	svc := newTestService(t, tmpl, custRepo, ordRepo, q,
		notification.WithOrderLineHooks(hookReg),
		notification.WithFallbackLanguage("fr"),
		notification.WithStoreLanguageLookup(func(context.Context) (string, error) {
			return "", errors.New("store down")
		}),
	)
	evt := event.New(order.EventOrderPaid, "core.order", order.OrderStatusChangedData{
		OrderID: "ord-legacy", OldStatus: "pending", NewStatus: "paid",
	})
	if err := svc.HandleOrderPaid(context.Background(), evt); err != nil {
		t.Fatalf("HandleOrderPaid: %v", err)
	}
	if sawLang != "fr" {
		t.Fatalf("hook language = %q, want fr static fallback after lookup error", sawLang)
	}
}

type boomOrderExtAPI struct {
	reg *extensionapp.Registry
}

func (b boomOrderExtAPI) Registry() *extensionapp.Registry { return b.reg }
func (b boomOrderExtAPI) List(context.Context, domainext.Target, bool) ([]domainext.Value, error) {
	return nil, nil
}
func (b boomOrderExtAPI) ListByOrderItems(context.Context, string, []string, bool) (map[string][]domainext.Value, error) {
	return nil, errors.New("list boom")
}
func (b boomOrderExtAPI) UpsertBatch(context.Context, domainext.Target, []domainext.ValueInput, string, bool) ([]domainext.Value, error) {
	return nil, errors.New("unused")
}
func (b boomOrderExtAPI) Delete(context.Context, domainext.Target, string, bool) error {
	return errors.New("unused")
}

type notifExtMemRepo struct {
	values map[string]domainext.Value
}

func (m *notifExtMemRepo) ListByTarget(_ context.Context, target domainext.Target) ([]domainext.Value, error) {
	var out []domainext.Value
	for _, v := range m.values {
		if v.TargetType == target.Type && v.TargetID == target.ID {
			out = append(out, v)
		}
	}
	return out, nil
}
func (m *notifExtMemRepo) ListByTargets(_ context.Context, targetType domainext.TargetType, targetIDs []string) ([]domainext.Value, error) {
	want := map[string]struct{}{}
	for _, id := range targetIDs {
		want[id] = struct{}{}
	}
	var out []domainext.Value
	for _, v := range m.values {
		if v.TargetType == targetType {
			if _, ok := want[v.TargetID]; ok {
				out = append(out, v)
			}
		}
	}
	return out, nil
}
func (m *notifExtMemRepo) Upsert(_ context.Context, value domainext.Value) error {
	m.values[string(value.TargetType)+":"+value.TargetID+":"+value.FieldCode] = value
	return nil
}
func (m *notifExtMemRepo) UpsertBatch(ctx context.Context, values []domainext.Value) error {
	for _, v := range values {
		if err := m.Upsert(ctx, v); err != nil {
			return err
		}
	}
	return nil
}
func (m *notifExtMemRepo) UpsertBatchSelective(ctx context.Context, values []domainext.Value, _ map[string]struct{}) error {
	return m.UpsertBatch(ctx, values)
}
func (m *notifExtMemRepo) ListTargetIDsByTypeAndIDPrefix(_ context.Context, _ domainext.TargetType, _ string) ([]string, error) {
	return nil, nil
}
func (m *notifExtMemRepo) Delete(_ context.Context, target domainext.Target, fieldCode string) error {
	delete(m.values, string(target.Type)+":"+target.ID+":"+fieldCode)
	return nil
}

func TestHandleOrderPaid_BadEventData(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	q := &mockQueue{}
	custRepo := &mockCustomerRepo{}
	ordRepo := &mockOrderRepo{}

	svc := newTestService(t, tmpl, custRepo, ordRepo, q)
	evt := event.New(order.EventOrderPaid, "core.order", "not-a-struct")

	err := svc.HandleOrderPaid(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for bad event data")
	}
	if len(q.enqueued) != 0 {
		t.Fatalf("expected 0 enqueued jobs, got %d", len(q.enqueued))
	}
}

// --- HandlePasswordReset tests ---

func TestHandlePasswordReset(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			if id == "cust-1" {
				c, _ := customer.NewCustomer("cust-1", "alice@example.com")
				c.FirstName = "Alice"
				return &c, nil
			}
			return nil, nil
		},
	}
	ordRepo := &mockOrderRepo{}

	svc := notification.New(tmpl, custRepo, ordRepo, q, mockLogger{},
		notification.WithResetBaseURL("https://shop.test/reset"),
	)

	evt := event.New(customer.EventPasswordResetRequested, "auth.service", customer.PasswordResetRequestedData{
		CustomerID: "cust-1",
		Token:      "abc123",
	})

	if err := svc.HandlePasswordReset(context.Background(), evt); err != nil {
		t.Fatalf("HandlePasswordReset: %v", err)
	}

	if len(q.enqueued) != 1 {
		t.Fatalf("expected 1 enqueued job, got %d", len(q.enqueued))
	}
	job := q.enqueued[0]
	if to, _ := job.Payload["to"].(string); to != "alice@example.com" {
		t.Errorf("payload.to = %q, want alice@example.com", to)
	}
	if subj, _ := job.Payload["subject"].(string); subj != "Reset your password" {
		t.Errorf("payload.subject = %q, want 'Reset your password'", subj)
	}
	if body, _ := job.Payload["body"].(string); body == "" {
		t.Error("payload.body is empty")
	}
}

func TestHandleEmailVerification(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			if id == "cust-1" {
				c, _ := customer.NewCustomer("cust-1", "alice@example.com")
				c.FirstName = "Alice"
				return &c, nil
			}
			return nil, nil
		},
	}
	ordRepo := &mockOrderRepo{}

	svc := newTestService(t, tmpl, custRepo, ordRepo, q)
	evt := event.New(customer.EventEmailVerificationRequested, "auth.service", customer.EmailVerificationRequestedData{
		CustomerID: "cust-1",
		VerifyURL:  "https://shop.test/account/verify-email?email_token=abc",
	})

	if err := svc.HandleEmailVerification(context.Background(), evt); err != nil {
		t.Fatalf("HandleEmailVerification: %v", err)
	}
	if len(q.enqueued) != 1 {
		t.Fatalf("expected 1 enqueued job, got %d", len(q.enqueued))
	}
	job := q.enqueued[0]
	if to, _ := job.Payload["to"].(string); to != "alice@example.com" {
		t.Fatalf("payload.to = %q, want alice@example.com", to)
	}
	body, _ := job.Payload["body"].(string)
	match := regexp.MustCompile(`href="([^"]+)"`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("payload.body = %q, want linked verification URL", body)
	}
	verifyURL, err := url.Parse(match[1])
	if err != nil {
		t.Fatalf("Parse verify URL: %v", err)
	}
	if verifyURL.Scheme != "https" && verifyURL.Scheme != "http" {
		t.Fatalf("verify URL scheme = %q, want http/https", verifyURL.Scheme)
	}
	if verifyURL.Host == "" {
		t.Fatalf("verify URL host is empty: %q", verifyURL.String())
	}
	if verifyURL.Path != "/account/verify-email" {
		t.Fatalf("verify URL path = %q, want /account/verify-email", verifyURL.Path)
	}
	if verifyURL.Query().Get("email_token") == "" {
		t.Fatalf("verify URL missing email_token: %q", verifyURL.String())
	}
}

func TestHandleEmailVerification_InvalidVerifyURL(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			if id == "cust-1" {
				c, _ := customer.NewCustomer("cust-1", "alice@example.com")
				c.FirstName = "Alice"
				return &c, nil
			}
			return nil, nil
		},
	}
	ordRepo := &mockOrderRepo{}

	svc := newTestService(t, tmpl, custRepo, ordRepo, q)
	evt := event.New(customer.EventEmailVerificationRequested, "auth.service", customer.EmailVerificationRequestedData{
		CustomerID: "cust-1",
		VerifyURL:  "   ",
	})

	err := svc.HandleEmailVerification(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for invalid verify URL")
	}
	if len(q.enqueued) != 0 {
		t.Fatalf("expected 0 enqueued jobs, got %d", len(q.enqueued))
	}
}

func TestHandleEmailVerification_CustomerNotFound(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, _ string) (*customer.Customer, error) {
			return nil, nil
		},
	}
	ordRepo := &mockOrderRepo{}

	svc := newTestService(t, tmpl, custRepo, ordRepo, q)
	evt := event.New(customer.EventEmailVerificationRequested, "auth.service", customer.EmailVerificationRequestedData{
		CustomerID: "missing",
		VerifyURL:  "https://shop.test/account/verify-email?email_token=abc",
	})

	err := svc.HandleEmailVerification(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for missing customer")
	}
	if len(q.enqueued) != 0 {
		t.Fatalf("expected 0 enqueued jobs, got %d", len(q.enqueued))
	}
}

func TestHandleEmailVerification_BadEventData(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	q := &mockQueue{}
	svc := newTestService(t, tmpl, &mockCustomerRepo{}, &mockOrderRepo{}, q)
	evt := event.New(customer.EventEmailVerificationRequested, "auth.service", "not-a-struct")

	err := svc.HandleEmailVerification(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for bad event data")
	}
	if len(q.enqueued) != 0 {
		t.Fatalf("expected 0 enqueued jobs, got %d", len(q.enqueued))
	}
}

func TestHandleEmailChangeRequested_SendsToNewAddress(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			if id == "cust-1" {
				c, _ := customer.NewCustomer("cust-1", "old@example.com")
				c.FirstName = "Alice"
				return &c, nil
			}
			return nil, nil
		},
	}

	svc := newTestService(t, tmpl, custRepo, &mockOrderRepo{}, q)
	evt := event.New(customer.EventEmailChangeRequested, "auth.service", customer.EmailChangeRequestedData{
		CustomerID: "cust-1",
		NewEmail:   "new@example.com",
		VerifyURL:  "https://shop.test/account/security/email/confirm?email_token=abc",
	})

	if err := svc.HandleEmailChangeRequested(context.Background(), evt); err != nil {
		t.Fatalf("HandleEmailChangeRequested: %v", err)
	}
	if len(q.enqueued) != 1 {
		t.Fatalf("expected 1 enqueued job, got %d", len(q.enqueued))
	}
	if to, _ := q.enqueued[0].Payload["to"].(string); to != "new@example.com" {
		t.Fatalf("payload.to = %q, want new@example.com (the new address)", to)
	}
	body, _ := q.enqueued[0].Payload["body"].(string)
	if !strings.Contains(body, "email_token=abc") {
		t.Fatalf("body missing confirmation link: %q", body)
	}
}

func TestHandleEmailChangeRequested_InvalidVerifyURL(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	q := &mockQueue{}
	svc := newTestService(t, tmpl, &mockCustomerRepo{}, &mockOrderRepo{}, q)
	evt := event.New(customer.EventEmailChangeRequested, "auth.service", customer.EmailChangeRequestedData{
		CustomerID: "cust-1",
		NewEmail:   "new@example.com",
		VerifyURL:  "   ",
	})

	if err := svc.HandleEmailChangeRequested(context.Background(), evt); err == nil {
		t.Fatal("expected error for invalid verify URL")
	}
	if len(q.enqueued) != 0 {
		t.Fatalf("expected 0 enqueued jobs, got %d", len(q.enqueued))
	}
}

func TestHandleEmailChangeNotified_SendsToOldAddress(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			if id == "cust-1" {
				c, _ := customer.NewCustomer("cust-1", "old@example.com")
				c.FirstName = "Alice"
				return &c, nil
			}
			return nil, nil
		},
	}

	svc := newTestService(t, tmpl, custRepo, &mockOrderRepo{}, q)
	evt := event.New(customer.EventEmailChangeNotified, "auth.service", customer.EmailChangeNotifiedData{
		CustomerID: "cust-1",
		OldEmail:   "old@example.com",
		NewEmail:   "new@example.com",
	})

	if err := svc.HandleEmailChangeNotified(context.Background(), evt); err != nil {
		t.Fatalf("HandleEmailChangeNotified: %v", err)
	}
	if len(q.enqueued) != 1 {
		t.Fatalf("expected 1 enqueued job, got %d", len(q.enqueued))
	}
	if to, _ := q.enqueued[0].Payload["to"].(string); to != "old@example.com" {
		t.Fatalf("payload.to = %q, want old@example.com (the current address)", to)
	}
}

func TestHandlePasswordReset_CustomerNotFound(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, _ string) (*customer.Customer, error) {
			return nil, nil
		},
	}
	ordRepo := &mockOrderRepo{}

	svc := newTestService(t, tmpl, custRepo, ordRepo, q)
	evt := event.New(customer.EventPasswordResetRequested, "auth.service", customer.PasswordResetRequestedData{
		CustomerID: "missing",
		Token:      "tok",
	})

	err := svc.HandlePasswordReset(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for missing customer")
	}
	if len(q.enqueued) != 0 {
		t.Fatalf("expected 0 enqueued jobs, got %d", len(q.enqueued))
	}
}

func TestHandlePasswordReset_BadEventData(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	q := &mockQueue{}
	svc := newTestService(t, tmpl, &mockCustomerRepo{}, &mockOrderRepo{}, q)

	evt := event.New(customer.EventPasswordResetRequested, "auth.service", "not-a-struct")
	err := svc.HandlePasswordReset(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for bad event data")
	}
}

func TestHandleSecurityVerification(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			if id == "cust-1" {
				c, _ := customer.NewCustomer("cust-1", "alice@example.com")
				c.FirstName = "Alice"
				return &c, nil
			}
			return nil, nil
		},
	}
	ordRepo := &mockOrderRepo{}

	svc := newTestService(t, tmpl, custRepo, ordRepo, q)
	evt := event.New(customer.EventSecurityVerificationRequested, "auth.service", customer.SecurityVerificationRequestedData{
		CustomerID: "cust-1",
		VerifyURL:  "https://shop.test/account/security/verify?email_token=abc",
	})

	if err := svc.HandleSecurityVerification(context.Background(), evt); err != nil {
		t.Fatalf("HandleSecurityVerification: %v", err)
	}
	if len(q.enqueued) != 1 {
		t.Fatalf("expected 1 enqueued job, got %d", len(q.enqueued))
	}
	job := q.enqueued[0]
	if to, _ := job.Payload["to"].(string); to != "alice@example.com" {
		t.Errorf("payload.to = %q, want alice@example.com", to)
	}
	if subj, _ := job.Payload["subject"].(string); subj != "Verify your security access" {
		t.Errorf("payload.subject = %q, want 'Verify your security access'", subj)
	}
	body, _ := job.Payload["body"].(string)
	if !strings.Contains(body, "https://shop.test/account/security/verify?email_token=abc") {
		t.Fatalf("expected verify URL in body, got %q", body)
	}
}

func TestHandleSecurityVerification_CustomerNotFound(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, _ string) (*customer.Customer, error) {
			return nil, nil
		},
	}
	svc := newTestService(t, tmpl, custRepo, &mockOrderRepo{}, q)

	err := svc.HandleSecurityVerification(context.Background(), event.New(customer.EventSecurityVerificationRequested, "auth.service", customer.SecurityVerificationRequestedData{
		CustomerID: "missing",
		VerifyURL:  "https://shop.test/account/security/verify?email_token=abc",
	}))
	if err == nil {
		t.Fatal("expected error for missing customer")
	}
	if len(q.enqueued) != 0 {
		t.Fatalf("expected 0 enqueued jobs, got %d", len(q.enqueued))
	}
}

func TestHandleSecurityVerification_BadEventData(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	q := &mockQueue{}
	svc := newTestService(t, tmpl, &mockCustomerRepo{}, &mockOrderRepo{}, q)

	err := svc.HandleSecurityVerification(context.Background(), event.New(customer.EventSecurityVerificationRequested, "auth.service", "not-a-struct"))
	if err == nil {
		t.Fatal("expected error for bad event data")
	}
}

// --- HandleShipmentShipped tests ---

func TestHandleShipmentShipped(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			if id == "cust-1" {
				c, _ := customer.NewCustomer("cust-1", "alice@example.com")
				c.FirstName = "Alice"
				return &c, nil
			}
			return nil, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			if id == "ord-1" {
				return &order.Order{ID: "ord-1", CustomerID: "cust-1"}, nil
			}
			return nil, nil
		},
	}

	dedupe := &mockDedupe{}
	svc := newTestService(t, tmpl, custRepo, ordRepo, q, notification.WithEmailDedupe(dedupe))
	evt := event.New(shipping.EventShipmentShipped, "core.shipping", shipping.ShipmentStatusChangedData{
		ShipmentID:     "ship-1",
		OrderID:        "ord-1",
		OldStatus:      shipping.StatusPending,
		NewStatus:      shipping.StatusShipped,
		TrackingNumber: "TRACK-123",
		ProviderRef:    "FedEx",
	})

	if err := svc.HandleShipmentShipped(context.Background(), evt); err != nil {
		t.Fatalf("HandleShipmentShipped: %v", err)
	}

	if len(q.enqueued) != 1 {
		t.Fatalf("expected 1 enqueued job, got %d", len(q.enqueued))
	}
	job := q.enqueued[0]
	if to, _ := job.Payload["to"].(string); to != "alice@example.com" {
		t.Errorf("payload.to = %q, want alice@example.com", to)
	}
	if subj, _ := job.Payload["subject"].(string); subj == "" {
		t.Error("payload.subject is empty")
	}

	// Heal republish must not double-enqueue.
	if err := svc.HandleShipmentShipped(context.Background(), evt); err != nil {
		t.Fatalf("second HandleShipmentShipped: %v", err)
	}
	if len(q.enqueued) != 1 {
		t.Fatalf("expected still 1 enqueued job after heal, got %d", len(q.enqueued))
	}
}

func TestHandleShipmentShipped_SkipsCancelledOrder(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	q := &mockQueue{}
	item, err := order.NewItem("v1", "SKU", "Item", 1, shared.MustNewMoney(100, "EUR"))
	if err != nil {
		t.Fatal(err)
	}
	o, err := order.NewOrder("ord-1", "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Cancel(); err != nil {
		t.Fatal(err)
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			if id == "ord-1" {
				return &o, nil
			}
			return nil, nil
		},
	}
	svc := newTestService(t, tmpl, &mockCustomerRepo{}, ordRepo, q)
	evt := event.New(shipping.EventShipmentShipped, "core.shipping", shipping.ShipmentStatusChangedData{
		ShipmentID: "ship-1",
		OrderID:    "ord-1",
	})
	if err := svc.HandleShipmentShipped(context.Background(), evt); err != nil {
		t.Fatalf("HandleShipmentShipped: %v", err)
	}
	if len(q.enqueued) != 0 {
		t.Fatalf("expected no email for cancelled order, got %d", len(q.enqueued))
	}
}

func TestHandleShipmentShipped_GuestOrder(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			t.Fatalf("unexpected customer lookup for guest order: %q", id)
			return nil, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			if id == "ord-1" {
				return &order.Order{ID: "ord-1", CustomerID: "", ContactEmail: "guest@example.com"}, nil
			}
			return nil, nil
		},
	}

	svc := newTestService(t, tmpl, custRepo, ordRepo, q)
	evt := event.New(shipping.EventShipmentShipped, "core.shipping", shipping.ShipmentStatusChangedData{
		ShipmentID:     "ship-1",
		OrderID:        "ord-1",
		OldStatus:      shipping.StatusPending,
		NewStatus:      shipping.StatusShipped,
		TrackingNumber: "TRACK-123",
	})

	if err := svc.HandleShipmentShipped(context.Background(), evt); err != nil {
		t.Fatalf("HandleShipmentShipped guest: %v", err)
	}
	if len(q.enqueued) != 1 {
		t.Fatalf("expected 1 enqueued job, got %d", len(q.enqueued))
	}
	if to, _ := q.enqueued[0].Payload["to"].(string); to != "guest@example.com" {
		t.Errorf("payload.to = %q, want guest@example.com", to)
	}
}

func TestHandleShipmentShipped_OrderNotFound(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	q := &mockQueue{}
	custRepo := &mockCustomerRepo{}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, _ string) (*order.Order, error) {
			return nil, nil
		},
	}

	svc := newTestService(t, tmpl, custRepo, ordRepo, q)
	evt := event.New(shipping.EventShipmentShipped, "core.shipping", shipping.ShipmentStatusChangedData{
		ShipmentID: "ship-1",
		OrderID:    "missing",
	})

	err := svc.HandleShipmentShipped(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for missing order")
	}
}

func TestHandleShipmentShipped_BadEventData(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	q := &mockQueue{}
	svc := newTestService(t, tmpl, &mockCustomerRepo{}, &mockOrderRepo{}, q)

	evt := event.New(shipping.EventShipmentShipped, "core.shipping", "bad-data")
	err := svc.HandleShipmentShipped(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for bad event data")
	}
}

// --- EmailSendHandler tests ---

func TestEmailSendHandler_Handle(t *testing.T) {
	m := &mockMailer{}
	h := notification.NewEmailSendHandler(m)

	if h.Type() != notification.JobTypeEmailSend {
		t.Fatalf("Type() = %q, want %q", h.Type(), notification.JobTypeEmailSend)
	}

	j := jobs.Job{
		ID:   "j1",
		Type: notification.JobTypeEmailSend,
		Payload: map[string]interface{}{
			"to":      "bob@example.com",
			"subject": "Hello",
			"body":    "<p>Hi</p>",
		},
	}

	if err := h.Handle(context.Background(), j); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(m.sent) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(m.sent))
	}
	msg := m.sent[0]
	if msg.To != "bob@example.com" {
		t.Errorf("To = %q", msg.To)
	}
	if msg.Subject != "Hello" {
		t.Errorf("Subject = %q", msg.Subject)
	}
	if msg.Body != "<p>Hi</p>" {
		t.Errorf("Body = %q", msg.Body)
	}
}

func TestEmailSendHandler_MissingTo(t *testing.T) {
	m := &mockMailer{}
	h := notification.NewEmailSendHandler(m)

	j := jobs.Job{
		ID:      "j2",
		Type:    notification.JobTypeEmailSend,
		Payload: map[string]interface{}{},
	}

	err := h.Handle(context.Background(), j)
	if err == nil {
		t.Fatal("expected error for missing 'to'")
	}
}

// Verify interface compliance.
var _ mail.Mailer = (*mockMailer)(nil)
var _ jobs.Handler = (*notification.EmailSendHandler)(nil)

// --- HandleInvoiceCreated tests ---

func newInvoiceTestService(t *testing.T, tmpl *mail.Templates, custRepo *mockCustomerRepo,
	ordRepo *mockOrderRepo, q *mockQueue, invRepo *mockInvoiceRepo, renderer *mockPDFRenderer,
) *notification.Service {
	t.Helper()
	return notification.New(tmpl, custRepo, ordRepo, q, mockLogger{},
		notification.WithInvoices(invRepo),
		notification.WithPDFRenderer(renderer),
	)
}

func TestHandleInvoiceCreated(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			if id == "cust-1" {
				c, _ := customer.NewCustomer("cust-1", "alice@example.com")
				c.FirstName = "Alice"
				return &c, nil
			}
			return nil, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			if id == "ord-1" {
				return &order.Order{ID: "ord-1", CustomerID: "cust-1"}, nil
			}
			return nil, nil
		},
	}

	taxAmount, _ := shared.NewMoney(0, "EUR")
	unitPrice, _ := shared.NewMoney(2500, "EUR")
	item, _ := domainInvoice.NewItem("var-1", "SKU-1", "Widget", 2, unitPrice)
	inv, _ := domainInvoice.NewInvoice("inv-1", "ord-1", "cust-1", "EUR", []domainInvoice.Item{item}, taxAmount)
	inv.SetInvoiceNumberFromDB(1001)

	invRepo := &mockInvoiceRepo{
		findByID: func(_ context.Context, id string) (*domainInvoice.Invoice, error) {
			if id == "inv-1" {
				return &inv, nil
			}
			return nil, nil
		},
	}
	renderer := &mockPDFRenderer{
		render: func(_ domainInvoice.Invoice) ([]byte, error) {
			return []byte("%PDF-test-content"), nil
		},
	}

	svc := newInvoiceTestService(t, tmpl, custRepo, ordRepo, q, invRepo, renderer)
	evt := event.New(domainInvoice.EventInvoiceCreated, "invoice.service", domainInvoice.InvoiceCreatedData{
		InvoiceID:     "inv-1",
		InvoiceNumber: 1001,
		OrderID:       "ord-1",
		CustomerID:    "cust-1",
		Currency:      "EUR",
	})

	if err := svc.HandleInvoiceCreated(context.Background(), evt); err != nil {
		t.Fatalf("HandleInvoiceCreated: %v", err)
	}

	if len(q.enqueued) != 1 {
		t.Fatalf("expected 1 enqueued job, got %d", len(q.enqueued))
	}
	job := q.enqueued[0]
	if to, _ := job.Payload["to"].(string); to != "alice@example.com" {
		t.Errorf("payload.to = %q, want alice@example.com", to)
	}
	if subj, _ := job.Payload["subject"].(string); subj == "" {
		t.Error("payload.subject is empty")
	}
	// Verify attachment is serialized in payload.
	attsRaw, ok := job.Payload["attachments"].([]map[string]interface{})
	if !ok || len(attsRaw) != 1 {
		t.Fatalf("expected 1 attachment in payload, got %v", job.Payload["attachments"])
	}
	att := attsRaw[0]
	if fn, _ := att["filename"].(string); fn != "invoice-1001.pdf" {
		t.Errorf("attachment filename = %q, want invoice-1001.pdf", fn)
	}
	if ct, _ := att["content_type"].(string); ct != "application/pdf" {
		t.Errorf("attachment content_type = %q", ct)
	}
	dataStr, _ := att["data"].(string)
	decoded, err := base64.StdEncoding.DecodeString(dataStr)
	if err != nil {
		t.Fatalf("decode attachment data: %v", err)
	}
	if string(decoded) != "%PDF-test-content" {
		t.Errorf("attachment data = %q", string(decoded))
	}
}

func TestHandleInvoiceCreated_HealReplacesFailedJobPayload(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	taxAmount, _ := shared.NewMoney(0, "EUR")
	unitPrice, _ := shared.NewMoney(2500, "EUR")
	item, _ := domainInvoice.NewItem("var-1", "SKU-1", "Widget", 1, unitPrice)
	inv, _ := domainInvoice.NewInvoice("inv-heal", "ord-heal", "cust-1", "EUR", []domainInvoice.Item{item}, taxAmount)
	inv.SetInvoiceNumberFromDB(42)

	q := &mockQueue{details: map[string]*jobs.Detail{}}
	dedupe := &mockDedupe{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			c, _ := customer.NewCustomer("cust-1", "alice@example.com")
			return &c, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			return &order.Order{ID: "ord-heal", CustomerID: "cust-1"}, nil
		},
	}
	invRepo := &mockInvoiceRepo{
		findByID: func(_ context.Context, id string) (*domainInvoice.Invoice, error) {
			return &inv, nil
		},
	}
	pdfN := 0
	renderer := &mockPDFRenderer{
		render: func(_ domainInvoice.Invoice) ([]byte, error) {
			pdfN++
			return []byte(fmt.Sprintf("%%PDF-heal-%d", pdfN)), nil
		},
	}
	svc := notification.New(tmpl, custRepo, ordRepo, q, mockLogger{},
		notification.WithInvoices(invRepo),
		notification.WithPDFRenderer(renderer),
		notification.WithEmailDedupe(dedupe),
	)
	evt := event.New(domainInvoice.EventInvoiceCreated, "invoice.service", domainInvoice.InvoiceCreatedData{
		InvoiceID:     "inv-heal",
		InvoiceNumber: 42,
		OrderID:       "ord-heal",
		CustomerID:    "cust-1",
		Currency:      "EUR",
	})
	if err := svc.HandleInvoiceCreated(context.Background(), evt); err != nil {
		t.Fatalf("first: %v", err)
	}
	if len(q.enqueued) != 1 {
		t.Fatalf("enqueued = %d", len(q.enqueued))
	}
	jobID := q.enqueued[0].ID
	q.details[jobID] = &jobs.Detail{Summary: jobs.Summary{ID: jobID, Status: jobs.StatusFailed, Attempts: 3}}

	if err := svc.HandleInvoiceCreated(context.Background(), evt); err != nil {
		t.Fatalf("heal: %v", err)
	}
	if len(q.enqueued) != 1 {
		t.Fatalf("heal must keep stable id (no second enqueue), got %d", len(q.enqueued))
	}
	if len(q.replaced) != 1 || q.replaced[0].ID != jobID {
		t.Fatalf("replaced = %#v, want job %s", q.replaced, jobID)
	}
	atts, _ := q.replaced[0].Payload["attachments"].([]map[string]interface{})
	if len(atts) != 1 {
		t.Fatalf("replaced attachments = %#v", q.replaced[0].Payload["attachments"])
	}
	dataStr, _ := atts[0]["data"].(string)
	decoded, err := base64.StdEncoding.DecodeString(dataStr)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(decoded) != "%PDF-heal-2" {
		t.Fatalf("replaced PDF = %q, want fresh heal-2 payload", decoded)
	}
}

func TestHandleInvoiceCreated_HealMissingJobSurfaces(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	taxAmount, _ := shared.NewMoney(0, "EUR")
	unitPrice, _ := shared.NewMoney(2500, "EUR")
	item, _ := domainInvoice.NewItem("var-1", "SKU-1", "Widget", 1, unitPrice)
	inv, _ := domainInvoice.NewInvoice("inv-miss", "ord-miss", "cust-1", "EUR", []domainInvoice.Item{item}, taxAmount)
	inv.SetInvoiceNumberFromDB(7)

	q := &mockQueue{details: map[string]*jobs.Detail{}}
	dedupe := &mockDedupe{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			c, _ := customer.NewCustomer("cust-1", "alice@example.com")
			return &c, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			return &order.Order{ID: "ord-miss", CustomerID: "cust-1"}, nil
		},
	}
	invRepo := &mockInvoiceRepo{
		findByID: func(_ context.Context, id string) (*domainInvoice.Invoice, error) {
			return &inv, nil
		},
	}
	renderer := &mockPDFRenderer{
		render: func(_ domainInvoice.Invoice) ([]byte, error) {
			return []byte("%PDF-miss"), nil
		},
	}
	svc := notification.New(tmpl, custRepo, ordRepo, q, mockLogger{},
		notification.WithInvoices(invRepo),
		notification.WithPDFRenderer(renderer),
		notification.WithEmailDedupe(dedupe),
	)
	evt := event.New(domainInvoice.EventInvoiceCreated, "invoice.service", domainInvoice.InvoiceCreatedData{
		InvoiceID:     "inv-miss",
		InvoiceNumber: 7,
		OrderID:       "ord-miss",
		CustomerID:    "cust-1",
		Currency:      "EUR",
	})
	if err := svc.HandleInvoiceCreated(context.Background(), evt); err != nil {
		t.Fatalf("first: %v", err)
	}
	jobID := q.enqueued[0].ID
	// Job row gone (lost queue entry); claim still completed.
	delete(q.byID, jobID)
	err := svc.HandleInvoiceCreated(context.Background(), evt)
	if err == nil || !strings.Contains(err.Error(), "not found for heal") {
		t.Fatalf("err = %v, want not found for heal", err)
	}
	if len(q.replaced) != 0 {
		t.Fatalf("ReplaceFailed must not run, replaced=%#v", q.replaced)
	}
	// Claim released → next republish can insert again under the same stable id.
	if err := svc.HandleInvoiceCreated(context.Background(), evt); err != nil {
		t.Fatalf("retry after claim release: %v", err)
	}
	if len(q.enqueued) != 2 || q.enqueued[1].ID != jobID {
		t.Fatalf("enqueued after recover = %#v, want second job %s", q.enqueued, jobID)
	}
}

func TestHandleOrderPaid_FailedJobNotReplaced(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	q := &mockQueue{details: map[string]*jobs.Detail{}, ignoreID: true}
	dedupe := &mockDedupe{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			c, _ := customer.NewCustomer("cust-1", "alice@example.com")
			return &c, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			return &order.Order{ID: "ord-1", CustomerID: "cust-1"}, nil
		},
	}
	svc := notification.New(tmpl, custRepo, ordRepo, q, mockLogger{},
		notification.WithEmailDedupe(dedupe),
	)
	evt := event.New(order.EventOrderPaid, "core.order", order.OrderStatusChangedData{
		OrderID:   "ord-1",
		OldStatus: "pending",
		NewStatus: "paid",
	})
	if err := svc.HandleOrderPaid(context.Background(), evt); err != nil {
		t.Fatalf("first: %v", err)
	}
	jobID := q.enqueued[0].ID
	q.details[jobID] = &jobs.Detail{Summary: jobs.Summary{ID: jobID, Status: jobs.StatusFailed, Attempts: 3}}

	if err := svc.HandleOrderPaid(context.Background(), evt); err != nil {
		t.Fatalf("republish: %v", err)
	}
	if len(q.replaced) != 0 {
		t.Fatalf("order_confirmed must not ReplaceFailed, replaced=%#v", q.replaced)
	}
	if len(q.enqueued) != 1 {
		t.Fatalf("enqueued = %d, want 1", len(q.enqueued))
	}
}

func TestHandleInvoiceCreated_InvoiceNotFound(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	q := &mockQueue{}
	custRepo := &mockCustomerRepo{}
	invRepo := &mockInvoiceRepo{
		findByID: func(_ context.Context, _ string) (*domainInvoice.Invoice, error) {
			return nil, nil
		},
	}
	renderer := &mockPDFRenderer{}

	svc := newInvoiceTestService(t, tmpl, custRepo, &mockOrderRepo{}, q, invRepo, renderer)
	evt := event.New(domainInvoice.EventInvoiceCreated, "invoice.service", domainInvoice.InvoiceCreatedData{
		InvoiceID:  "missing",
		CustomerID: "cust-1",
	})

	err := svc.HandleInvoiceCreated(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for missing invoice")
	}
}

func TestHandleInvoiceCreated_CustomerNotFound(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, _ string) (*customer.Customer, error) {
			return nil, nil
		},
	}

	taxAmount, _ := shared.NewMoney(0, "EUR")
	unitPrice, _ := shared.NewMoney(1000, "EUR")
	item, _ := domainInvoice.NewItem("v-1", "S-1", "X", 1, unitPrice)
	inv, _ := domainInvoice.NewInvoice("inv-1", "ord-1", "cust-1", "EUR", []domainInvoice.Item{item}, taxAmount)

	invRepo := &mockInvoiceRepo{
		findByID: func(_ context.Context, _ string) (*domainInvoice.Invoice, error) {
			return &inv, nil
		},
	}
	renderer := &mockPDFRenderer{}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, _ string) (*order.Order, error) {
			return &order.Order{ID: "ord-1", CustomerID: "cust-1"}, nil
		},
	}

	svc := newInvoiceTestService(t, tmpl, custRepo, ordRepo, q, invRepo, renderer)
	evt := event.New(domainInvoice.EventInvoiceCreated, "invoice.service", domainInvoice.InvoiceCreatedData{
		InvoiceID:  "inv-1",
		OrderID:    "ord-1",
		CustomerID: "cust-1",
	})

	err := svc.HandleInvoiceCreated(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for missing customer")
	}
}

func TestHandleInvoiceCreated_GuestOrder(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	q := &mockQueue{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, id string) (*customer.Customer, error) {
			t.Fatalf("unexpected customer lookup for guest invoice: %q", id)
			return nil, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, id string) (*order.Order, error) {
			if id == "ord-1" {
				return &order.Order{ID: "ord-1", CustomerID: "", ContactEmail: "guest@example.com"}, nil
			}
			return nil, nil
		},
	}

	taxAmount, _ := shared.NewMoney(0, "EUR")
	unitPrice, _ := shared.NewMoney(2500, "EUR")
	item, _ := domainInvoice.NewItem("var-1", "SKU-1", "Widget", 2, unitPrice)
	inv, err := domainInvoice.NewInvoice("inv-1", "ord-1", "", "EUR", []domainInvoice.Item{item}, taxAmount)
	if err != nil {
		t.Fatalf("NewInvoice guest: %v", err)
	}
	inv.SetInvoiceNumberFromDB(1002)

	invRepo := &mockInvoiceRepo{
		findByID: func(_ context.Context, id string) (*domainInvoice.Invoice, error) {
			if id == "inv-1" {
				return &inv, nil
			}
			return nil, nil
		},
	}
	renderer := &mockPDFRenderer{}

	svc := newInvoiceTestService(t, tmpl, custRepo, ordRepo, q, invRepo, renderer)
	evt := event.New(domainInvoice.EventInvoiceCreated, "invoice.service", domainInvoice.InvoiceCreatedData{
		InvoiceID:     "inv-1",
		InvoiceNumber: 1002,
		OrderID:       "ord-1",
		CustomerID:    "",
		Currency:      "EUR",
	})

	if err := svc.HandleInvoiceCreated(context.Background(), evt); err != nil {
		t.Fatalf("HandleInvoiceCreated guest: %v", err)
	}
	if len(q.enqueued) != 1 {
		t.Fatalf("expected 1 enqueued job, got %d", len(q.enqueued))
	}
	if to, _ := q.enqueued[0].Payload["to"].(string); to != "guest@example.com" {
		t.Errorf("payload.to = %q, want guest@example.com", to)
	}
}

func TestHandleInvoiceCreated_CustomerMismatch(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	q := &mockQueue{}

	taxAmount, _ := shared.NewMoney(0, "EUR")
	unitPrice, _ := shared.NewMoney(1000, "EUR")
	item, _ := domainInvoice.NewItem("v-1", "S-1", "X", 1, unitPrice)
	inv, _ := domainInvoice.NewInvoice("inv-1", "ord-1", "cust-1", "EUR", []domainInvoice.Item{item}, taxAmount)

	invRepo := &mockInvoiceRepo{
		findByID: func(_ context.Context, _ string) (*domainInvoice.Invoice, error) {
			return &inv, nil
		},
	}
	renderer := &mockPDFRenderer{}

	svc := newInvoiceTestService(t, tmpl, &mockCustomerRepo{}, &mockOrderRepo{}, q, invRepo, renderer)
	evt := event.New(domainInvoice.EventInvoiceCreated, "invoice.service", domainInvoice.InvoiceCreatedData{
		InvoiceID:  "inv-1",
		OrderID:    "ord-1",
		CustomerID: "cust-WRONG",
	})

	err := svc.HandleInvoiceCreated(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for customer mismatch")
	}
	if !strings.Contains(err.Error(), "customer") {
		t.Errorf("error should mention customer mismatch: %v", err)
	}
}

func TestHandleInvoiceCreated_OrderMismatch(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	q := &mockQueue{}

	taxAmount, _ := shared.NewMoney(0, "EUR")
	unitPrice, _ := shared.NewMoney(1000, "EUR")
	item, _ := domainInvoice.NewItem("v-1", "S-1", "X", 1, unitPrice)
	inv, _ := domainInvoice.NewInvoice("inv-1", "ord-1", "cust-1", "EUR", []domainInvoice.Item{item}, taxAmount)

	invRepo := &mockInvoiceRepo{
		findByID: func(_ context.Context, _ string) (*domainInvoice.Invoice, error) {
			return &inv, nil
		},
	}
	renderer := &mockPDFRenderer{}

	svc := newInvoiceTestService(t, tmpl, &mockCustomerRepo{}, &mockOrderRepo{}, q, invRepo, renderer)
	evt := event.New(domainInvoice.EventInvoiceCreated, "invoice.service", domainInvoice.InvoiceCreatedData{
		InvoiceID:  "inv-1",
		OrderID:    "ord-WRONG",
		CustomerID: "cust-1",
	})

	err := svc.HandleInvoiceCreated(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for order mismatch")
	}
	if !strings.Contains(err.Error(), "order") {
		t.Errorf("error should mention order mismatch: %v", err)
	}
}

func TestHandleInvoiceCreated_BadEventData(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	q := &mockQueue{}
	invRepo := &mockInvoiceRepo{}
	renderer := &mockPDFRenderer{}

	svc := newInvoiceTestService(t, tmpl, &mockCustomerRepo{}, &mockOrderRepo{}, q, invRepo, renderer)
	evt := event.New(domainInvoice.EventInvoiceCreated, "invoice.service", "not-a-struct")

	err := svc.HandleInvoiceCreated(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for bad event data")
	}
}

func TestHandleInvoiceCreated_SkippedNoDeps(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	q := &mockQueue{}

	// Service without invoice deps.
	svc := newTestService(t, tmpl, &mockCustomerRepo{}, &mockOrderRepo{}, q)
	evt := event.New(domainInvoice.EventInvoiceCreated, "invoice.service", domainInvoice.InvoiceCreatedData{
		InvoiceID: "inv-1",
	})

	err := svc.HandleInvoiceCreated(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected nil error when deps missing, got: %v", err)
	}
	if len(q.enqueued) != 0 {
		t.Fatalf("expected 0 enqueued jobs, got %d", len(q.enqueued))
	}
}

// --- EmailSendHandler attachment test ---

func TestEmailSendHandler_HandleWithAttachment(t *testing.T) {
	m := &mockMailer{}
	h := notification.NewEmailSendHandler(m)

	pdfData := []byte("%PDF-test")
	j := jobs.Job{
		ID:   "j3",
		Type: notification.JobTypeEmailSend,
		Payload: map[string]interface{}{
			"to":      "bob@example.com",
			"subject": "Invoice",
			"body":    "<p>Attached</p>",
			"attachments": []interface{}{
				map[string]interface{}{
					"filename":     "invoice-1.pdf",
					"content_type": "application/pdf",
					"data":         base64.StdEncoding.EncodeToString(pdfData),
				},
			},
		},
	}

	if err := h.Handle(context.Background(), j); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(m.sent) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(m.sent))
	}
	msg := m.sent[0]
	if len(msg.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(msg.Attachments))
	}
	att := msg.Attachments[0]
	if att.Filename != "invoice-1.pdf" {
		t.Errorf("Filename = %q", att.Filename)
	}
	if att.ContentType != "application/pdf" {
		t.Errorf("ContentType = %q", att.ContentType)
	}
	if string(att.Data) != "%PDF-test" {
		t.Errorf("Data = %q", string(att.Data))
	}
}

func TestEmailSendHandler_HandleWithMalformedAttachment(t *testing.T) {
	m := &mockMailer{}
	h := notification.NewEmailSendHandler(m)

	j := jobs.Job{
		ID:   "j4",
		Type: notification.JobTypeEmailSend,
		Payload: map[string]interface{}{
			"to":      "bob@example.com",
			"subject": "Invoice",
			"body":    "<p>Attached</p>",
			"attachments": []interface{}{
				map[string]interface{}{
					"filename":     "",
					"content_type": "application/pdf",
					"data":         "dGVzdA==",
				},
			},
		},
	}

	err := h.Handle(context.Background(), j)
	if err == nil {
		t.Fatal("expected error for missing filename")
	}
	if !strings.Contains(err.Error(), "missing filename") {
		t.Errorf("error = %q, want 'missing filename'", err)
	}
}
