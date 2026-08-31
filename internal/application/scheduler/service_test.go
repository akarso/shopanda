package scheduler_test

import (
	"context"
	"errors"
	"testing"

	schedulerApp "github.com/akarso/shopanda/internal/application/scheduler"
	domainscheduler "github.com/akarso/shopanda/internal/domain/scheduler"
)

type fakeCatalog struct {
	listResult []domainscheduler.CatalogEntry
	listErr    error

	triggerName string
	triggerErr  error

	setEnabledName    string
	setEnabledEnabled bool
	setEnabledErr     error
}

func (f *fakeCatalog) List(context.Context) ([]domainscheduler.CatalogEntry, error) {
	return f.listResult, f.listErr
}

func (f *fakeCatalog) Trigger(_ context.Context, name string) error {
	f.triggerName = name
	return f.triggerErr
}

func (f *fakeCatalog) SetEnabled(_ context.Context, name string, enabled bool) error {
	f.setEnabledName = name
	f.setEnabledEnabled = enabled
	return f.setEnabledErr
}

func TestNewService_NilCatalogErrors(t *testing.T) {
	_, err := schedulerApp.NewService(nil)
	if err == nil {
		t.Fatal("expected error for nil catalog")
	}
}

func TestService_List_PassesThroughResult(t *testing.T) {
	want := []domainscheduler.CatalogEntry{{Name: "task-a"}, {Name: "task-b"}}
	svc, err := schedulerApp.NewService(&fakeCatalog{listResult: want})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	got, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List = %+v, want 2 entries", got)
	}
}

func TestService_List_PropagatesCatalogError(t *testing.T) {
	svc, _ := schedulerApp.NewService(&fakeCatalog{listErr: errors.New("db down")})

	_, err := svc.List(context.Background())
	if err == nil {
		t.Fatal("expected error to propagate from catalog")
	}
}

func TestService_Trigger_EmptyNameErrors(t *testing.T) {
	svc, _ := schedulerApp.NewService(&fakeCatalog{})
	if err := svc.Trigger(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestService_Trigger_PassesThroughName(t *testing.T) {
	c := &fakeCatalog{}
	svc, _ := schedulerApp.NewService(c)

	if err := svc.Trigger(context.Background(), "task"); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if c.triggerName != "task" {
		t.Errorf("triggerName = %q, want %q", c.triggerName, "task")
	}
}

func TestService_Trigger_PropagatesCatalogError(t *testing.T) {
	c := &fakeCatalog{triggerErr: errors.New("conflict")}
	svc, _ := schedulerApp.NewService(c)

	if err := svc.Trigger(context.Background(), "task"); err == nil {
		t.Fatal("expected error to propagate from catalog")
	}
}

func TestService_SetEnabled_EmptyNameErrors(t *testing.T) {
	svc, _ := schedulerApp.NewService(&fakeCatalog{})
	if err := svc.SetEnabled(context.Background(), "", true); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestService_SetEnabled_PassesThroughArgs(t *testing.T) {
	c := &fakeCatalog{}
	svc, _ := schedulerApp.NewService(c)

	if err := svc.SetEnabled(context.Background(), "task", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if c.setEnabledName != "task" || c.setEnabledEnabled != false {
		t.Errorf("SetEnabled call = (%q, %v), want (task, false)", c.setEnabledName, c.setEnabledEnabled)
	}
}

func TestService_SetEnabled_PropagatesCatalogError(t *testing.T) {
	c := &fakeCatalog{setEnabledErr: errors.New("not found")}
	svc, _ := schedulerApp.NewService(c)

	if err := svc.SetEnabled(context.Background(), "task", true); err == nil {
		t.Fatal("expected error to propagate from catalog")
	}
}
