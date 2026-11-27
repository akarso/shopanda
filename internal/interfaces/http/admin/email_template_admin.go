package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	storeidentity "github.com/akarso/shopanda/internal/application/storeidentity"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/store"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/logger"
)

const emailTemplateOriginDB = "db"

// EmailTemplateAdminHandler serves admin transactional email template endpoints.
type EmailTemplateAdminHandler struct {
	templates *mail.Templates
	layout    mail.Layout
	repo      mail.EmailTemplateRepository
	mailer    mail.Mailer
	stores    store.StoreRepository
	brand     storeidentity.EmailBrandSource
	storeURL  string
	auditor   *adminapp.Auditor
}

// EmailTemplateAdminConfig configures EmailTemplateAdminHandler.
type EmailTemplateAdminConfig struct {
	Templates *mail.Templates
	Layout    mail.Layout
	Repo      mail.EmailTemplateRepository
	Mailer    mail.Mailer
	Stores    store.StoreRepository
	Brand     storeidentity.EmailBrandSource
	StoreURL  string
	Auditor   *adminapp.Auditor
}

// NewEmailTemplateAdminHandler creates an EmailTemplateAdminHandler.
func NewEmailTemplateAdminHandler(cfg EmailTemplateAdminConfig) *EmailTemplateAdminHandler {
	if cfg.Templates == nil {
		panic("EmailTemplateAdminHandler: nil templates")
	}
	if cfg.Repo == nil {
		panic("EmailTemplateAdminHandler: nil repo")
	}
	if cfg.Stores == nil {
		panic("EmailTemplateAdminHandler: nil stores")
	}
	auditor := cfg.Auditor
	if auditor == nil {
		auditor = adminapp.NewAuditor(logger.New("info"))
	}
	return &EmailTemplateAdminHandler{
		templates: cfg.Templates,
		layout:    cfg.Layout,
		repo:      cfg.Repo,
		mailer:    cfg.Mailer,
		stores:    cfg.Stores,
		brand:     cfg.Brand,
		storeURL:  strings.TrimRight(strings.TrimSpace(cfg.StoreURL), "/"),
		auditor:   auditor,
	}
}

type emailTemplateListItem struct {
	Name     string   `json:"name"`
	Source   string   `json:"source"`
	Locales  []string `json:"locales"`
	Writable bool     `json:"writable"`
}

type emailTemplateGetResponse struct {
	Name      string `json:"name"`
	Locale    string `json:"locale"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	Source    string `json:"source"`
	Writable  bool   `json:"writable"`
	UpdatedAt string `json:"updated_at,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`
}

type emailTemplatePutRequest struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
	Locale  string `json:"locale"`
}

