package main

import (
	"errors"
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
	Force    bool // done/cancelled, or exhausted failed (API remains ungated)
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

// validateJobsRequeueStatus applies CLI-only preflight before Queue.Requeue.
// Queue.Requeue + WithRequeueTerminal does not enforce --force.
func validateJobsRequeueStatus(status domainjobs.Status, attempts, maxRetries int, terminal, force bool) error {
	if !terminal {
		if status != domainjobs.StatusProcessing {
			return fmt.Errorf("jobs:requeue: job is %s, not processing — use --terminal for failed (keeps attempts; --force for done/cancelled or exhausted) or jobs:retry for failed (resets attempts)", status)
		}
		return nil
	}
	if !domainjobs.IsTerminalRequeueStatus(status) {
		return fmt.Errorf("jobs:requeue: job is %s, not failed/done/cancelled — omit --terminal for stale processing claims (use jobs:retry to reset attempts on failed)", status)
	}
	if domainjobs.TerminalRequeueNeedsForce(status) && !force {
		return fmt.Errorf("jobs:requeue: job is %s — re-pending done/cancelled can double-run side effects; pass --force with --terminal (Queue.Requeue API is ungated; CLI only)", status)
	}
	exhausted := maxRetries > 0 && attempts >= maxRetries
	if status == domainjobs.StatusFailed && exhausted && !force {
		return fmt.Errorf("jobs:requeue: attempts=%d >= max_retries=%d — next Fail is permanent; use jobs:retry for a fresh attempt budget, or pass --force to re-pend keeping attempts", attempts, maxRetries)
	}
	if force && status == domainjobs.StatusFailed && !exhausted {
		return fmt.Errorf("jobs:requeue: --force is not needed for non-exhausted failed (omit --force, or use jobs:retry to reset attempts)")
	}
	return nil
}

// effectiveJobsRequeueForce is what we audit: only true when force was
// required for this status/attempts snapshot.
func effectiveJobsRequeueForce(status domainjobs.Status, attempts, maxRetries int, force bool) bool {
	if !force {
		return false
	}
	if domainjobs.TerminalRequeueNeedsForce(status) {
		return true
	}
	return status == domainjobs.StatusFailed && maxRetries > 0 && attempts >= maxRetries
}

func jobsRequeueAuditDetails(prior domainjobs.Status, attempts, maxRetries int, terminal, force bool, minAge time.Duration) map[string]interface{} {
	d := map[string]interface{}{
		"terminal":     terminal,
		"force":        effectiveJobsRequeueForce(prior, attempts, maxRetries, force),
		"prior_status": string(prior),
		"attempts":     attempts,
		"max_retries":  maxRetries,
		"snapshot":     "pre_lock_get",
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
			msg += fmt.Sprintf("; WARNING: attempts>=max_retries (%d) — next Fail is permanent (jobs:retry resets attempts)", maxRetries)
		}
		if domainjobs.TerminalRequeueNeedsForce(prior) {
			msg += "; WARNING: resurrected terminal success/cancel — handlers must be idempotent"
		}
		return msg + ".\n"
	}
	return fmt.Sprintf("Job %s requeued (processing → pending, attempts=%d kept; min-age=%s).\n", id, attempts, minAge)
}

func formatJobsRequeueError(requeueErr error, minAge time.Duration) error {
	switch {
	case errors.Is(requeueErr, domainjobs.ErrRequeueTooFresh):
		return fmt.Errorf("jobs:requeue: claim younger than --min-age=%s (still a plausible live handler): %w", minAge, requeueErr)
	case errors.Is(requeueErr, domainjobs.ErrRequeueInProgress):
		return fmt.Errorf("jobs:requeue: another reclaim is in progress (wait for it to finish or for the default %s lease to expire): %w", domainjobs.DefaultRequeueMinAge, requeueErr)
	case errors.Is(requeueErr, domainjobs.ErrRequeueWrongAttempt):
		return fmt.Errorf("jobs:requeue: attempt mismatch (claim moved under TOCTOU): %w", requeueErr)
	case errors.Is(requeueErr, domainjobs.ErrRequeueNotFound):
		return fmt.Errorf("jobs:requeue: job disappeared (deleted under TOCTOU): %w", requeueErr)
	case errors.Is(requeueErr, domainjobs.ErrRequeueNotProcessing):
		return fmt.Errorf("jobs:requeue: status no longer processing (moved under TOCTOU): %w", requeueErr)
	case errors.Is(requeueErr, domainjobs.ErrRequeueNotTerminal):
		return fmt.Errorf("jobs:requeue: status no longer terminal (moved under TOCTOU): %w", requeueErr)
	case errors.Is(requeueErr, domainjobs.ErrRequeueConflict):
		return fmt.Errorf("jobs:requeue: concurrent update lost the reclaim race: %w", requeueErr)
	case errors.Is(requeueErr, domainjobs.ErrRequeueUnsupported):
		return fmt.Errorf("jobs:requeue: unsupported for this queue driver: %w", requeueErr)
	default:
		return fmt.Errorf("jobs:requeue: %w", requeueErr)
	}
}
