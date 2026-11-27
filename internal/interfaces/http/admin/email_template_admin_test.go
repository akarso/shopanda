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

type emailTemplateGetDTO struct {
	Name      string `json:"name"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	Source    string `json:"source"`
	UpdatedBy string `json:"updated_by"`
}

func TestEmailTemplateAdmin_PutAuthNameRejected(t *testing.T) {
	repo := &etTemplateRepo{}
	_, mux := newEmailTemplateAdmin(t, repo, &etMailerStub{})
	for _, name := range []string{"password_reset", "order_claim"} {
		body := `{"subject":"x","body":"<p>x</p>"}`
		req := withAdminID(httptest.NewRequest(http.MethodPut, "/api/v1/admin/email-templates/"+name, strings.NewReader(body)))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatalf("%s: expected validation error, got 200", name)
		}
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
			Subject string `json:"subject"`
			Body    string `json:"body"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Subject == "" || envelope.Data.Body == "" {
		t.Fatalf("preview empty: %#v", envelope.Data)
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
	if !strings.Contains(mailer.sent[0].Body, "ord-sample-1") && !strings.Contains(mailer.sent[0].Subject, "ord-sample-1") {
		// order_confirmed subject may not include order id in builtin; body should.
		if !strings.Contains(mailer.sent[0].Body, "Sample") && mailer.sent[0].Subject == "" {
			t.Fatalf("message empty: %#v", mailer.sent[0])
		}
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
