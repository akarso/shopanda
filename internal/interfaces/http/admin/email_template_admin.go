package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	netmail "net/mail"
	"strings"
	"sync"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	storeidentity "github.com/akarso/shopanda/internal/application/storeidentity"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/store"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/logger"
)

const (
	emailTemplateOriginDB    = "db"
	emailTemplateTestPrefix  = "[TEST] "
	emailTemplateTestSendTTL = 15 * time.Second
	emailTemplateTestSendGap = 30 * time.Second
)

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

	testSendMu   sync.Mutex
	testSendLast map[string]time.Time
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
		templates:    cfg.Templates,
		layout:       cfg.Layout,
		repo:         cfg.Repo,
		mailer:       cfg.Mailer,
		stores:       cfg.Stores,
		brand:        cfg.Brand,
		storeURL:     strings.TrimRight(strings.TrimSpace(cfg.StoreURL), "/"),
		auditor:      auditor,
		testSendLast: map[string]time.Time{},
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
	Name              string `json:"name"`
	RequestedLocale   string `json:"requested_locale"`
	ResolvedLocale    string `json:"resolved_locale"`
	Source            string `json:"source"`
	Fallback          bool   `json:"fallback"`
	Subject           string `json:"subject"`
	Body              string `json:"body"`
	AttachmentOmitted string `json:"attachment_omitted,omitempty"`
}

type emailTemplateTestSendRequest struct {
	To     string `json:"to"`
	Locale string `json:"locale"`
}

