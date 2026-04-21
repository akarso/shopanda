package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domainCfg "github.com/akarso/shopanda/internal/domain/config"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	appconfig "github.com/akarso/shopanda/internal/platform/config"
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

func testConfigAdminHandler(repo domainCfg.Repository, testEmail shophttp.SMTPTestFunc) *shophttp.ConfigAdminHandler {
	cfg := &appconfig.Config{}
	cfg.Mail.SMTP.Host = "smtp.default.test"
	cfg.Mail.SMTP.Port = 2525
	cfg.Mail.SMTP.From = "ops@example.com"
	cfg.Media.Storage = "local"
	cfg.Media.Local.BasePath = "./public/media"
	cfg.Media.Local.BaseURL = "/media"
	return shophttp.NewConfigAdminHandler(repo, cfg, testEmail)
}

func TestConfigAdmin_Get_GroupEmail(t *testing.T) {
	repo := newMockConfigRepo()
	repo.entries["mail.smtp.host"] = "smtp.db.test"
	repo.entries["mail.smtp.password"] = "super-secret"
	h := testConfigAdminHandler(repo, func(context.Context, shophttp.SMTPTestConfig, string) error { return nil })

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
	h := testConfigAdminHandler(repo, func(context.Context, shophttp.SMTPTestConfig, string) error { return nil })

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

func TestConfigAdmin_Update_LeavesPasswordUnchanged(t *testing.T) {
	repo := newMockConfigRepo()
	repo.entries["mail.smtp.password"] = "persisted-secret"
	h := testConfigAdminHandler(repo, func(context.Context, shophttp.SMTPTestConfig, string) error { return nil })

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
	h := testConfigAdminHandler(repo, func(context.Context, shophttp.SMTPTestConfig, string) error { return nil })

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
	h := testConfigAdminHandler(repo, func(_ context.Context, cfg shophttp.SMTPTestConfig, to string) error {
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
	h := testConfigAdminHandler(repo, func(_ context.Context, cfg shophttp.SMTPTestConfig, _ string) error {
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
	h := testConfigAdminHandler(repo, func(_ context.Context, cfg shophttp.SMTPTestConfig, _ string) error {
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
