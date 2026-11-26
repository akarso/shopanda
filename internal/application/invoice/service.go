package invoice

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	domainInvoice "github.com/akarso/shopanda/internal/domain/invoice"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// SellerLoader resolves the invoice PDF seller block for a store at issue time.
type SellerLoader interface {
	InvoiceSeller(ctx context.Context, storeID string) (domainInvoice.SellerBlock, error)
}

// Service orchestrates invoice generation use cases.
type Service struct {
	orders       order.OrderRepository
	invoices     domainInvoice.InvoiceRepository
	renderer     domainInvoice.PDFRenderer
	storage      media.Storage
	bus          *event.Bus
	log          logger.Logger
	seller       SellerLoader
	defaultStore func(context.Context) (string, error)
}

// NewService creates an invoice application service.
func NewService(
	orders order.OrderRepository,
	invoices domainInvoice.InvoiceRepository,
	renderer domainInvoice.PDFRenderer,
	storage media.Storage,
	bus *event.Bus,
	log logger.Logger,
) *Service {
	if orders == nil {
		panic("invoice.NewService: nil orders")
	}
	if invoices == nil {
		panic("invoice.NewService: nil invoices")
	}
	if renderer == nil {
		panic("invoice.NewService: nil renderer")
	}
	if storage == nil {
		panic("invoice.NewService: nil storage")
	}
	if bus == nil {
		panic("invoice.NewService: nil bus")
	}
	if log == nil {
		panic("invoice.NewService: nil log")
	}
	return &Service{
		orders:   orders,
		invoices: invoices,
		renderer: renderer,
		storage:  storage,
		bus:      bus,
		log:      log,
	}
}

// WithSellerLoader wires store identity for the sealed seller snapshot.
func (s *Service) WithSellerLoader(loader SellerLoader) *Service {
	s.seller = loader
	return s
}

// WithDefaultStoreLookup resolves the store id when orders have no store_id
// (FindDefault until orders carry store scope). Lookup errors fail generation.
func (s *Service) WithDefaultStoreLookup(fn func(context.Context) (string, error)) *Service {
	s.defaultStore = fn
	return s
}

// GenerateInput holds the parameters for invoice generation.
type GenerateInput struct {
	OrderID   string
	TaxAmount shared.Money
}

// GenerateResult holds the result of invoice generation.
type GenerateResult struct {
	Invoice *domainInvoice.Invoice
	PDFPath string
}

// HandleOrderPaid issues an invoice when an order becomes paid.
// Duplicate invoices (already issued) are treated as success so the bus
// handler is idempotent under at-least-once delivery.
func (s *Service) HandleOrderPaid(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(order.OrderStatusChangedData)
	if !ok {
		return fmt.Errorf("invoice: unexpected event data type %T", evt.Data)
	}
	orderID := strings.TrimSpace(data.OrderID)
	if orderID == "" {
		return fmt.Errorf("invoice: order.paid missing order id")
	}
	ord, err := s.orders.FindByID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("invoice: find order: %w", err)
	}
	if ord == nil {
		return apperror.NotFound("order not found")
	}
	_, err = s.GenerateFromOrder(ctx, GenerateInput{
		OrderID:   orderID,
		TaxAmount: ord.TaxAmount,
	})
	if err != nil {
		if apperror.Is(err, apperror.CodeConflict) {
			return nil
		}
		return err
	}
	return nil
}

