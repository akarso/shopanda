package jobs_test

import (
	"context"
	"errors"
	"testing"

	jobsApp "github.com/akarso/shopanda/internal/application/jobs"
	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
)

type fakeReader struct {
	listFilter domainjobs.ListFilter
	listResult []domainjobs.Summary
	listErr    error

	getID     string
	getResult *domainjobs.Detail
	getErr    error

	countsResult map[domainjobs.Status]int
	countsErr    error
}

func (f *fakeReader) List(_ context.Context, filter domainjobs.ListFilter) ([]domainjobs.Summary, error) {
	f.listFilter = filter
	return f.listResult, f.listErr
}

func (f *fakeReader) Get(_ context.Context, id string) (*domainjobs.Detail, error) {
	f.getID = id
	return f.getResult, f.getErr
}

func (f *fakeReader) CountsByStatus(context.Context) (map[domainjobs.Status]int, error) {
	return f.countsResult, f.countsErr
}

func TestNewService_NilReaderErrors(t *testing.T) {
	_, err := jobsApp.NewService(nil)
	if err == nil {
		t.Fatal("expected error for nil reader")
	}
}

func TestService_List_DefaultsLimitWhenUnset(t *testing.T) {
	r := &fakeReader{}
	svc, err := jobsApp.NewService(r)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := svc.List(context.Background(), domainjobs.ListFilter{}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if r.listFilter.Limit != 20 {
		t.Errorf("Limit = %d, want default 20", r.listFilter.Limit)
	}
}

func TestService_List_CapsLimitAboveMax(t *testing.T) {
	r := &fakeReader{}
	svc, _ := jobsApp.NewService(r)

	if _, err := svc.List(context.Background(), domainjobs.ListFilter{Limit: 500}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if r.listFilter.Limit != 100 {
		t.Errorf("Limit = %d, want capped at 100", r.listFilter.Limit)
	}
}

func TestService_List_PassesThroughValidLimit(t *testing.T) {
	r := &fakeReader{}
	svc, _ := jobsApp.NewService(r)

	if _, err := svc.List(context.Background(), domainjobs.ListFilter{Limit: 50}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if r.listFilter.Limit != 50 {
		t.Errorf("Limit = %d, want unchanged 50", r.listFilter.Limit)
	}
}

func TestService_List_FloorsNegativeOffset(t *testing.T) {
	r := &fakeReader{}
	svc, _ := jobsApp.NewService(r)

	if _, err := svc.List(context.Background(), domainjobs.ListFilter{Offset: -5}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if r.listFilter.Offset != 0 {
		t.Errorf("Offset = %d, want floored at 0", r.listFilter.Offset)
	}
}

// TestService_List_CapsExcessiveOffset pins the fix for an unbounded
// offset forcing Postgres to scan and discard an arbitrarily large number
// of rows for a request that returns nothing useful past a reasonable
// paging depth.
func TestService_List_CapsExcessiveOffset(t *testing.T) {
	r := &fakeReader{}
	svc, _ := jobsApp.NewService(r)

	if _, err := svc.List(context.Background(), domainjobs.ListFilter{Offset: 10_000_000}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if r.listFilter.Offset != 100_000 {
		t.Errorf("Offset = %d, want capped at 100000", r.listFilter.Offset)
	}
}

func TestService_List_PassesThroughValidOffset(t *testing.T) {
	r := &fakeReader{}
	svc, _ := jobsApp.NewService(r)

	if _, err := svc.List(context.Background(), domainjobs.ListFilter{Offset: 40}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if r.listFilter.Offset != 40 {
		t.Errorf("Offset = %d, want unchanged 40", r.listFilter.Offset)
	}
}

func TestService_List_PassesThroughTypeAndStatus(t *testing.T) {
	r := &fakeReader{}
	svc, _ := jobsApp.NewService(r)

	if _, err := svc.List(context.Background(), domainjobs.ListFilter{Type: "webhook.deliver", Status: "failed"}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if r.listFilter.Type != "webhook.deliver" || r.listFilter.Status != "failed" {
		t.Errorf("filter = %+v, want Type/Status preserved", r.listFilter)
	}
}

func TestService_List_PropagatesReaderError(t *testing.T) {
	r := &fakeReader{listErr: errors.New("db down")}
	svc, _ := jobsApp.NewService(r)

	_, err := svc.List(context.Background(), domainjobs.ListFilter{})
	if err == nil {
		t.Fatal("expected error to propagate from reader")
	}
}

func TestService_Get_EmptyIDErrors(t *testing.T) {
	svc, _ := jobsApp.NewService(&fakeReader{})
	_, err := svc.Get(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestService_Get_PassesThroughID(t *testing.T) {
	r := &fakeReader{getResult: &domainjobs.Detail{}}
	svc, _ := jobsApp.NewService(r)

	if _, err := svc.Get(context.Background(), "job-1"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if r.getID != "job-1" {
		t.Errorf("getID = %q, want job-1", r.getID)
	}
}

func TestService_Get_NotFoundReturnsNilNil(t *testing.T) {
	svc, _ := jobsApp.NewService(&fakeReader{getResult: nil})

	got, err := svc.Get(context.Background(), "missing")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Fatalf("Get = %+v, want nil for not found", got)
	}
}

func TestService_CountsByStatus(t *testing.T) {
	want := map[domainjobs.Status]int{domainjobs.StatusPending: 3, domainjobs.StatusFailed: 1}
	svc, _ := jobsApp.NewService(&fakeReader{countsResult: want})

	got, err := svc.CountsByStatus(context.Background())
	if err != nil {
		t.Fatalf("CountsByStatus: %v", err)
	}
	if got[domainjobs.StatusPending] != 3 || got[domainjobs.StatusFailed] != 1 {
		t.Errorf("got = %v, want %v", got, want)
	}
}
