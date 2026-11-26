package notification

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/application/hooks"
	storeidentity "github.com/akarso/shopanda/internal/application/storeidentity"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/invoice"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/legal"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/domain/translation"
	domainvoucher "github.com/akarso/shopanda/internal/domain/voucher"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// JobTypeEmailSend is the job type for email delivery.
const JobTypeEmailSend = "email.send"

// KindOrderConfirmed is the notification_dedupe kind for paid-order emails.
const KindOrderConfirmed = "order_confirmed"

// KindOrderShipped is the notification_dedupe kind for shipment.shipped emails.
const KindOrderShipped = "order_shipped"

// KindVoucherIssued is the notification_dedupe kind for voucher.issued emails.
const KindVoucherIssued = "voucher_issued"

// KindInvoiceCreated is the notification_dedupe kind for invoice.created emails.
const KindInvoiceCreated = "invoice_created"

// EmailDedupeStore claims one send per (subject_key, kind) so confirmation
// dedupe does not depend on queue-driver job-id primary-key behavior.
type EmailDedupeStore interface {
	// TryInsert records a pending (subjectKey, kind). inserted is true when newly claimed.
	TryInsert(ctx context.Context, subjectKey, kind string) (inserted bool, err error)
	// MarkCompleted makes a successful claim permanent (not reclaimable).
	MarkCompleted(ctx context.Context, subjectKey, kind string) error
	// Delete removes a claim so a later attempt can retry after enqueue failure.
	Delete(ctx context.Context, subjectKey, kind string) error
}

