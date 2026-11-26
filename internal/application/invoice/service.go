package invoice

import (
	"bytes"
	"context"
	"fmt"

	domainInvoice "github.com/akarso/shopanda/internal/domain/invoice"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// SellerLoader resolves the invoice PDF seller block for a store.
// Optional — missing or failing loaders omit the seller section.
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

// WithSellerLoader wires store identity for the PDF seller block.
func (s *Service) WithSellerLoader(loader SellerLoader) *Service {
	s.seller = loader
	return s
}

// WithDefaultStoreLookup resolves the store id when orders have no store_id
// (FindDefault). Lookup errors omit the seller block.
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

// GenerateFromOrder creates an invoice from a paid order, renders a PDF,
// stores it, and publishes an invoice.created event.
func (s *Service) GenerateFromOrder(ctx context.Context, input GenerateInput) (*GenerateResult, error) {
	if input.OrderID == "" {
		return nil, apperror.Validation("order id must not be empty")
	}

	// 1. Load order.
	ord, err := s.orders.FindByID(ctx, input.OrderID)
	if err != nil {
		return nil, fmt.Errorf("invoice: find order: %w", err)
	}
	if ord == nil {
		return nil, apperror.NotFound("order not found")
	}

	// 2. Only paid orders may be invoiced.
	if ord.Status() != order.OrderStatusPaid {
		return nil, apperror.Validation("order must be paid before invoicing")
	}

	// 3. Check for existing invoice.
	existing, err := s.invoices.FindByOrderID(ctx, input.OrderID)
	if err != nil {
		return nil, fmt.Errorf("invoice: check existing: %w", err)
	}
	if existing != nil {
		return nil, apperror.Conflict("invoice already exists for this order")
	}

	// 4. Convert order items to invoice items.
	orderItems := ord.Items()
	invoiceItems := make([]domainInvoice.Item, len(orderItems))
	for i, oi := range orderItems {
		ii, err := domainInvoice.NewItem(oi.VariantID, oi.SKU, oi.Name, oi.Quantity, oi.UnitPrice)
		if err != nil {
			return nil, fmt.Errorf("invoice: convert item %d: %w", i, err)
		}
		invoiceItems[i] = ii
	}

	// 5. Create Invoice entity.
	inv, err := domainInvoice.NewInvoice(
		id.New(), input.OrderID, ord.CustomerID, ord.Currency,
		invoiceItems, input.TaxAmount,
	)
	if err != nil {
		return nil, fmt.Errorf("invoice: create: %w", err)
	}

	// 6. Save (assigns invoice number from DB sequence).
	if err := s.invoices.Save(ctx, &inv); err != nil {
		return nil, fmt.Errorf("invoice: save: %w", err)
	}

	// 7. Render PDF (seller block is best-effort from store identity).
	pdfBytes, err := s.renderer.Render(inv, s.loadSeller(ctx))
	if err != nil {
		return nil, fmt.Errorf("invoice: render pdf: %w", err)
	}

	// 8. Store PDF.
	pdfPath := fmt.Sprintf("invoices/%s/invoice-%d.pdf", inv.ID(), inv.InvoiceNumber())
	if err := s.storage.Save(pdfPath, bytes.NewReader(pdfBytes)); err != nil {
		return nil, fmt.Errorf("invoice: store pdf: %w", err)
	}

	// 9. Publish event.
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

func (s *Service) loadSeller(ctx context.Context) domainInvoice.SellerBlock {
	if s == nil || s.seller == nil {
		return domainInvoice.SellerBlock{}
	}
	storeID := ""
	if s.defaultStore != nil {
		id, err := s.defaultStore(ctx)
		if err != nil {
			s.log.Warn("invoice: default store lookup failed", map[string]interface{}{
				"error": err.Error(),
			})
			return domainInvoice.SellerBlock{}
		}
		storeID = id
	}
	if storeID == "" {
		return domainInvoice.SellerBlock{}
	}
	seller, err := s.seller.InvoiceSeller(ctx, storeID)
	if err != nil {
		s.log.Warn("invoice: load seller failed", map[string]interface{}{
			"store_id": storeID,
			"error":    err.Error(),
		})
		return domainInvoice.SellerBlock{}
	}
	return seller
}
