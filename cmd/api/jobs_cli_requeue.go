package main

import (
	"fmt"
	"strings"
	"time"

	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
)

// jobsRequeueArgs is the parsed jobs:requeue CLI surface.
type jobsRequeueArgs struct {
	ID       string
	MinAge   time.Duration
	Terminal bool
	Force    bool // with --terminal: allow done/cancelled (failed needs no force)
}

func parseJobsRequeueArgs(args []string) (jobsRequeueArgs, error) {
	out := jobsRequeueArgs{MinAge: domainjobs.DefaultRequeueMinAge}
	usage := "usage: app jobs:requeue <id> [--min-age=5m|--terminal [--force]]"
	var positional []string
	var sawMinAge bool
	for _, arg := range args {
		switch {
		case arg == "--terminal":
			out.Terminal = true
		case arg == "--force":
			out.Force = true
		case strings.HasPrefix(arg, "--min-age="):
			raw := strings.TrimPrefix(arg, "--min-age=")
			d, perr := time.ParseDuration(raw)
			if perr != nil || d < time.Second {
				return jobsRequeueArgs{}, fmt.Errorf("%s: invalid --min-age %q (minimum 1s)", usage, raw)
			}
			out.MinAge = d
			sawMinAge = true
		case arg == "--min-age":
			return jobsRequeueArgs{}, fmt.Errorf("%s", usage)
		case strings.HasPrefix(arg, "--"):
			return jobsRequeueArgs{}, fmt.Errorf("%s: unrecognized option %q", usage, arg)
		default:
			positional = append(positional, arg)
		}
	}
	if out.Force && !out.Terminal {
		return jobsRequeueArgs{}, fmt.Errorf("%s: --force requires --terminal", usage)
	}
	if out.Terminal && sawMinAge {
		return jobsRequeueArgs{}, fmt.Errorf("%s: --terminal ignores --min-age (do not combine)", usage)
	}
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return jobsRequeueArgs{}, fmt.Errorf("%s", usage)
	}
	out.ID = positional[0]
	return out, nil
}

// validateJobsRequeueStatus applies CLI preflight before Queue.Requeue.
// --terminal without --force accepts failed only; --force also allows
// done/cancelled (duplicate-execution risk).
func validateJobsRequeueStatus(status domainjobs.Status, terminal, force bool) error {
	if terminal {
		switch status {
		case domainjobs.StatusFailed:
			return nil
		case domainjobs.StatusDone, domainjobs.StatusCancelled:
			if force {
				return nil
			}
			return fmt.Errorf("jobs:requeue: job is %s — re-pending done/cancelled can double-run side effects; pass --force with --terminal (or omit --terminal for stale processing)", status)
		default:
			return fmt.Errorf("jobs:requeue: job is %s, not failed — omit --terminal for stale processing claims (use jobs:retry to reset attempts on failed)", status)
		}
	}
	if status != domainjobs.StatusProcessing {
		return fmt.Errorf("jobs:requeue: job is %s, not processing — use --terminal for failed (keeps attempts; --force for done/cancelled) or jobs:retry for failed (resets attempts)", status)
	}
	return nil
}

func jobsRequeueAuditDetails(prior domainjobs.Status, attempts, maxRetries int, terminal, force bool, minAge time.Duration) map[string]interface{} {
	d := map[string]interface{}{
		"terminal":     terminal,
		"force":        force,
		"prior_status": string(prior),
		"attempts":     attempts,
		"max_retries":  maxRetries,
	}
	if !terminal {
		d["min_age"] = minAge.String()
	}
	return d
}

func jobsRequeueSuccessMessage(id string, prior domainjobs.Status, attempts, maxRetries int, terminal bool, minAge time.Duration) string {
	if terminal {
		msg := fmt.Sprintf("Job %s requeued (%s → pending, attempts=%d kept; terminal)", id, prior, attempts)
		if attempts >= maxRetries && maxRetries > 0 {
			msg += fmt.Sprintf("; WARNING: attempts>=max_retries (%d) — next Fail is permanent", maxRetries)
		}
		if prior == domainjobs.StatusDone || prior == domainjobs.StatusCancelled {
			msg += "; WARNING: resurrected terminal success/cancel — handlers must be idempotent"
		}
		return msg + ".\n"
	}
	return fmt.Sprintf("Job %s requeued (processing → pending, attempts=%d kept; min-age=%s).\n", id, attempts, minAge)
}
