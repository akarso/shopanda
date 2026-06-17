package runtime_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/runtime"
)

type stubScheduler struct {
	tasks []string
}

func (s *stubScheduler) Register(name, _ string, _ func()) {
	s.tasks = append(s.tasks, name)
}

func (s *stubScheduler) Start(context.Context) {}

func (s *stubScheduler) Stop() {}

type stubQueue struct{}

func (stubQueue) Enqueue(context.Context, jobs.Job) error { return nil }
func (stubQueue) Dequeue(context.Context) (*jobs.Job, error) {
	return nil, nil
}
func (stubQueue) Complete(context.Context, string) error { return nil }
func (stubQueue) Fail(context.Context, string, error) error {
	return nil
}

func TestRegisterCacheCleanup(t *testing.T) {
	sched := &stubScheduler{}
	runtime.RegisterCacheCleanup(stubQueue{}, logger.New("error"), sched)
	if len(sched.tasks) != 1 || sched.tasks[0] != "cache.cleanup" {
		t.Fatalf("tasks = %v, want [cache.cleanup]", sched.tasks)
	}
}

var _ scheduler.Scheduler = (*stubScheduler)(nil)
