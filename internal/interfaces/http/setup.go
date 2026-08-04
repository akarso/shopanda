package http

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	setupApp "github.com/akarso/shopanda/internal/application/setup"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

var setupWizardTemplate = template.Must(template.New("setup-wizard").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Shopanda Setup</title>
    <style>
        :root { color-scheme: light dark; font-family: system-ui, sans-serif; line-height: 1.5; }
        body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: #f4f4f5; color: #18181b; }
        .panel { width: min(32rem, calc(100vw - 2rem)); background: #fff; border: 1px solid #e4e4e7; border-radius: 0.75rem; padding: 1.5rem; box-shadow: 0 10px 30px rgba(0,0,0,.06); }
        h1 { margin: 0 0 .25rem; font-size: 1.5rem; }
        .lead { margin: 0 0 1rem; color: #52525b; }
        label { display: block; font-weight: 600; margin: .75rem 0 .25rem; }
        input { width: 100%; box-sizing: border-box; padding: .55rem .65rem; border: 1px solid #d4d4d8; border-radius: .5rem; }
        button { margin-top: 1rem; width: 100%; padding: .65rem 1rem; border: 0; border-radius: .5rem; background: #18181b; color: #fff; font-weight: 600; cursor: pointer; }
        button:disabled { opacity: .6; cursor: not-allowed; }
        .status { padding: .75rem; border-radius: .5rem; background: #fafafa; border: 1px solid #e4e4e7; margin-bottom: 1rem; font-size: .95rem; }
        .status.error { background: #fef2f2; border-color: #fecaca; color: #991b1b; }
        .status.ok { background: #ecfdf5; border-color: #a7f3d0; color: #065f46; }
        .hidden { display: none; }
        .muted { color: #71717a; font-size: .9rem; }
        .row { display: grid; grid-template-columns: 1fr 1fr; gap: .75rem; }
        a { color: inherit; }
    </style>
</head>
<body>
    <main class="panel">
        <h1>Shopanda setup</h1>
        <p class="lead">Create your store admin account and finish first-time installation.</p>
        <div id="status" class="status">Checking installation status…</div>
        <form id="install-form" class="hidden" novalidate>
            <p class="muted">Database connection comes from your environment (.env). This step runs migrations, seeds defaults, and creates the first admin user.</p>
            <label for="store_name">Store name <span class="muted">(optional)</span></label>
            <input id="store_name" name="store_name" autocomplete="organization">
            <label for="email">Admin email</label>
            <input id="email" name="email" type="email" required autocomplete="username">
            <div class="row">
                <div>
                    <label for="first_name">First name</label>
                    <input id="first_name" name="first_name" autocomplete="given-name">
                </div>
                <div>
                    <label for="last_name">Last name</label>
                    <input id="last_name" name="last_name" autocomplete="family-name">
                </div>
            </div>
            <label for="password">Password</label>
            <input id="password" name="password" type="password" required minlength="8" autocomplete="new-password">
            <label for="password_confirm">Confirm password</label>
            <input id="password_confirm" name="password_confirm" type="password" required minlength="8" autocomplete="new-password">
            <button type="submit" id="submit-btn">Install Shopanda</button>
        </form>
        <div id="done" class="hidden">
            <p class="status ok">Installation complete. You can sign in to the admin area now.</p>
            <p><a href="/admin">Open admin →</a></p>
        </div>
    </main>
    <script>
    (function () {
        var statusEl = document.getElementById('status');
        var formEl = document.getElementById('install-form');
        var doneEl = document.getElementById('done');
        var submitBtn = document.getElementById('submit-btn');

        function setStatus(message, kind) {
            statusEl.textContent = message;
            statusEl.className = 'status' + (kind ? ' ' + kind : '');
        }

        function loadStatus() {
            fetch('/api/v1/setup/status')
                .then(function (res) { return res.json().then(function (body) { return { ok: res.ok, body: body }; }); })
                .then(function (result) {
                    if (!result.ok) {
                        setStatus('Could not read setup status.', 'error');
                        return;
                    }
                    var s = result.body.data;
                    if (!s) {
                        setStatus('Could not read setup status.', 'error');
                        return;
                    }
                    if (!s.database_ok) {
                        setStatus('Database is not reachable. Check DATABASE_URL / SHOPANDA_DATABASE_* in your environment, then restart the server.', 'error');
                        return;
                    }
                    if (!s.needs_setup) {
                        setStatus('This store is already installed.', 'ok');
                        doneEl.classList.remove('hidden');
                        return;
                    }
                    var pending = s.pending_migrations || 0;
                    var msg = pending > 0
                        ? 'Ready to install. ' + pending + ' database migration(s) will be applied.'
                        : 'Ready to install. Database schema is up to date.';
                    setStatus(msg, 'ok');
                    formEl.classList.remove('hidden');
                })
                .catch(function () {
                    setStatus('Could not read setup status.', 'error');
                });
        }

        formEl.addEventListener('submit', function (event) {
            event.preventDefault();
            var password = document.getElementById('password').value;
            var confirm = document.getElementById('password_confirm').value;
            if (password !== confirm) {
                setStatus('Passwords do not match.', 'error');
                return;
            }
            submitBtn.disabled = true;
            setStatus('Installing…');
            fetch('/api/v1/setup/install', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    email: document.getElementById('email').value.trim(),
                    password: password,
                    first_name: document.getElementById('first_name').value.trim(),
                    last_name: document.getElementById('last_name').value.trim(),
                    store_name: document.getElementById('store_name').value.trim()
                })
            })
                .then(function (res) {
                    return res.json().then(function (body) {
                        if (!res.ok) {
                            var message = (body.error && body.error.message) ? body.error.message : 'Installation failed.';
                            throw new Error(message);
                        }
                        return body;
                    });
                })
                .then(function () {
                    formEl.classList.add('hidden');
                    doneEl.classList.remove('hidden');
                    setStatus('Installation complete.', 'ok');
                })
                .catch(function (err) {
                    setStatus(err.message || 'Installation failed.', 'error');
                    submitBtn.disabled = false;
                });
        });

        loadStatus();
    })();
    </script>
</body>
</html>`))

// SetupService performs first-time installation checks and actions.
type SetupService interface {
	Status(ctx context.Context) (setupApp.Status, error)
	Install(ctx context.Context, in setupApp.InstallInput) (*setupApp.InstallResult, error)
}

// SetupHandler serves the web installer and setup API.
type SetupHandler struct {
	svc SetupService
}

// NewSetupHandler creates a SetupHandler.
func NewSetupHandler(svc SetupService) *SetupHandler {
	if svc == nil {
		panic("http: setup service must not be nil")
	}
	return &SetupHandler{svc: svc}
}

type setupInstallRequest struct {
	Email     string `json:"email"`
	Password  string `json:"password"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	StoreName string `json:"store_name"`
}

type setupInstallResponse struct {
	AdminEmail        string `json:"admin_email"`
	MigrationsApplied int    `json:"migrations_applied"`
}

// Page serves GET /setup.
func (h *SetupHandler) Page() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, err := h.svc.Status(r.Context())
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if !status.NeedsSetup {
			http.Redirect(w, r, "/admin", http.StatusFound)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		setupWizardTemplate.Execute(w, nil)
	}
}

// Status handles GET /api/v1/setup/status.
func (h *SetupHandler) Status() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, err := h.svc.Status(r.Context())
		if err != nil {
			JSONError(w, err)
			return
		}
		JSON(w, http.StatusOK, status)
	}
}

// Install handles POST /api/v1/setup/install.
func (h *SetupHandler) Install() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req setupInstallRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		result, err := h.svc.Install(r.Context(), setupApp.InstallInput{
			Email:     strings.TrimSpace(req.Email),
			Password:  req.Password,
			FirstName: strings.TrimSpace(req.FirstName),
			LastName:  strings.TrimSpace(req.LastName),
			StoreName: strings.TrimSpace(req.StoreName),
		})
		if err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusCreated, setupInstallResponse{
			AdminEmail:        result.AdminEmail,
			MigrationsApplied: result.MigrationsApplied,
		})
	}
}

// SetupGate redirects /admin traffic to /setup while the store has no admin user.
func SetupGate(svc SetupService, next http.Handler) http.Handler {
	if svc == nil || next == nil {
		panic("http: setup gate requires service and handler")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status, err := svc.Status(r.Context())
		if err == nil && status.NeedsSetup {
			http.Redirect(w, r, "/setup", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}
