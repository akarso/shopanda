package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	setupApp "github.com/akarso/shopanda/internal/application/setup"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type fakeSetupService struct {
	status      setupApp.Status
	statusErr   error
	installErr  error
	installOut  *setupApp.InstallResult
	installSeen setupApp.InstallInput
}

func (f *fakeSetupService) Status(context.Context) (setupApp.Status, error) {
	return f.status, f.statusErr
}

func (f *fakeSetupService) Install(_ context.Context, in setupApp.InstallInput) (*setupApp.InstallResult, error) {
	f.installSeen = in
	if f.installErr != nil {
		return nil, f.installErr
	}
	if f.installOut != nil {
		return f.installOut, nil
	}
	return &setupApp.InstallResult{AdminEmail: in.Email}, nil
}

func TestSetupHandler_Status_OK(t *testing.T) {
	t.Parallel()

	handler := shophttp.NewSetupHandler(&fakeSetupService{
		status: setupApp.Status{
			NeedsSetup:        true,
			DatabaseOK:        true,
			PendingMigrations: 2,
			HasAdmin:          false,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil)
	rec := httptest.NewRecorder()
	handler.Status()(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var envelope struct {
		Data setupApp.Status `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	body := envelope.Data
	if !body.NeedsSetup || !body.DatabaseOK || body.PendingMigrations != 2 {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestSetupHandler_Install_CreatesAdmin(t *testing.T) {
	t.Parallel()

	fake := &fakeSetupService{
		status: setupApp.Status{NeedsSetup: true, DatabaseOK: true},
		installOut: &setupApp.InstallResult{
			AdminEmail:        "owner@example.com",
			MigrationsApplied: 1,
		},
	}
	handler := shophttp.NewSetupHandler(fake)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/install", strings.NewReader(`{
		"email":"owner@example.com",
		"password":"password123",
		"first_name":"Ada",
		"last_name":"Lovelace",
		"store_name":"Ada Shop"
	}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.Install()(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if fake.installSeen.Email != "owner@example.com" || fake.installSeen.StoreName != "Ada Shop" {
		t.Fatalf("install input = %+v", fake.installSeen)
	}
}

func TestSetupHandler_Install_ConflictWhenInstalled(t *testing.T) {
	t.Parallel()

	handler := shophttp.NewSetupHandler(&fakeSetupService{
		status:     setupApp.Status{NeedsSetup: false, DatabaseOK: true, HasAdmin: true},
		installErr: apperror.Conflict("store is already installed"),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/install", strings.NewReader(`{"email":"a@b.com","password":"password123"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.Install()(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestSetupHandler_Page_RedirectsWhenInstalled(t *testing.T) {
	t.Parallel()

	handler := shophttp.NewSetupHandler(&fakeSetupService{
		status: setupApp.Status{NeedsSetup: false, DatabaseOK: true, HasAdmin: true},
	})

	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	rec := httptest.NewRecorder()
	handler.Page()(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin" {
		t.Fatalf("location = %q, want /admin", loc)
	}
}

func TestSetupGate_RedirectsAdminToSetup(t *testing.T) {
	t.Parallel()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	gate := shophttp.SetupGate(&fakeSetupService{
		status: setupApp.Status{NeedsSetup: true, DatabaseOK: true},
	}, next)

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/setup" {
		t.Fatalf("location = %q, want /setup", loc)
	}
}

func TestSetupGate_AllowsAdminWhenInstalled(t *testing.T) {
	t.Parallel()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	gate := shophttp.SetupGate(&fakeSetupService{
		status: setupApp.Status{NeedsSetup: false, DatabaseOK: true, HasAdmin: true},
	}, next)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected next handler to run")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestSetupHandler_Page_RendersWizardWhenNeeded(t *testing.T) {
	t.Parallel()

	handler := shophttp.NewSetupHandler(&fakeSetupService{
		status: setupApp.Status{NeedsSetup: true, DatabaseOK: true},
	})

	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	rec := httptest.NewRecorder()
	handler.Page()(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Shopanda setup") || !strings.Contains(body, "/api/v1/setup/install") {
		t.Fatalf("expected setup wizard HTML, got: %s", body[:min(200, len(body))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
