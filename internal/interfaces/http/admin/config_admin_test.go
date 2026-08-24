package admin_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/interfaces/http/admin"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	domainCfg "github.com/akarso/shopanda/internal/domain/config"
	appconfig "github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

type mockConfigRepo struct {
	entries    map[string]interface{}
	allErr     error
	getErr     error
	setErr     error
	setManyErr error
}

func newMockConfigRepo() *mockConfigRepo {
	return &mockConfigRepo{entries: make(map[string]interface{})}
}

func (m *mockConfigRepo) Get(_ context.Context, key string) (interface{}, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.entries[key], nil
}

func (m *mockConfigRepo) Set(_ context.Context, key string, value interface{}) error {
	if m.setErr != nil {
		return m.setErr
	}
	m.entries[key] = value
	return nil
}

func (m *mockConfigRepo) SetMany(ctx context.Context, entries map[string]interface{}) error {
	if m.setManyErr != nil {
		return m.setManyErr
	}
	for key, value := range entries {
		if err := m.Set(ctx, key, value); err != nil {
			return err
		}
	}
	return nil
}

func (m *mockConfigRepo) Delete(_ context.Context, key string) error {
	delete(m.entries, key)
	return nil
}

func (m *mockConfigRepo) All(_ context.Context) ([]domainCfg.Entry, error) {
	if m.allErr != nil {
		return nil, m.allErr
	}
	out := make([]domainCfg.Entry, 0, len(m.entries))
	for key, value := range m.entries {
		out = append(out, domainCfg.Entry{Key: key, Value: value})
	}
	return out, nil
}

func testConfigAdminHandler(repo domainCfg.Repository, testEmail admin.SMTPTestFunc) *admin.ConfigAdminHandler {
	cfg := &appconfig.Config{}
	cfg.Mail.SMTP.Host = "smtp.default.test"
	cfg.Mail.SMTP.Port = 2525
	cfg.Mail.SMTP.From = "ops@example.com"
	cfg.Media.Storage = "local"
	cfg.Media.Local.BasePath = "./public/media"
	cfg.Media.Local.BaseURL = "/media"
	return admin.NewConfigAdminHandler(repo, cfg, testEmail, adminapp.NewAuditor(logger.NewWithWriter(io.Discard, "error")), nil)
}

func withAdminStoreScope(req *http.Request, storeID string) *http.Request {
	ctx := (&adminapp.AdminContext{StoreID: storeID}).WithContext(req.Context())
	return req.WithContext(ctx)
}

func withAdminScope(req *http.Request, storeID, language, currency string) *http.Request {
	ctx := (&adminapp.AdminContext{StoreID: storeID, Language: language, Currency: currency}).WithContext(req.Context())
	return req.WithContext(ctx)
}

func applyTestPluginFeeMinorUnits(c *appconfig.Config, value interface{}, requirePositive bool) error {
	switch v := value.(type) {
	case int64:
		if requirePositive && v <= 0 {
			return fmt.Errorf("fee must be positive")
		}
		c.Plugins.Example.FeeMinorUnits = v
	case float64:
		if requirePositive && v <= 0 {
			return fmt.Errorf("fee must be positive")
		}
		c.Plugins.Example.FeeMinorUnits = int64(v)
	default:
		return fmt.Errorf("unsupported fee value type %T", value)
	}
	return nil
}

