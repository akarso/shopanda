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
	"github.com/akarso/shopanda/internal/platform/logger"
)

// JobTypeEmailSend is the job type for email delivery.
const JobTypeEmailSend = "email.send"

// Service wires order events to email notifications via the job queue.
type Service struct {
	templates *mail.Templates
	customers customer.CustomerRepository
	orders    order.OrderRepository
	queue     jobs.Queue
	log       logger.Logger
}

// New creates a notification Service.
// Panics if any dependency is nil.
func New(templates *mail.Templates, customers customer.CustomerRepository, orders order.OrderRepository, queue jobs.Queue, log logger.Logger) *Service {
	if templates == nil {
		panic("notification.New: nil templates")
	}
	if customers == nil {
		panic("notification.New: nil customers")
	}
	if orders == nil {
		panic("notification.New: nil orders")
	}
	if queue == nil {
		panic("notification.New: nil queue")
	}
	if log == nil {
		panic("notification.New: nil log")
	}
	return &Service{
		templates: templates,
		customers: customers,
		orders:    orders,
		queue:     queue,
		log:       log,
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
		s.log.Error("HandleOrderPaid.order_lookup_failed", err, map[string]interface{}{"order_id": data.OrderID})
		return fmt.Errorf("notification: find order %s: %w", data.OrderID, err)
	}
	if o == nil {
		err := fmt.Errorf("notification: order %s not found", data.OrderID)
		s.log.Error("HandleOrderPaid.order_not_found", err, map[string]interface{}{"order_id": data.OrderID})
		return err
	}

	cust, err := s.customers.FindByID(ctx, o.CustomerID)
	if err != nil {
		s.log.Error("HandleOrderPaid.customer_lookup_failed", err, map[string]interface{}{"order_id": data.OrderID, "customer_id": o.CustomerID})
		return fmt.Errorf("notification: find customer %s: %w", o.CustomerID, err)
	}
	if cust == nil {
		err := fmt.Errorf("notification: customer %s not found", o.CustomerID)
		s.log.Error("HandleOrderPaid.customer_not_found", err, map[string]interface{}{"order_id": data.OrderID, "customer_id": o.CustomerID})
		return err
	}

	msg, err := s.templates.Render("order.confirmed", cust.Email, map[string]string{
		"OrderID":   data.OrderID,
		"FirstName": cust.FirstName,
	})
	if err != nil {
		s.log.Error("HandleOrderPaid.template_render_failed", err, map[string]interface{}{"order_id": data.OrderID, "customer_id": o.CustomerID})
		return fmt.Errorf("notification: render template: %w", err)
	}

	payload := map[string]interface{}{
		"to":      msg.To,
		"subject": msg.Subject,
		"body":    msg.Body,
	}

	job, err := jobs.NewJob(id.New(), JobTypeEmailSend, payload)
	if err != nil {
		s.log.Error("HandleOrderPaid.job_create_failed", err, map[string]interface{}{"order_id": data.OrderID})
		return fmt.Errorf("notification: create job: %w", err)
	}

	if err := s.queue.Enqueue(ctx, job); err != nil {
		s.log.Error("HandleOrderPaid.enqueue_failed", err, map[string]interface{}{"order_id": data.OrderID, "job_id": job.ID})
		return fmt.Errorf("notification: enqueue email job %s for order %s: %w", job.ID, data.OrderID, err)
	}

	s.log.Info("HandleOrderPaid.email_enqueued", map[string]interface{}{
		"job_id":      job.ID,
		"job_type":    JobTypeEmailSend,
		"order_id":    data.OrderID,
		"customer_id": o.CustomerID,
	})

	return nil
}
