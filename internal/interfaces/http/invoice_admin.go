package http

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	domainInvoice "github.com/akarso/shopanda/internal/domain/invoice"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// InvoiceAdminHandler serves admin invoice list and PDF download endpoints.
type InvoiceAdminHandler struct {
	invoices domainInvoice.InvoiceRepository
	orders   order.OrderRepository
	renderer domainInvoice.PDFRenderer
	storage  media.Storage
}

// NewInvoiceAdminHandler creates an InvoiceAdminHandler.
func NewInvoiceAdminHandler(
	invoices domainInvoice.InvoiceRepository,
	orders order.OrderRepository,
	renderer domainInvoice.PDFRenderer,
	storage media.Storage,
) *InvoiceAdminHandler {
	if invoices == nil {
		panic("http: invoice repository must not be nil")
	}
	if orders == nil {
		panic("http: order repository must not be nil")
	}
	if renderer == nil {
		panic("http: invoice pdf renderer must not be nil")
	}
	return &InvoiceAdminHandler{
		invoices: invoices,
		orders:   orders,
		renderer: renderer,
		storage:  storage,
	}
}

type invoiceSummaryResponse struct {
	ID            string `json:"id"`
	InvoiceNumber int64  `json:"invoice_number"`
	OrderID       string `json:"order_id"`
	CreatedAt     string `json:"created_at"`
}

func toInvoiceSummary(inv domainInvoice.Invoice) invoiceSummaryResponse {
	return invoiceSummaryResponse{
		ID:            inv.ID(),
		InvoiceNumber: inv.InvoiceNumber(),
		OrderID:       inv.OrderID(),
		CreatedAt:     inv.CreatedAt().UTC().Format(time.RFC3339),
	}
}

// ListByOrder handles GET /api/v1/admin/orders/{orderId}/invoices.
func (h *InvoiceAdminHandler) ListByOrder() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderID := strings.TrimSpace(r.PathValue("orderId"))
		if orderID == "" {
			JSONError(w, apperror.Validation("order id is required"))
			return
		}

		ord, err := h.orders.FindByID(r.Context(), orderID)
		if err != nil {
			JSONError(w, apperror.Wrap(apperror.CodeInternal, "list order invoices failed", err))
			return
		}
		if ord == nil {
			JSONError(w, apperror.NotFound("order not found"))
			return
		}

		inv, err := h.invoices.FindByOrderID(r.Context(), orderID)
		if err != nil {
			JSONError(w, apperror.Wrap(apperror.CodeInternal, "list order invoices failed", err))
			return
		}

		invoices := make([]invoiceSummaryResponse, 0)
		if inv != nil {
			invoices = append(invoices, toInvoiceSummary(*inv))
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"invoices": invoices,
		})
	}
}

// DownloadPDF handles GET /api/v1/admin/invoices/{invoiceId}/pdf.
func (h *InvoiceAdminHandler) DownloadPDF() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		invoiceID := strings.TrimSpace(r.PathValue("invoiceId"))
		if invoiceID == "" {
			JSONError(w, apperror.Validation("invoice id is required"))
			return
		}

		inv, err := h.invoices.FindByID(r.Context(), invoiceID)
		if err != nil {
			JSONError(w, apperror.Wrap(apperror.CodeInternal, "download invoice pdf failed", err))
			return
		}
		if inv == nil {
			JSONError(w, apperror.NotFound("invoice not found"))
			return
		}

		pdfBytes, err := h.loadPDFBytes(*inv)
		if err != nil {
			JSONError(w, apperror.Wrap(apperror.CodeInternal, "download invoice pdf failed", err))
			return
		}

		filename := fmt.Sprintf("invoice-%d.pdf", inv.InvoiceNumber())
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		if _, err := w.Write(pdfBytes); err != nil {
			return
		}
	}
}

func (h *InvoiceAdminHandler) loadPDFBytes(inv domainInvoice.Invoice) ([]byte, error) {
	pdfPath := fmt.Sprintf("invoices/%s/invoice-%d.pdf", inv.ID(), inv.InvoiceNumber())
	if reader, ok := h.storage.(media.ReadableStorage); ok {
		data, err := reader.Read(pdfPath)
		if err == nil && len(data) > 0 {
			return data, nil
		}
		if err != nil && !media.IsNotFound(err) {
			return nil, fmt.Errorf("read stored invoice pdf: %w", err)
		}
	}
	return h.renderer.Render(inv)
}
