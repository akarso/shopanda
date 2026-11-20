package pluginsdk_test

import (
	"context"
	"strings"
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
	byType map[string]map[jobs.Status]int
}

func (r *stubJobsReader) List(context.Context, jobs.ListFilter) ([]jobs.Summary, error) {
	return nil, nil
}
func (r *stubJobsReader) Get(_ context.Context, id string) (*jobs.Detail, error) {
	if r.detail != nil && r.detail.ID == id {
		return r.detail, nil
	}
	return nil, nil
}
func (r *stubJobsReader) CountsByStatus(context.Context) (map[jobs.Status]int, error) {
	return r.counts, nil
}
func (r *stubJobsReader) CountsByType(_ context.Context, jobType string) (map[jobs.Status]int, error) {
	// Mirror Postgres Reader: empty/whitespace fail closed (port also validates).
	if strings.TrimSpace(jobType) == "" {
		return nil, errEmptyJobType
	}
	if r.byType == nil {
		return map[jobs.Status]int{}, nil
	}
	got, ok := r.byType[jobType]
	if !ok {
		return map[jobs.Status]int{}, nil
	}
	out := make(map[jobs.Status]int, len(got))
	for k, v := range got {
		out[k] = v
	}
	return out, nil
}

type emptyJobTypeError struct{}

func (emptyJobTypeError) Error() string { return "job type required" }

var errEmptyJobType = emptyJobTypeError{}

func TestNewJobsPort_NilArgs(t *testing.T) {
	q := &stubJobsQueue{}
	if _, err := pluginsdk.NewJobsPort(nil, q); err == nil {
		t.Fatal("want error for nil reader")
	}
	if _, err := pluginsdk.NewJobsPort(&stubJobsReader{}, nil); err == nil {
		t.Fatal("want error for nil queue")
	}
}

func TestJobsPort_GetCountsRequeue(t *testing.T) {
	q := &stubJobsQueue{}
	detail := &jobs.Detail{Summary: jobs.Summary{ID: "job-1", Status: jobs.StatusFailed, Attempts: 2}}
	reader := &stubJobsReader{
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
	miss, err := port.Get(context.Background(), "missing-id")
	if err != nil || miss != nil {
		t.Fatalf("Get miss = (%v, %v), want (nil, nil)", miss, err)
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

func TestJobsPort_CountsByType_TwoTypes(t *testing.T) {
	const (
		typeA = "integration.sync.acme.gen.run"
		typeB = "integration.sync.acme.prep.run"
	)
	reader := &stubJobsReader{
		byType: map[string]map[jobs.Status]int{
			typeA: {jobs.StatusPending: 2, jobs.StatusFailed: 1},
			typeB: {jobs.StatusProcessing: 3},
		},
	}
	port, err := pluginsdk.NewJobsPort(reader, &stubJobsQueue{})
	if err != nil {
		t.Fatal(err)
	}
	a, err := port.CountsByType(context.Background(), typeA)
	if err != nil {
		t.Fatalf("CountsByType A: %v", err)
	}
	if a[pluginsdk.JobStatusPending] != 2 || a[pluginsdk.JobStatusFailed] != 1 {
		t.Fatalf("type A counts = %#v", a)
	}
	if _, ok := a[pluginsdk.JobStatusProcessing]; ok {
		t.Fatalf("type A must omit zero processing: %#v", a)
	}
	b, err := port.CountsByType(context.Background(), typeB)
	if err != nil {
		t.Fatalf("CountsByType B: %v", err)
	}
	if b[pluginsdk.JobStatusProcessing] != 3 {
		t.Fatalf("type B counts = %#v", b)
	}
	if _, ok := b[pluginsdk.JobStatusPending]; ok {
		t.Fatalf("type B must omit zero pending: %#v", b)
	}
	empty, err := port.CountsByType(context.Background(), "integration.sync.acme.missing")
	if err != nil || len(empty) != 0 {
		t.Fatalf("missing type = (%v, %v), want empty map", empty, err)
	}
	for _, typ := range []string{"", "   "} {
		if _, err := port.CountsByType(context.Background(), typ); err == nil {
			t.Fatalf("CountsByType(%q): want empty/whitespace error", typ)
		}
	}
}

func TestAssertNeedsMet_CapJobs(t *testing.T) {
	app := &plugin.App{}
	app.RecordNeeds(plugin.CapJobs)
	if err := app.AssertNeedsMet(); err == nil {
		t.Fatal("expected error for unbound CapJobs")
	}
	port, err := pluginsdk.NewJobsPort(&stubJobsReader{}, &stubJobsQueue{})
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
	port, err := pluginsdk.NewJobsPort(&stubJobsReader{
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
	_, err = jp.CountsByType(context.Background(), "integration.sync.acme.demo")
	if err != nil {
		t.Fatalf("CountsByType via Host.Jobs: %v", err)
	}
}
