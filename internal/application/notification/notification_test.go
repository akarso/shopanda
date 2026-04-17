package notification_test

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/notification"
	"github.com/akarso/shopanda/internal/domain/customer"
	domainInvoice "github.com/akarso/shopanda/internal/domain/invoice"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/platform/event"
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
	enqueued []jobs.Job
}

func (m *mockQueue) Enqueue(_ context.Context, job jobs.Job) error {
	m.enqueued = append(m.enqueued, job)
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

func (m *mockPDFRenderer) Render(inv domainInvoice.Invoice) ([]byte, error) {
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
func newTestService(t *testing.T, tmpl *mail.Templates, custRepo *mockCustomerRepo, ordRepo *mockOrderRepo, q *mockQueue) *notification.Service {
	t.Helper()
	return notification.New(tmpl, custRepo, ordRepo, q, mockLogger{})
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

	svc := newTestService(t, tmpl, custRepo, ordRepo, q)
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

	svc := newTestService(t, tmpl, custRepo, ordRepo, q)
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
	ordRepo := &mockOrderRepo{}

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

	svc := newInvoiceTestService(t, tmpl, custRepo, &mockOrderRepo{}, q, invRepo, renderer)
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
