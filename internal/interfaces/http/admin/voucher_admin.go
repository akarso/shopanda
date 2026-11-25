package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	voucherapp "github.com/akarso/shopanda/internal/application/voucher"
	"github.com/akarso/shopanda/internal/domain/media"
	domain "github.com/akarso/shopanda/internal/domain/voucher"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type voucherEmailResender interface {
	ResendVoucherIssued(ctx context.Context, orderID string, force bool) error
}

// VoucherAdminHandler lists, looks up, redeems, and PDFs vouchers.
type VoucherAdminHandler struct {
	svc      *voucherapp.Service
	issuer   *voucherapp.Issuer
	resender voucherEmailResender
	pdf      domain.PDFRenderer
	auditor  *adminapp.Auditor
	now      func() time.Time
}

// NewVoucherAdminHandler creates a VoucherAdminHandler. pdf may be nil
// (DownloadPDF returns 503).
func NewVoucherAdminHandler(svc *voucherapp.Service, auditor *adminapp.Auditor) *VoucherAdminHandler {
	if svc == nil {
		panic("http: voucher handler: service must not be nil")
	}
	if auditor == nil {
		panic("http: voucher handler: auditor must not be nil")
	}
	return &VoucherAdminHandler{
		svc:     svc,
		auditor: auditor,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

// WithPDFRenderer wires voucher pack PDF download.
func (h *VoucherAdminHandler) WithPDFRenderer(r domain.PDFRenderer) *VoucherAdminHandler {
	if h != nil {
		h.pdf = r
	}
	return h
}

// WithIssuer wires admin reissue (idempotent IssueForOrder).
func (h *VoucherAdminHandler) WithIssuer(issuer *voucherapp.Issuer) *VoucherAdminHandler {
	if h != nil {
		h.issuer = issuer
	}
	return h
}

// WithEmailResender wires staff resend of the voucher pack email.
func (h *VoucherAdminHandler) WithEmailResender(r voucherEmailResender) *VoucherAdminHandler {
	if h != nil {
		h.resender = r
	}
	return h
}

type voucherDTO struct {
	ID             string `json:"id"`
	Code           string `json:"code"`
	OrderID        string `json:"order_id"`
	OrderItemID    string `json:"order_item_id"`
	ProductID      string `json:"product_id"`
	VariantID      string `json:"variant_id"`
	CustomerID     string `json:"customer_id"`
	Seq            int    `json:"seq"`
	MaxRedemptions int    `json:"max_redemptions"`
	RedeemedCount  int    `json:"redeemed_count"`
	Status         string `json:"status"`
	IssuedAt       string `json:"issued_at"`
	CreatedAt      string `json:"created_at"`
}

type redemptionDTO struct {
	ID        string `json:"id"`
	VoucherID string `json:"voucher_id"`
	Action    string `json:"action"`
	ActorID   string `json:"actor_id"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`
}

type redeemRequest struct {
	Note string `json:"note"`
}

func toVoucherDTO(v *domain.Voucher) voucherDTO {
	if v == nil {
		return voucherDTO{}
	}
	return voucherDTO{
		ID:             v.ID,
		Code:           domain.FormatCode(v.Code),
		OrderID:        v.OrderID,
		OrderItemID:    v.OrderItemID,
		ProductID:      v.ProductID,
		VariantID:      v.VariantID,
		CustomerID:     v.CustomerID,
		Seq:            v.Seq,
		MaxRedemptions: v.MaxRedemptions,
		RedeemedCount:  v.RedeemedCount,
		Status:         v.Status(),
		IssuedAt:       v.IssuedAt.UTC().Format(time.RFC3339),
		CreatedAt:      v.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func (h *VoucherAdminHandler) audit(r *http.Request, action adminapp.AuditAction, voucherID string, details map[string]interface{}, err error) {
	merged := mergeAuditDetails(details, fullAdminScopeDetailsFromRequest(r))
	result := "success"
	errMsg := ""
	if err != nil {
		result = "error"
		errMsg = err.Error()
	}
	h.auditor.LogAction(r.Context(), adminapp.AuditEntry{
		AdminID:      adminIDFromRequest(r),
		Action:       action,
		ResourceType: "voucher",
		ResourceID:   voucherID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}

// List handles GET /api/v1/admin/vouchers.
func (h *VoucherAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f := domain.ListFilter{
			OrderID:    strings.TrimSpace(r.URL.Query().Get("order_id")),
			CustomerID: strings.TrimSpace(r.URL.Query().Get("customer_id")),
			Code:       r.URL.Query().Get("code"),
			Status:     strings.TrimSpace(r.URL.Query().Get("status")),
		}
		if lim := strings.TrimSpace(r.URL.Query().Get("limit")); lim != "" {
			n, err := strconv.Atoi(lim)
			if err != nil {
				httpshared.JSONError(w, apperror.Validation("limit must be an integer"))
				return
			}
			f.Limit = n
		}
		if off := strings.TrimSpace(r.URL.Query().Get("offset")); off != "" {
			n, err := strconv.Atoi(off)
			if err != nil {
				httpshared.JSONError(w, apperror.Validation("offset must be an integer"))
				return
			}
			f.Offset = n
		}
		rows, err := h.svc.List(r.Context(), f)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		out := make([]voucherDTO, 0, len(rows))
		for i := range rows {
			out = append(out, toVoucherDTO(&rows[i]))
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"vouchers": out})
	}
}

// Get handles GET /api/v1/admin/vouchers/{id}.
func (h *VoucherAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		v, err := h.svc.FindByID(r.Context(), id)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, toVoucherDTO(v))
	}
}

// Lookup handles GET /api/v1/admin/vouchers/lookup?code=.
func (h *VoucherAdminHandler) Lookup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimSpace(r.URL.Query().Get("code"))
		if code == "" {
			httpshared.JSONError(w, apperror.Validation("code is required"))
			return
		}
		v, err := h.svc.FindByCode(r.Context(), code)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, toVoucherDTO(v))
	}
}

