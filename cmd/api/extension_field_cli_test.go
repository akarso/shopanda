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
	dsn := os.Getenv("SHOPANDA_TEST_DSN")
	if dsn == "" {
		t.Skip("SHOPANDA_TEST_DSN not set")
	}
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cerr := sql.Open("pgx", dsn)
		if cerr != nil {
			return
		}
		defer cleanup.Close()
		_, _ = cleanup.Exec(`DELETE FROM admin_audit_log WHERE resource_type = 'extension_field' AND resource_id = 'acme.cli.refused'`)
	})

	extReg := extensionapp.NewRegistry()
	if err := extReg.Register(domainext.FieldDef{
		Code: "acme.cli.refused", Label: "X", Type: domainext.FieldTypeString,
		Scope: domainext.TargetProduct, StorageMode: domainext.StorageStored,
	}); err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	store := &memRetireStore{
		active:      map[string]domainext.ExtensionField{},
		softDeleted: map[string]domainext.ExtensionField{},
	}
	orig := openExtensionFieldRetireRuntime
	openExtensionFieldRetireRuntime = func(*config.Config, logger.Logger) (*extensionFieldRetireRuntime, error) {
		// Ownership of conn transfers to runExtensionFieldRetire (defer Close).
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

	verify, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer verify.Close()
	auditRepo, err := postgres.NewAuditLogRepo(verify)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := auditRepo.List(context.Background(), domainadmin.AuditLogFilter{
		Action: string(adminApp.AuditExtensionFieldRetire), ResourceType: "extension_field",
		ResourceID: "acme.cli.refused", Limit: 5,
	})
	if err != nil || len(rows) == 0 || rows[0].Result != "error" {
		t.Fatalf("audit = %+v err=%v", rows, err)
	}
	if rows[0].Metadata["plugin_registered"] != true {
		t.Fatalf("metadata = %#v", rows[0].Metadata)
	}
}

func TestRunExtensionFieldRetire_RefusesWhenPluginInitFailsAfterRegister(t *testing.T) {
	// Fail-closed: openExtensionFieldRetireRuntime returns Failed Init error before soft-delete.
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
	cfg.Queue.Driver = "postgres"
	cfg.Cache.Driver = "postgres"
	cfg.Search.Engine = "postgres"

	var out bytes.Buffer
	log := logger.NewWithWriter(os.Stderr, "error")
	if err := runExtensionFieldRetire(&out, cfg, log, []string{code, "--purge-values"}); err == nil {
		t.Fatal("expected error without --confirm-idle")
	}
	out.Reset()
	if err := runExtensionFieldRetire(&out, cfg, log, []string{code, "--purge-values", "--confirm-idle"}); err != nil {
		t.Fatalf("runExtensionFieldRetire: %v\n(media=%s)", err, filepath.Clean(mediaDir))
	}
	if !strings.Contains(out.String(), code) || !strings.Contains(out.String(), "purged 1") {
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
