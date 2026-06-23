package admin_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/application/admin"
	domainadmin "github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type mockAuditLogRepo struct {
	inserted []domainadmin.AuditLogRecord
	insertFn func(ctx context.Context, record domainadmin.AuditLogRecord) error
}

func (m *mockAuditLogRepo) Insert(ctx context.Context, record domainadmin.AuditLogRecord) error {
	if m.insertFn != nil {
		return m.insertFn(ctx, record)
	}
	m.inserted = append(m.inserted, record)
	return nil
}

func (m *mockAuditLogRepo) List(_ context.Context, _ domainadmin.AuditLogFilter) ([]domainadmin.AuditLogRecord, error) {
	return nil, nil
}

func TestAuditor_LogAction_PersistsEntryWithScopeTriad(t *testing.T) {
	repo := &mockAuditLogRepo{}
	log := logger.NewWithWriter(io.Discard, "error")
	auditor := admin.NewAuditor(log)
	auditor.SetAuditLogRepository(repo)

	auditor.LogAction(context.Background(), admin.AuditEntry{
		AdminID:      "admin-1",
		Action:       admin.AuditProductUpdate,
		ResourceType: "product",
		ResourceID:   "prod-1",
		Result:       "success",
		Details: map[string]interface{}{
			"store_id": "store-eu",
			"language": "en",
			"currency": "EUR",
			"name":     "Updated name",
		},
	})

	if len(repo.inserted) != 1 {
		t.Fatalf("inserted len = %d, want 1", len(repo.inserted))
	}
	rec := repo.inserted[0]
	if rec.AdminID != "admin-1" || rec.Action != string(admin.AuditProductUpdate) {
		t.Fatalf("unexpected record: %+v", rec)
	}
	if rec.StoreID != "store-eu" || rec.Language != "en" || rec.Currency != "EUR" {
		t.Fatalf("scope triad = store:%q lang:%q currency:%q", rec.StoreID, rec.Language, rec.Currency)
	}
	if rec.Metadata["name"] != "Updated name" {
		t.Fatalf("metadata = %#v", rec.Metadata)
	}
}

func TestAuditor_LogAction_PersistFailureDoesNotPanic(t *testing.T) {
	repo := &mockAuditLogRepo{
		insertFn: func(context.Context, domainadmin.AuditLogRecord) error {
			return errors.New("db down")
		},
	}
	log := logger.NewWithWriter(io.Discard, "error")
	auditor := admin.NewAuditor(log)
	auditor.SetAuditLogRepository(repo)

	auditor.LogAction(context.Background(), admin.AuditEntry{
		AdminID:      "admin-1",
		Action:       admin.AuditProductUpdate,
		ResourceType: "product",
		ResourceID:   "prod-1",
		Result:       "success",
	})
}