// GenerateFromOrder creates an invoice from a paid order, seals the seller
// snapshot, renders a PDF, stores it, and publishes invoice.created.
// If render or storage fails after Save, the invoice row is deleted so a
// retry can issue the same order again.
func (s *Service) GenerateFromOrder(ctx context.Context, input GenerateInput) (*GenerateResult, error) {
	if input.OrderID == "" {
		return nil, apperror.Validation("order id must not be empty")
	}

	ord, err := s.orders.FindByID(ctx, input.OrderID)
	if err != nil {
		return nil, fmt.Errorf("invoice: find order: %w", err)
	}
	if ord == nil {
		return nil, apperror.NotFound("order not found")
	}

	if ord.Status() != order.OrderStatusPaid {
		return nil, apperror.Validation("order must be paid before invoicing")
	}

	existing, err := s.invoices.FindByOrderID(ctx, input.OrderID)
	if err != nil {
		return nil, fmt.Errorf("invoice: check existing: %w", err)
	}
	if existing != nil {
		return nil, apperror.Conflict("invoice already exists for this order")
	}

	orderItems := ord.Items()
	invoiceItems := make([]domainInvoice.Item, len(orderItems))
	for i, oi := range orderItems {
		ii, err := domainInvoice.NewItem(oi.VariantID, oi.SKU, oi.Name, oi.Quantity, oi.UnitPrice)
		if err != nil {
			return nil, fmt.Errorf("invoice: convert item %d: %w", i, err)
		}
		invoiceItems[i] = ii
	}

	inv, err := domainInvoice.NewInvoice(
		id.New(), input.OrderID, ord.CustomerID, ord.Currency,
		invoiceItems, input.TaxAmount,
	)
	if err != nil {
		return nil, fmt.Errorf("invoice: create: %w", err)
	}

	// Seal seller before Save so the row and the PDF agree for the life of the document.
	seller, err := s.loadSeller(ctx)
	if err != nil {
		return nil, err
	}
	inv.SetSellerSnapshot(seller)

	if err := s.invoices.Save(ctx, &inv); err != nil {
		return nil, fmt.Errorf("invoice: save: %w", err)
	}

	pdfBytes, err := s.renderer.Render(inv, inv.Seller())
	if err != nil {
		s.rollbackIssue(ctx, inv.ID(), "render")
		return nil, fmt.Errorf("invoice: render pdf: %w", err)
	}

	pdfPath := fmt.Sprintf("invoices/%s/invoice-%d.pdf", inv.ID(), inv.InvoiceNumber())
	if err := s.storage.Save(pdfPath, bytes.NewReader(pdfBytes)); err != nil {
		s.rollbackIssue(ctx, inv.ID(), "store")
		return nil, fmt.Errorf("invoice: store pdf: %w", err)
	}

	evt := event.New(domainInvoice.EventInvoiceCreated, "invoice.service", domainInvoice.InvoiceCreatedData{
		InvoiceID:     inv.ID(),
		InvoiceNumber: inv.InvoiceNumber(),
		OrderID:       inv.OrderID(),
		CustomerID:    inv.CustomerID(),
		Currency:      inv.Currency(),
	})
	if pubErr := s.bus.Publish(ctx, evt); pubErr != nil {
		s.log.Warn("invoice: publish event failed", map[string]interface{}{
			"event":      domainInvoice.EventInvoiceCreated,
			"invoice_id": inv.ID(),
			"error":      pubErr.Error(),
		})
	}

	s.log.Info("invoice.generated", map[string]interface{}{
		"invoice_id":     inv.ID(),
		"invoice_number": inv.InvoiceNumber(),
		"order_id":       inv.OrderID(),
		"guest":          inv.CustomerID() == "",
		"pdf_path":       pdfPath,
	})

	return &GenerateResult{Invoice: &inv, PDFPath: pdfPath}, nil
}

func (s *Service) rollbackIssue(ctx context.Context, invoiceID, stage string) {
	if err := s.invoices.Delete(ctx, invoiceID); err != nil {
		s.log.Error("invoice.rollback_failed", err, map[string]interface{}{
			"invoice_id": invoiceID,
			"stage":      stage,
		})
	}
}

func (s *Service) loadSeller(ctx context.Context) (domainInvoice.SellerBlock, error) {
	if s == nil || s.seller == nil {
		// No identity wiring (unit tests without store identity): empty block.
		return domainInvoice.SellerBlock{}, nil
	}
	if s.defaultStore == nil {
		return domainInvoice.SellerBlock{}, fmt.Errorf("invoice: default store lookup not configured")
	}
	storeID, err := s.defaultStore(ctx)
	if err != nil {
		return domainInvoice.SellerBlock{}, fmt.Errorf("invoice: default store lookup: %w", err)
	}
	if strings.TrimSpace(storeID) == "" {
		return domainInvoice.SellerBlock{}, fmt.Errorf("invoice: no default store")
	}
	seller, err := s.seller.InvoiceSeller(ctx, storeID)
	if err != nil {
		return domainInvoice.SellerBlock{}, fmt.Errorf("invoice: load seller: %w", err)
	}
	return seller, nil
}
