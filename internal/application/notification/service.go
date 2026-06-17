package notification

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/invoice"
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
	templates   *mail.Templates
	customers   customer.CustomerRepository
	orders      order.OrderRepository
	queue       jobs.Queue
	log         logger.Logger
	resetURL    string                    // base URL for password reset links
	storeURL    string                    // public store URL
	invoices    invoice.InvoiceRepository // optional — for invoice emails
	pdfRenderer invoice.PDFRenderer       // optional — for invoice PDF attachments
}

// Option configures optional Service fields.
type Option func(*Service)

// WithResetBaseURL sets the base URL prepended to reset tokens.
func WithResetBaseURL(u string) Option { return func(s *Service) { s.resetURL = u } }

// WithStoreURL sets the public store URL used in template links.
func WithStoreURL(u string) Option { return func(s *Service) { s.storeURL = u } }

// WithInvoices sets the invoice repository for invoice email lookups.
func WithInvoices(repo invoice.InvoiceRepository) Option {
	return func(s *Service) { s.invoices = repo }
}

// WithPDFRenderer sets the PDF renderer for invoice attachments.
func WithPDFRenderer(r invoice.PDFRenderer) Option {
	return func(s *Service) { s.pdfRenderer = r }
}

// New creates a notification Service.
// Panics if any required dependency is nil.
func New(
	templates *mail.Templates,
	customers customer.CustomerRepository,
	orders order.OrderRepository,
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
		"{{if .Data.FirstName}}<h1>Thank you, {{.Data.FirstName}}!</h1>{{else}}<h1>Thank you for your order!</h1>{{end}}"+
			"<p>Your order <strong>{{.Data.OrderID}}</strong> has been paid and confirmed.</p>")

	t.Register("password_reset",
		"Reset your password",
		"<h1>Password Reset</h1>"+
			"<p>Hi {{.Data.FirstName}},</p>"+
			"<p><a href=\"{{.Data.ResetURL}}\">Reset Password</a></p>"+
			"<p>This link expires in {{.Data.ExpiresIn}}.</p>")

	t.Register("email_verification",
		"Verify your email address",
		"<h1>Verify Your Email</h1>"+
			"<p>Hi {{.Data.FirstName}},</p>"+
			"<p><a href=\"{{.Data.VerifyURL}}\">Verify your account email address</a></p>"+
			"<p>This link is time-limited.</p>")

	t.Register("security_verification",
		"Verify your security access",
		"<h1>Verify Security Access</h1>"+
			"<p>Hi {{.Data.FirstName}},</p>"+
			"<p><a href=\"{{.Data.VerifyURL}}\">Verify access to your account security settings</a></p>"+
			"<p>This link is time-limited and only works while you are signed in.</p>")

	t.Register("email_change_verification",
		"Confirm your new email address",
		"<h1>Confirm Your New Email</h1>"+
			"<p>Hi {{.Data.FirstName}},</p>"+
			"<p>Confirm <strong>{{.Data.NewEmail}}</strong> as the new email for your account.</p>"+
			"<p><a href=\"{{.Data.VerifyURL}}\">Confirm new email address</a></p>"+
			"<p>This link is time-limited. Your current email keeps working until you confirm.</p>")

	t.Register("email_change_notice",
		"A change to your account email was requested",
		"<h1>Email Change Requested</h1>"+
			"<p>Hi {{.Data.FirstName}},</p>"+
			"<p>Someone requested to change your account email to <strong>{{.Data.NewEmail}}</strong>.</p>"+
			"<p>The change only takes effect after the new address is confirmed. "+
			"If this wasn't you, change your password immediately.</p>")

	t.Register("order_shipped",
		"Order {{.Data.OrderID}} — Shipped",
		"<h1>Your order is on its way!</h1>"+
			"<p>Your order <strong>{{.Data.OrderID}}</strong> has been shipped.</p>"+
			"{{if .Data.TrackingNumber}}<p>Tracking: {{.Data.TrackingNumber}}</p>{{end}}")

	t.Register("invoice_created",
		"Invoice #{{.Data.InvoiceNumber}} for Order {{.Data.OrderID}}",
		"<h1>Your Invoice</h1>"+
			"<p>Hi{{if .Data.FirstName}} {{.Data.FirstName}}{{end}},</p>"+
			"<p>Invoice <strong>#{{.Data.InvoiceNumber}}</strong> for order "+
			"<strong>{{.Data.OrderID}}</strong> is attached as a PDF.</p>"+
			"<p>Total: <strong>{{.Data.Total}}</strong></p>")
}

