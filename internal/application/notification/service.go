package notification

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// JobTypeEmailSend is the job type for email delivery.
const JobTypeEmailSend = "email.send"

// Service wires order/shipping/customer events to email notifications
// via the job queue.
type Service struct {
	templates *mail.Templates
	customers customer.CustomerRepository
	orders    order.OrderRepository
	shipments shipping.ShipmentRepository
	queue     jobs.Queue
	log       logger.Logger
	resetURL  string // base URL for password reset links
	storeURL  string // public store URL
}

// Option configures optional Service fields.
type Option func(*Service)

// WithResetBaseURL sets the base URL prepended to reset tokens.
func WithResetBaseURL(u string) Option { return func(s *Service) { s.resetURL = u } }

// WithStoreURL sets the public store URL used in template links.
func WithStoreURL(u string) Option { return func(s *Service) { s.storeURL = u } }

// New creates a notification Service.
// Panics if any required dependency is nil.
func New(
	templates *mail.Templates,
	customers customer.CustomerRepository,
	orders order.OrderRepository,
	shipments shipping.ShipmentRepository,
	queue jobs.Queue,
	log logger.Logger,
	opts ...Option,
) *Service {
	if templates == nil {
		panic("notification.New: nil templates")
	}
	if customers == nil {
		panic("notification.New: nil customers")
	}
	if orders == nil {
		panic("notification.New: nil orders")
	}
	if shipments == nil {
		panic("notification.New: nil shipments")
	}
	if queue == nil {
		panic("notification.New: nil queue")
	}
	if log == nil {
		panic("notification.New: nil log")
	}
	s := &Service{
		templates: templates,
		customers: customers,
		orders:    orders,
		shipments: shipments,
		queue:     queue,
		log:       log,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// RegisterTemplates registers built-in fallback templates.
// File-based templates loaded via LoadDir overwrite these.
func RegisterTemplates(t *mail.Templates) {
	t.Register("order_confirmed",
		"Order {{.Data.OrderID}} — Confirmation",
		"<h1>Thank you, {{.Data.FirstName}}!</h1>"+
			"<p>Your order <strong>{{.Data.OrderID}}</strong> has been paid and confirmed.</p>")

	t.Register("password_reset",
		"Reset your password",
		"<h1>Password Reset</h1>"+
			"<p>Hi {{.Data.FirstName}},</p>"+
			"<p><a href=\"{{.Data.ResetURL}}\">Reset Password</a></p>"+
			"<p>This link expires in {{.Data.ExpiresIn}}.</p>")

	t.Register("order_shipped",
		"Order {{.Data.OrderID}} — Shipped",
		"<h1>Your order is on its way!</h1>"+
			"<p>Your order <strong>{{.Data.OrderID}}</strong> has been shipped.</p>"+
			"{{if .Data.TrackingNumber}}<p>Tracking: {{.Data.TrackingNumber}}</p>{{end}}")
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

	items := o.Items()
	tmplItems := make([]map[string]interface{}, len(items))
	for i, it := range items {
		tmplItems[i] = map[string]interface{}{
			"Name":  it.Name,
			"Qty":   it.Quantity,
			"Price": it.UnitPrice.String(),
		}
	}

	ed := mail.EmailData{
		StoreURL: s.storeURL,
		Data: map[string]interface{}{
			"OrderID":   data.OrderID,
			"FirstName": cust.FirstName,
			"Items":     tmplItems,
			"Total":     o.TotalAmount.String(),
		},
	}

	msg, err := s.templates.Render("order_confirmed", cust.Email, ed)
	if err != nil {
		s.log.Error("HandleOrderPaid.template_render_failed", err, map[string]interface{}{"order_id": data.OrderID, "customer_id": o.CustomerID})
		return fmt.Errorf("notification: render template: %w", err)
	}

	return s.enqueueEmail(ctx, msg, "HandleOrderPaid", map[string]interface{}{
		"order_id":    data.OrderID,
		"customer_id": o.CustomerID,
	})
}

// HandlePasswordReset is an event handler for customer.password_reset.requested.
func (s *Service) HandlePasswordReset(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(customer.PasswordResetRequestedData)
	if !ok {
		return fmt.Errorf("notification: unexpected event data type %T", evt.Data)
	}

	cust, err := s.customers.FindByID(ctx, data.CustomerID)
	if err != nil {
		s.log.Error("HandlePasswordReset.customer_lookup_failed", err, map[string]interface{}{"customer_id": data.CustomerID})
		return fmt.Errorf("notification: find customer %s: %w", data.CustomerID, err)
	}
	if cust == nil {
		err := fmt.Errorf("notification: customer %s not found", data.CustomerID)
		s.log.Error("HandlePasswordReset.customer_not_found", err, map[string]interface{}{"customer_id": data.CustomerID})
		return err
	}

	resetURL := s.resetURL + "?token=" + data.Token
	ed := mail.EmailData{
		StoreURL: s.storeURL,
		Data: map[string]interface{}{
			"FirstName": cust.FirstName,
			"ResetURL":  resetURL,
			"ExpiresIn": "1 hour",
		},
	}

	msg, err := s.templates.Render("password_reset", cust.Email, ed)
	if err != nil {
		s.log.Error("HandlePasswordReset.template_render_failed", err, map[string]interface{}{"customer_id": data.CustomerID})
		return fmt.Errorf("notification: render template: %w", err)
	}

	return s.enqueueEmail(ctx, msg, "HandlePasswordReset", map[string]interface{}{
		"customer_id": data.CustomerID,
	})
}

// HandleShipmentShipped is an event handler for shipment.shipped.
func (s *Service) HandleShipmentShipped(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(shipping.ShipmentStatusChangedData)
	if !ok {
		return fmt.Errorf("notification: unexpected event data type %T", evt.Data)
	}

	o, err := s.orders.FindByID(ctx, data.OrderID)
	if err != nil {
		s.log.Error("HandleShipmentShipped.order_lookup_failed", err, map[string]interface{}{"order_id": data.OrderID, "shipment_id": data.ShipmentID})
		return fmt.Errorf("notification: find order %s: %w", data.OrderID, err)
	}
	if o == nil {
		err := fmt.Errorf("notification: order %s not found", data.OrderID)
		s.log.Error("HandleShipmentShipped.order_not_found", err, map[string]interface{}{"order_id": data.OrderID})
		return err
	}

	cust, err := s.customers.FindByID(ctx, o.CustomerID)
	if err != nil {
		s.log.Error("HandleShipmentShipped.customer_lookup_failed", err, map[string]interface{}{"order_id": data.OrderID, "customer_id": o.CustomerID})
		return fmt.Errorf("notification: find customer %s: %w", o.CustomerID, err)
	}
	if cust == nil {
		err := fmt.Errorf("notification: customer %s not found", o.CustomerID)
		s.log.Error("HandleShipmentShipped.customer_not_found", err, map[string]interface{}{"order_id": data.OrderID, "customer_id": o.CustomerID})
		return err
	}

	ed := mail.EmailData{
		StoreURL: s.storeURL,
		Data: map[string]interface{}{
			"OrderID":   data.OrderID,
			"FirstName": cust.FirstName,
		},
	}
	if data.TrackingNumber != "" {
		ed.Data["TrackingNumber"] = data.TrackingNumber
	}
	if data.ProviderRef != "" {
		ed.Data["Carrier"] = data.ProviderRef
	}

	msg, err := s.templates.Render("order_shipped", cust.Email, ed)
	if err != nil {
		s.log.Error("HandleShipmentShipped.template_render_failed", err, map[string]interface{}{"order_id": data.OrderID, "shipment_id": data.ShipmentID})
		return fmt.Errorf("notification: render template: %w", err)
	}

	return s.enqueueEmail(ctx, msg, "HandleShipmentShipped", map[string]interface{}{
		"order_id":    data.OrderID,
		"shipment_id": data.ShipmentID,
		"customer_id": o.CustomerID,
	})
}

// enqueueEmail creates and enqueues an email.send job.
func (s *Service) enqueueEmail(ctx context.Context, msg mail.Message, handler string, logFields map[string]interface{}) error {
	payload := map[string]interface{}{
		"to":      msg.To,
		"subject": msg.Subject,
		"body":    msg.Body,
	}

	job, err := jobs.NewJob(id.New(), JobTypeEmailSend, payload)
	if err != nil {
		s.log.Error(handler+".job_create_failed", err, logFields)
		return fmt.Errorf("notification: create job: %w", err)
	}

	if err := s.queue.Enqueue(ctx, job); err != nil {
		logFields["job_id"] = job.ID
		s.log.Error(handler+".enqueue_failed", err, logFields)
		return fmt.Errorf("notification: enqueue email job %s: %w", job.ID, err)
	}

	logFields["job_id"] = job.ID
	logFields["job_type"] = JobTypeEmailSend
	s.log.Info(handler+".email_enqueued", logFields)
	return nil
}
