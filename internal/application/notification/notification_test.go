package notification_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/application/notification"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/order"
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
