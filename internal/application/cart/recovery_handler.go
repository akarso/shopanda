package cart

import (
	"context"
	"fmt"
	"strings"
	"time"

	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"

	"github.com/akarso/shopanda/internal/application/notification"
)

// RecoveryJobType is the job type for abandoned cart recovery scans.
const RecoveryJobType = "cart.recovery"

// DefaultRecoveryStaleAfter is how long a cart must be idle before recovery.
const DefaultRecoveryStaleAfter = 24 * time.Hour

const recoveryBatchSize = 50

// RecoveryHandler scans stale carts and enqueues recovery emails.
type RecoveryHandler struct {
	carts     domainCart.CartRepository
	customers customer.CustomerRepository
	variants  catalog.VariantRepository
	products  catalog.ProductRepository
	templates *mail.Templates
	queue     jobs.Queue
	storeURL  string
	staleAfter time.Duration
	log       logger.Logger
}

// RecoveryHandlerConfig configures a RecoveryHandler.
type RecoveryHandlerConfig struct {
	Carts      domainCart.CartRepository
	Customers  customer.CustomerRepository
	Variants   catalog.VariantRepository
	Products   catalog.ProductRepository
	Templates  *mail.Templates
	Queue      jobs.Queue
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
		queue:      cfg.Queue,
		storeURL:   strings.TrimRight(strings.TrimSpace(cfg.StoreURL), "/"),
		staleAfter: staleAfter,
		log:        cfg.Log,
	}
}

// Type returns the job type this handler processes.
func (h *RecoveryHandler) Type() string { return RecoveryJobType }

// Handle scans for stale carts and enqueues recovery emails.
func (h *RecoveryHandler) Handle(ctx context.Context, _ jobs.Job) error {
	staleBefore := time.Now().UTC().Add(-h.staleAfter)
	candidates, err := h.carts.FindRecoveryCandidates(ctx, staleBefore, recoveryBatchSize)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		h.log.Info("cart.recovery.complete", map[string]interface{}{"processed": 0})
		return nil
	}

	sent := 0
	for _, c := range candidates {
		if c == nil || len(c.Items) == 0 || c.CustomerID == "" {
			continue
		}
		if err := h.processCart(ctx, c); err != nil {
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

func (h *RecoveryHandler) processCart(ctx context.Context, c *domainCart.Cart) error {
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

	marked, err := h.carts.MarkRecoveryEmailSent(ctx, c.ID, time.Now().UTC())
	if err != nil {
		return err
	}
	if !marked {
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

	ed := mail.EmailData{
		StoreName: "Shopanda",
		StoreURL:  h.storeURL,
		Data: map[string]interface{}{
			"FirstName": cust.FirstName,
			"CartURL":   cartURL,
			"Items":     items,
			"ItemCount": c.TotalQuantity(),
		},
	}
	msg, err := h.templates.Render("cart_recovery", email, ed)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}
	return h.enqueueEmail(ctx, msg, c.ID, c.CustomerID)
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

func (h *RecoveryHandler) enqueueEmail(ctx context.Context, msg mail.Message, cartID, customerID string) error {
	payload := map[string]interface{}{
		"to":      msg.To,
		"subject": msg.Subject,
		"body":    msg.Body,
	}
	job, err := jobs.NewJob(id.New(), notification.JobTypeEmailSend, payload)
	if err != nil {
		return fmt.Errorf("create email job: %w", err)
	}
	if err := h.queue.Enqueue(ctx, job); err != nil {
		return fmt.Errorf("enqueue email job: %w", err)
	}
	h.log.Info("cart.recovery.email_enqueued", map[string]interface{}{
		"cart_id":     cartID,
		"customer_id": customerID,
		"job_id":      job.ID,
	})
	return nil
}
