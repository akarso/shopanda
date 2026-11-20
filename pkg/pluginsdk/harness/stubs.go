package harness

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// harnessJobsStub is the default JobsPort when Satisfy includes CapJobs
// without Options.JobsPort — Host.Jobs() returns a real port (miss → nil, nil).
type harnessJobsStub struct{}

func (harnessJobsStub) Get(context.Context, string) (*pluginsdk.JobDetail, error) {
	return nil, nil
}
func (harnessJobsStub) CountsByStatus(context.Context) (map[pluginsdk.JobStatus]int, error) {
	return map[pluginsdk.JobStatus]int{}, nil
}
func (harnessJobsStub) CountsByType(_ context.Context, jobType string) (map[pluginsdk.JobStatus]int, error) {
	if strings.TrimSpace(jobType) == "" {
		return nil, fmt.Errorf("harness: CountsByType: empty job type")
	}
	return map[pluginsdk.JobStatus]int{}, nil
}
func (harnessJobsStub) Requeue(context.Context, string, int, ...pluginsdk.RequeueOption) error {
	return nil
}

var _ pluginsdk.JobsPort = harnessJobsStub{}

type harnessMediaStub struct{}

func (harnessMediaStub) Name() string                 { return "harness-stub" }
func (harnessMediaStub) Save(string, io.Reader) error { return nil }
func (harnessMediaStub) Delete(string) error          { return nil }
func (harnessMediaStub) URL(path string) string       { return "harness:///" + path }

var _ media.Storage = harnessMediaStub{}
