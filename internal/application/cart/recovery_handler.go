package cart

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"strings"
	"time"

	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/config"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/logger"

	"github.com/akarso/shopanda/internal/application/notification"
)

// RecoveryJobType is the job type for abandoned cart recovery scans.
const RecoveryJobType = "cart.recovery"

// DefaultRecoveryStaleAfter is how long a cart must be idle before recovery.
const DefaultRecoveryStaleAfter = 24 * time.Hour

const recoveryBatchSize = 50

// RecoveryBrand fills EmailData store name/address/logo for recovery mail.
type RecoveryBrand interface {
	BrandEmailData(ctx context.Context, storeURL string) mail.EmailData
}

// RecoveryHandler scans stale carts and enqueues recovery emails.
type RecoveryHandler struct {
	carts      domainCart.CartRepository
	customers  customer.CustomerRepository
	variants   catalog.VariantRepository
	products   catalog.ProductRepository
	templates  *mail.Templates
	layout     mail.Layout
	queue      jobs.Queue
	storeURL   string
	settings   config.Repository
	brand      RecoveryBrand
	staleAfter time.Duration
	log        logger.Logger
}

// RecoveryHandlerConfig configures a RecoveryHandler.
type RecoveryHandlerConfig struct {
	Carts      domainCart.CartRepository
	Customers  customer.CustomerRepository
	Variants   catalog.VariantRepository
	Products   catalog.ProductRepository
	Templates  *mail.Templates
	Layout     mail.Layout // optional — _layout.html chrome (PR-1210)
	Queue      jobs.Queue
	Settings   config.Repository
	Brand      RecoveryBrand
	StoreURL   string
	StaleAfter time.Duration
	Log        logger.Logger
}

// NewRecoveryHandler creates a RecoveryHandler.
func NewRecoveryHandler(cfg RecoveryHandlerConfig) *RecoveryHandler {
	if cfg.Carts == nil {
		panic("cart.NewRecoveryHandler: nil carts")
	}
	if cfg.Customers == nil {
		panic("cart.NewRecoveryHandler: nil customers")
	}
	if cfg.Variants == nil {
		panic("cart.NewRecoveryHandler: nil variants")
	}
	if cfg.Products == nil {
		panic("cart.NewRecoveryHandler: nil products")
	}
	if cfg.Templates == nil {
		panic("cart.NewRecoveryHandler: nil templates")
	}
	if cfg.Queue == nil {
		panic("cart.NewRecoveryHandler: nil queue")
	}
	if cfg.Log == nil {
		panic("cart.NewRecoveryHandler: nil log")
	}
	staleAfter := cfg.StaleAfter
	if staleAfter <= 0 {
		staleAfter = DefaultRecoveryStaleAfter
	}
	return &RecoveryHandler{
		carts:      cfg.Carts,
		customers:  cfg.Customers,
		variants:   cfg.Variants,
		products:   cfg.Products,
		templates:  cfg.Templates,
		layout:     cfg.Layout,
		queue:      cfg.Queue,
		storeURL:   strings.TrimRight(strings.TrimSpace(cfg.StoreURL), "/"),
		settings:   cfg.Settings,
		brand:      cfg.Brand,
		staleAfter: staleAfter,
		log:        cfg.Log,
	}
}

// Type returns the job type this handler processes.
func (h *RecoveryHandler) Type() string { return RecoveryJobType }

// Handle scans for stale carts and enqueues recovery emails.
func (h *RecoveryHandler) Handle(ctx context.Context, _ jobs.Job) error {
	settings, err := LoadRecoverySettings(ctx, h.settings, h.staleAfter)
	if err != nil {
		return err
	}
	if !settings.Enabled {
		h.log.Info("cart.recovery.skipped", map[string]interface{}{"reason": "disabled"})
		return nil
	}

	staleBefore := time.Now().UTC().Add(-settings.StaleAfter)
	candidates, err := h.carts.FindRecoveryCandidates(ctx, staleBefore, recoveryBatchSize)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		h.log.Info("cart.recovery.complete", map[string]interface{}{"processed": 0})
		return nil
	}

	brand := mail.EmailData{StoreURL: h.storeURL}
	if h.brand != nil {
		brand = h.brand.BrandEmailData(ctx, h.storeURL)
	}

	sent := 0
	for _, c := range candidates {
		if c == nil || len(c.Items) == 0 || c.CustomerID == "" {
			continue
		}
		if err := h.processCart(ctx, c, brand); err != nil {
			h.log.Error("cart.recovery.cart_failed", err, map[string]interface{}{
				"cart_id":     c.ID,
				"customer_id": c.CustomerID,
			})
			continue
		}
		sent++
	}
	h.log.Info("cart.recovery.complete", map[string]interface{}{
		"processed": len(candidates),
		"sent":      sent,
	})
	return nil
}

