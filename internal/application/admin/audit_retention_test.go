package admin_test

import (
	"context"
	"testing"
	"time"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type retentionConfigRepo struct {
	values map[string]interface{}
}

func (s *retentionConfigRepo) Get(_ context.Context, key string) (interface{}, error) {
	if s.values == nil {
		return nil, nil
	}
	return s.values[key], nil
}

type stubRetentionRepo struct {
	cutoff  time.Time
	deleted int64
}

func (s *stubRetentionRepo) DeleteBefore(_ context.Context, cutoff time.Time) (int64, error) {
	s.cutoff = cutoff
	return s.deleted, nil
}

type retentionLogSpy struct {
	infos []map[string]interface{}
}

func (s *retentionLogSpy) Info(_ string, fields map[string]interface{}) {
	s.infos = append(s.infos, fields)
}

func TestRetentionHandler_SkipsWhenDisabled(t *testing.T) {
	repo := &stubRetentionRepo{}
	logSpy := &retentionLogSpy{}
	h := adminApp.NewRetentionHandler(repo, &retentionConfigRepo{}, logSpy)
	if err := h.Handle(context.Background(), jobs.Job{}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !repo.cutoff.IsZero() {
		t.Fatal("expected no delete when retention disabled")
	}
}

func TestRetentionHandler_DeletesOlderRows(t *testing.T) {
	repo := &stubRetentionRepo{deleted: 3}
	logSpy := &retentionLogSpy{}
	h := adminApp.NewRetentionHandler(repo, &retentionConfigRepo{
		values: map[string]interface{}{admin.RetentionDaysConfigKey: 30},
	}, logSpy)
	if err := h.Handle(context.Background(), jobs.Job{}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if repo.deleted != 3 {
		t.Fatalf("deleted = %d, want 3", repo.deleted)
	}
	wantCutoff := time.Now().UTC().AddDate(0, 0, -30)
	if repo.cutoff.After(wantCutoff.Add(time.Minute)) || repo.cutoff.Before(wantCutoff.Add(-time.Minute)) {
		t.Fatalf("cutoff = %v, want near %v", repo.cutoff, wantCutoff)
	}
}

func TestRetentionHandler_Type(t *testing.T) {
	h := adminApp.NewRetentionHandler(&stubRetentionRepo{}, &retentionConfigRepo{}, logger.New("error"))
	if h.Type() != adminApp.RetentionJobType {
		t.Fatalf("type = %q", h.Type())
	}
}
