package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/migrate"
)

func TestParseExtensionFieldRetireArgs(t *testing.T) {
	code, purge, err := parseExtensionFieldRetireArgs([]string{"acme.design", "--purge-values"})
	if err != nil || code != "acme.design" || !purge {
		t.Fatalf("got code=%q purge=%v err=%v", code, purge, err)
	}
	if _, _, err := parseExtensionFieldRetireArgs(nil); err == nil {
		t.Fatal("expected missing code error")
	}
	if _, _, err := parseExtensionFieldRetireArgs([]string{"--purge-values"}); err == nil {
		t.Fatal("expected missing code with only flag")
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
	t.Cleanup(func() {
		_, _ = conn.Exec(`DELETE FROM extension_values WHERE field_code LIKE 'acme.retirecli.%'`)
		_, _ = conn.Exec(`DELETE FROM extension_fields WHERE code LIKE 'acme.retirecli.%'`)
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
	code := "acme.retirecli.design"
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
	if err := runExtensionFieldRetire(&out, cfg, log, []string{code, "--purge-values"}); err != nil {
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
}
