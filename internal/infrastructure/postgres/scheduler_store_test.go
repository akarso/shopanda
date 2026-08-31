package postgres_test

import (
	"context"
	"errors"
	"testing"

	domainscheduler "github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/migrate"
)

func TestSchedulerStore_UpsertRegistrations_InsertsNew(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM scheduler_tasks") })

	store, err := postgres.NewSchedulerStore(db)
	if err != nil {
		t.Fatalf("NewSchedulerStore: %v", err)
	}
	ctx := context.Background()

	err = store.UpsertRegistrations(ctx, []domainscheduler.RegistrationEntry{
		{Name: "task-a", Spec: "* * * * *"},
		{Name: "task-b", Spec: "0 3 * * *"},
	})
	if err != nil {
		t.Fatalf("UpsertRegistrations: %v", err)
	}

	entries, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("List len = %d, want 2", len(entries))
	}
	if entries[0].Name != "task-a" || entries[1].Name != "task-b" {
		t.Errorf("List = %+v, want task-a, task-b in order", entries)
	}
	for _, e := range entries {
		if !e.Enabled {
			t.Errorf("entry %q: Enabled = false, want true (default-on for a fresh registration)", e.Name)
		}
	}
}

// TestSchedulerStore_UpsertRegistrations_PreservesEnabledOverride pins the
// fix for a process restart clobbering an admin's disable override: a
// re-registration of an already-known task must update spec/updated_at
// without touching enabled.
func TestSchedulerStore_UpsertRegistrations_PreservesEnabledOverride(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM scheduler_tasks") })

	store, err := postgres.NewSchedulerStore(db)
	if err != nil {
		t.Fatalf("NewSchedulerStore: %v", err)
	}
	ctx := context.Background()

	if err := store.UpsertRegistrations(ctx, []domainscheduler.RegistrationEntry{{Name: "task", Spec: "* * * * *"}}); err != nil {
		t.Fatalf("UpsertRegistrations (initial): %v", err)
	}
	if err := store.SetEnabled(ctx, "task", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}

	// Simulate a process restart re-registering the same task with a
	// (hypothetically) updated spec.
	if err := store.UpsertRegistrations(ctx, []domainscheduler.RegistrationEntry{{Name: "task", Spec: "*/5 * * * *"}}); err != nil {
		t.Fatalf("UpsertRegistrations (re-register): %v", err)
	}

	entries, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("List len = %d, want 1", len(entries))
	}
	if entries[0].Spec != "*/5 * * * *" {
		t.Errorf("Spec = %q, want updated spec", entries[0].Spec)
	}
	if entries[0].Enabled {
		t.Error("Enabled = true, want the disable override to survive re-registration")
	}
}

func TestSchedulerStore_IsEnabledBatch_UnknownTaskAbsentFromResult(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM scheduler_tasks") })

	store, err := postgres.NewSchedulerStore(db)
	if err != nil {
		t.Fatalf("NewSchedulerStore: %v", err)
	}

	result, err := store.IsEnabledBatch(context.Background(), []string{"never-registered"})
	if err != nil {
		t.Fatalf("IsEnabledBatch: %v", err)
	}
	if _, ok := result["never-registered"]; ok {
		t.Error("result contains an entry for an unregistered task, want it absent (caller treats absence as default-on)")
	}
}

func TestSchedulerStore_IsEnabledBatch_MixedKnownAndUnknown(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM scheduler_tasks") })

	store, err := postgres.NewSchedulerStore(db)
	if err != nil {
		t.Fatalf("NewSchedulerStore: %v", err)
	}
	ctx := context.Background()
	if err := store.UpsertRegistrations(ctx, []domainscheduler.RegistrationEntry{
		{Name: "task-a", Spec: "* * * * *"},
		{Name: "task-b", Spec: "* * * * *"},
	}); err != nil {
		t.Fatalf("UpsertRegistrations: %v", err)
	}
	if err := store.SetEnabled(ctx, "task-b", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}

	result, err := store.IsEnabledBatch(ctx, []string{"task-a", "task-b", "never-registered"})
	if err != nil {
		t.Fatalf("IsEnabledBatch: %v", err)
	}
	if enabled, ok := result["task-a"]; !ok || !enabled {
		t.Errorf("task-a = (%v, %v), want (true, present)", enabled, ok)
	}
	if enabled, ok := result["task-b"]; !ok || enabled {
		t.Errorf("task-b = (%v, %v), want (false, present)", enabled, ok)
	}
	if _, ok := result["never-registered"]; ok {
		t.Error("never-registered present in result, want absent")
	}
}

func TestSchedulerStore_List_ComputesNextRun(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM scheduler_tasks") })

	store, err := postgres.NewSchedulerStore(db)
	if err != nil {
		t.Fatalf("NewSchedulerStore: %v", err)
	}
	ctx := context.Background()

	if err := store.UpsertRegistrations(ctx, []domainscheduler.RegistrationEntry{{Name: "task", Spec: "0 * * * *"}}); err != nil {
		t.Fatalf("UpsertRegistrations: %v", err)
	}

	entries, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("List len = %d, want 1", len(entries))
	}
	if entries[0].NextRun.IsZero() {
		t.Error("NextRun is zero, want a computed future time")
	}
	if entries[0].NextRun.Minute() != 0 {
		t.Errorf("NextRun = %v, want minute 0 for spec %q", entries[0].NextRun, entries[0].Spec)
	}
}