// ListByOrder handles GET /api/v1/admin/orders/{orderId}/vouchers.
func (h *VoucherAdminHandler) ListByOrder() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderID := strings.TrimSpace(r.PathValue("orderId"))
		if orderID == "" {
			httpshared.JSONError(w, apperror.Validation("order id is required"))
			return
		}
		rows, err := h.svc.ListByOrder(r.Context(), orderID)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		out := make([]voucherDTO, 0, len(rows))
		for i := range rows {
			out = append(out, toVoucherDTO(&rows[i]))
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"vouchers": out})
	}
}

// ListRedemptions handles GET /api/v1/admin/vouchers/{id}/redemptions.
func (h *VoucherAdminHandler) ListRedemptions() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		rows, err := h.svc.ListRedemptions(r.Context(), id)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		out := make([]redemptionDTO, 0, len(rows))
		for _, row := range rows {
			out = append(out, redemptionDTO{
				ID:        row.ID,
				VoucherID: row.VoucherID,
				Action:    row.Action,
				ActorID:   row.ActorID,
				Note:      row.Note,
				CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
			})
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"redemptions": out})
	}
}

func decodeRedeemRequest(r *http.Request) (redeemRequest, error) {
	var req redeemRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return redeemRequest{}, apperror.Validation("invalid JSON body")
	}
	return req, nil
}

// Redeem handles POST /api/v1/admin/vouchers/{id}/redeem.
func (h *VoucherAdminHandler) Redeem() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		req, err := decodeRedeemRequest(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		actor := adminIDFromRequest(r)
		v, err := h.svc.Redeem(r.Context(), id, actor, req.Note)
		h.audit(r, adminapp.AuditVoucherRedeem, id, map[string]interface{}{"note": req.Note}, err)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, toVoucherDTO(v))
	}
}

// Unredeem handles POST /api/v1/admin/vouchers/{id}/unredeem.
func (h *VoucherAdminHandler) Unredeem() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		req, err := decodeRedeemRequest(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		actor := adminIDFromRequest(r)
		v, err := h.svc.Unredeem(r.Context(), id, actor, req.Note)
		h.audit(r, adminapp.AuditVoucherUnredeem, id, map[string]interface{}{"note": req.Note}, err)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, toVoucherDTO(v))
	}
}