// orderRecipient resolves the notification recipient for an order.
// Authenticated orders resolve via the customer record; guest orders
// (empty CustomerID) fall back to the order's contact email.
type orderRecipient struct {
	Email     string
	FirstName string
	Guest     bool
}

func (s *Service) resolveOrderRecipient(ctx context.Context, o *order.Order, handler string) (orderRecipient, error) {
	if o.CustomerID == "" {
		contact := strings.TrimSpace(o.ContactEmail)
		if contact == "" {
			err := fmt.Errorf("notification: guest order %s has no contact email", o.ID)
			s.log.Error(handler+".guest_contact_email_missing", err, map[string]interface{}{"order_id": o.ID})
			return orderRecipient{}, err
		}
		return orderRecipient{Email: contact, Guest: true}, nil
	}

	cust, err := s.customers.FindByID(ctx, o.CustomerID)
	if err != nil {
		s.log.Error(handler+".customer_lookup_failed", err, map[string]interface{}{"order_id": o.ID, "customer_id": o.CustomerID})
		return orderRecipient{}, fmt.Errorf("notification: find customer %s: %w", o.CustomerID, err)
	}
	if cust == nil {
		err := fmt.Errorf("notification: customer %s not found", o.CustomerID)
		s.log.Error(handler+".customer_not_found", err, map[string]interface{}{"order_id": o.ID, "customer_id": o.CustomerID})
		return orderRecipient{}, err
	}
	return orderRecipient{Email: cust.Email, FirstName: cust.FirstName}, nil
}