func TestSchedulerStore_SetEnabled_NotFound(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM scheduler_tasks") })

	store, err := postgres.NewSchedulerStore(db)
	if err != nil {
		t.Fatalf("NewSchedulerStore: %v", err)
	}

	err = store.SetEnabled(context.Background(), "never-registered", false)
	if err == nil {
		t.Fatal("expected error for unregistered task")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeNotFound {
		t.Fatalf("err = %v, want *apperror.Error{Code: not_found}", err)
	}
}

func TestSchedulerStore_Trigger_NotFound(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM scheduler_tasks") })

	store, err := postgres.NewSchedulerStore(db)
	if err != nil {
		t.Fatalf("NewSchedulerStore: %v", err)
	}

	err = store.Trigger(context.Background(), "never-registered")
	if err == nil {
		t.Fatal("expected error for unregistered task")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeNotFound {
		t.Fatalf("err = %v, want *apperror.Error{Code: not_found}", err)
	}
}

// TestSchedulerStore_Trigger_ConflictWithoutLocalTrigger pins the
// "no embedded scheduler in this process" contract: a known task with no
// attached LocalTrigger must fail clearly, not silently no-op.
func TestSchedulerStore_Trigger_ConflictWithoutLocalTrigger(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM scheduler_tasks") })

	store, err := postgres.NewSchedulerStore(db)
	if err != nil {
		t.Fatalf("NewSchedulerStore: %v", err)
	}
	ctx := context.Background()
	if err := store.UpsertRegistrations(ctx, []domainscheduler.RegistrationEntry{{Name: "task", Spec: "* * * * *"}}); err != nil {
		t.Fatalf("UpsertRegistrations: %v", err)
	}

	err = store.Trigger(ctx, "task")
	if err == nil {
		t.Fatal("expected error when no LocalTrigger is attached")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeConflict {
		t.Fatalf("err = %v, want *apperror.Error{Code: conflict}", err)
	}
}

type fakeLocalTrigger struct {
	triggeredName string
	err           error
}

func (f *fakeLocalTrigger) TriggerLocal(name string) error {
	f.triggeredName = name
	return f.err
}

func TestSchedulerStore_Trigger_DelegatesToLocalTrigger(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM scheduler_tasks") })

	store, err := postgres.NewSchedulerStore(db)
	if err != nil {
		t.Fatalf("NewSchedulerStore: %v", err)
	}
	ctx := context.Background()
	if err := store.UpsertRegistrations(ctx, []domainscheduler.RegistrationEntry{{Name: "task", Spec: "* * * * *"}}); err != nil {
		t.Fatalf("UpsertRegistrations: %v", err)
	}

	local := &fakeLocalTrigger{}
	store.SetLocalTrigger(local)

	if err := store.Trigger(ctx, "task"); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if local.triggeredName != "task" {
		t.Errorf("triggeredName = %q, want %q", local.triggeredName, "task")
	}
}

// TestSchedulerStore_Trigger_DivergentRegistrationsReturnsConflict pins the
// fix for a misleading error when this process embeds a scheduler whose
// local task set doesn't include a task the shared registry knows about
// (e.g. two `serve --embed-scheduler` instances with different plugin
// configs). The shared-registry check confirms the task exists, so
// LocalTrigger's own not-found (correct from its narrower point of view)
// must not be passed straight through as "no such task anywhere" — that
// would send an operator looking for a typo that isn't there.
func TestSchedulerStore_Trigger_DivergentRegistrationsReturnsConflict(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM scheduler_tasks") })

	store, err := postgres.NewSchedulerStore(db)
	if err != nil {
		t.Fatalf("NewSchedulerStore: %v", err)
	}
	ctx := context.Background()
	// Registered in the shared table (e.g. by a different process), but
	// this process's own local scheduler doesn't have it.
	if err := store.UpsertRegistrations(ctx, []domainscheduler.RegistrationEntry{{Name: "other-process-task", Spec: "* * * * *"}}); err != nil {
		t.Fatalf("UpsertRegistrations: %v", err)
	}

	local := &fakeLocalTrigger{err: apperror.NotFound(`no scheduled task named "other-process-task"`)}
	store.SetLocalTrigger(local)

	err = store.Trigger(ctx, "other-process-task")
	if err == nil {
		t.Fatal("expected an error")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("err = %v, want *apperror.Error", err)
	}
	if appErr.Code != apperror.CodeConflict {
		t.Errorf("Code = %q, want %q — a task known to the shared registry but missing locally must not read as CodeNotFound (\"doesn't exist anywhere\")", appErr.Code, apperror.CodeConflict)
	}
}