// Reissue handles POST /api/v1/admin/orders/{orderId}/vouchers/reissue.
// Idempotent on (order_id, variant_id, seq) — creates any missing codes.
// When nothing new is created, republishes voucher.issued for live codes so a
// pending/absent email dedupe claim can still deliver the pack.
func (h *VoucherAdminHandler) Reissue() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.issuer == nil {
			httpshared.JSONError(w, apperror.Unavailable("voucher issuer not configured"))
			return
		}
		orderID := strings.TrimSpace(r.PathValue("orderId"))
		if orderID == "" {
			httpshared.JSONError(w, apperror.Validation("order id is required"))
			return
		}
		n, err := h.issuer.IssueForOrder(r.Context(), orderID)
		if err != nil {
			h.audit(r, adminapp.AuditVoucherReissue, orderID, map[string]interface{}{"created": n}, err)
			httpshared.JSONError(w, err)
			return
		}
		resent := false
		if n == 0 {
			if pubErr := h.issuer.PublishExistingIssued(r.Context(), orderID); pubErr != nil {
				if !apperror.Is(pubErr, apperror.CodeNotFound) {
					h.audit(r, adminapp.AuditVoucherReissue, orderID, map[string]interface{}{"created": n}, pubErr)
					httpshared.JSONError(w, pubErr)
					return
				}
			} else {
				resent = true
			}
		}
		h.audit(r, adminapp.AuditVoucherReissue, orderID, map[string]interface{}{"created": n, "resent": resent}, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"created": n, "resent": resent})
	}
}

type resendVoucherEmailRequest struct {
	Force bool `json:"force"`
}

// ResendEmail handles POST /api/v1/admin/orders/{orderId}/vouchers/resend-email.
// force=true clears a completed dedupe claim first.
func (h *VoucherAdminHandler) ResendEmail() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.resender == nil {
			httpshared.JSONError(w, apperror.Unavailable("voucher email resend not configured"))
			return
		}
		orderID := strings.TrimSpace(r.PathValue("orderId"))
		if orderID == "" {
			httpshared.JSONError(w, apperror.Validation("order id is required"))
			return
		}
		var req resendVoucherEmailRequest
		if r.Body != nil && r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				httpshared.JSONError(w, apperror.Validation("invalid JSON body"))
				return
			}
		}
		err := h.resender.ResendVoucherIssued(r.Context(), orderID, req.Force)
		h.audit(r, adminapp.AuditVoucherResendEmail, orderID, map[string]interface{}{"force": req.Force}, err)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"ok": true})
	}
}

func voucherPDFFilename(prefix, id string) string {
	safe := media.SanitizeContentDispositionFilename(id)
	safe = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			return r
		}
		return -1
	}, safe)
	if safe == "" || safe == "download" {
		safe = "pack"
	}
	return fmt.Sprintf("%s-%s.pdf", prefix, safe)
}

// DownloadPDF handles GET /api/v1/admin/vouchers/{id}/pdf — single-code pack.
func (h *VoucherAdminHandler) DownloadPDF() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.pdf == nil {
			httpshared.JSONError(w, apperror.Unavailable("voucher PDF renderer not configured"))
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		v, err := h.svc.FindByID(r.Context(), id)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		doc := domain.PDFDocument{
			OrderID:    v.OrderID,
			CustomerID: v.CustomerID,
			IssuedAt:   v.IssuedAt.UTC().Format("2006-01-02"),
			Codes: []domain.PDFCode{{
				Code:   v.Code,
				Status: v.Status(),
			}},
		}
		raw, err := h.pdf.Render(doc)
		if err != nil {
			httpshared.JSONError(w, apperror.Internal("voucher PDF render failed"))
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", media.ContentDispositionAttachment(voucherPDFFilename("voucher", v.ID)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(raw)
	}
}

// DownloadOrderPDF handles GET /api/v1/admin/orders/{orderId}/vouchers/pdf.
func (h *VoucherAdminHandler) DownloadOrderPDF() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.pdf == nil {
			httpshared.JSONError(w, apperror.Unavailable("voucher PDF renderer not configured"))
			return
		}
		orderID := strings.TrimSpace(r.PathValue("orderId"))
		rows, err := h.svc.ListByOrder(r.Context(), orderID)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		if len(rows) == 0 {
			httpshared.JSONError(w, apperror.NotFound("no vouchers for order"))
			return
		}
		codes := make([]domain.PDFCode, 0, len(rows))
		for i := range rows {
			codes = append(codes, domain.PDFCode{
				Code:   rows[i].Code,
				Status: rows[i].Status(),
			})
		}
		doc := domain.PDFDocument{
			OrderID:    orderID,
			CustomerID: rows[0].CustomerID,
			IssuedAt:   rows[0].IssuedAt.UTC().Format("2006-01-02"),
			Codes:      codes,
		}
		raw, err := h.pdf.Render(doc)
		if err != nil {
			httpshared.JSONError(w, apperror.Internal("voucher PDF render failed"))
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", media.ContentDispositionAttachment(voucherPDFFilename("vouchers", orderID)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(raw)
	}
}