func (h *RecoveryHandler) processCart(ctx context.Context, c *domainCart.Cart, brand mail.EmailData) error {
	cust, err := h.customers.FindByID(ctx, c.CustomerID)
	if err != nil {
		return fmt.Errorf("find customer: %w", err)
	}
	if cust == nil || cust.Status != customer.StatusActive {
		h.terminalSkipRecovery(ctx, c.ID)
		return nil
	}
	email := strings.TrimSpace(cust.Email)
	if email == "" {
		h.terminalSkipRecovery(ctx, c.ID)
		return nil
	}

	items, err := h.recoveryItems(ctx, c)
	if err != nil {
		return err
	}
	cartURL := h.storeURL + "/cart"
	if h.storeURL == "" {
		cartURL = "/cart"
	}

	ed := brand
	ed.StoreURL = h.storeURL
	ed.Data = map[string]interface{}{
		"FirstName": cust.FirstName,
		"CartURL":   cartURL,
		"Items":     items,
		"ItemCount": c.TotalQuantity(),
	}
	msg, err := h.templates.Render("cart_recovery", email, ed)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}
	msg, err = mail.ApplyLayout(h.layout, msg, ed)
	if err != nil {
		return fmt.Errorf("wrap layout: %w", err)
	}
	// Claim first so brokers that ignore job ids cannot double-publish across
	// overlapping scans. Stable job id still collapses retries on Postgres/Redis.
	sentAt := time.Now().UTC()
	marked, err := h.carts.MarkRecoveryEmailSent(ctx, c.ID, sentAt)
	if err != nil {
		return err
	}
	if !marked {
		return nil
	}
	if err := h.enqueueEmail(ctx, msg, c.ID, c.CustomerID); err != nil {
		if clearErr := h.carts.ClearRecoveryEmailSent(ctx, c.ID, sentAt); clearErr != nil {
			h.log.Error("cart.recovery.clear_claim_failed", clearErr, map[string]interface{}{
				"cart_id": c.ID,
			})
		}
		return err
	}
	return nil
}

func (h *RecoveryHandler) terminalSkipRecovery(ctx context.Context, cartID string) {
	if _, err := h.carts.MarkRecoveryEmailSent(ctx, cartID, time.Now().UTC()); err != nil {
		h.log.Error("cart.recovery.skip_failed", err, map[string]interface{}{"cart_id": cartID})
	}
}

func (h *RecoveryHandler) recoveryItems(ctx context.Context, c *domainCart.Cart) ([]map[string]interface{}, error) {
	out := make([]map[string]interface{}, 0, len(c.Items))
	for _, item := range c.Items {
		label := item.VariantID
		variant, err := h.variants.FindByID(ctx, item.VariantID)
		if err != nil {
			return nil, fmt.Errorf("variant %q: %w", item.VariantID, err)
		}
		if variant != nil {
			label = strings.TrimSpace(variant.Name)
			if label == "" {
				label = variant.SKU
			}
			if label == "" {
				product, err := h.products.FindByID(ctx, variant.ProductID)
				if err != nil {
					return nil, fmt.Errorf("product %q: %w", variant.ProductID, err)
				}
				if product != nil {
					label = product.Name
				}
			}
		}
		out = append(out, map[string]interface{}{
			"Name":     label,
			"Quantity": item.Quantity,
			"Price":    item.UnitPrice.String(),
		})
	}
	return out, nil
}

