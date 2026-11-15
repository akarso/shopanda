package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	domainadmin "github.com/akarso/shopanda/internal/domain/admin"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/migrate"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

func TestParseExtensionFieldRetireArgs(t *testing.T) {
	got, err := parseExtensionFieldRetireArgs([]string{"acme.design", "--purge-values", "--confirm-idle"})
	if err != nil || got.code != "acme.design" || !got.purge || !got.confirmIdle {
		t.Fatalf("got %+v err=%v", got, err)
	}
	if _, err := parseExtensionFieldRetireArgs(nil); err == nil {
		t.Fatal("expected missing code error")
	}
	if _, err := parseExtensionFieldRetireArgs([]string{"--purge-values"}); err == nil {
		t.Fatal("expected missing code with only flag")
	}
	if _, err := parseExtensionFieldRetireArgs([]string{"--bogus", "acme.design"}); err == nil {
		t.Fatal("expected unknown flag error")
	}
	if _, err := parseExtensionFieldRetireArgs([]string{"--purge-values", "acme.design"}); err != nil {
		t.Fatalf("flag before code: %v", err)
	}
	if _, err := parseExtensionFieldRetireArgs([]string{"--help"}); !errors.Is(err, errExtensionFieldRetireHelp) {
		t.Fatalf("help err = %v", err)
	}
}

type memRetireStore struct {
	active      map[string]domainext.ExtensionField
	softDeleted map[string]domainext.ExtensionField
}

func (m *memRetireStore) FindByCode(_ context.Context, code string) (domainext.ExtensionField, error) {
	f, ok := m.active[code]
	if !ok {
		return domainext.ExtensionField{}, apperror.NotFound("extension field not found")
	}
	return f, nil
}
func (m *memRetireStore) Create(context.Context, domainext.ExtensionField) error {
	return nil
}
func (m *memRetireStore) Save(context.Context, domainext.ExtensionField) error { return nil }
func (m *memRetireStore) ListActive(context.Context, domainext.TargetType) ([]domainext.ExtensionField, error) {
	return nil, nil
}
func (m *memRetireStore) SoftDelete(context.Context, string) error { return nil }
func (m *memRetireStore) Retire(context.Context, string) error     { return nil }
func (m *memRetireStore) RetireActive(_ context.Context, code string, _ bool) (int64, error) {
	f, ok := m.active[code]
	if !ok {
		return 0, apperror.NotFound("not found")
	}
	delete(m.active, code)
	m.softDeleted[code] = f
	return 0, nil
}
func (m *memRetireStore) PurgeIfSoftDeletedPluginOnly(context.Context, string) (int64, error) {
	return 0, nil
}