// Service wires order/shipping/customer events to email notifications
// via the job queue.
type Service struct {
	templates           *mail.Templates
	customers           customer.CustomerRepository
	orders              order.OrderRepository
	queue               jobs.Queue
	dedupe              EmailDedupeStore
	log                 logger.Logger
	resetURL            string                    // base URL for password reset links
	storeURL            string                    // public store URL
	invoices            invoice.InvoiceRepository // optional — for invoice emails
	pdfRenderer         invoice.PDFRenderer       // optional — for invoice PDF attachments
	extensions          extensionapp.ValueAPI     // optional — public order_item values on confirmation lines
	hooks               *hooks.Registry           // optional — order.line.display enrich
	fallbackLanguage    string                    // static fallback when lookup unset (tests)
	storeLanguageLookup func(context.Context) (string, error)
	vouchers            domainvoucher.Repository
	voucherPDF          domainvoucher.PDFRenderer
	identity            *storeidentity.Service
	stores              store.StoreRepository
	config              legal.ConfigGetter    // optional — interim store.logo for LogoURL
	invoicePDF          media.ReadableStorage // optional — prefer sealed stored PDF over re-render
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

// WithEmailDedupe sets the persistent send claim store used by
// enqueueEmailOnce when a subject/kind key is provided (order confirmation,
// order shipped).
func WithEmailDedupe(store EmailDedupeStore) Option {
	return func(s *Service) { s.dedupe = store }
}

// WithOrderExtensions attaches public order_item extension values onto
// order_confirmed line maps (plus VariantID/SKU on every line). Private
// fields stay omitted. Nil is a no-op for extensions; VariantID/SKU still
// come from the order.
func WithOrderExtensions(api extensionapp.ValueAPI) Option {
	return func(s *Service) { s.extensions = api }
}

// WithOrderLineHooks wires the hook registry for order.line.display enrich
// (derived fields such as signed thumbnail URLs). Nil is a no-op.
func WithOrderLineHooks(reg *hooks.Registry) Option {
	return func(s *Service) { s.hooks = reg }
}

// WithFallbackLanguage sets a static BCP 47 tag used when the order has no
// stored language and no store lookup is configured (unit tests). Empty falls
// through to "en" in DisplayLanguage.
func WithFallbackLanguage(lang string) Option {
	return func(s *Service) { s.fallbackLanguage = strings.TrimSpace(lang) }
}

// WithStoreLanguageLookup resolves the default store language at email-build
// time (legacy orders with NULL language). Prefer this over a boot-time string
// so admin language changes apply without restart. Lookup errors are logged
// and the static fallback (if any) is used.
func WithStoreLanguageLookup(fn func(context.Context) (string, error)) Option {
	return func(s *Service) { s.storeLanguageLookup = fn }
}

// WithVouchers wires voucher lookup for voucher.issued emails.
func WithVouchers(repo domainvoucher.Repository) Option {
	return func(s *Service) { s.vouchers = repo }
}

// WithVoucherPDF wires the voucher pack PDF renderer for email attachments.
func WithVoucherPDF(r domainvoucher.PDFRenderer) Option {
	return func(s *Service) { s.voucherPDF = r }
}

// WithStoreIdentity fills EmailData brand fields from store identity (PR-1205).
// Nil is a no-op. Invoice PDF seller comes from the sealed invoice snapshot.
func WithStoreIdentity(svc *storeidentity.Service, stores store.StoreRepository) Option {
	return func(s *Service) {
		s.identity = svc
		s.stores = stores
	}
}

// WithConfig enables interim LogoURL from store.logo until Site Identity (PR-1222).
func WithConfig(cfg legal.ConfigGetter) Option {
	return func(s *Service) { s.config = cfg }
}

// WithInvoicePDFStorage prefers the sealed stored PDF for invoice emails.
func WithInvoicePDFStorage(store media.ReadableStorage) Option {
	return func(s *Service) { s.invoicePDF = store }
}

// baseEmailData starts every template payload with StoreURL plus brand fields
// from the default store's identity (best-effort).
func (s *Service) baseEmailData(ctx context.Context) mail.EmailData {
	if s == nil {
		return mail.EmailData{}
	}
	ed := mail.EmailData{StoreURL: s.storeURL}
	storeID, storeName := s.resolveDefaultStore(ctx)
	ed.StoreName = storeName
	if s.identity != nil && storeID != "" {
		addr, err := s.identity.FormattedGeneralAddress(ctx, storeID)
		if err != nil {
			s.log.Warn("notification: identity address failed", map[string]interface{}{
				"store_id": storeID,
				"error":    err.Error(),
			})
		} else {
			ed.StoreAddress = addr
		}
	}
	ed.LogoURL = s.logoURL(ctx, storeID)
	return ed
}

func (s *Service) resolveDefaultStore(ctx context.Context) (storeID, storeName string) {
	if s == nil || s.stores == nil {
		return "", ""
	}
	st, err := s.stores.FindDefault(ctx)
	if err != nil {
		s.log.Warn("notification: default store lookup failed", map[string]interface{}{
			"error": err.Error(),
		})
		return "", ""
	}
	if st == nil {
		return "", ""
	}
	return st.ID, strings.TrimSpace(st.Name)
}

func (s *Service) logoURL(ctx context.Context, storeID string) string {
	if s == nil || s.config == nil {
		return ""
	}
	read := func(key string) string {
		v, err := s.config.Get(ctx, key)
		if err != nil || v == nil {
			return ""
		}
		t, ok := v.(string)
		if !ok {
			return ""
		}
		return strings.TrimSpace(t)
	}
	if storeID != "" {
		if u := read(legal.ScopedConfigKey(storeID, "store.logo")); u != "" {
			return u
		}
	}
	return read("store.logo")
}

// invoicePDFBytes prefers the sealed object in storage; otherwise re-renders
// from the invoice's seller snapshot (never live identity).
func (s *Service) invoicePDFBytes(ctx context.Context, inv invoice.Invoice) ([]byte, error) {
	pdfPath := fmt.Sprintf("invoices/%s/invoice-%d.pdf", inv.ID(), inv.InvoiceNumber())
	if s.invoicePDF != nil {
		data, err := s.invoicePDF.Read(ctx, pdfPath)
		if err == nil && len(data) > 0 {
			return data, nil
		}
		if err != nil && !media.IsNotFound(err) {
			return nil, fmt.Errorf("read stored invoice pdf: %w", err)
		}
	}
	if s.pdfRenderer == nil {
		return nil, fmt.Errorf("invoice pdf renderer not configured")
	}
	return s.pdfRenderer.Render(inv, inv.Seller())
}

// orderDisplayLanguage resolves the sealed hook language once per email.
// Lookup runs only when the order has no usable primary language; a failed
// lookup keeps the static fallback for every line (no per-line retry).
func (s *Service) orderDisplayLanguage(ctx context.Context, o *order.Order) string {
	if s == nil {
		return translation.DisplayLanguage("", "")
	}
	orderLang := ""
	orderID := ""
	if o != nil {
		orderLang = o.Language
		orderID = o.ID
	}
	if translation.PrimaryLanguage(orderLang) != "" {
		return translation.DisplayLanguage(orderLang, "")
	}
	fallback := s.fallbackLanguage
	if s.storeLanguageLookup != nil {
		lang, err := s.storeLanguageLookup(ctx)
		if err != nil {
			fields := map[string]interface{}{}
			if orderID != "" {
				fields["order_id"] = orderID
			}
			s.log.Error("HandleOrderPaid.store_language_failed", err, fields)
		} else if strings.TrimSpace(lang) != "" {
			fallback = lang
		}
	}
	return translation.DisplayLanguage(orderLang, fallback)
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

	t.Register("voucher_issued",
		"Your vouchers for order {{.Data.OrderID}}",
		"{{if .Data.FirstName}}<h1>Hi {{.Data.FirstName}},</h1>{{else}}<h1>Your vouchers</h1>{{end}}"+
			"<p>Vouchers for order <strong>{{.Data.OrderID}}</strong> are attached as a PDF.</p>"+
			"<ul>{{range .Data.Codes}}<li><code>{{.}}</code></li>{{end}}</ul>")

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

	t.Register("cart_recovery",
		"You left items in your cart",
		"<h1>Complete your order</h1>"+
			"{{if .Data.FirstName}}<p>Hi {{.Data.FirstName}},</p>{{end}}"+
			"<p>You still have {{.Data.ItemCount}} item(s) waiting in your cart.</p>"+
			"<ul>{{range .Data.Items}}<li>{{.Name}} × {{.Quantity}} — {{.Price}}</li>{{end}}</ul>"+
			"<p><a href=\"{{.Data.CartURL}}\">Return to your cart</a></p>")
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

	tmplItems := s.orderConfirmedItems(ctx, o)

	ed := s.baseEmailData(ctx)
	ed.Data = map[string]interface{}{
		"OrderID":   data.OrderID,
		"FirstName": recipient.FirstName,
		"Guest":     recipient.Guest,
		"Items":     tmplItems,
		"Total":     o.TotalAmount.String(),
	}

	msg, err := s.templates.Render("order_confirmed", recipient.Email, ed)
	if err != nil {
		s.log.Error("HandleOrderPaid.template_render_failed", err, map[string]interface{}{"order_id": data.OrderID, "customer_id": o.CustomerID})
		return fmt.Errorf("notification: render template: %w", err)
	}

	// Persistent dedupe (notification_dedupe) is primary so SQS/RabbitMQ
	// order.paid republish cannot double-send; stable job id remains secondary.
	return s.enqueueEmailOnce(ctx, msg, orderConfirmedJobID(data.OrderID), "HandleOrderPaid", map[string]interface{}{
		"order_id":    data.OrderID,
		"customer_id": o.CustomerID,
		"guest":       recipient.Guest,
	}, data.OrderID, KindOrderConfirmed)
}

// orderConfirmedItems builds order_confirmed line maps: Name/Qty/Price plus
// VariantID/SKU. When ValueAPI is wired, public Extensions (code→value for
// product overrides) and ExtensionsList (Label/Kind/Value/Alt) are attached.
// order.line.display enrich (when hooks wired) appends derived fields.
// Extension lookup and enrich-handler failures are logged and skipped so
// confirmation still enqueues.
func (s *Service) orderConfirmedItems(ctx context.Context, o *order.Order) []map[string]interface{} {
	items := o.Items()
	tmplItems := make([]map[string]interface{}, len(items))
	variantIDs := make([]string, len(items))
	for i, it := range items {
		variantIDs[i] = it.VariantID
		tmplItems[i] = map[string]interface{}{
			"Name":      it.Name,
			"Qty":       it.Quantity,
			"Price":     it.UnitPrice.String(),
			"VariantID": it.VariantID,
			"SKU":       it.SKU,
		}
	}
	if s == nil || len(items) == 0 {
		return tmplItems
	}
	var byVariant map[string][]extensionapp.APIValueItem
	var reg *extensionapp.Registry
	if s.extensions != nil {
		var err error
		byVariant, err = extensionapp.PublicOrderItemAPIByVariant(ctx, s.extensions, o.ID, variantIDs, func(code string, fieldErr error) {
			s.log.Error("HandleOrderPaid.order_item_extension_field_failed", fieldErr, map[string]interface{}{
				"order_id":   o.ID,
				"field_code": code,
			})
		})
		if err != nil {
			s.log.Error("HandleOrderPaid.order_item_extensions_failed", err, map[string]interface{}{
				"order_id": o.ID,
			})
			byVariant = nil
		} else {
			reg = s.extensions.Registry()
		}
	}
	// Language is only sealed into order.line.display; skip store lookup when
	// no hooks are wired. Resolve once so a multi-line email does one FindDefault.
	lang := ""
	if s.hooks != nil {
		lang = s.orderDisplayLanguage(ctx, o)
	}
	for i, it := range items {
		apiItems := byVariant[it.VariantID]
		extMap := extensionapp.ExtensionMapFromAPIItems(apiItems)
		rows := extensionapp.ExtensionDisplayRows(reg, apiItems)
		baseLen := len(rows)
		// Hook gets a clone so handlers cannot mutate the template map.
		hookExt := extensionapp.CloneExtensionMap(extMap)
		rows = extensionapp.EnrichOrderLineDisplayRows(ctx, s.hooks, o.ID, it.VariantID, lang, hookExt, rows,
			func(registrant string, hookErr error) {
				s.log.Error("HandleOrderPaid.order_line_display_failed", hookErr, map[string]interface{}{
					"order_id":   o.ID,
					"variant_id": it.VariantID,
					"registrant": registrant,
				})
			},
			func(fieldCode, reason string) {
				s.log.Info("HandleOrderPaid.order_line_display_row_skipped", map[string]interface{}{
					"order_id":   o.ID,
					"variant_id": it.VariantID,
					"field_code": fieldCode,
					"reason":     reason,
				})
			},
		)
		if list := extensionapp.DisplayRowsToTemplateList(rows); len(list) > 0 {
			tmplItems[i]["ExtensionsList"] = list
		}
		// Template Extensions map stays private; enrich codes overwrite from
		// normalized rows only (never from a live hook mutation).
		extOut := extensionapp.ApplyEnrichedOverrides(extensionapp.CloneExtensionMap(extMap), rows, baseLen)
		if len(extOut) > 0 {
			tmplItems[i]["Extensions"] = extOut
		}
	}
	return tmplItems
}

// orderConfirmedJobID returns a stable UUID v5-shaped id for one confirmation
// per order (sha1 of a fixed namespace + order id).
func orderConfirmedJobID(orderID string) string {
	sum := sha1.Sum([]byte("shopanda.email.order_confirmed:" + strings.TrimSpace(orderID)))
	sum[6] = (sum[6] & 0x0f) | 0x50 // version 5
	sum[8] = (sum[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
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

	ed := s.baseEmailData(ctx)
	ed.Data = map[string]interface{}{
		"FirstName": cust.FirstName,
		"ResetURL":  resetURL.String(),
		"ExpiresIn": "1 hour",
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

	ed := s.baseEmailData(ctx)
	ed.Data = map[string]interface{}{
		"FirstName": cust.FirstName,
		"VerifyURL": verifyURL,
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

	ed := s.baseEmailData(ctx)
	ed.Data = map[string]interface{}{
		"FirstName": cust.FirstName,
		"VerifyURL": data.VerifyURL,
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

	ed := s.baseEmailData(ctx)
	ed.Data = map[string]interface{}{
		"FirstName": firstName,
		"VerifyURL": verifyURL,
		"NewEmail":  newEmail,
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

	ed := s.baseEmailData(ctx)
	ed.Data = map[string]interface{}{
		"FirstName": firstName,
		"NewEmail":  strings.TrimSpace(data.NewEmail),
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
	if o.Status() == order.OrderStatusCancelled || o.Status() == order.OrderStatusFailed {
		s.log.Info("HandleShipmentShipped.skipped_order_not_fulfillable", map[string]interface{}{
			"order_id":    data.OrderID,
			"shipment_id": data.ShipmentID,
			"status":      string(o.Status()),
		})
		return nil
	}

	recipient, err := s.resolveOrderRecipient(ctx, o, "HandleShipmentShipped")
	if err != nil {
		return err
	}

	ed := s.baseEmailData(ctx)
	ed.Data = map[string]interface{}{
		"OrderID":   data.OrderID,
		"FirstName": recipient.FirstName,
		"Guest":     recipient.Guest,
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

	// Persistent dedupe + stable job id so MarkShipped heal republish cannot double-send.
	return s.enqueueEmailOnce(ctx, msg, orderShippedJobID(data.ShipmentID), "HandleShipmentShipped", map[string]interface{}{
		"order_id":    data.OrderID,
		"shipment_id": data.ShipmentID,
		"customer_id": o.CustomerID,
		"guest":       recipient.Guest,
	}, data.ShipmentID, KindOrderShipped)
}

// orderShippedJobID returns a stable UUID v5-shaped id for one shipped email per shipment.
func orderShippedJobID(shipmentID string) string {
	sum := sha1.Sum([]byte("shopanda.email.order_shipped:" + strings.TrimSpace(shipmentID)))
	sum[6] = (sum[6] & 0x0f) | 0x50 // version 5
	sum[8] = (sum[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

// ResendVoucherIssued rebuilds and enqueues the pack email for live codes on
// the order. When force is true, any completed dedupe claim is cleared first
// so staff can recover from a lost mail after a successful enqueue mark.
func (s *Service) ResendVoucherIssued(ctx context.Context, orderID string, force bool) error {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return apperror.Validation("order id is required")
	}
	if s.vouchers == nil || s.voucherPDF == nil {
		return apperror.Unavailable("voucher email dependencies not configured")
	}
	rows, err := s.vouchers.ListByOrderID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("notification: list vouchers: %w", err)
	}
	ids := make([]string, 0, len(rows))
	customerID := ""
	for i := range rows {
		if rows[i].VoidedAt != nil {
			continue
		}
		ids = append(ids, rows[i].ID)
		if customerID == "" {
			customerID = rows[i].CustomerID
		}
	}
	if len(ids) == 0 {
		return apperror.NotFound("no vouchers for order")
	}
	if force && s.dedupe != nil {
		if err := s.dedupe.Delete(ctx, voucherIssuedSubjectKey(ids), KindVoucherIssued); err != nil {
			return fmt.Errorf("notification: clear voucher dedupe: %w", err)
		}
	}
	return s.HandleVoucherIssued(ctx, event.New(domainvoucher.EventVoucherIssued, "admin.voucher.resend", domainvoucher.IssuedData{
		OrderID:    orderID,
		CustomerID: customerID,
		VoucherIDs: ids,
	}))
}

// HandleVoucherIssued emails the buyer a PDF pack of newly issued codes.
// Dedupe is keyed by a fixed-length hash of the issued id set so a later
// reissue batch is not swallowed, btree index limits are respected, and a
// failed send can retry after enqueueEmailOnce deletes the pending claim.
func (s *Service) HandleVoucherIssued(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(domainvoucher.IssuedData)
	if !ok {
		return fmt.Errorf("notification: unexpected event data type %T", evt.Data)
	}
	if s.vouchers == nil || s.voucherPDF == nil {
		s.log.Warn("HandleVoucherIssued.skipped_no_deps", map[string]interface{}{
			"order_id": data.OrderID,
		})
		return nil
	}
	if len(data.VoucherIDs) == 0 {
		return nil
	}
	o, err := s.orders.FindByID(ctx, data.OrderID)
	if err != nil {
		return fmt.Errorf("notification: find order %s: %w", data.OrderID, err)
	}
	if o == nil {
		return fmt.Errorf("notification: order %s not found", data.OrderID)
	}
	recipient, err := s.resolveOrderRecipient(ctx, o, "HandleVoucherIssued")
	if err != nil {
		return err
	}
	want := make(map[string]struct{}, len(data.VoucherIDs))
	for _, id := range data.VoucherIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			want[id] = struct{}{}
		}
	}
	rows, err := s.vouchers.ListByOrderID(ctx, data.OrderID)
	if err != nil {
		return fmt.Errorf("notification: list vouchers: %w", err)
	}
	codes := make([]domainvoucher.PDFCode, 0, len(want))
	codeStrs := make([]string, 0, len(want))
	var issuedAt time.Time
	for i := range rows {
		if _, ok := want[rows[i].ID]; !ok {
			continue
		}
		formatted := domainvoucher.FormatCode(rows[i].Code)
		codes = append(codes, domainvoucher.PDFCode{Code: rows[i].Code, Status: rows[i].Status()})
		codeStrs = append(codeStrs, formatted)
		if issuedAt.IsZero() || rows[i].IssuedAt.Before(issuedAt) {
			issuedAt = rows[i].IssuedAt
		}
	}
	if len(codes) == 0 {
		return nil
	}
	if issuedAt.IsZero() {
		issuedAt = time.Now().UTC()
	}
	pdfBytes, err := s.voucherPDF.Render(domainvoucher.PDFDocument{
		OrderID:    data.OrderID,
		CustomerID: data.CustomerID,
		IssuedAt:   issuedAt.UTC().Format("2006-01-02"),
		Codes:      codes,
	})
	if err != nil {
		s.log.Error("HandleVoucherIssued.pdf_render_failed", err, map[string]interface{}{"order_id": data.OrderID})
		return fmt.Errorf("notification: render voucher PDF: %w", err)
	}
	ed := s.baseEmailData(ctx)
	ed.Data = map[string]interface{}{
		"OrderID":   data.OrderID,
		"FirstName": recipient.FirstName,
		"Guest":     recipient.Guest,
		"Codes":     codeStrs,
	}
	msg, err := s.templates.Render("voucher_issued", recipient.Email, ed)
	if err != nil {
		return fmt.Errorf("notification: render voucher template: %w", err)
	}
	msg.Attachments = []mail.Attachment{{
		Filename:    fmt.Sprintf("vouchers-%s.pdf", data.OrderID),
		ContentType: "application/pdf",
		Data:        pdfBytes,
	}}
	// Dedupe key is the fixed-length hash; job id must be fresh so force
	// resend can enqueue after the original email.send row is still pending
	// or failed in jobs (Enqueue conflict on the hashed id is not a resend).
	subjectKey := voucherIssuedSubjectKey(data.VoucherIDs)
	return s.enqueueEmailOnce(ctx, msg, id.New(), "HandleVoucherIssued", map[string]interface{}{
		"order_id":    data.OrderID,
		"customer_id": data.CustomerID,
		"guest":       recipient.Guest,
	}, subjectKey, KindVoucherIssued)
}

// voucherIssuedSubjectKey returns a fixed-length UUID derived from the sorted
// issued id set. Comma-joining raw UUIDs overflows the notification_dedupe
// btree unique index (~2700 bytes) above ~74 codes; a legal line can issue 100+.
func voucherIssuedSubjectKey(ids []string) string {
	cleaned := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			cleaned = append(cleaned, id)
		}
	}
	sort.Strings(cleaned)
	sum := sha1.Sum([]byte("shopanda.email.voucher_issued:" + strings.Join(cleaned, ",")))
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
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

	pdfBytes, err := s.invoicePDFBytes(ctx, *inv)
	if err != nil {
		s.log.Error("HandleInvoiceCreated.pdf_render_failed", err, map[string]interface{}{"invoice_id": data.InvoiceID})
		return fmt.Errorf("notification: invoice PDF: %w", err)
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

	ed := s.baseEmailData(ctx)
	ed.Data = map[string]interface{}{
		"OrderID":       data.OrderID,
		"InvoiceNumber": data.InvoiceNumber,
		"FirstName":     recipient.FirstName,
		"Guest":         recipient.Guest,
		"Items":         tmplItems,
		"Total":         inv.TotalAmount().String(),
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

	// Stable job id + dedupe so a repaired re-publish of invoice.created
	// does not enqueue a second email when the first already succeeded.
	return s.enqueueEmailOnce(ctx, msg, invoiceCreatedJobID(data.InvoiceID), "HandleInvoiceCreated", map[string]interface{}{
		"invoice_id":  data.InvoiceID,
		"order_id":    data.OrderID,
		"customer_id": data.CustomerID,
		"guest":       recipient.Guest,
	}, data.InvoiceID, KindInvoiceCreated)
}

// invoiceCreatedJobID returns a stable UUID v5-shaped id for one invoice email.
func invoiceCreatedJobID(invoiceID string) string {
	sum := sha1.Sum([]byte("shopanda.email.invoice_created:" + strings.TrimSpace(invoiceID)))
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

// enqueueEmail creates and enqueues an email.send job with a fresh id.
func (s *Service) enqueueEmail(ctx context.Context, msg mail.Message, handler string, logFields map[string]interface{}) error {
	return s.enqueueEmailOnce(ctx, msg, id.New(), handler, logFields, "", "")
}

// emailJobGetter is the optional introspection surface used to heal a
// terminal-failed email.send after a completed dedupe claim (Postgres Reader).
type emailJobGetter interface {
	Get(ctx context.Context, id string) (*jobs.Detail, error)
}

// enqueueEmailOnce enqueues with a caller-supplied job id. When subjectKey and
// kind are set and a dedupe store is wired, the store is claimed first (pending);
// enqueue runs only on a new claim. After a successful enqueue (or queue Conflict),
// the claim is marked completed so it is never reclaimed. Enqueue failure deletes
// the claim so a later HandleOrderPaid can retry. Queue Conflict (duplicate job id)
// remains a secondary success path for Postgres-backed queues.
//
// When the claim already exists (heal republish), a terminal-failed job for the
// stable id is requeued instead of treating the claim as success.
//
// Claim release uses a detached timeout context so shutdown cancellation of the
// request ctx cannot leave a pending claim without a job. Stuck pending claims
// are reclaimable when older than the store stale threshold via TryInsert;
// completed claims are permanent.
func (s *Service) enqueueEmailOnce(ctx context.Context, msg mail.Message, jobID, handler string, logFields map[string]interface{}, subjectKey, kind string) error {
	claimed := false
	if s.dedupe != nil && strings.TrimSpace(subjectKey) != "" && strings.TrimSpace(kind) != "" {
		inserted, err := s.dedupe.TryInsert(ctx, subjectKey, kind)
		if err != nil {
			s.log.Error(handler+".dedupe_claim_failed", err, logFields)
			return fmt.Errorf("notification: claim dedupe %s/%s: %w", subjectKey, kind, err)
		}
		if !inserted {
			return s.healExistingEmailJob(ctx, jobID, handler, logFields)
		}
		claimed = true
	}

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

	job, err := jobs.NewJob(jobID, JobTypeEmailSend, payload)
	if err != nil {
		if claimed {
			s.releaseDedupeClaim(subjectKey, kind, handler, logFields)
		}
		s.log.Error(handler+".job_create_failed", err, logFields)
		return fmt.Errorf("notification: create job: %w", err)
	}

	if err := s.queue.Enqueue(ctx, job); err != nil {
		logFields["job_id"] = job.ID
		if apperror.Is(err, apperror.CodeConflict) {
			// Job-id secondary dedupe: mark claim completed, then heal if failed.
			if claimed {
				s.completeDedupeClaim(subjectKey, kind, handler, logFields)
			}
			return s.healExistingEmailJob(ctx, job.ID, handler, logFields)
		}
		if claimed {
			s.releaseDedupeClaim(subjectKey, kind, handler, logFields)
		}
		s.log.Error(handler+".enqueue_failed", err, logFields)
		return fmt.Errorf("notification: enqueue email job %s: %w", job.ID, err)
	}

	if claimed {
		s.completeDedupeClaim(subjectKey, kind, handler, logFields)
	}
	logFields["job_id"] = job.ID
	logFields["job_type"] = JobTypeEmailSend
	s.log.Info(handler+".email_enqueued", logFields)
	return nil
}

// healExistingEmailJob requeues a terminal-failed email.send for a stable job
// id when a heal republish hits an existing dedupe claim. Pending/done jobs
// are left alone. Queues without Get (non-Postgres) skip the heal.
func (s *Service) healExistingEmailJob(ctx context.Context, jobID, handler string, logFields map[string]interface{}) error {
	logFields = copyLogFields(logFields)
	logFields["job_id"] = jobID
	s.log.Info(handler+".email_already_enqueued", logFields)

	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil
	}
	getter, ok := s.queue.(emailJobGetter)
	if !ok {
		return nil
	}
	detail, err := getter.Get(ctx, jobID)
	if err != nil {
		return fmt.Errorf("notification: get email job %s: %w", jobID, err)
	}
	if detail == nil || detail.Status != jobs.StatusFailed {
		return nil
	}
	if err := s.queue.Requeue(ctx, jobID, detail.Attempts, jobs.WithRequeueTerminal()); err != nil {
		if errors.Is(err, jobs.ErrRequeueNotFound) ||
			errors.Is(err, jobs.ErrRequeueNotTerminal) ||
			errors.Is(err, jobs.ErrRequeueUnsupported) {
			return nil
		}
		s.log.Error(handler+".email_requeue_failed", err, logFields)
		return fmt.Errorf("notification: requeue failed email job %s: %w", jobID, err)
	}
	s.log.Info(handler+".email_requeued", logFields)
	return nil
}

func copyLogFields(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}

// releaseDedupeClaim deletes a claim using a bounded background context so a
// cancelled request/shutdown ctx cannot skip cleanup and strand the claim.
func (s *Service) releaseDedupeClaim(subjectKey, kind, handler string, logFields map[string]interface{}) {
	delCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if delErr := s.dedupe.Delete(delCtx, subjectKey, kind); delErr != nil {
		s.log.Error(handler+".dedupe_release_failed", delErr, logFields)
	}
}

// completeDedupeClaim marks a claim completed using a bounded background
// context so shutdown cancellation cannot leave a successful send reclaimable.
func (s *Service) completeDedupeClaim(subjectKey, kind, handler string, logFields map[string]interface{}) {
	markCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if markErr := s.dedupe.MarkCompleted(markCtx, subjectKey, kind); markErr != nil {
		s.log.Error(handler+".dedupe_complete_failed", markErr, logFields)
	}
}