// recoveryJobGetter is optional introspection for Conflict handling.
type recoveryJobGetter interface {
	Get(ctx context.Context, id string) (*jobs.Detail, error)
}

// recoveryFailedReplacer replaces a failed email.send payload and re-pends it
// with attempts=0 (Postgres JobQueue.ReplaceFailed — same as invoice heal).
type recoveryFailedReplacer interface {
	ReplaceFailed(ctx context.Context, job jobs.Job) error
}

func (h *RecoveryHandler) enqueueEmail(ctx context.Context, msg mail.Message, cartID, customerID string) error {
	payload := map[string]interface{}{
		"to":      msg.To,
		"subject": msg.Subject,
		"body":    msg.Body,
	}
	jobID := CartRecoveryEmailJobID(cartID)
	job, err := jobs.NewJob(jobID, notification.JobTypeEmailSend, payload)
	if err != nil {
		return fmt.Errorf("create email job: %w", err)
	}
	if err := h.queue.Enqueue(ctx, job); err != nil {
		if apperror.Is(err, apperror.CodeConflict) {
			return h.handleEnqueueConflict(ctx, job, cartID, customerID)
		}
		return fmt.Errorf("enqueue email job: %w", err)
	}
	h.log.Info("cart.recovery.email_enqueued", map[string]interface{}{
		"cart_id":     cartID,
		"customer_id": customerID,
		"job_id":      job.ID,
	})
	return nil
}

func (h *RecoveryHandler) handleEnqueueConflict(ctx context.Context, job jobs.Job, cartID, customerID string) error {
	jobID := job.ID
	logFields := map[string]interface{}{
		"cart_id":     cartID,
		"customer_id": customerID,
		"job_id":      jobID,
	}
	getter, ok := h.queue.(recoveryJobGetter)
	if !ok {
		// Brokers without Get: keep the claim. We cannot tell terminal from
		// in-flight; clearing would reopen double-send on SQS/Rabbit/Kafka.
		h.log.Info("cart.recovery.email_already_enqueued", logFields)
		return nil
	}
	detail, err := getter.Get(ctx, jobID)
	if err != nil {
		return fmt.Errorf("get email job %s: %w", jobID, err)
	}
	if detail == nil {
		h.log.Info("cart.recovery.email_conflict_missing", logFields)
		return fmt.Errorf("enqueue email job: conflict but job %s not found", jobID)
	}
	switch detail.Status {
	case jobs.StatusPending, jobs.StatusProcessing, jobs.StatusDone, jobs.StatusReclaiming:
		h.log.Info("cart.recovery.email_already_enqueued", logFields)
		return nil
	case jobs.StatusCancelled:
		// Operator cancelled; leave the mark so we do not revive the id.
		h.log.Info("cart.recovery.email_cancelled_kept", logFields)
		return nil
	case jobs.StatusFailed:
		replacer, ok := h.queue.(recoveryFailedReplacer)
		if !ok {
			// Redis etc: Get sees failed but cannot rewrite/reset attempts.
			h.log.Info("cart.recovery.email_replace_unsupported", logFields)
			return nil
		}
		if replaceErr := replacer.ReplaceFailed(ctx, job); replaceErr != nil {
			if errors.Is(replaceErr, jobs.ErrRequeueNotTerminal) {
				h.log.Info("cart.recovery.email_already_enqueued", logFields)
				return nil
			}
			return fmt.Errorf("replace failed email job %s: %w", jobID, replaceErr)
		}
		h.log.Info("cart.recovery.email_replaced", logFields)
		return nil
	default:
		h.log.Info("cart.recovery.email_conflict_unknown_status", logFields)
		return fmt.Errorf("enqueue email job: existing job %s has status %s", jobID, detail.Status)
	}
}

// CartRecoveryEmailJobID returns a stable UUID v5-shaped id for one recovery
// email per cart (sha1 of a fixed namespace + cart id).
func CartRecoveryEmailJobID(cartID string) string {
	sum := sha1.Sum([]byte("shopanda.email.cart_recovery:" + strings.TrimSpace(cartID)))
	sum[6] = (sum[6] & 0x0f) | 0x50 // version 5
	sum[8] = (sum[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}
