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
	Force    bool // accept risk: done/cancelled, or exhausted failed; no-op otherwise
}

func parseJobsRequeueArgs(args []string) (jobsRequeueArgs, error) {
	out := jobsRequeueArgs{MinAge: domainjobs.DefaultRequeueMinAge}
	usage := "usage: app jobs:requeue <id> [--min-age=5m|--terminal --force]  (keep-attempts terminal: --terminal --force; bare --terminal rare)"
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

// terminalAttemptsExhausted matches Fail's permanent-fail predicate
// (attempts >= max_retries), including max_retries == 0.
func terminalAttemptsExhausted(attempts, maxRetries int) bool {
	return attempts >= maxRetries
}

// terminalRequeueForceRequired is the CLI-only risk gate (API remains ungated).
func terminalRequeueForceRequired(status domainjobs.Status, attempts, maxRetries int) bool {
	if domainjobs.TerminalRequeueNeedsForce(status) {
		return true
	}
	return status == domainjobs.StatusFailed && terminalAttemptsExhausted(attempts, maxRetries)
}

// validateJobsRequeueStatus applies CLI-only preflight before Queue.Requeue.
// Production failed rows are exhausted by Fail (attempts >= max_retries), so
// the common ops paths are jobs:retry or --terminal --force. Bare --terminal
// without --force only fits anomalous non-exhausted failed. --force is a
// no-op when not required. Queue.Requeue does not enforce --force.
func validateJobsRequeueStatus(status domainjobs.Status, attempts, maxRetries int, terminal, force bool) error {
	if !terminal {
		if status != domainjobs.StatusProcessing {
			switch status {
			case domainjobs.StatusFailed:
				return fmt.Errorf("jobs:requeue: job is failed, not processing — use jobs:retry (reset attempts) or --terminal --force (keep attempts; production failed is usually exhausted)")
			case domainjobs.StatusDone, domainjobs.StatusCancelled:
				return fmt.Errorf("jobs:requeue: job is %s, not processing — use --terminal --force to re-pend keeping attempts (duplicate side-effect risk)", status)
			default:
				return fmt.Errorf("jobs:requeue: job is %s, not processing — omit flags and reclaim only a stale processing claim (--min-age)", status)
			}
		}
		return nil
	}
	if !domainjobs.IsTerminalRequeueStatus(status) {
		return fmt.Errorf("jobs:requeue: job is %s, not failed/done/cancelled — omit --terminal for stale processing claims", status)
	}
	if terminalRequeueForceRequired(status, attempts, maxRetries) && !force {
		if domainjobs.TerminalRequeueNeedsForce(status) {
			return fmt.Errorf("jobs:requeue: job is %s — re-pending done/cancelled can double-run side effects; pass --terminal --force (Queue.Requeue API is ungated; CLI only)", status)
		}
		return fmt.Errorf("jobs:requeue: attempts=%d >= max_retries=%d (normal after Fail) — use jobs:retry for a fresh attempt budget, or --terminal --force to re-pend keeping attempts", attempts, maxRetries)
	}
	return nil
}

// jobsRequeueAuditDetails builds a single audit schema for every requeue path
// (including early Get failures). force_flag is what the operator passed;
// force_required is whether this snapshot needed --force. Unknown status
// (pre-Get) uses snapshot=pre_get and leaves force_required false.
func jobsRequeueAuditDetails(prior domainjobs.Status, attempts, maxRetries int, terminal, forceFlag bool, minAge time.Duration, knownStatus bool) map[string]interface{} {
	d := map[string]interface{}{
		"terminal":       terminal,
		"force_flag":     forceFlag,
		"force_required": false,
	}
	if knownStatus {
		d["snapshot"] = "pre_lock_get"
		d["prior_status"] = string(prior)
		d["attempts"] = attempts
		d["max_retries"] = maxRetries
		if terminal {
			d["force_required"] = terminalRequeueForceRequired(prior, attempts, maxRetries)
		}
	} else {
		d["snapshot"] = "pre_get"
	}
	if !terminal {
		d["min_age"] = minAge.String()
	}
	return d
}

func jobsRequeueSuccessMessage(id string, prior domainjobs.Status, attempts, maxRetries int, terminal bool, minAge time.Duration) string {
	if terminal {
		msg := fmt.Sprintf("Job %s requeued (%s → pending, attempts=%d kept; terminal)", id, prior, attempts)
		if terminalAttemptsExhausted(attempts, maxRetries) && prior == domainjobs.StatusFailed {
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
		// Get miss / early UUID reject use "no job with id"; this path is
		// Requeue after a successful Get (TOCTOU delete) or a direct Queue call.
		return fmt.Errorf("jobs:requeue: job disappeared (missing or deleted under TOCTOU): %w", requeueErr)
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