type emailTemplatePreviewResponse struct {
	Name    string `json:"name"`
	Locale  string `json:"locale"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type emailTemplateTestSendRequest struct {
	To     string `json:"to"`
	Locale string `json:"locale"`
}

func (h *EmailTemplateAdminHandler) audit(r *http.Request, action adminapp.AuditAction, resourceID string, details map[string]interface{}, err error) {
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
		ResourceType: "email_template",
		ResourceID:   resourceID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}

func (h *EmailTemplateAdminHandler) defaultStoreID(ctx context.Context) (string, error) {
	st, err := h.stores.FindDefault(ctx)
	if err != nil {
		return "", err
	}
	if st == nil || strings.TrimSpace(st.ID) == "" {
		return "", apperror.NotFound("default store not found")
	}
	return st.ID, nil
}

func (h *EmailTemplateAdminHandler) parseLocaleParam(r *http.Request) (string, error) {
	raw := r.URL.Query().Get("locale")
	locale, err := mail.ParseTemplateLocale(raw)
	if err != nil {
		return "", apperror.Validation(err.Error())
	}
	return locale, nil
}

func (h *EmailTemplateAdminHandler) sampleEmailData(ctx context.Context) mail.EmailData {
	ed := h.brand.BrandEmailData(ctx, h.storeURL)
	if ed.StoreURL == "" {
		ed.StoreURL = h.storeURL
	}
	base := ed.StoreURL
	if base == "" {
		base = "https://shop.example"
	}
	ed.Data = map[string]interface{}{
		"OrderID":        "ord-sample-1",
		"InvoiceNumber":  int64(1001),
		"FirstName":      "Alex",
		"Guest":          false,
		"ResetURL":       base + "/auth/reset-password?token=sample-reset",
		"VerifyURL":      base + "/auth/verify-email?token=sample-verify",
		"ClaimURL":       base + "/account/orders/claim?claim_token=sample-claim",
		"CartURL":        base + "/cart",
		"NewEmail":       "new@example.com",
		"ExpiresIn":      "1 hour",
		"TrackingNumber": "TRACK123",
		"Carrier":        "demo-carrier",
		"Codes":          []string{"ABCD-EFGH-IJKL"},
		"Items": []map[string]interface{}{
			{"Name": "Sample Item", "Qty": 1, "Price": "19.99", "VariantID": "var-1", "SKU": "SKU-1"},
		},
		"ItemCount": 1,
		"Total":     "19.99",
	}
	return ed
}

func (h *EmailTemplateAdminHandler) renderNamed(ctx context.Context, storeID, name, locale, to string) (mail.Message, error) {
	ed := h.sampleEmailData(ctx)
	res, err := mail.RenderWithOverrides(ctx, h.templates, mail.RepoOverrideSource{Repo: h.repo}, storeID, name, locale, to, ed)
	if err != nil {
		return mail.Message{}, err
	}
	return mail.ApplyLayout(h.layout, res.Message, ed)
}

// List handles GET /api/v1/admin/email-templates.
func (h *EmailTemplateAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		storeID, err := h.defaultStoreID(r.Context())
		if err != nil {
			h.audit(r, adminapp.AuditEmailTemplateList, "", nil, err)
			httpshared.JSONError(w, err)
			return
		}
		rows, err := h.repo.ListByStore(r.Context(), storeID)
		if err != nil {
			h.audit(r, adminapp.AuditEmailTemplateList, "", nil, err)
			httpshared.JSONError(w, err)
			return
		}
		localesByName := map[string][]string{}
		dbExact := map[string]struct{}{} // name|locale
		for _, row := range rows {
			localesByName[row.Name] = append(localesByName[row.Name], row.Locale)
			dbExact[row.Name+"|"+row.Locale] = struct{}{}
		}
		names := h.templates.Names()
		out := make([]emailTemplateListItem, 0, len(names))
		for _, name := range names {
			item := emailTemplateListItem{
				Name:     name,
				Writable: mail.IsOverridableName(name),
				Locales:  localesByName[name],
			}
			if item.Locales == nil {
				item.Locales = []string{}
			}
			src, ok := h.templates.Source(name)
			origin := mail.OriginBuiltin
			if ok && src.Origin != "" {
				origin = src.Origin
			}
			// Effective list source for empty locale (store default): db wins when present.
			if _, hasDB := dbExact[name+"|"]; hasDB && mail.IsOverridableName(name) {
				item.Source = emailTemplateOriginDB
			} else {
				item.Source = origin
			}
			out = append(out, item)
		}
		h.audit(r, adminapp.AuditEmailTemplateList, "", map[string]interface{}{"count": len(out)}, nil)
		httpshared.JSON(w, http.StatusOK, out)
	}
}

// Get handles GET /api/v1/admin/email-templates/{name}.
func (h *EmailTemplateAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSpace(r.PathValue("name"))
		if name == "" || !h.templates.Has(name) {
			httpshared.JSONError(w, apperror.NotFound("email template not found"))
			return
		}
		locale, err := h.parseLocaleParam(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		storeID, err := h.defaultStoreID(r.Context())
		if err != nil {
			h.audit(r, adminapp.AuditEmailTemplateRead, name, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		resp := emailTemplateGetResponse{
			Name:     name,
			Locale:   locale,
			Writable: mail.IsOverridableName(name),
		}
		if mail.IsOverridableName(name) {
			row, getErr := h.repo.Get(r.Context(), storeID, name, locale)
			if getErr != nil {
				h.audit(r, adminapp.AuditEmailTemplateRead, name, map[string]interface{}{"locale": locale}, getErr)
				httpshared.JSONError(w, getErr)
				return
			}
			if row != nil {
				resp.Subject = row.Subject
				resp.Body = row.Body
				resp.Source = emailTemplateOriginDB
				resp.UpdatedBy = row.UpdatedBy
				if !row.UpdatedAt.IsZero() {
					resp.UpdatedAt = row.UpdatedAt.UTC().Format(time.RFC3339)
				}
				h.audit(r, adminapp.AuditEmailTemplateRead, name, map[string]interface{}{"locale": locale, "source": resp.Source}, nil)
				httpshared.JSON(w, http.StatusOK, resp)
				return
			}
		}
		src, ok := h.templates.Source(name)
		if !ok {
			h.audit(r, adminapp.AuditEmailTemplateRead, name, map[string]interface{}{"locale": locale}, apperror.NotFound("email template source not found"))
			httpshared.JSONError(w, apperror.NotFound("email template source not found"))
			return
		}
		resp.Subject = src.Subject
		resp.Body = src.Body
		resp.Source = src.Origin
		if resp.Source == "" {
			resp.Source = mail.OriginBuiltin
		}
		h.audit(r, adminapp.AuditEmailTemplateRead, name, map[string]interface{}{"locale": locale, "source": resp.Source}, nil)
		httpshared.JSON(w, http.StatusOK, resp)
	}
}

// Put handles PUT /api/v1/admin/email-templates/{name}.
func (h *EmailTemplateAdminHandler) Put() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSpace(r.PathValue("name"))
		if name == "" || !h.templates.Has(name) {
			httpshared.JSONError(w, apperror.NotFound("email template not found"))
			return
		}
		var req emailTemplatePutRequest
		if err := decodeStrict(r, &req); err != nil {
			h.audit(r, adminapp.AuditEmailTemplateUpdate, name, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		locale := req.Locale
		if locale == "" {
			locale = r.URL.Query().Get("locale")
		}
		parsedLocale, err := mail.ParseTemplateLocale(locale)
		if err != nil {
			err = apperror.Validation(err.Error())
			h.audit(r, adminapp.AuditEmailTemplateUpdate, name, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		storeID, err := h.defaultStoreID(r.Context())
		if err != nil {
			h.audit(r, adminapp.AuditEmailTemplateUpdate, name, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		actor := adminIDFromRequest(r)
		row := &mail.EmailTemplate{
			StoreID:   storeID,
			Name:      name,
			Locale:    parsedLocale,
			Subject:   req.Subject,
			Body:      req.Body,
			UpdatedBy: actor,
		}
		err = h.repo.Upsert(r.Context(), row)
		h.audit(r, adminapp.AuditEmailTemplateUpdate, name, map[string]interface{}{
			"locale": parsedLocale,
		}, err)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, emailTemplateGetResponse{
			Name:      name,
			Locale:    row.Locale,
			Subject:   row.Subject,
			Body:      row.Body,
			Source:    emailTemplateOriginDB,
			Writable:  true,
			UpdatedAt: row.UpdatedAt.UTC().Format(time.RFC3339),
			UpdatedBy: row.UpdatedBy,
		})
	}
}

// Delete handles DELETE /api/v1/admin/email-templates/{name}.
func (h *EmailTemplateAdminHandler) Delete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSpace(r.PathValue("name"))
		if name == "" || !h.templates.Has(name) {
			httpshared.JSONError(w, apperror.NotFound("email template not found"))
			return
		}
		locale, err := h.parseLocaleParam(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		storeID, err := h.defaultStoreID(r.Context())
		if err != nil {
			h.audit(r, adminapp.AuditEmailTemplateDelete, name, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		err = h.repo.Delete(r.Context(), storeID, name, locale)
		h.audit(r, adminapp.AuditEmailTemplateDelete, name, map[string]interface{}{"locale": locale}, err)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// Preview handles POST /api/v1/admin/email-templates/{name}/preview.
func (h *EmailTemplateAdminHandler) Preview() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSpace(r.PathValue("name"))
		if name == "" || !h.templates.Has(name) {
			httpshared.JSONError(w, apperror.NotFound("email template not found"))
			return
		}
		locale, err := h.parseLocaleParam(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		// Optional body locale override for preview without query.
		var body struct {
			Locale string `json:"locale"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.TrimSpace(body.Locale) != "" {
			locale, err = mail.ParseTemplateLocale(body.Locale)
			if err != nil {
				httpshared.JSONError(w, apperror.Validation(err.Error()))
				return
			}
		}
		storeID, err := h.defaultStoreID(r.Context())
		if err != nil {
			h.audit(r, adminapp.AuditEmailTemplatePreview, name, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		msg, err := h.renderNamed(r.Context(), storeID, name, locale, "preview@example.com")
		h.audit(r, adminapp.AuditEmailTemplatePreview, name, map[string]interface{}{"locale": locale}, err)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, emailTemplatePreviewResponse{
			Name:    name,
			Locale:  locale,
			Subject: msg.Subject,
			Body:    msg.Body,
		})
	}
}