func TestConfigAdmin_Get_GroupEmail(t *testing.T) {
	repo := newMockConfigRepo()
	repo.entries["mail.smtp.host"] = "smtp.db.test"
	repo.entries["mail.smtp.password"] = "super-secret"
	h := testConfigAdminHandler(repo, func(context.Context, admin.SMTPTestConfig, string) error { return nil })

	t.Run("happy path redacts password", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/config?group=email", nil)
		h.Get().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
		}

		var envelope struct {
			Data struct {
				Entries map[string]interface{} `json:"entries"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if envelope.Data.Entries["mail.smtp.host"] != "smtp.db.test" {
			t.Fatalf("host = %v, want smtp.db.test", envelope.Data.Entries["mail.smtp.host"])
		}
		if envelope.Data.Entries["mail.smtp.from"] != "ops@example.com" {
			t.Fatalf("from = %v, want ops@example.com", envelope.Data.Entries["mail.smtp.from"])
		}
		if envelope.Data.Entries["mail.smtp.port"].(float64) != 2525 {
			t.Fatalf("port = %v, want 2525", envelope.Data.Entries["mail.smtp.port"])
		}
		if envelope.Data.Entries["mail.smtp.password"] == "super-secret" {
			t.Fatal("mail.smtp.password was returned in plaintext")
		}
		if envelope.Data.Entries["mail.smtp.password"] != "***" {
			t.Fatalf("password = %v, want ***", envelope.Data.Entries["mail.smtp.password"])
		}
	})

	t.Run("unknown group", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/config?group=does-not-exist", nil)
		h.Get().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
		}
	})
}

func TestConfigAdmin_Update_OK(t *testing.T) {
	repo := newMockConfigRepo()
	h := testConfigAdminHandler(repo, func(context.Context, admin.SMTPTestConfig, string) error { return nil })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config", strings.NewReader(`{"entries":{"media.storage":"s3","tax.included":true}}`))
	req.Header.Set("Content-Type", "application/json")
	h.Update().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if repo.entries["media.storage"] != "s3" {
		t.Fatalf("media.storage = %v, want s3", repo.entries["media.storage"])
	}
	if repo.entries["tax.included"] != true {
		t.Fatalf("tax.included = %v, want true", repo.entries["tax.included"])
	}
}

func TestConfigAdmin_Get_GroupMarketing(t *testing.T) {
	repo := newMockConfigRepo()
	repo.entries["marketing.cart_recovery.enabled"] = false
	repo.entries["marketing.cart_recovery.delay_hours"] = float64(48)
	h := testConfigAdminHandler(repo, func(context.Context, admin.SMTPTestConfig, string) error { return nil })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/config?group=marketing", nil)
	h.Get().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var envelope struct {
		Data struct {
			Entries map[string]interface{} `json:"entries"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if envelope.Data.Entries["marketing.cart_recovery.enabled"] != false {
		t.Fatalf("enabled = %v, want false", envelope.Data.Entries["marketing.cart_recovery.enabled"])
	}
	gotDelay := envelope.Data.Entries["marketing.cart_recovery.delay_hours"]
	if gotDelay != float64(48) && gotDelay != int64(48) && gotDelay != int(48) {
		t.Fatalf("delay_hours = %v, want 48", gotDelay)
	}
}

func TestConfigAdmin_Update_MarketingCartRecovery(t *testing.T) {
	repo := newMockConfigRepo()
	h := testConfigAdminHandler(repo, func(context.Context, admin.SMTPTestConfig, string) error { return nil })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config", strings.NewReader(`{"entries":{"marketing.cart_recovery.enabled":false,"marketing.cart_recovery.delay_hours":72}}`))
	req.Header.Set("Content-Type", "application/json")
	h.Update().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if repo.entries["marketing.cart_recovery.enabled"] != false {
		t.Fatalf("enabled = %v, want false", repo.entries["marketing.cart_recovery.enabled"])
	}
	gotDelay := repo.entries["marketing.cart_recovery.delay_hours"]
	if gotDelay != float64(72) && gotDelay != int64(72) && gotDelay != int(72) {
		t.Fatalf("delay_hours = %v, want 72", gotDelay)
	}
}

func TestConfigAdmin_Update_MarketingCartRecovery_InvalidDelay(t *testing.T) {
	repo := newMockConfigRepo()
	h := testConfigAdminHandler(repo, func(context.Context, admin.SMTPTestConfig, string) error { return nil })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config", strings.NewReader(`{"entries":{"marketing.cart_recovery.delay_hours":0}}`))
	req.Header.Set("Content-Type", "application/json")
	h.Update().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if _, ok := repo.entries["marketing.cart_recovery.delay_hours"]; ok {
		t.Fatalf("expected invalid delay not persisted")
	}
}

func TestConfigAdmin_Update_LeavesPasswordUnchanged(t *testing.T) {
	repo := newMockConfigRepo()
	repo.entries["mail.smtp.password"] = "persisted-secret"
	h := testConfigAdminHandler(repo, func(context.Context, admin.SMTPTestConfig, string) error { return nil })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config", strings.NewReader(`{"entries":{"mail.smtp.host":"smtp.changed.test","mail.smtp.password":"***"}}`))
	req.Header.Set("Content-Type", "application/json")
	h.Update().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if repo.entries["mail.smtp.host"] != "smtp.changed.test" {
		t.Fatalf("mail.smtp.host = %v, want smtp.changed.test", repo.entries["mail.smtp.host"])
	}
	if repo.entries["mail.smtp.password"] != "persisted-secret" {
		t.Fatalf("mail.smtp.password = %v, want persisted-secret", repo.entries["mail.smtp.password"])
	}
}

func TestConfigAdmin_Update_InvalidKey(t *testing.T) {
	repo := newMockConfigRepo()
	h := testConfigAdminHandler(repo, func(context.Context, admin.SMTPTestConfig, string) error { return nil })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config", strings.NewReader(`{"entries":{"database.password":"secret"}}`))
	req.Header.Set("Content-Type", "application/json")
	h.Update().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestConfigAdmin_TestEmail_OK(t *testing.T) {
	repo := newMockConfigRepo()
	repo.entries["mail.smtp.host"] = "smtp.db.test"
	repo.entries["mail.smtp.port"] = 2526.0
	repo.entries["mail.smtp.from"] = "db@example.com"
	called := false
	h := testConfigAdminHandler(repo, func(_ context.Context, cfg admin.SMTPTestConfig, to string) error {
		called = true
		if cfg.Host != "smtp.db.test" {
			t.Fatalf("cfg.Host = %q, want smtp.db.test", cfg.Host)
		}
		if cfg.Port != 2526 {
			t.Fatalf("cfg.Port = %d, want 2526", cfg.Port)
		}
		if cfg.From != "db@example.com" {
			t.Fatalf("cfg.From = %q, want db@example.com", cfg.From)
		}
		if to != "merchant@example.com" {
			t.Fatalf("to = %q, want merchant@example.com", to)
		}
		return nil
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/config/test-email", strings.NewReader(`{"to":"merchant@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	h.TestEmail().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !called {
		t.Fatal("test email function was not called")
	}
}

func TestConfigAdmin_TestEmail_ExplicitlyClearsAuth(t *testing.T) {
	repo := newMockConfigRepo()
	repo.entries["mail.smtp.host"] = "smtp.db.test"
	repo.entries["mail.smtp.port"] = 2526.0
	repo.entries["mail.smtp.from"] = "db@example.com"
	repo.entries["mail.smtp.user"] = "saved-user"
	repo.entries["mail.smtp.password"] = "saved-password"
	h := testConfigAdminHandler(repo, func(_ context.Context, cfg admin.SMTPTestConfig, _ string) error {
		if cfg.User != "" {
			t.Fatalf("cfg.User = %q, want empty", cfg.User)
		}
		if cfg.Password != "" {
			t.Fatalf("cfg.Password = %q, want empty", cfg.Password)
		}
		return nil
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/config/test-email", strings.NewReader(`{"to":"merchant@example.com","user":"","password":""}`))
	req.Header.Set("Content-Type", "application/json")
	h.TestEmail().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestConfigAdmin_TestEmail_RepoError(t *testing.T) {
	repo := newMockConfigRepo()
	repo.getErr = errors.New("db unavailable")
	called := false
	h := testConfigAdminHandler(repo, func(_ context.Context, cfg admin.SMTPTestConfig, _ string) error {
		called = true
		return nil
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/config/test-email", strings.NewReader(`{"to":"merchant@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	h.TestEmail().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if called {
		t.Fatal("test email function should not be called on repo error")
	}
}

func TestConfigAdmin_Get_FieldScopesIncluded(t *testing.T) {
	repo := newMockConfigRepo()
	h := testConfigAdminHandler(repo, func(context.Context, admin.SMTPTestConfig, string) error { return nil })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/config?group=currency", nil)
	h.Get().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var envelope struct {
		Data struct {
			FieldScopes map[string]string `json:"field_scopes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if envelope.Data.FieldScopes["currency.display_format"] != "store" {
		t.Fatalf("currency.display_format scope = %q, want %q", envelope.Data.FieldScopes["currency.display_format"], "store")
	}
	if envelope.Data.FieldScopes["default_currency"] != "store" {
		t.Fatalf("default_currency scope = %q, want %q", envelope.Data.FieldScopes["default_currency"], "store")
	}
}

func TestConfigAdmin_ScopedUpdateAndRead_BackwardFallback(t *testing.T) {
	repo := newMockConfigRepo()
	h := testConfigAdminHandler(repo, func(context.Context, admin.SMTPTestConfig, string) error { return nil })

	// Write global value first.
	globalRec := httptest.NewRecorder()
	globalReq := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config", strings.NewReader(`{"entries":{"currency.display_format":"{currency} {amount}"}}`))
	globalReq.Header.Set("Content-Type", "application/json")
	h.Update().ServeHTTP(globalRec, globalReq)
	if globalRec.Code != http.StatusOK {
		t.Fatalf("global update status = %d, want %d; body: %s", globalRec.Code, http.StatusOK, globalRec.Body.String())
	}

	// Write store override with explicit scoped context.
	scopedRec := httptest.NewRecorder()
	scopedReq := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config", strings.NewReader(`{"entries":{"currency.display_format":"{amount} {currency}"}}`))
	scopedReq.Header.Set("Content-Type", "application/json")
	scopedReq = withAdminStoreScope(scopedReq, "store-eu")
	h.Update().ServeHTTP(scopedRec, scopedReq)
	if scopedRec.Code != http.StatusOK {
		t.Fatalf("scoped update status = %d, want %d; body: %s", scopedRec.Code, http.StatusOK, scopedRec.Body.String())
	}

	// Scoped read gets override.
	scopedGetRec := httptest.NewRecorder()
	scopedGetReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/config?group=currency", nil)
	scopedGetReq = withAdminStoreScope(scopedGetReq, "store-eu")
	h.Get().ServeHTTP(scopedGetRec, scopedGetReq)
	if scopedGetRec.Code != http.StatusOK {
		t.Fatalf("scoped get status = %d, want %d; body: %s", scopedGetRec.Code, http.StatusOK, scopedGetRec.Body.String())
	}
	var scopedEnv struct {
		Data struct {
			Entries map[string]interface{} `json:"entries"`
		} `json:"data"`
	}
	if err := json.Unmarshal(scopedGetRec.Body.Bytes(), &scopedEnv); err != nil {
		t.Fatalf("unmarshal scoped get: %v", err)
	}
	if scopedEnv.Data.Entries["currency.display_format"] != "{amount} {currency}" {
		t.Fatalf("scoped currency.display_format = %v, want {amount} {currency}", scopedEnv.Data.Entries["currency.display_format"])
	}

	// Global read keeps global value.
	globalGetRec := httptest.NewRecorder()
	globalGetReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/config?group=currency", nil)
	h.Get().ServeHTTP(globalGetRec, globalGetReq)
	if globalGetRec.Code != http.StatusOK {
		t.Fatalf("global get status = %d, want %d; body: %s", globalGetRec.Code, http.StatusOK, globalGetRec.Body.String())
	}
	var globalEnv struct {
		Data struct {
			Entries map[string]interface{} `json:"entries"`
		} `json:"data"`
	}
	if err := json.Unmarshal(globalGetRec.Body.Bytes(), &globalEnv); err != nil {
		t.Fatalf("unmarshal global get: %v", err)
	}
	if globalEnv.Data.Entries["currency.display_format"] != "{currency} {amount}" {
		t.Fatalf("global currency.display_format = %v, want {currency} {amount}", globalEnv.Data.Entries["currency.display_format"])
	}
}

func TestConfigAdmin_ScopedUpdateAndRead_ContextScopeOverridesConflictingQuery(t *testing.T) {
	repo := newMockConfigRepo()
	h := testConfigAdminHandler(repo, func(context.Context, admin.SMTPTestConfig, string) error { return nil })

	globalRec := httptest.NewRecorder()
	globalReq := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config", strings.NewReader(`{"entries":{"currency.display_format":"{currency} {amount}"}}`))
	globalReq.Header.Set("Content-Type", "application/json")
	h.Update().ServeHTTP(globalRec, globalReq)
	if globalRec.Code != http.StatusOK {
		t.Fatalf("global update status = %d, want %d; body: %s", globalRec.Code, http.StatusOK, globalRec.Body.String())
	}

	euRec := httptest.NewRecorder()
	euReq := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config", strings.NewReader(`{"entries":{"currency.display_format":"{amount} EUR"}}`))
	euReq.Header.Set("Content-Type", "application/json")
	euReq = withAdminStoreScope(euReq, "store-eu")
	h.Update().ServeHTTP(euRec, euReq)
	if euRec.Code != http.StatusOK {
		t.Fatalf("eu scoped update status = %d, want %d; body: %s", euRec.Code, http.StatusOK, euRec.Body.String())
	}

	usRec := httptest.NewRecorder()
	usReq := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config", strings.NewReader(`{"entries":{"currency.display_format":"USD {amount}"}}`))
	usReq.Header.Set("Content-Type", "application/json")
	usReq = withAdminStoreScope(usReq, "store-us")
	h.Update().ServeHTTP(usRec, usReq)
	if usRec.Code != http.StatusOK {
		t.Fatalf("us scoped update status = %d, want %d; body: %s", usRec.Code, http.StatusOK, usRec.Body.String())
	}

	conflictGetRec := httptest.NewRecorder()
	conflictGetReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/config?group=currency&store_id=store-eu", nil)
	conflictGetReq = withAdminStoreScope(conflictGetReq, "store-us")
	h.Get().ServeHTTP(conflictGetRec, conflictGetReq)
	if conflictGetRec.Code != http.StatusOK {
		t.Fatalf("conflict scoped get status = %d, want %d; body: %s", conflictGetRec.Code, http.StatusOK, conflictGetRec.Body.String())
	}

	var conflictEnv struct {
		Data struct {
			Entries map[string]interface{} `json:"entries"`
			Scope   struct {
				StoreID string `json:"store_id"`
			} `json:"scope"`
		} `json:"data"`
	}
	if err := json.Unmarshal(conflictGetRec.Body.Bytes(), &conflictEnv); err != nil {
		t.Fatalf("unmarshal conflict scoped get: %v", err)
	}
	if conflictEnv.Data.Scope.StoreID != "store-us" {
		t.Fatalf("resolved store scope = %q, want %q", conflictEnv.Data.Scope.StoreID, "store-us")
	}
	if conflictEnv.Data.Entries["currency.display_format"] != "USD {amount}" {
		t.Fatalf("currency.display_format = %v, want USD {amount}", conflictEnv.Data.Entries["currency.display_format"])
	}
}

func TestConfigAdmin_Get_ScopeIncludesLanguageAndCurrency(t *testing.T) {
	repo := newMockConfigRepo()
	h := testConfigAdminHandler(repo, func(context.Context, admin.SMTPTestConfig, string) error { return nil })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/config?group=currency", nil)
	req = withAdminScope(req, "store-eu", "en", "EUR")
	h.Get().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var envelope struct {
		Data struct {
			Scope struct {
				StoreID  string `json:"store_id"`
				Language string `json:"language"`
				Currency string `json:"currency"`
			} `json:"scope"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if envelope.Data.Scope.StoreID != "store-eu" {
		t.Fatalf("scope.store_id = %q, want %q", envelope.Data.Scope.StoreID, "store-eu")
	}
	if envelope.Data.Scope.Language != "en" {
		t.Fatalf("scope.language = %q, want %q", envelope.Data.Scope.Language, "en")
	}
	if envelope.Data.Scope.Currency != "EUR" {
		t.Fatalf("scope.currency = %q, want %q", envelope.Data.Scope.Currency, "EUR")
	}
}

func TestConfigAdmin_Update_ScopeIncludesLanguageAndCurrency(t *testing.T) {
	repo := newMockConfigRepo()
	h := testConfigAdminHandler(repo, func(context.Context, admin.SMTPTestConfig, string) error { return nil })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config", strings.NewReader(`{"entries":{"currency.display_format":"{amount} {currency}"}}`))
	req.Header.Set("Content-Type", "application/json")
	req = withAdminScope(req, "store-eu", "en", "EUR")
	h.Update().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var envelope struct {
		Data struct {
			Scope struct {
				StoreID  string `json:"store_id"`
				Language string `json:"language"`
				Currency string `json:"currency"`
			} `json:"scope"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if envelope.Data.Scope.StoreID != "store-eu" {
		t.Fatalf("scope.store_id = %q, want %q", envelope.Data.Scope.StoreID, "store-eu")
	}
	if envelope.Data.Scope.Language != "en" {
		t.Fatalf("scope.language = %q, want %q", envelope.Data.Scope.Language, "en")
	}
	if envelope.Data.Scope.Currency != "EUR" {
		t.Fatalf("scope.currency = %q, want %q", envelope.Data.Scope.Currency, "EUR")
	}
}

func TestConfigAdmin_Update_UsesQueryStoreScopeWhenContextStoreIsEmpty(t *testing.T) {
	repo := newMockConfigRepo()
	h := testConfigAdminHandler(repo, func(context.Context, admin.SMTPTestConfig, string) error { return nil })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config?store_id=store-eu", strings.NewReader(`{"entries":{"currency.display_format":"{amount} EUR"}}`))
	req.Header.Set("Content-Type", "application/json")
	req = withAdminScope(req, "", "en", "EUR")
	h.Update().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if repo.entries["store::store-eu::currency.display_format"] != "{amount} EUR" {
		t.Fatalf("scoped entry = %v, want %v", repo.entries["store::store-eu::currency.display_format"], "{amount} EUR")
	}

	var envelope struct {
		Data struct {
			Scope struct {
				StoreID  string `json:"store_id"`
				Language string `json:"language"`
				Currency string `json:"currency"`
			} `json:"scope"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if envelope.Data.Scope.StoreID != "store-eu" {
		t.Fatalf("scope.store_id = %q, want %q", envelope.Data.Scope.StoreID, "store-eu")
	}
	if envelope.Data.Scope.Language != "en" {
		t.Fatalf("scope.language = %q, want %q", envelope.Data.Scope.Language, "en")
	}
	if envelope.Data.Scope.Currency != "EUR" {
		t.Fatalf("scope.currency = %q, want %q", envelope.Data.Scope.Currency, "EUR")
	}
}

func TestConfigAdmin_Update_InvalidBody_DoesNotPanicAndReturnsValidation(t *testing.T) {
	repo := newMockConfigRepo()
	h := testConfigAdminHandler(repo, func(context.Context, admin.SMTPTestConfig, string) error { return nil })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	h.Update().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func testConfigAdminHandlerWithAudit(repo domainCfg.Repository, sink logger.Logger) *admin.ConfigAdminHandler {
	cfg := &appconfig.Config{}
	cfg.Mail.SMTP.Host = "smtp.default.test"
	cfg.Mail.SMTP.Port = 2525
	cfg.Mail.SMTP.From = "ops@example.com"
	cfg.Media.Storage = "local"
	cfg.Media.Local.BasePath = "./public/media"
	cfg.Media.Local.BaseURL = "/media"
	return admin.NewConfigAdminHandler(repo, cfg, func(context.Context, admin.SMTPTestConfig, string) error { return nil }, adminapp.NewAuditor(sink), nil)
}

func TestConfigAdmin_Get_AuditOmitsPartialScopeContext(t *testing.T) {
	repo := newMockConfigRepo()
	repo.entries["mail.smtp.host"] = "smtp.db.test"
	sink := &auditSink{}
	h := testConfigAdminHandlerWithAudit(repo, sink)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/config?group=email", nil)
	req = withAdminScope(req, "store-eu", "en", "")
	h.Get().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
	}
	if got := entry.context["action"]; got != adminapp.AuditSettingsRead {
		t.Errorf("action = %v, want %q", got, adminapp.AuditSettingsRead)
	}
	if got := entry.context["resource_type"]; got != "config_group" {
		t.Errorf("resource_type = %v, want %q", got, "config_group")
	}
	if got := entry.context["result"]; got != "success" {
		t.Errorf("result = %v, want %q", got, "success")
	}
	if _, ok := entry.context["store_id"]; ok {
		t.Errorf("store_id present = %v, want absent", entry.context["store_id"])
	}
	if _, ok := entry.context["language"]; ok {
		t.Errorf("language present = %v, want absent", entry.context["language"])
	}
	if _, ok := entry.context["currency"]; ok {
		t.Errorf("currency present = %v, want absent", entry.context["currency"])
	}
	if _, ok := entry.context["error"]; ok {
		t.Errorf("error present = %v, want absent", entry.context["error"])
	}
}

func TestConfigAdmin_Get_AuditFailureOmitsPartialScopeContext(t *testing.T) {
	repo := newMockConfigRepo()
	repo.getErr = errors.New("database error")
	sink := &auditSink{}
	h := testConfigAdminHandlerWithAudit(repo, sink)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/config?group=email", nil)
	req = withAdminScope(req, "store-eu", "en", "")
	h.Get().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action.failed" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action.failed")
	}
	if got := entry.context["action"]; got != adminapp.AuditSettingsRead {
		t.Errorf("action = %v, want %q", got, adminapp.AuditSettingsRead)
	}
	if got := entry.context["resource_type"]; got != "config_group" {
		t.Errorf("resource_type = %v, want %q", got, "config_group")
	}
	if got := entry.context["result"]; got != "error" {
		t.Errorf("result = %v, want %q", got, "error")
	}
	if _, ok := entry.context["store_id"]; ok {
		t.Errorf("store_id present = %v, want absent", entry.context["store_id"])
	}
	if _, ok := entry.context["language"]; ok {
		t.Errorf("language present = %v, want absent", entry.context["language"])
	}
	if _, ok := entry.context["currency"]; ok {
		t.Errorf("currency present = %v, want absent", entry.context["currency"])
	}
	if err, ok := entry.context["error"]; !ok || err == "" {
		t.Errorf("error = %v, want non-empty string", err)
	}
}

func TestConfigAdmin_Update_AuditOmitsPartialScopeContext(t *testing.T) {
	repo := newMockConfigRepo()
	sink := &auditSink{}
	h := testConfigAdminHandlerWithAudit(repo, sink)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config", strings.NewReader(`{"entries":{"mail.smtp.host":"smtp.new.test"}}`))
	req.Header.Set("Content-Type", "application/json")
	req = withAdminScope(req, "store-eu", "en", "")
	h.Update().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
	}
	if got := entry.context["action"]; got != adminapp.AuditSettingsChange {
		t.Errorf("action = %v, want %q", got, adminapp.AuditSettingsChange)
	}
	if got := entry.context["resource_type"]; got != "config_group" {
		t.Errorf("resource_type = %v, want %q", got, "config_group")
	}
	if got := entry.context["result"]; got != "success" {
		t.Errorf("result = %v, want %q", got, "success")
	}
	if _, ok := entry.context["store_id"]; ok {
		t.Errorf("store_id present = %v, want absent", entry.context["store_id"])
	}
	if _, ok := entry.context["language"]; ok {
		t.Errorf("language present = %v, want absent", entry.context["language"])
	}
	if _, ok := entry.context["currency"]; ok {
		t.Errorf("currency present = %v, want absent", entry.context["currency"])
	}
	if _, ok := entry.context["error"]; ok {
		t.Errorf("error present = %v, want absent", entry.context["error"])
	}
}

func TestConfigAdmin_Update_AuditFailureOmitsPartialScopeContext(t *testing.T) {
	repo := newMockConfigRepo()
	repo.setManyErr = errors.New("database error")
	sink := &auditSink{}
	h := testConfigAdminHandlerWithAudit(repo, sink)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config", strings.NewReader(`{"entries":{"mail.smtp.host":"smtp.new.test"}}`))
	req.Header.Set("Content-Type", "application/json")
	req = withAdminScope(req, "store-eu", "en", "")
	h.Update().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action.failed" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action.failed")
	}
	if got := entry.context["action"]; got != adminapp.AuditSettingsChange {
		t.Errorf("action = %v, want %q", got, adminapp.AuditSettingsChange)
	}
	if got := entry.context["resource_type"]; got != "config_group" {
		t.Errorf("resource_type = %v, want %q", got, "config_group")
	}
	if got := entry.context["result"]; got != "error" {
		t.Errorf("result = %v, want %q", got, "error")
	}
	if _, ok := entry.context["store_id"]; ok {
		t.Errorf("store_id present = %v, want absent", entry.context["store_id"])
	}
	if _, ok := entry.context["language"]; ok {
		t.Errorf("language present = %v, want absent", entry.context["language"])
	}
	if _, ok := entry.context["currency"]; ok {
		t.Errorf("currency present = %v, want absent", entry.context["currency"])
	}
	if err, ok := entry.context["error"]; !ok || err == "" {
		t.Errorf("error = %v, want non-empty string", err)
	}
}

func TestConfigAdmin_Get_GroupPlugins(t *testing.T) {
	repo := newMockConfigRepo()
	cfg := &appconfig.Config{
		Plugins: appconfig.PluginsConfig{
			Example: appconfig.ExamplePluginConfig{Enabled: true, FeeMinorUnits: 150},
		},
	}
	pluginReg := plugin.NewConfigRegistry()
	if err := pluginReg.Register(plugin.ConfigDefinition{
		Plugin: "example/demo",
		Fields: []plugin.ConfigField{
			{
				Key:   "plugins.example.fee_minor_units",
				Label: "Example fee (minor units)",
				Type:  plugin.ConfigFieldInt,
				Get: func(c *appconfig.Config) interface{} {
					return c.Plugins.Example.FeeMinorUnits
				},
				Apply: func(c *appconfig.Config, value interface{}) error {
					return applyTestPluginFeeMinorUnits(c, value, false)
				},
			},
		},
	}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}
	h := admin.NewConfigAdminHandler(repo, cfg, func(context.Context, admin.SMTPTestConfig, string) error { return nil }, adminapp.NewAuditor(logger.NewWithWriter(io.Discard, "error")), pluginReg)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/config?group=plugins", nil)
	h.Get().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var envelope struct {
		Data struct {
			Entries   map[string]interface{}   `json:"entries"`
			FieldDefs []map[string]interface{} `json:"field_defs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := envelope.Data.Entries["plugins.example.fee_minor_units"]
	if got != float64(150) && got != int64(150) {
		t.Fatalf("fee = %v, want 150", got)
	}
	if len(envelope.Data.FieldDefs) != 1 {
		t.Fatalf("field_defs len = %d, want 1", len(envelope.Data.FieldDefs))
	}
}

func TestConfigAdmin_Update_PluginConfigAppliesRuntime(t *testing.T) {
	repo := newMockConfigRepo()
	cfg := &appconfig.Config{
		Plugins: appconfig.PluginsConfig{
			Example: appconfig.ExamplePluginConfig{Enabled: true, FeeMinorUnits: 100},
		},
	}
	pluginReg := plugin.NewConfigRegistry()
	if err := pluginReg.Register(plugin.ConfigDefinition{
		Plugin: "example/demo",
		Fields: []plugin.ConfigField{
			{
				Key:   "plugins.example.fee_minor_units",
				Label: "Example fee (minor units)",
				Type:  plugin.ConfigFieldInt,
				Get: func(c *appconfig.Config) interface{} {
					return c.Plugins.Example.FeeMinorUnits
				},
				Apply: func(c *appconfig.Config, value interface{}) error {
					return applyTestPluginFeeMinorUnits(c, value, false)
				},
			},
		},
	}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}
	h := admin.NewConfigAdminHandler(repo, cfg, func(context.Context, admin.SMTPTestConfig, string) error { return nil }, adminapp.NewAuditor(logger.NewWithWriter(io.Discard, "error")), pluginReg)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config", strings.NewReader(`{"entries":{"plugins.example.fee_minor_units":300}}`))
	req.Header.Set("Content-Type", "application/json")
	h.Update().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if cfg.Plugins.Example.FeeMinorUnits != 300 {
		t.Fatalf("runtime FeeMinorUnits = %d, want 300", cfg.Plugins.Example.FeeMinorUnits)
	}
	persisted := repo.entries["plugins.example.fee_minor_units"]
	if persisted != float64(300) && persisted != int64(300) {
		t.Fatalf("persisted value = %v, want 300", persisted)
	}
}

func TestConfigAdmin_Update_PluginConfigZeroRejectedBeforePersist(t *testing.T) {
	repo := newMockConfigRepo()
	cfg := &appconfig.Config{
		Plugins: appconfig.PluginsConfig{
			Example: appconfig.ExamplePluginConfig{Enabled: true, FeeMinorUnits: 100},
		},
	}
	pluginReg := plugin.NewConfigRegistry()
	if err := pluginReg.Register(plugin.ConfigDefinition{
		Plugin: "example/demo",
		Fields: []plugin.ConfigField{
			{
				Key:   "plugins.example.fee_minor_units",
				Label: "Example fee (minor units)",
				Type:  plugin.ConfigFieldInt,
				Get: func(c *appconfig.Config) interface{} {
					return c.Plugins.Example.FeeMinorUnits
				},
				Apply: func(c *appconfig.Config, value interface{}) error {
					return applyTestPluginFeeMinorUnits(c, value, true)
				},
			},
		},
	}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}
	h := admin.NewConfigAdminHandler(repo, cfg, func(context.Context, admin.SMTPTestConfig, string) error { return nil }, adminapp.NewAuditor(logger.NewWithWriter(io.Discard, "error")), pluginReg)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config", strings.NewReader(`{"entries":{"plugins.example.fee_minor_units":0}}`))
	req.Header.Set("Content-Type", "application/json")
	h.Update().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if cfg.Plugins.Example.FeeMinorUnits != 100 {
		t.Fatalf("runtime FeeMinorUnits = %d, want unchanged 100", cfg.Plugins.Example.FeeMinorUnits)
	}
	if _, ok := repo.entries["plugins.example.fee_minor_units"]; ok {
		t.Fatal("zero fee should not be persisted")
	}
}

func TestConfigAdmin_Update_PluginConfigRollbackOnPersistFailure(t *testing.T) {
	repo := newMockConfigRepo()
	repo.setManyErr = errors.New("database error")
	cfg := &appconfig.Config{
		Plugins: appconfig.PluginsConfig{
			Example: appconfig.ExamplePluginConfig{Enabled: true, FeeMinorUnits: 100},
		},
	}
	pluginReg := plugin.NewConfigRegistry()
	if err := pluginReg.Register(plugin.ConfigDefinition{
		Plugin: "example/demo",
		Fields: []plugin.ConfigField{
			{
				Key:   "plugins.example.fee_minor_units",
				Label: "Example fee (minor units)",
				Type:  plugin.ConfigFieldInt,
				Get: func(c *appconfig.Config) interface{} {
					return c.Plugins.Example.FeeMinorUnits
				},
				Apply: func(c *appconfig.Config, value interface{}) error {
					return applyTestPluginFeeMinorUnits(c, value, true)
				},
			},
		},
	}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}
	h := admin.NewConfigAdminHandler(repo, cfg, func(context.Context, admin.SMTPTestConfig, string) error { return nil }, adminapp.NewAuditor(logger.NewWithWriter(io.Discard, "error")), pluginReg)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config", strings.NewReader(`{"entries":{"plugins.example.fee_minor_units":300}}`))
	req.Header.Set("Content-Type", "application/json")
	h.Update().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if cfg.Plugins.Example.FeeMinorUnits != 100 {
		t.Fatalf("runtime FeeMinorUnits = %d, want rolled back to 100", cfg.Plugins.Example.FeeMinorUnits)
	}
	if _, ok := repo.entries["plugins.example.fee_minor_units"]; ok {
		t.Fatal("failed update should not be persisted")
	}
}
