package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

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

	t.Setenv("DATABASE_URL", dsn)
	cfg := &config.Config{}
	var out bytes.Buffer
	log := logger.NewWithWriter(os.Stderr, "error")
	if err := runExtensionFieldRetire(&out, cfg, log, []string{code, "--purge-values"}); err == nil {
		t.Fatal("expected error without --confirm-idle")
	}
	if err := runExtensionFieldRetire(&out, cfg, log, []string{code, "--purge-values", "--confirm-idle"}); err != nil {
		t.Fatalf("runExtensionFieldRetire: %v", err)
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
		Action:       "extension_field.retire",
		ResourceType: "extension_field",
		ResourceID:   code,
		Limit:        5,
	})
	if err != nil {
		t.Fatalf("audit list: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("expected audit row for extension_field.retire")
	}
	row := rows[0]
	if row.Result != "success" || !strings.HasPrefix(row.AdminID, "cli:") {
		t.Fatalf("audit row = %+v", row)
	}
	if row.Metadata["purge_values"] != true {
		t.Fatalf("purge_values metadata = %#v", row.Metadata["purge_values"])
	}
	switch n := row.Metadata["values_purged"].(type) {
	case float64:
		if n != 1 {
			t.Fatalf("values_purged = %v", n)
		}
	case json.Number:
		if n.String() != "1" {
			t.Fatalf("values_purged = %v", n)
		}
	case int64:
		if n != 1 {
			t.Fatalf("values_purged = %v", n)
		}
	default:
		t.Fatalf("values_purged metadata = %#v", row.Metadata["values_purged"])
	}
}
