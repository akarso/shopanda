package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/application/notification"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type etStoreRepo struct {
	store.StoreRepository
	st  *store.Store
	err error
}

func (r *etStoreRepo) FindDefault(context.Context) (*store.Store, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.st, nil
}

type etTemplateRepo struct {
	rows map[string]*mail.EmailTemplate // key name|locale
}

func (r *etTemplateRepo) key(storeID, name, locale string) string {
	return storeID + "|" + name + "|" + locale
}

func (r *etTemplateRepo) Get(_ context.Context, storeID, name, locale string) (*mail.EmailTemplate, error) {
	locale, err := mail.ParseTemplateLocale(locale)
	if err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if r.rows == nil {
		return nil, nil
	}
	row := r.rows[r.key(storeID, name, locale)]
	if row == nil {
		return nil, nil
	}
	cp := *row
	return &cp, nil
}

func (r *etTemplateRepo) Upsert(_ context.Context, t *mail.EmailTemplate) error {
	if err := t.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	if r.rows == nil {
		r.rows = map[string]*mail.EmailTemplate{}
	}
	t.UpdatedAt = time.Now().UTC()
	cp := *t
	r.rows[r.key(t.StoreID, t.Name, t.Locale)] = &cp
	return nil
}

func (r *etTemplateRepo) Delete(_ context.Context, storeID, name, locale string) error {
	locale, err := mail.ParseTemplateLocale(locale)
	if err != nil {
		return apperror.Validation(err.Error())
	}
	if r.rows != nil {
		delete(r.rows, r.key(storeID, name, locale))
	}
	return nil
}

func (r *etTemplateRepo) ListByStore(_ context.Context, storeID string) ([]mail.EmailTemplate, error) {
	var out []mail.EmailTemplate
	for _, row := range r.rows {
		if row.StoreID == storeID {
			out = append(out, *row)
		}
	}
	return out, nil
}

func (r *etTemplateRepo) ListMetaByStore(_ context.Context, storeID string) ([]mail.EmailTemplateMeta, error) {
	var out []mail.EmailTemplateMeta
	for _, row := range r.rows {
		if row.StoreID != storeID {
			continue
		}
		out = append(out, mail.EmailTemplateMeta{
			Name:      row.Name,
			Locale:    row.Locale,
			UpdatedAt: row.UpdatedAt,
			UpdatedBy: row.UpdatedBy,
		})
	}
	return out, nil
}

type etMailerStub struct {
	sent    []mail.Message
	sendErr error
}

func (m *etMailerStub) Send(_ context.Context, msg mail.Message) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sent = append(m.sent, msg)
	return nil
}