func TestRunExtensionFieldRetire_RefusesPluginRegisteredCode(t *testing.T) {
	extReg := extensionapp.NewRegistry()
	if err := extReg.Register(domainext.FieldDef{
		Code: "acme.cli.refused", Label: "X", Type: domainext.FieldTypeString,
		Scope: domainext.TargetProduct, StorageMode: domainext.StorageStored,
	}); err != nil {
		t.Fatal(err)
	}
	store := &memRetireStore{
		active:      map[string]domainext.ExtensionField{},
		softDeleted: map[string]domainext.ExtensionField{},
	}
	// Invalid DSN: refusal must not require a live DB; audit write fails closed via log.
	conn, err := sql.Open("pgx", "postgres://invalid:invalid@127.0.0.1:1/shopanda?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	orig := openExtensionFieldRetireRuntime
	openExtensionFieldRetireRuntime = func(*config.Config, logger.Logger) (*extensionFieldRetireRuntime, error) {
		return &extensionFieldRetireRuntime{
			svc:       extensionapp.NewFieldService(extReg, store),
			store:     store,
			pluginExt: extReg,
			conn:      conn,
		}, nil
	}
	t.Cleanup(func() { openExtensionFieldRetireRuntime = orig })

	var out bytes.Buffer
	log := logger.NewWithWriter(os.Stderr, "error")
	err = runExtensionFieldRetire(&out, &config.Config{}, log, []string{"acme.cli.refused"})
	if err == nil || !strings.Contains(err.Error(), "still registers") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunExtensionFieldRetire_ConfirmIdleBeforeOpen(t *testing.T) {
	orig := openExtensionFieldRetireRuntime
	openExtensionFieldRetireRuntime = func(*config.Config, logger.Logger) (*extensionFieldRetireRuntime, error) {
		t.Fatal("openExtensionFieldRetireRuntime must not run without --confirm-idle")
		return nil, errors.New("unreachable")
	}
	t.Cleanup(func() { openExtensionFieldRetireRuntime = orig })

	err := runExtensionFieldRetire(&bytes.Buffer{}, &config.Config{}, logger.NewWithWriter(os.Stderr, "error"), []string{"acme.any", "--purge-values"})
	if err == nil || !strings.Contains(err.Error(), "--confirm-idle") {
		t.Fatalf("err = %v", err)
	}
}

type registerThenOKPlugin struct{ code string }

func (p *registerThenOKPlugin) Name() string { return "test/register-ok" }
func (p *registerThenOKPlugin) Init(app *plugin.App) error {
	return app.Extensions().RegisterField(domainext.FieldDef{
		Code: p.code, Label: "X", Type: domainext.FieldTypeString,
		Scope: domainext.TargetProduct, StorageMode: domainext.StorageStored,
		Access: domainext.Access{PluginOnlyWrite: true},
	})
}

type alwaysFailPlugin struct{}

func (p *alwaysFailPlugin) Name() string                  { return "test/always-fail" }
func (p *alwaysFailPlugin) Init(*plugin.App) error        { return errors.New("init boom after peer registered") }

func TestDefaultOpenExtensionFieldRetireRuntime_FailClosedWhenPeerInitFails(t *testing.T) {
	dsn := os.Getenv("SHOPANDA_TEST_DSN")
	if dsn == "" {
		t.Skip("SHOPANDA_TEST_DSN not set")
	}
	mediaDir := t.TempDir()
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("SHOPANDA_DEV_MODE", "true")
	cfg := &config.Config{}
	cfg.Media.Storage = "local"
	cfg.Media.Local.BasePath = mediaDir
	cfg.Media.Local.BaseURL = "http://127.0.0.1/media"
	cfg.Media.Local.SignSecret = strings.Repeat("s", 32)
	cfg.Queue.Driver = "postgres"
	cfg.Cache.Driver = "postgres"
	cfg.Search.Engine = "postgres"

	code := "acme.cli.failclosed"
	orig := retireBootRegisterPlugins
	retireBootRegisterPlugins = func(reg *plugin.Registry, bootCfg *config.Config) {
		reg.Register(&registerThenOKPlugin{code: code})
		reg.Register(&alwaysFailPlugin{})
	}
	t.Cleanup(func() { retireBootRegisterPlugins = orig })

	_, err := defaultOpenExtensionFieldRetireRuntime(cfg, logger.NewWithWriter(os.Stderr, "error"))
	if err == nil || !strings.Contains(err.Error(), "plugin init failed") {
		t.Fatalf("err = %v, want plugin init failed", err)
	}
}

func TestRunExtensionFieldRetire_RefusesWhenPluginInitFails(t *testing.T) {
	orig := openExtensionFieldRetireRuntime
	openExtensionFieldRetireRuntime = func(*config.Config, logger.Logger) (*extensionFieldRetireRuntime, error) {
		return nil, errors.New("plugin init failed: 1 plugin(s) failed to initialize (refuse retire while boot is incomplete)")
	}
	t.Cleanup(func() { openExtensionFieldRetireRuntime = orig })

	var out bytes.Buffer
	err := runExtensionFieldRetire(&out, &config.Config{}, logger.NewWithWriter(os.Stderr, "error"), []string{"acme.any"})
	if err == nil || !strings.Contains(err.Error(), "plugin init failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunExtensionFieldRetire_Integration(t *testing.T) {
	dsn := os.Getenv("SHOPANDA_TEST_DSN")
	if dsn == "" {
		t.Skip("SHOPANDA_TEST_DSN not set")
	}
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.Ping(); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Run(conn, "../../migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	code := "acme.retirecli.design"
	t.Cleanup(func() {
		_, _ = conn.Exec(`DELETE FROM extension_values WHERE field_code LIKE 'acme.retirecli.%'`)
		_, _ = conn.Exec(`DELETE FROM extension_fields WHERE code LIKE 'acme.retirecli.%'`)
		_, _ = conn.Exec(`DELETE FROM admin_audit_log WHERE resource_type = 'extension_field' AND resource_id LIKE 'acme.retirecli.%'`)
	})

	fieldRepo, err := postgres.NewExtensionFieldRepo(conn)
	if err != nil {
		t.Fatal(err)
	}
	valueRepo, err := postgres.NewExtensionValueRepo(conn)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	field, err := domainext.NewExtensionField(domainext.FieldDef{
		Code: code, Label: "Design", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageSnapshot,
		Access: domainext.Access{PluginOnlyWrite: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fieldRepo.Save(ctx, field); err != nil {
		t.Fatal(err)
	}
	text := "design-1"
	if err := valueRepo.Upsert(ctx, domainext.Value{
		FieldCode: code, TargetType: domainext.TargetCartItem, TargetID: "cart:var",
		Payload: domainext.ValuePayload{StringValue: &text}, UpdatedBy: "plugin:acme", UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	mediaDir := t.TempDir()
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("SHOPANDA_DEV_MODE", "true")
	cfg := &config.Config{}
	cfg.Media.Storage = "local"
	cfg.Media.Local.BasePath = mediaDir
	cfg.Media.Local.BaseURL = "http://127.0.0.1/media"
	cfg.Media.Local.SignSecret = strings.Repeat("s", 32)
	cfg.Queue.Driver = "postgres"
	cfg.Cache.Driver = "postgres"
	cfg.Search.Engine = "postgres"

	var out bytes.Buffer
	log := logger.NewWithWriter(os.Stderr, "error")
	if err := runExtensionFieldRetire(&out, cfg, log, []string{code, "--purge-values"}); err == nil {
		t.Fatal("expected error without --confirm-idle")
	}
	out.Reset()
	if err := runExtensionFieldRetire(&out, cfg, log, []string{code, "--purge-values", "--confirm-idle"}); err == nil || !strings.Contains(err.Error(), "soft-delete") {
		t.Fatalf("purge while active err = %v", err)
	}
	out.Reset()
	if err := runExtensionFieldRetire(&out, cfg, log, []string{code}); err != nil {
		t.Fatalf("soft-delete: %v\n(media=%s)", err, filepath.Clean(mediaDir))
	}
	out.Reset()
	if err := runExtensionFieldRetire(&out, cfg, log, []string{code, "--purge-values", "--confirm-idle"}); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if !strings.Contains(out.String(), code) || !strings.Contains(out.String(), "Purged 1") {
		t.Fatalf("output = %q", out.String())
	}

	if _, err := fieldRepo.FindByCode(ctx, code); !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("field still active: %v", err)
	}
	values, err := valueRepo.ListByTarget(ctx, domainext.Target{Type: domainext.TargetCartItem, ID: "cart:var"})
	if err != nil || len(values) != 0 {
		t.Fatalf("values after purge = %+v err=%v", values, err)
	}

	reg := extensionapp.NewRegistry()
	if err := reg.LoadPersisted(ctx, fieldRepo, nil); err != nil {
		t.Fatalf("LoadPersisted: %v", err)
	}
	if _, ok := reg.Get(code); ok {
		t.Fatal("retired field must not reload into the registry")
	}

	auditRepo, err := postgres.NewAuditLogRepo(conn)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := auditRepo.List(ctx, domainadmin.AuditLogFilter{
		Action: string(adminApp.AuditExtensionFieldRetire), ResourceType: "extension_field",
		ResourceID: code, Limit: 5,
	})
	if err != nil || len(rows) == 0 {
		t.Fatalf("audit rows=%v err=%v", rows, err)
	}
	var success bool
	for _, row := range rows {
		if row.Result == "success" && strings.HasPrefix(row.AdminID, "cli:") {
			success = true
			if row.Metadata["purge_values"] != true {
				t.Fatalf("metadata = %#v", row.Metadata)
			}
			break
		}
	}
	if !success {
		t.Fatalf("no success audit among %+v", rows)
	}
}

func TestMapExtensionFieldRetireError_PurgeIncompleteHint(t *testing.T) {
	err := mapExtensionFieldRetireError(domainext.ErrPurgeIncomplete, 0)
	if !errors.Is(err, domainext.ErrPurgeIncomplete) || !strings.Contains(err.Error(), "re-run") {
		t.Fatalf("err = %v", err)
	}
	err = mapExtensionFieldRetireError(domainext.ErrPurgeDefinitionRestored, 0)
	if !errors.Is(err, domainext.ErrPurgeDefinitionRestored) || strings.Contains(err.Error(), "--purge-values --confirm-idle") {
		t.Fatalf("restored err = %v, must not suggest purge retry", err)
	}
	err = mapExtensionFieldRetireError(domainext.ErrPurgeDefinitionMissing, 0)
	if !errors.Is(err, domainext.ErrPurgeDefinitionMissing) || !strings.Contains(err.Error(), "leftover values") {
		t.Fatalf("missing err = %v", err)
	}
	err = mapExtensionFieldRetireError(extensionapp.ErrFieldNotFound, 0)
	if err.Error() != "extension-field retire: field not found" {
		t.Fatalf("total miss = %v", err)
	}
	err = mapExtensionFieldRetireError(extensionapp.ErrFieldNotFound, 3)
	if !strings.Contains(err.Error(), "purged 3 value row(s) before stop") {
		t.Fatalf("partial miss = %v", err)
	}
	err = mapExtensionFieldRetireError(extensionapp.ErrPurgeWhileActive, 2)
	if !errors.Is(err, extensionapp.ErrPurgeWhileActive) || !strings.Contains(err.Error(), "purged 2 value row(s) before stop") {
		t.Fatalf("active re-read = %v", err)
	}
	err = mapExtensionFieldRetireError(errors.New("db timeout"), 5)
	if !strings.Contains(err.Error(), "db timeout") || !strings.Contains(err.Error(), "purged 5 value row(s) before stop") {
		t.Fatalf("re-read db err = %v", err)
	}
}
