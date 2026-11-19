package pluginsdk_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

type stubJobsQueue struct {
	requeueID string
	attempt   int
}

func (stubJobsQueue) Enqueue(context.Context, jobs.Job) error        { return nil }
func (stubJobsQueue) Dequeue(context.Context) (*jobs.Job, error)     { return nil, nil }
func (stubJobsQueue) Complete(context.Context, string, int) error    { return nil }
func (stubJobsQueue) Fail(context.Context, string, int, error) error { return nil }
func (q *stubJobsQueue) Requeue(_ context.Context, id string, attempt int, _ ...jobs.RequeueOption) error {
	q.requeueID = id
	q.attempt = attempt
	return nil
}

type stubJobsReader struct {
	detail *jobs.Detail
	counts map[jobs.Status]int
}

func (r stubJobsReader) List(context.Context, jobs.ListFilter) ([]jobs.Summary, error) {
	return nil, nil
}
func (r stubJobsReader) Get(_ context.Context, id string) (*jobs.Detail, error) {
	if r.detail != nil && r.detail.ID == id {
		return r.detail, nil
	}
	return nil, nil
}
func (r stubJobsReader) CountsByStatus(context.Context) (map[jobs.Status]int, error) {
	return r.counts, nil
}
func (r stubJobsReader) CountsByType(context.Context, string) (map[jobs.Status]int, error) {
	return nil, errors.New("unused")
}

func TestNewJobsPort_NilArgs(t *testing.T) {
	q := &stubJobsQueue{}
	if _, err := pluginsdk.NewJobsPort(nil, q); err == nil {
		t.Fatal("want error for nil reader")
	}
	if _, err := pluginsdk.NewJobsPort(stubJobsReader{}, nil); err == nil {
		t.Fatal("want error for nil queue")
	}
}

func TestJobsPort_GetCountsRequeue(t *testing.T) {
	q := &stubJobsQueue{}
	detail := &jobs.Detail{Summary: jobs.Summary{ID: "job-1", Status: jobs.StatusFailed, Attempts: 2}}
	reader := stubJobsReader{
		detail: detail,
		counts: map[jobs.Status]int{jobs.StatusFailed: 3},
	}
	port, err := pluginsdk.NewJobsPort(reader, q)
	if err != nil {
		t.Fatal(err)
	}
	got, err := port.Get(context.Background(), "job-1")
	if err != nil || got == nil || got.ID != "job-1" {
		t.Fatalf("Get = (%v, %v)", got, err)
	}
	counts, err := port.CountsByStatus(context.Background())
	if err != nil || counts[pluginsdk.JobStatusFailed] != 3 {
		t.Fatalf("CountsByStatus = (%v, %v)", counts, err)
	}
	if err := port.Requeue(context.Background(), "job-1", 2, pluginsdk.WithRequeueTerminal()); err != nil {
		t.Fatal(err)
	}
	if q.requeueID != "job-1" || q.attempt != 2 {
		t.Fatalf("Requeue captured (%q, %d)", q.requeueID, q.attempt)
	}
	_ = pluginsdk.WithRequeueMinAge(time.Minute)
}

func TestAssertNeedsMet_CapJobs(t *testing.T) {
	app := &plugin.App{}
	app.RecordNeeds(plugin.CapJobs)
	if err := app.AssertNeedsMet(); err == nil {
		t.Fatal("expected error for unbound CapJobs")
	}
	port, err := pluginsdk.NewJobsPort(stubJobsReader{}, &stubJobsQueue{})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.BindJobsPort(port); err != nil {
		t.Fatal(err)
	}
	if err := app.AssertNeedsMet(); err != nil {
		t.Fatalf("AssertNeedsMet: %v", err)
	}
}

func TestAsHostFor_Jobs_GatedByCap(t *testing.T) {
	app := &plugin.App{}
	port, err := pluginsdk.NewJobsPort(stubJobsReader{
		detail: &jobs.Detail{Summary: jobs.Summary{ID: "j1"}},
	}, &stubJobsQueue{})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.BindJobsPort(port); err != nil {
		t.Fatal(err)
	}

	denied := pluginsdk.AsHostFor(app, "acme/jobs")
	if _, ok := denied.Jobs(); ok {
		t.Fatal("expected Jobs() denied without Require(CapJobs)")
	}

	allowed := pluginsdk.AsHostFor(app, "acme/jobs")
	allowed.Require(pluginsdk.CapJobs)
	jp, ok := allowed.Jobs()
	if !ok || jp == nil {
		t.Fatal("expected Jobs() after Require(CapJobs)")
	}
	got, err := jp.Get(context.Background(), "j1")
	if err != nil || got == nil || got.ID != "j1" {
		t.Fatalf("Get via Host.Jobs = (%v, %v)", got, err)
	}
}