func newEmailTemplateAdmin(t *testing.T, repo *etTemplateRepo, mailer mail.Mailer) (*admin.EmailTemplateAdminHandler, *http.ServeMux) {
	t.Helper()
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	st := &store.Store{ID: "store-1", Name: "Main", Language: "en", IsDefault: true}
	h := admin.NewEmailTemplateAdminHandler(admin.EmailTemplateAdminConfig{
		Templates: tmpl,
		Repo:      repo,
		Mailer:    mailer,
		Stores:    &etStoreRepo{st: st},
		StoreURL:  "https://shop.example",
		Auditor:   adminapp.NewAuditor(logger.NewWithWriter(ioDiscard{}, "error")),
	})
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/email-templates", h.List())
	mux.Handle("GET /api/v1/admin/email-templates/{name}", h.Get())
	mux.Handle("PUT /api/v1/admin/email-templates/{name}", h.Put())
	mux.Handle("DELETE /api/v1/admin/email-templates/{name}", h.Delete())
	mux.Handle("POST /api/v1/admin/email-templates/{name}/preview", h.Preview())
	mux.Handle("POST /api/v1/admin/email-templates/{name}/test-send", h.TestSend())
	return h, mux
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func withAdminID(r *http.Request) *http.Request {
	return r.WithContext((&adminapp.AdminContext{AdminID: "admin-1"}).WithContext(r.Context()))
}

type emailTemplateGetDTO struct {
	Name      string `json:"name"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	Source    string `json:"source"`
	UpdatedBy string `json:"updated_by"`
}

func TestEmailTemplateAdmin_PutThenGet(t *testing.T) {
	repo := &etTemplateRepo{}
	_, mux := newEmailTemplateAdmin(t, repo, &etMailerStub{})

	body := `{"subject":"Thanks {{.Data.OrderID}}","body":"<p>db-{{.Data.OrderID}}</p>"}`
	req := withAdminID(httptest.NewRequest(http.MethodPut, "/api/v1/admin/email-templates/order_confirmed", strings.NewReader(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = withAdminID(httptest.NewRequest(http.MethodGet, "/api/v1/admin/email-templates/order_confirmed", nil))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data emailTemplateGetDTO `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Source != "db" || envelope.Data.Body != "<p>db-{{.Data.OrderID}}</p>" {
		t.Fatalf("get = %#v", envelope.Data)
	}
	if envelope.Data.UpdatedBy != "admin-1" {
		t.Fatalf("updated_by = %q", envelope.Data.UpdatedBy)
	}
}

func TestEmailTemplateAdmin_PutBrokenLeavesNoRow(t *testing.T) {
	repo := &etTemplateRepo{}
	_, mux := newEmailTemplateAdmin(t, repo, &etMailerStub{})

	for _, tc := range []struct {
		name string
		body string
	}{
		{"unclosed", `{"subject":"Hi {{","body":"<p>x</p>"}`},
		{"crlf_literal", `{"subject":"Hi\r\nBcc: evil@x.com","body":"<p>x</p>"}`},
		{"crlf_render", `{"subject":"{{printf \"a\\r\\nb\"}}","body":"<p>x</p>"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := withAdminID(httptest.NewRequest(http.MethodPut, "/api/v1/admin/email-templates/order_confirmed", strings.NewReader(tc.body)))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code == http.StatusOK {
				t.Fatalf("expected validation error, got 200 body=%s", rec.Body.String())
			}
			if len(repo.rows) != 0 {
				t.Fatalf("broken PUT must not upsert, rows=%d", len(repo.rows))
			}
		})
	}

	// Later render still uses file/builtin (no bad row).
	req := withAdminID(httptest.NewRequest(http.MethodPost, "/api/v1/admin/email-templates/order_confirmed/preview", bytes.NewReader([]byte("{}"))))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Source string `json:"source"`
			Body   string `json:"body"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Source == "db" {
		t.Fatalf("expected file/builtin after rejected PUT, got %#v", envelope.Data)
	}
}

func TestEmailTemplateAdmin_PutAuthNameRejected(t *testing.T) {
	repo := &etTemplateRepo{}
	_, mux := newEmailTemplateAdmin(t, repo, &etMailerStub{})
	for _, name := range []string{"password_reset", "order_claim", "voucher_issued"} {
		body := `{"subject":"x","body":"<p>x</p>"}`
		req := withAdminID(httptest.NewRequest(http.MethodPut, "/api/v1/admin/email-templates/"+name, strings.NewReader(body)))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatalf("%s: expected validation error, got 200", name)
		}
	}
}

func TestEmailTemplateAdmin_DeleteAuthNameRejected(t *testing.T) {
	repo := &etTemplateRepo{}
	_, mux := newEmailTemplateAdmin(t, repo, &etMailerStub{})
	req := withAdminID(httptest.NewRequest(http.MethodDelete, "/api/v1/admin/email-templates/password_reset", nil))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusNoContent || rec.Code == http.StatusOK {
		t.Fatalf("DELETE password_reset must not succeed, got %d", rec.Code)
	}
}

func TestEmailTemplateAdmin_PreviewDoesNotSend(t *testing.T) {
	repo := &etTemplateRepo{}
	mailer := &etMailerStub{}
	_, mux := newEmailTemplateAdmin(t, repo, mailer)
	req := withAdminID(httptest.NewRequest(http.MethodPost, "/api/v1/admin/email-templates/order_confirmed/preview", bytes.NewReader([]byte("{}"))))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(mailer.sent) != 0 {
		t.Fatalf("preview must not call Send, got %d", len(mailer.sent))
	}
	var envelope struct {
		Data struct {
			Subject         string `json:"subject"`
			Body            string `json:"body"`
			Source          string `json:"source"`
			RequestedLocale string `json:"requested_locale"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Subject == "" || envelope.Data.Body == "" {
		t.Fatalf("preview empty: %#v", envelope.Data)
	}
	if envelope.Data.Source == "" {
		t.Fatal("preview must report source")
	}
}

func TestEmailTemplateAdmin_PreviewInvalidJSON(t *testing.T) {
	repo := &etTemplateRepo{}
	_, mux := newEmailTemplateAdmin(t, repo, &etMailerStub{})
	req := withAdminID(httptest.NewRequest(http.MethodPost, "/api/v1/admin/email-templates/order_confirmed/preview",
		strings.NewReader(`{"locale":`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("invalid JSON must be 422, got 200")
	}
}

func TestEmailTemplateAdmin_PreviewEmptyBodyOK(t *testing.T) {
	repo := &etTemplateRepo{}
	_, mux := newEmailTemplateAdmin(t, repo, &etMailerStub{})
	req := withAdminID(httptest.NewRequest(http.MethodPost, "/api/v1/admin/email-templates/order_confirmed/preview", nil))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty body status=%d", rec.Code)
	}
}

func TestEmailTemplateAdmin_PreviewLocaleFallbackVisible(t *testing.T) {
	repo := &etTemplateRepo{}
	_, mux := newEmailTemplateAdmin(t, repo, &etMailerStub{})

	// Empty-locale DB row only.
	req := withAdminID(httptest.NewRequest(http.MethodPut, "/api/v1/admin/email-templates/order_confirmed",
		strings.NewReader(`{"subject":"Default DB","body":"<p>default-db</p>"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %s", rec.Body.String())
	}

	// GET ?locale=de → exact miss → file/builtin source text
	req = withAdminID(httptest.NewRequest(http.MethodGet, "/api/v1/admin/email-templates/order_confirmed?locale=de", nil))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var getEnv struct {
		Data emailTemplateGetDTO `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &getEnv); err != nil {
		t.Fatal(err)
	}
	if getEnv.Data.Source == "db" || getEnv.Data.Body == "<p>default-db</p>" {
		t.Fatalf("GET exact locale must not return empty-locale row: %#v", getEnv.Data)
	}

	// Preview ?locale=de → falls through to empty-locale DB
	req = withAdminID(httptest.NewRequest(http.MethodPost, "/api/v1/admin/email-templates/order_confirmed/preview?locale=de",
		bytes.NewReader([]byte("{}"))))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var prevEnv struct {
		Data struct {
			Source          string `json:"source"`
			ResolvedLocale  string `json:"resolved_locale"`
			Fallback        bool   `json:"fallback"`
			Body            string `json:"body"`
			RequestedLocale string `json:"requested_locale"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &prevEnv); err != nil {
		t.Fatal(err)
	}
	if prevEnv.Data.Source != "db" || !prevEnv.Data.Fallback || prevEnv.Data.ResolvedLocale != "" {
		t.Fatalf("preview fallback = %#v", prevEnv.Data)
	}
	if !strings.Contains(prevEnv.Data.Body, "default-db") {
		t.Fatalf("expected default-db body, got %q", prevEnv.Data.Body)
	}

	// exact=1 skips empty-locale fallthrough
	req = withAdminID(httptest.NewRequest(http.MethodPost, "/api/v1/admin/email-templates/order_confirmed/preview?locale=de&exact=1",
		bytes.NewReader([]byte("{}"))))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &prevEnv); err != nil {
		t.Fatal(err)
	}
	if prevEnv.Data.Source == "db" && strings.Contains(prevEnv.Data.Body, "default-db") {
		t.Fatalf("exact preview must not use empty-locale row: %#v", prevEnv.Data)
	}
}

func TestEmailTemplateAdmin_CartRecoverySampleFields(t *testing.T) {
	repo := &etTemplateRepo{}
	_, mux := newEmailTemplateAdmin(t, repo, &etMailerStub{})
	req := withAdminID(httptest.NewRequest(http.MethodPut, "/api/v1/admin/email-templates/cart_recovery",
		strings.NewReader(`{"subject":"Cart","body":"<p>{{range .Data.Items}}{{.Quantity}} {{.UnitPrice}} {{.LineTotal}} {{.Price}}{{end}}</p>"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %s", rec.Body.String())
	}
	req = withAdminID(httptest.NewRequest(http.MethodPost, "/api/v1/admin/email-templates/cart_recovery/preview", bytes.NewReader([]byte("{}"))))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview: %s", rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "2") || !strings.Contains(body, "9.99") || !strings.Contains(body, "19.98") {
		t.Fatalf("cart recovery sample missing Quantity/UnitPrice/LineTotal/Price: %s", body)
	}
}

func TestEmailTemplateAdmin_CartRecoveryBuiltinUsesPrice(t *testing.T) {
	repo := &etTemplateRepo{}
	_, mux := newEmailTemplateAdmin(t, repo, &etMailerStub{})
	// No DB row → builtin body reads .Quantity and .Price.
	req := withAdminID(httptest.NewRequest(http.MethodPost, "/api/v1/admin/email-templates/cart_recovery/preview", bytes.NewReader([]byte("{}"))))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "9.99") || !strings.Contains(rec.Body.String(), "× 2") {
		t.Fatalf("builtin cart_recovery sample missing Price/Quantity: %s", rec.Body.String())
	}
}

func TestEmailTemplateAdmin_PutRequiresAdminActor(t *testing.T) {
	repo := &etTemplateRepo{}
	_, mux := newEmailTemplateAdmin(t, repo, &etMailerStub{})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/email-templates/order_confirmed",
		strings.NewReader(`{"subject":"Hi","body":"<p>x</p>"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("PUT without admin id must fail")
	}
	if len(repo.rows) != 0 {
		t.Fatal("must not upsert as system")
	}
}

func TestEmailTemplateAdmin_TestSend(t *testing.T) {
	repo := &etTemplateRepo{}
	mailer := &etMailerStub{}
	_, mux := newEmailTemplateAdmin(t, repo, mailer)
	req := withAdminID(httptest.NewRequest(http.MethodPost, "/api/v1/admin/email-templates/order_confirmed/test-send",
		strings.NewReader(`{"to":"ops@example.com"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(mailer.sent) != 1 || mailer.sent[0].To != "ops@example.com" {
		t.Fatalf("sent=%#v", mailer.sent)
	}
	if !strings.HasPrefix(mailer.sent[0].Subject, "[TEST] ") {
		t.Fatalf("subject must have [TEST] prefix: %q", mailer.sent[0].Subject)
	}
	if !strings.Contains(mailer.sent[0].Body, "ord-sample-1") && !strings.Contains(mailer.sent[0].Body, "Sample") {
		t.Fatalf("sample payload not applied: %#v", mailer.sent[0])
	}
}

func TestEmailTemplateAdmin_TestSendAuthBlocked(t *testing.T) {
	repo := &etTemplateRepo{}
	mailer := &etMailerStub{}
	_, mux := newEmailTemplateAdmin(t, repo, mailer)
	for _, name := range []string{"password_reset", "order_claim", "voucher_issued"} {
		req := withAdminID(httptest.NewRequest(http.MethodPost, "/api/v1/admin/email-templates/"+name+"/test-send",
			strings.NewReader(`{"to":"ops@example.com"}`)))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatalf("%s: test-send must be blocked", name)
		}
		if len(mailer.sent) != 0 {
			t.Fatalf("%s: Send must not be called, sent=%d", name, len(mailer.sent))
		}
	}
}

func TestEmailTemplateAdmin_TestSendInvalidTo(t *testing.T) {
	repo := &etTemplateRepo{}
	mailer := &etMailerStub{}
	_, mux := newEmailTemplateAdmin(t, repo, mailer)
	req := withAdminID(httptest.NewRequest(http.MethodPost, "/api/v1/admin/email-templates/order_confirmed/test-send",
		strings.NewReader(`{"to":"not-an-email"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("invalid to must fail")
	}
	if len(mailer.sent) != 0 {
		t.Fatal("Send must not dial on bad address")
	}
	// Bad address must not stamp the rate window — a valid send still works.
	req = withAdminID(httptest.NewRequest(http.MethodPost, "/api/v1/admin/email-templates/order_confirmed/test-send",
		strings.NewReader(`{"to":"ops@example.com"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid send after bad to: %d %s", rec.Code, rec.Body.String())
	}
}

func TestEmailTemplateAdmin_TestSendRateLimited(t *testing.T) {
	repo := &etTemplateRepo{}
	mailer := &etMailerStub{}
	_, mux := newEmailTemplateAdmin(t, repo, mailer)
	body := `{"to":"ops@example.com"}`
	req := withAdminID(httptest.NewRequest(http.MethodPost, "/api/v1/admin/email-templates/order_confirmed/test-send", strings.NewReader(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first send: %d %s", rec.Code, rec.Body.String())
	}
	req = withAdminID(httptest.NewRequest(http.MethodPost, "/api/v1/admin/email-templates/order_confirmed/test-send", strings.NewReader(body)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second send want 429, got %d %s", rec.Code, rec.Body.String())
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("rate-limited Send count=%d", len(mailer.sent))
	}
}

func TestEmailTemplateAdmin_TestSendNilMailer(t *testing.T) {
	repo := &etTemplateRepo{}
	_, mux := newEmailTemplateAdmin(t, repo, nil)
	req := withAdminID(httptest.NewRequest(http.MethodPost, "/api/v1/admin/email-templates/order_confirmed/test-send",
		strings.NewReader(`{"to":"ops@example.com"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("nil mailer must fail")
	}
}

func TestEmailTemplateAdmin_ListSourceAnyLocale(t *testing.T) {
	repo := &etTemplateRepo{}
	_, mux := newEmailTemplateAdmin(t, repo, &etMailerStub{})
	req := withAdminID(httptest.NewRequest(http.MethodPut, "/api/v1/admin/email-templates/order_confirmed?locale=de",
		strings.NewReader(`{"subject":"DE","body":"<p>de</p>","locale":"de"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %s", rec.Body.String())
	}

	req = withAdminID(httptest.NewRequest(http.MethodGet, "/api/v1/admin/email-templates", nil))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var envelope struct {
		Data []struct {
			Name    string   `json:"name"`
			Source  string   `json:"source"`
			Locales []string `json:"locales"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range envelope.Data {
		if item.Name != "order_confirmed" {
			continue
		}
		found = true
		if item.Source != "db" {
			t.Fatalf("de-only override must set source=db, got %#v", item)
		}
		if len(item.Locales) != 1 || item.Locales[0] != "de" {
			t.Fatalf("locales = %#v", item.Locales)
		}
	}
	if !found {
		t.Fatal("order_confirmed missing from list")
	}
}

func TestEmailTemplateAdmin_GetFileWithoutRow(t *testing.T) {
	repo := &etTemplateRepo{}
	_, mux := newEmailTemplateAdmin(t, repo, &etMailerStub{})
	req := withAdminID(httptest.NewRequest(http.MethodGet, "/api/v1/admin/email-templates/order_confirmed", nil))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var envelope struct {
		Data emailTemplateGetDTO `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Source == "db" || envelope.Data.Subject == "" {
		t.Fatalf("expected file/builtin source text: %#v", envelope.Data)
	}
}

func TestEmailTemplateAdmin_DeleteRevertsToFile(t *testing.T) {
	repo := &etTemplateRepo{}
	_, mux := newEmailTemplateAdmin(t, repo, &etMailerStub{})

	req := withAdminID(httptest.NewRequest(http.MethodPut, "/api/v1/admin/email-templates/order_confirmed",
		strings.NewReader(`{"subject":"DB","body":"<p>db</p>"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %s", rec.Body.String())
	}

	req = withAdminID(httptest.NewRequest(http.MethodDelete, "/api/v1/admin/email-templates/order_confirmed", nil))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status=%d", rec.Code)
	}

	req = withAdminID(httptest.NewRequest(http.MethodGet, "/api/v1/admin/email-templates/order_confirmed", nil))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var envelope struct {
		Data emailTemplateGetDTO `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Source == "db" || envelope.Data.Body == "<p>db</p>" {
		t.Fatalf("expected file/builtin after delete: %#v", envelope.Data)
	}
}

func TestEmailTemplateAdmin_InvalidLocale(t *testing.T) {
	repo := &etTemplateRepo{}
	_, mux := newEmailTemplateAdmin(t, repo, &etMailerStub{})
	req := withAdminID(httptest.NewRequest(http.MethodPut, "/api/v1/admin/email-templates/order_confirmed",
		strings.NewReader(`{"subject":"x","body":"<p>x</p>","locale":"und"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("expected und rejected")
	}
}

func TestEmailTemplateAdmin_InvoiceAttachmentOmitted(t *testing.T) {
	repo := &etTemplateRepo{}
	_, mux := newEmailTemplateAdmin(t, repo, &etMailerStub{})
	req := withAdminID(httptest.NewRequest(http.MethodPost, "/api/v1/admin/email-templates/invoice_created/preview", bytes.NewReader([]byte("{}"))))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invoice PDF") {
		t.Fatalf("expected attachment_omitted note: %s", rec.Body.String())
	}
}
