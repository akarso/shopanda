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
// An existing row is not treated as done: ensureIssued repairs a missing PDF
// and re-publishes invoice.created (mail handler dedupes on a stable job id).
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
	return err
}

// GenerateFromOrder creates an invoice from a paid order, seals the seller
// snapshot, renders a PDF, stores it, and publishes invoice.created.
// If a row already exists (pre-check or UNIQUE race), it repairs PDF/storage
// and re-publishes invoice.created. If render/storage fails after Save, the
// invoice row is deleted so a cold retry can insert again.
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
		return s.ensureIssued(ctx, existing)
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

	seller, err := s.loadSeller(ctx)
	if err != nil {
		return nil, err
	}
	inv.SetSellerSnapshot(seller)

	if err := s.invoices.Save(ctx, &inv); err != nil {
		if apperror.Is(err, apperror.CodeConflict) {
			existing, findErr := s.invoices.FindByOrderID(ctx, input.OrderID)
			if findErr != nil {
				return nil, fmt.Errorf("invoice: reload after conflict: %w", findErr)
			}
			if existing == nil {
				return nil, fmt.Errorf("invoice: save conflict but no row for order %s", input.OrderID)
			}
			return s.ensureIssued(ctx, existing)
		}
		return nil, fmt.Errorf("invoice: save: %w", err)
	}

	pdfPath := invoicePDFPath(inv)
	pdfBytes, err := s.renderer.Render(inv, inv.Seller())
	if err != nil {
		return nil, s.rollbackIssue(ctx, inv.ID(), "", "render", err)
	}
	if err := s.storage.Save(pdfPath, bytes.NewReader(pdfBytes)); err != nil {
		return nil, s.rollbackIssue(ctx, inv.ID(), pdfPath, "store", err)
	}

	if err := s.publishCreated(ctx, &inv); err != nil {
		return nil, err
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

// ensureIssued repairs a leftover invoice row: store PDF if missing, then
// (re)publish invoice.created so the email path can run/dedupe.
func (s *Service) ensureIssued(ctx context.Context, inv *domainInvoice.Invoice) (*GenerateResult, error) {
	if inv == nil {
		return nil, fmt.Errorf("invoice: ensureIssued: nil invoice")
	}
	pdfPath := invoicePDFPath(*inv)
	exists, err := s.pdfStored(ctx, pdfPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		pdfBytes, renderErr := s.renderer.Render(*inv, inv.Seller())
		if renderErr != nil {
			return nil, fmt.Errorf("invoice: repair render pdf: %w", renderErr)
		}
		if storeErr := s.storage.Save(pdfPath, bytes.NewReader(pdfBytes)); storeErr != nil {
			return nil, fmt.Errorf("invoice: repair store pdf: %w", storeErr)
		}
		s.log.Info("invoice.pdf_repaired", map[string]interface{}{
			"invoice_id":     inv.ID(),
			"invoice_number": inv.InvoiceNumber(),
			"order_id":       inv.OrderID(),
			"pdf_path":       pdfPath,
		})
	}
	if err := s.publishCreated(ctx, inv); err != nil {
		return nil, err
	}
	return &GenerateResult{Invoice: inv, PDFPath: pdfPath}, nil
}

func (s *Service) publishCreated(ctx context.Context, inv *domainInvoice.Invoice) error {
	evt := event.New(domainInvoice.EventInvoiceCreated, "invoice.service", domainInvoice.InvoiceCreatedData{
		InvoiceID:     inv.ID(),
		InvoiceNumber: inv.InvoiceNumber(),
		OrderID:       inv.OrderID(),
		CustomerID:    inv.CustomerID(),
		Currency:      inv.Currency(),
	})
	if err := s.bus.Publish(ctx, evt); err != nil {
		s.log.Error("invoice.publish_failed", err, map[string]interface{}{
			"event":      domainInvoice.EventInvoiceCreated,
			"invoice_id": inv.ID(),
		})
		return fmt.Errorf("invoice: publish %s: %w", domainInvoice.EventInvoiceCreated, err)
	}
	return nil
}

func invoicePDFPath(inv domainInvoice.Invoice) string {
	return fmt.Sprintf("invoices/%s/invoice-%d.pdf", inv.ID(), inv.InvoiceNumber())
}

func (s *Service) pdfStored(ctx context.Context, path string) (bool, error) {
	reader, ok := s.storage.(media.ReadableStorage)
	if !ok {
		// Cannot probe: treat as missing so repair re-writes the object.
		return false, nil
	}
	data, err := reader.Read(ctx, path)
	if err != nil {
		if media.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("invoice: read stored pdf: %w", err)
	}
	return len(data) > 0, nil
}

// rollbackIssue removes a failed issue attempt. On store failure, deletes the
// object first. If Delete of the row fails, that error is returned (wrapped
// with the original cause) so a leftover row is not silently ignored.
func (s *Service) rollbackIssue(ctx context.Context, invoiceID, pdfPath, stage string, cause error) error {
	if pdfPath != "" {
		if delErr := s.storage.Delete(pdfPath); delErr != nil && !media.IsNotFound(delErr) {
			s.log.Warn("invoice.rollback_object_failed", map[string]interface{}{
				"invoice_id": invoiceID,
				"pdf_path":   pdfPath,
				"stage":      stage,
				"error":      delErr.Error(),
			})
		}
	}
	if err := s.invoices.Delete(ctx, invoiceID); err != nil {
		s.log.Error("invoice.rollback_failed", err, map[string]interface{}{
			"invoice_id": invoiceID,
			"stage":      stage,
		})
		return fmt.Errorf("invoice: %s failed (%v); rollback delete failed: %w", stage, cause, err)
	}
	return fmt.Errorf("invoice: %s pdf: %w", stage, cause)
}

func (s *Service) loadSeller(ctx context.Context) (domainInvoice.SellerBlock, error) {
	if s == nil || s.seller == nil {
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