// HandleOrderPaid is an event handler for order.paid.
// It resolves the recipient (customer or guest contact email), renders the
// confirmation template, and enqueues an email.send job.
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

	recipient, err := s.resolveOrderRecipient(ctx, o, "HandleOrderPaid")
	if err != nil {
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
			"FirstName": recipient.FirstName,
			"Guest":     recipient.Guest,
			"Items":     tmplItems,
			"Total":     o.TotalAmount.String(),
		},
	}

	msg, err := s.templates.Render("order_confirmed", recipient.Email, ed)
	if err != nil {
		s.log.Error("HandleOrderPaid.template_render_failed", err, map[string]interface{}{"order_id": data.OrderID, "customer_id": o.CustomerID})
		return fmt.Errorf("notification: render template: %w", err)
	}

	return s.enqueueEmail(ctx, msg, "HandleOrderPaid", map[string]interface{}{
		"order_id":    data.OrderID,
		"customer_id": o.CustomerID,
		"guest":       recipient.Guest,
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

	resetURL, err := url.Parse(s.resetURL)
	if err != nil {
		s.log.Error("HandlePasswordReset.invalid_reset_url", err, map[string]interface{}{"customer_id": data.CustomerID})
		return fmt.Errorf("notification: parse reset URL: %w", err)
	}
	q := resetURL.Query()
	q.Set("token", data.Token)
	resetURL.RawQuery = q.Encode()

	ed := mail.EmailData{
		StoreURL: s.storeURL,
		Data: map[string]interface{}{
			"FirstName": cust.FirstName,
			"ResetURL":  resetURL.String(),
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

// HandleEmailVerification is an event handler for
// customer.email_verification.requested.
func (s *Service) HandleEmailVerification(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(customer.EmailVerificationRequestedData)
	if !ok {
		return fmt.Errorf("notification: unexpected event data type %T", evt.Data)
	}

	cust, err := s.customers.FindByID(ctx, data.CustomerID)
	if err != nil {
		s.log.Error("HandleEmailVerification.customer_lookup_failed", err, map[string]interface{}{"customer_id": data.CustomerID})
		return fmt.Errorf("notification: find customer %s: %w", data.CustomerID, err)
	}
	if cust == nil {
		err := fmt.Errorf("notification: customer %s not found", data.CustomerID)
		s.log.Error("HandleEmailVerification.customer_not_found", err, map[string]interface{}{"customer_id": data.CustomerID})
		return err
	}
	verifyURL := strings.TrimSpace(data.VerifyURL)
	parsedVerifyURL, err := url.Parse(verifyURL)
	if verifyURL == "" || err != nil || !parsedVerifyURL.IsAbs() || strings.TrimSpace(parsedVerifyURL.Host) == "" {
		return fmt.Errorf("notification: invalid verify url for customer %s", data.CustomerID)
	}

	ed := mail.EmailData{
		StoreURL: s.storeURL,
		Data: map[string]interface{}{
			"FirstName": cust.FirstName,
			"VerifyURL": verifyURL,
		},
	}

	msg, err := s.templates.Render("email_verification", cust.Email, ed)
	if err != nil {
		s.log.Error("HandleEmailVerification.template_render_failed", err, map[string]interface{}{"customer_id": data.CustomerID})
		return fmt.Errorf("notification: render template: %w", err)
	}

	return s.enqueueEmail(ctx, msg, "HandleEmailVerification", map[string]interface{}{
		"customer_id": data.CustomerID,
	})
}

// HandleSecurityVerification is an event handler for
// customer.security_verification.requested.
func (s *Service) HandleSecurityVerification(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(customer.SecurityVerificationRequestedData)
	if !ok {
		return fmt.Errorf("notification: unexpected event data type %T", evt.Data)
	}

	cust, err := s.customers.FindByID(ctx, data.CustomerID)
	if err != nil {
		s.log.Error("HandleSecurityVerification.customer_lookup_failed", err, map[string]interface{}{"customer_id": data.CustomerID})
		return fmt.Errorf("notification: find customer %s: %w", data.CustomerID, err)
	}
	if cust == nil {
		err := fmt.Errorf("notification: customer %s not found", data.CustomerID)
		s.log.Error("HandleSecurityVerification.customer_not_found", err, map[string]interface{}{"customer_id": data.CustomerID})
		return err
	}

	ed := mail.EmailData{
		StoreURL: s.storeURL,
		Data: map[string]interface{}{
			"FirstName": cust.FirstName,
			"VerifyURL": data.VerifyURL,
		},
	}

	msg, err := s.templates.Render("security_verification", cust.Email, ed)
	if err != nil {
		s.log.Error("HandleSecurityVerification.template_render_failed", err, map[string]interface{}{"customer_id": data.CustomerID})
		return fmt.Errorf("notification: render template: %w", err)
	}

	return s.enqueueEmail(ctx, msg, "HandleSecurityVerification", map[string]interface{}{
		"customer_id": data.CustomerID,
	})
}

// HandleEmailChangeRequested is an event handler for
// customer.email_change.requested. The verification link is delivered to the
// NEW address the customer is switching to (carried in the event payload),
// not the account's current email.
func (s *Service) HandleEmailChangeRequested(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(customer.EmailChangeRequestedData)
	if !ok {
		return fmt.Errorf("notification: unexpected event data type %T", evt.Data)
	}

	newEmail := strings.TrimSpace(data.NewEmail)
	if newEmail == "" {
		return fmt.Errorf("notification: email change for customer %s has no new email", data.CustomerID)
	}
	verifyURL := strings.TrimSpace(data.VerifyURL)
	parsedVerifyURL, err := url.Parse(verifyURL)
	if verifyURL == "" || err != nil || !parsedVerifyURL.IsAbs() || strings.TrimSpace(parsedVerifyURL.Host) == "" {
		return fmt.Errorf("notification: invalid verify url for customer %s", data.CustomerID)
	}

	// Best-effort personalization: the address and link come from the event, so a
	// lookup failure should not block delivery of the verification email.
	firstName := ""
	if cust, err := s.customers.FindByID(ctx, data.CustomerID); err != nil {
		s.log.Warn("HandleEmailChangeRequested.customer_lookup_failed", map[string]interface{}{"customer_id": data.CustomerID, "error": err.Error()})
	} else if cust != nil {
		firstName = cust.FirstName
	}

	ed := mail.EmailData{
		StoreURL: s.storeURL,
		Data: map[string]interface{}{
			"FirstName": firstName,
			"VerifyURL": verifyURL,
			"NewEmail":  newEmail,
		},
	}

	msg, err := s.templates.Render("email_change_verification", newEmail, ed)
	if err != nil {
		s.log.Error("HandleEmailChangeRequested.template_render_failed", err, map[string]interface{}{"customer_id": data.CustomerID})
		return fmt.Errorf("notification: render template: %w", err)
	}

	return s.enqueueEmail(ctx, msg, "HandleEmailChangeRequested", map[string]interface{}{
		"customer_id": data.CustomerID,
	})
}

// HandleEmailChangeNotified is an event handler for
// customer.email_change.notified. It alerts the current (old) address that a
// change to a different address was requested.
func (s *Service) HandleEmailChangeNotified(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(customer.EmailChangeNotifiedData)
	if !ok {
		return fmt.Errorf("notification: unexpected event data type %T", evt.Data)
	}

	oldEmail := strings.TrimSpace(data.OldEmail)
	if oldEmail == "" {
		return fmt.Errorf("notification: email change notice for customer %s has no old email", data.CustomerID)
	}

	// Best-effort personalization: the old address comes from the event, so a
	// lookup failure should not block delivery of the security notice.
	firstName := ""
	if cust, err := s.customers.FindByID(ctx, data.CustomerID); err != nil {
		s.log.Warn("HandleEmailChangeNotified.customer_lookup_failed", map[string]interface{}{"customer_id": data.CustomerID, "error": err.Error()})
	} else if cust != nil {
		firstName = cust.FirstName
	}

	ed := mail.EmailData{
		StoreURL: s.storeURL,
		Data: map[string]interface{}{
			"FirstName": firstName,
			"NewEmail":  strings.TrimSpace(data.NewEmail),
		},
	}

	msg, err := s.templates.Render("email_change_notice", oldEmail, ed)
	if err != nil {
		s.log.Error("HandleEmailChangeNotified.template_render_failed", err, map[string]interface{}{"customer_id": data.CustomerID})
		return fmt.Errorf("notification: render template: %w", err)
	}

	return s.enqueueEmail(ctx, msg, "HandleEmailChangeNotified", map[string]interface{}{
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

	recipient, err := s.resolveOrderRecipient(ctx, o, "HandleShipmentShipped")
	if err != nil {
		return err
	}

	ed := mail.EmailData{
		StoreURL: s.storeURL,
		Data: map[string]interface{}{
			"OrderID":   data.OrderID,
			"FirstName": recipient.FirstName,
			"Guest":     recipient.Guest,
		},
	}
	if data.TrackingNumber != "" {
		ed.Data["TrackingNumber"] = data.TrackingNumber
	}
	if data.ProviderRef != "" {
		ed.Data["Carrier"] = data.ProviderRef
	}

	msg, err := s.templates.Render("order_shipped", recipient.Email, ed)
	if err != nil {
		s.log.Error("HandleShipmentShipped.template_render_failed", err, map[string]interface{}{"order_id": data.OrderID, "shipment_id": data.ShipmentID})
		return fmt.Errorf("notification: render template: %w", err)
	}

	return s.enqueueEmail(ctx, msg, "HandleShipmentShipped", map[string]interface{}{
		"order_id":    data.OrderID,
		"shipment_id": data.ShipmentID,
		"customer_id": o.CustomerID,
		"guest":       recipient.Guest,
	})
}

// HandleInvoiceCreated is an event handler for invoice.created.
// It renders the invoice email template, attaches the PDF, and enqueues a job.
func (s *Service) HandleInvoiceCreated(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(invoice.InvoiceCreatedData)
	if !ok {
		return fmt.Errorf("notification: unexpected event data type %T", evt.Data)
	}

	if s.invoices == nil || s.pdfRenderer == nil {
		s.log.Warn("HandleInvoiceCreated.skipped_no_invoice_deps", map[string]interface{}{
			"invoice_id": data.InvoiceID,
		})
		return nil
	}

	inv, err := s.invoices.FindByID(ctx, data.InvoiceID)
	if err != nil {
		s.log.Error("HandleInvoiceCreated.invoice_lookup_failed", err, map[string]interface{}{"invoice_id": data.InvoiceID})
		return fmt.Errorf("notification: find invoice %s: %w", data.InvoiceID, err)
	}
	if inv == nil {
		err := fmt.Errorf("notification: invoice %s not found", data.InvoiceID)
		s.log.Error("HandleInvoiceCreated.invoice_not_found", err, map[string]interface{}{"invoice_id": data.InvoiceID})
		return err
	}

	if inv.CustomerID() != data.CustomerID {
		err := fmt.Errorf("notification: invoice %s customer %s does not match event customer %s", data.InvoiceID, inv.CustomerID(), data.CustomerID)
		s.log.Error("HandleInvoiceCreated.customer_mismatch", err, map[string]interface{}{
			"invoice_id":       data.InvoiceID,
			"invoice_customer": inv.CustomerID(),
			"event_customer":   data.CustomerID,
		})
		return err
	}
	if inv.OrderID() != data.OrderID {
		err := fmt.Errorf("notification: invoice %s order %s does not match event order %s", data.InvoiceID, inv.OrderID(), data.OrderID)
		s.log.Error("HandleInvoiceCreated.order_mismatch", err, map[string]interface{}{
			"invoice_id":    data.InvoiceID,
			"invoice_order": inv.OrderID(),
			"event_order":   data.OrderID,
		})
		return err
	}

	o, err := s.orders.FindByID(ctx, inv.OrderID())
	if err != nil {
		s.log.Error("HandleInvoiceCreated.order_lookup_failed", err, map[string]interface{}{"invoice_id": data.InvoiceID, "order_id": inv.OrderID()})
		return fmt.Errorf("notification: find order %s: %w", inv.OrderID(), err)
	}
	if o == nil {
		err := fmt.Errorf("notification: order %s not found", inv.OrderID())
		s.log.Error("HandleInvoiceCreated.order_not_found", err, map[string]interface{}{"invoice_id": data.InvoiceID, "order_id": inv.OrderID()})
		return err
	}

	recipient, err := s.resolveOrderRecipient(ctx, o, "HandleInvoiceCreated")
	if err != nil {
		return err
	}

	pdfBytes, err := s.pdfRenderer.Render(*inv)
	if err != nil {
		s.log.Error("HandleInvoiceCreated.pdf_render_failed", err, map[string]interface{}{"invoice_id": data.InvoiceID})
		return fmt.Errorf("notification: render invoice PDF: %w", err)
	}

	items := inv.Items()
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
			"OrderID":       data.OrderID,
			"InvoiceNumber": data.InvoiceNumber,
			"FirstName":     recipient.FirstName,
			"Guest":         recipient.Guest,
			"Items":         tmplItems,
			"Total":         inv.TotalAmount().String(),
		},
	}

	msg, err := s.templates.Render("invoice_created", recipient.Email, ed)
	if err != nil {
		s.log.Error("HandleInvoiceCreated.template_render_failed", err, map[string]interface{}{"invoice_id": data.InvoiceID})
		return fmt.Errorf("notification: render template: %w", err)
	}

	msg.Attachments = []mail.Attachment{{
		Filename:    fmt.Sprintf("invoice-%d.pdf", data.InvoiceNumber),
		ContentType: "application/pdf",
		Data:        pdfBytes,
	}}

	return s.enqueueEmail(ctx, msg, "HandleInvoiceCreated", map[string]interface{}{
		"invoice_id":  data.InvoiceID,
		"order_id":    data.OrderID,
		"customer_id": data.CustomerID,
		"guest":       recipient.Guest,
	})
}

// enqueueEmail creates and enqueues an email.send job.
func (s *Service) enqueueEmail(ctx context.Context, msg mail.Message, handler string, logFields map[string]interface{}) error {
	payload := map[string]interface{}{
		"to":      msg.To,
		"subject": msg.Subject,
		"body":    msg.Body,
	}
	if len(msg.Attachments) > 0 {
		atts := make([]map[string]interface{}, len(msg.Attachments))
		for i, a := range msg.Attachments {
			atts[i] = map[string]interface{}{
				"filename":     a.Filename,
				"content_type": a.ContentType,
				"data":         base64.StdEncoding.EncodeToString(a.Data),
			}
		}
		payload["attachments"] = atts
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
