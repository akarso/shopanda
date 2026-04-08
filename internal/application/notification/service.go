package notification

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
)

// JobTypeEmailSend is the job type for email delivery.
const JobTypeEmailSend = "email.send"

// Service wires order events to email notifications via the job queue.
type Service struct {
	templates *mail.Templates
	customers customer.CustomerRepository
	orders    order.OrderRepository
	queue     jobs.Queue
}

// New creates a notification Service.
func New(templates *mail.Templates, customers customer.CustomerRepository, orders order.OrderRepository, queue jobs.Queue) *Service {
	return &Service{
		templates: templates,
		customers: customers,
		orders:    orders,
		queue:     queue,
	}
}

// RegisterTemplates registers the email templates used by this service.
func RegisterTemplates(t *mail.Templates) {
	t.Register("order.confirmed",
		"Order {{.OrderID}} — Confirmation",
		"<h1>Thank you, {{.FirstName}}!</h1>"+
			"<p>Your order <strong>{{.OrderID}}</strong> has been paid and confirmed.</p>")
}

// HandleOrderPaid is an event handler for order.paid.
// It looks up the customer, renders the confirmation template,
// and enqueues an email.send job.
func (s *Service) HandleOrderPaid(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(order.OrderStatusChangedData)
	if !ok {
		return fmt.Errorf("notification: unexpected event data type %T", evt.Data)
	}

	o, err := s.orders.FindByID(ctx, data.OrderID)
	if err != nil {
		return fmt.Errorf("notification: find order %s: %w", data.OrderID, err)
	}
	if o == nil {
		return fmt.Errorf("notification: order %s not found", data.OrderID)
	}

	cust, err := s.customers.FindByID(ctx, o.CustomerID)
	if err != nil {
		return fmt.Errorf("notification: find customer %s: %w", o.CustomerID, err)
	}
	if cust == nil {
		return fmt.Errorf("notification: customer %s not found", o.CustomerID)
	}

	msg, err := s.templates.Render("order.confirmed", cust.Email, map[string]string{
		"OrderID":   data.OrderID,
		"FirstName": cust.FirstName,
	})
	if err != nil {
		return fmt.Errorf("notification: render template: %w", err)
	}

	payload := map[string]interface{}{
		"to":      msg.To,
		"subject": msg.Subject,
		"body":    msg.Body,
	}

	job, err := jobs.NewJob(id.New(), JobTypeEmailSend, payload)
	if err != nil {
		return fmt.Errorf("notification: create job: %w", err)
	}

	return s.queue.Enqueue(ctx, job)
}
