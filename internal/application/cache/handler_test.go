package cache_test

import (
	"context"
	"testing"

	appCache "github.com/akarso/shopanda/internal/application/cache"
	"github.com/akarso/shopanda/internal/domain/jobs"
)

type stubDeleter struct {
	deleted int64
	err     error
	called  bool
}

func (s *stubDeleter) DeleteExpired() (int64, error) {
	s.called = true
	return s.deleted, s.err
}

type stubLogger struct {
	infos  []string
	fields map[string]interface{}
}

func (l *stubLogger) Info(msg string, f map[string]interface{}) {
	l.infos = append(l.infos, msg)
	l.fields = f
}

func TestCleanupHandler_Type(t *testing.T) {
	h := appCache.NewCleanupHandler(&stubDeleter{}, &stubLogger{})
	if h.Type() != appCache.JobType {
		t.Errorf("Type() = %q, want %q", h.Type(), appCache.JobType)
	}
}

func TestCleanupHandler_Handle_Success(t *testing.T) {
	d := &stubDeleter{deleted: 42}
	log := &stubLogger{}
	h := appCache.NewCleanupHandler(d, log)

	job := jobs.Job{ID: "j1", Type: "cache.cleanup"}
	err := h.Handle(context.Background(), job)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !d.called {
		t.Error("expected DeleteExpired to be called")
	}
	if len(log.infos) != 1 || log.infos[0] != "cache.cleanup.complete" {
		t.Errorf("infos = %v, want [cache.cleanup.complete]", log.infos)
	}
	if got, ok := log.fields["deleted"]; !ok {
		t.Error("expected deleted field in log")
	} else if got != int64(42) {
		t.Errorf("log.fields[deleted] = %v, want 42", got)
	}
}

func TestCleanupHandler_Handle_Error(t *testing.T) {
	d := &stubDeleter{err: context.DeadlineExceeded}
	log := &stubLogger{}
	h := appCache.NewCleanupHandler(d, log)

	job := jobs.Job{ID: "j2", Type: "cache.cleanup"}
	err := h.Handle(context.Background(), job)
	if err == nil {
		t.Fatal("expected error")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
}

func TestCleanupHandler_PanicsOnNilDeleter(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil deleter")
		}
	}()
	appCache.NewCleanupHandler(nil, &stubLogger{})
}

func TestCleanupHandler_PanicsOnNilLogger(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil logger")
		}
	}()
	appCache.NewCleanupHandler(&stubDeleter{}, nil)
}