// TestSend handles POST /api/v1/admin/email-templates/{name}/test-send.
func (h *EmailTemplateAdminHandler) TestSend() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSpace(r.PathValue("name"))
		if name == "" || !h.templates.Has(name) {
			httpshared.JSONError(w, apperror.NotFound("email template not found"))
			return
		}
		if h.mailer == nil {
			err := apperror.Unavailable("mailer not configured")
			h.audit(r, adminapp.AuditEmailTemplateTestSend, name, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		var req emailTemplateTestSendRequest
		if err := decodeStrict(r, &req); err != nil {
			h.audit(r, adminapp.AuditEmailTemplateTestSend, name, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		to := strings.TrimSpace(req.To)
		if to == "" {
			err := apperror.Validation("to is required")
			h.audit(r, adminapp.AuditEmailTemplateTestSend, name, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		locale := req.Locale
		if locale == "" {
			locale = r.URL.Query().Get("locale")
		}
		parsedLocale, err := mail.ParseTemplateLocale(locale)
		if err != nil {
			err = apperror.Validation(err.Error())
			h.audit(r, adminapp.AuditEmailTemplateTestSend, name, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		storeID, err := h.defaultStoreID(r.Context())
		if err != nil {
			h.audit(r, adminapp.AuditEmailTemplateTestSend, name, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		msg, err := h.renderNamed(r.Context(), storeID, name, parsedLocale, to)
		if err != nil {
			h.audit(r, adminapp.AuditEmailTemplateTestSend, name, map[string]interface{}{"locale": parsedLocale}, err)
			httpshared.JSONError(w, err)
			return
		}
		err = h.mailer.Send(r.Context(), msg)
		h.audit(r, adminapp.AuditEmailTemplateTestSend, name, map[string]interface{}{
			"locale": parsedLocale,
			"to":     to,
		}, err)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"sent":    true,
			"to":      to,
			"subject": msg.Subject,
		})
	}
}
