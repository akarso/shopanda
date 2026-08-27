package runtime_test

import (
	"context"
	"testing"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	cacheApp "github.com/akarso/shopanda/internal/application/cache"
	inventoryApp "github.com/akarso/shopanda/internal/application/inventory"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/runtime"
)

type registeredTask struct {
	name     string
	schedule string
	fn       func()
}

type stubScheduler struct {
	tasks []registeredTask
}

func (s *stubScheduler) Register(name, schedule string, fn func()) {
	s.tasks = append(s.tasks, registeredTask{name: name, schedule: schedule, fn: fn})
}

func (s *stubScheduler) Start(context.Context) {}

func (s *stubScheduler) Stop() {}

type recordingQueue struct {
	enqueued []jobs.Job
}

func (q *recordingQueue) Enqueue(_ context.Context, job jobs.Job) error {
	q.enqueued = append(q.enqueued, job)
	return nil
}

func (recordingQueue) Dequeue(context.Context) (*jobs.Job, error) {
	return nil, nil
}

func (recordingQueue) Complete(context.Context, string) error { return nil }

func (recordingQueue) Fail(context.Context, string, error) error { return nil }

func TestRegisterCacheCleanup(t *testing.T) {
	sched := &stubScheduler{}
	queue := &recordingQueue{}
	runtime.RegisterCacheCleanup(queue, cacheApp.JobType, logger.New("error"), sched)

	if len(sched.tasks) != 1 {
		t.Fatalf("tasks len = %d, want 1", len(sched.tasks))
	}
	task := sched.tasks[0]
	if task.name != "cache.cleanup" {
		t.Fatalf("task name = %q, want cache.cleanup", task.name)
	}
	if task.schedule != "*/5 * * * *" {
		t.Fatalf("task schedule = %q, want */5 * * * *", task.schedule)
	}
	if task.fn == nil {
		t.Fatal("expected callback to be registered")
	}

	task.fn()
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued len = %d, want 1", len(queue.enqueued))
	}
	if queue.enqueued[0].Type != cacheApp.JobType {
		t.Fatalf("job type = %q, want %q", queue.enqueued[0].Type, cacheApp.JobType)
	}
}

func TestRegisterAuditRetention(t *testing.T) {
	sched := &stubScheduler{}
	queue := &recordingQueue{}
	runtime.RegisterAuditRetention(queue, logger.New("error"), sched)

	if len(sched.tasks) != 1 {
		t.Fatalf("tasks len = %d, want 1", len(sched.tasks))
	}
	task := sched.tasks[0]
	if task.name != "audit.retention" {
		t.Fatalf("task name = %q, want audit.retention", task.name)
	}
	if task.schedule != "0 3 * * *" {
		t.Fatalf("task schedule = %q, want 0 3 * * *", task.schedule)
	}
	task.fn()
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued len = %d, want 1", len(queue.enqueued))
	}
	if queue.enqueued[0].Type != adminApp.RetentionJobType {
		t.Fatalf("job type = %q, want %q", queue.enqueued[0].Type, adminApp.RetentionJobType)
	}
}

func TestRegisterReservationExpiry(t *testing.T) {
	sched := &stubScheduler{}
	queue := &recordingQueue{}
	runtime.RegisterReservationExpiry(queue, logger.New("error"), sched)

	if len(sched.tasks) != 1 {
		t.Fatalf("tasks len = %d, want 1", len(sched.tasks))
	}
	task := sched.tasks[0]
	if task.name != "inventory.reservation_expiry" {
		t.Fatalf("task name = %q, want inventory.reservation_expiry", task.name)
	}
	if task.schedule != "*/15 * * * *" {
		t.Fatalf("task schedule = %q, want */15 * * * *", task.schedule)
	}
	task.fn()
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued len = %d, want 1", len(queue.enqueued))
	}
	if queue.enqueued[0].Type != inventoryApp.ReservationExpiryJobType {
		t.Fatalf("job type = %q, want %q", queue.enqueued[0].Type, inventoryApp.ReservationExpiryJobType)
	}
}

var _ scheduler.Scheduler = (*stubScheduler)(nil)