type emailTemplateLocaleBody struct {
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

func (h *EmailTemplateAdminHandler) sampleEmailData(ctx context.Context, name string) mail.EmailData {
	ed := h.brand.BrandEmailData(ctx, h.storeURL)
	if ed.StoreURL == "" {
		ed.StoreURL = h.storeURL
	}
	base := ed.StoreURL
	if base == "" {
		base = "https://shop.example"
	}
	common := map[string]interface{}{
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
		"ItemCount":      1,
		"Total":          "19.99",
	}
	switch name {
	case "cart_recovery":
		common["Items"] = []map[string]interface{}{
			{
				"Name":      "Sample Item",
				"Quantity":  2,
				"UnitPrice": "9.99",
				"LineTotal": "19.98",
				"VariantID": "var-1",
				"SKU":       "SKU-1",
			},
		}
	default:
		common["Items"] = []map[string]interface{}{
			{"Name": "Sample Item", "Qty": 1, "Price": "19.99", "VariantID": "var-1", "SKU": "SKU-1"},
		}
	}
	ed.Data = common
	return ed
}

func attachmentOmittedNote(name string) string {
	switch name {
	case "invoice_created":
		return "invoice PDF"
	case "voucher_issued":
		return "voucher PDF"
	default:
		return ""
	}
}

func (h *EmailTemplateAdminHandler) renderNamed(ctx context.Context, storeID, name, locale, to string, exact bool) (mail.RenderResult, mail.EmailData, error) {
	ed := h.sampleEmailData(ctx, name)
	var opts []mail.RenderOption
	if exact {
		opts = append(opts, mail.WithExactLocale())
	}
	res, err := mail.RenderWithOverrides(ctx, h.templates, mail.RepoOverrideSource{Repo: h.repo}, storeID, name, locale, to, ed, opts...)
	if err != nil {
		return mail.RenderResult{}, ed, err
	}
	msg, err := mail.ApplyLayout(h.layout, res.Message, ed)
	if err != nil {
		return mail.RenderResult{}, ed, err
	}
	res.Message = msg
	return res, ed, nil
}

func (h *EmailTemplateAdminHandler) allowTestSend(adminID string) error {
	key := strings.TrimSpace(adminID)
	if key == "" {
		key = "anonymous"
	}
	now := time.Now()
	h.testSendMu.Lock()
	defer h.testSendMu.Unlock()
	if last, ok := h.testSendLast[key]; ok && now.Sub(last) < emailTemplateTestSendGap {
		return apperror.Validation("test-send rate limited; try again shortly")
	}
	h.testSendLast[key] = now
	return nil
}

func validateTestMailbox(to string) (string, error) {
	to = strings.TrimSpace(to)
	if to == "" {
		return "", apperror.Validation("to is required")
	}
	addr, err := netmail.ParseAddress(to)
	if err != nil || addr == nil || strings.TrimSpace(addr.Address) == "" {
		return "", apperror.Validation("invalid email address")
	}
	return addr.Address, nil
}

func decodeOptionalLocaleBody(r *http.Request, queryLocale string) (string, error) {
	locale := queryLocale
	if r.Body == nil {
		return locale, nil
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return "", apperror.Validation("invalid request body")
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return locale, nil
	}
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.DisallowUnknownFields()
	var body emailTemplateLocaleBody
	if err := dec.Decode(&body); err != nil {
		return "", apperror.Validation("invalid request body")
	}
	if dec.More() {
		return "", apperror.Validation("invalid request body")
	}
	if strings.TrimSpace(body.Locale) == "" {
		return locale, nil
	}
	parsed, err := mail.ParseTemplateLocale(body.Locale)
	if err != nil {
		return "", apperror.Validation(err.Error())
	}
	return parsed, nil
}

func mapMailerError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*apperror.Error); ok {
		return err
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "address") || strings.Contains(lower, "recipient") || strings.Contains(lower, "mailbox") {
		return apperror.Validation("invalid email address")
	}
	return apperror.Unavailable("mail send failed")
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
		rows, err := h.repo.ListMetaByStore(r.Context(), storeID)
		if err != nil {
			h.audit(r, adminapp.AuditEmailTemplateList, "", nil, err)
			httpshared.JSONError(w, err)
			return
		}
		localesByName := map[string][]string{}
		hasDB := map[string]bool{}
		for _, row := range rows {
			if !mail.IsOverridableName(row.Name) {
				continue // ignore stray non-allowlisted rows
			}
			localesByName[row.Name] = append(localesByName[row.Name], row.Locale)
			hasDB[row.Name] = true
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
			if hasDB[name] {
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
		if !mail.IsOverridableName(name) {
			err := apperror.Validation("email template is not overridable")
			h.audit(r, adminapp.AuditEmailTemplateUpdate, name, nil, err)
			httpshared.JSONError(w, err)
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
		sample := h.sampleEmailData(r.Context(), name)
		if err := mail.ValidateRenderable(name, req.Subject, req.Body, sample); err != nil {
			err = apperror.Validation(err.Error())
			h.audit(r, adminapp.AuditEmailTemplateUpdate, name, map[string]interface{}{"locale": parsedLocale}, err)
			httpshared.JSONError(w, err)
			return
		}
		actor := strings.TrimSpace(adminIDFromRequest(r))
		if actor == "" {
			err := apperror.Unauthorized("admin identity required")
			h.audit(r, adminapp.AuditEmailTemplateUpdate, name, nil, err)
			httpshared.JSONError(w, err)
			return
		}
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
		if !mail.IsOverridableName(name) {
			err := apperror.Validation("email template is not overridable")
			h.audit(r, adminapp.AuditEmailTemplateDelete, name, nil, err)
			httpshared.JSONError(w, err)
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
		locale, err = decodeOptionalLocaleBody(r, locale)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		exact := r.URL.Query().Get("exact") == "1" || strings.EqualFold(r.URL.Query().Get("exact"), "true")
		storeID, err := h.defaultStoreID(r.Context())
		if err != nil {
			h.audit(r, adminapp.AuditEmailTemplatePreview, name, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		res, _, err := h.renderNamed(r.Context(), storeID, name, locale, "preview@example.com", exact)
		h.audit(r, adminapp.AuditEmailTemplatePreview, name, map[string]interface{}{
			"locale": locale,
			"exact":  exact,
			"source": res.Source,
		}, err)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, emailTemplatePreviewResponse{
			Name:              name,
			RequestedLocale:   locale,
			ResolvedLocale:    res.ResolvedLocale,
			Source:            res.Source,
			Fallback:          res.Fallback,
			Subject:           res.Message.Subject,
			Body:              res.Message.Body,
			AttachmentOmitted: attachmentOmittedNote(name),
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
		if !mail.IsOverridableName(name) {
			err := apperror.Validation("test-send is limited to overridable templates")
			h.audit(r, adminapp.AuditEmailTemplateTestSend, name, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		if h.mailer == nil {
			err := apperror.Unavailable("mailer not configured")
			h.audit(r, adminapp.AuditEmailTemplateTestSend, name, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		adminID := adminIDFromRequest(r)
		if err := h.allowTestSend(adminID); err != nil {
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
		to, err := validateTestMailbox(req.To)
		if err != nil {
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
		exact := r.URL.Query().Get("exact") == "1" || strings.EqualFold(r.URL.Query().Get("exact"), "true")
		storeID, err := h.defaultStoreID(r.Context())
		if err != nil {
			h.audit(r, adminapp.AuditEmailTemplateTestSend, name, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		res, _, err := h.renderNamed(r.Context(), storeID, name, parsedLocale, to, exact)
		if err != nil {
			h.audit(r, adminapp.AuditEmailTemplateTestSend, name, map[string]interface{}{"locale": parsedLocale}, err)
			httpshared.JSONError(w, err)
			return
		}
		msg := res.Message
		if !strings.HasPrefix(msg.Subject, emailTemplateTestPrefix) {
			msg.Subject = emailTemplateTestPrefix + msg.Subject
		}
		sendCtx, cancel := context.WithTimeout(r.Context(), emailTemplateTestSendTTL)
		defer cancel()
		err = mapMailerError(h.mailer.Send(sendCtx, msg))
		h.audit(r, adminapp.AuditEmailTemplateTestSend, name, map[string]interface{}{
			"locale": parsedLocale,
			"to":     to,
			"source": res.Source,
			"exact":  exact,
		}, err)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"sent":               true,
			"to":                 to,
			"subject":            msg.Subject,
			"source":             res.Source,
			"requested_locale":   parsedLocale,
			"resolved_locale":    res.ResolvedLocale,
			"fallback":           res.Fallback,
			"attachment_omitted": attachmentOmittedNote(name),
		})
	}
}
