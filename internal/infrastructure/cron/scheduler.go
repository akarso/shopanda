package cron

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// Compile-time checks.
var (
	_ scheduler.Scheduler    = (*Scheduler)(nil)
	_ scheduler.LocalTrigger = (*Scheduler)(nil)
)

// Logger is the logging interface used by the scheduler.
type Logger interface {
	Info(msg string, fields map[string]interface{})
	Error(msg string, err error, fields map[string]interface{})
}

// entry is a registered scheduled task.
type entry struct {
	name string
	spec string
	expr *cronExpr
	fn   func()
}

// Scheduler is an in-process cron scheduler that evaluates tasks every minute.
type Scheduler struct {
	entries  []entry
	log      Logger
	store    scheduler.Store
	stop     chan struct{}
	stopOnce sync.Once
	stopMu   sync.RWMutex // guards stopped, closing the wg.Add/Wait race (see tryAdd)
	stopped  bool
	wg       sync.WaitGroup
}

// isEnabledTimeout bounds each tick's override-check round-trip. The tick
// loop is single-threaded (one Start goroutine evaluates every entry in
// sequence), so a hung Store call would otherwise block all scheduled
// tasks — including ones with no override at all — indefinitely.
const isEnabledTimeout = 500 * time.Millisecond

// startRegistrationUpsertTimeout bounds Start's one-time
// UpsertRegistrations call. The alignment timer and tick loop below don't
// even begin until this call returns, so a hung/unreachable Postgres would
// otherwise delay this process's actual scheduling indefinitely — the
// same reasoning as isEnabledTimeout, just a longer allowance since this
// happens once at startup rather than every minute. A var, not a const,
// so tests can shrink it instead of waiting out the real duration.
var startRegistrationUpsertTimeout = 2 * time.Second

// Option configures optional Scheduler dependencies.
type Option func(*Scheduler)

// WithStore attaches a Store for cross-process admin introspection and
// control (PR-1030) — registrations are upserted once at Start, and every
// tick checks the store for a per-task disable override before firing.
// Optional; omitting it preserves pre-PR-1030 behavior exactly (always
// enabled, not introspectable from another process).
func WithStore(store scheduler.Store) Option {
	return func(s *Scheduler) { s.store = store }
}

// New creates a Scheduler.
func New(log Logger, opts ...Option) *Scheduler {
	s := &Scheduler{
		log:  log,
		stop: make(chan struct{}),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Register adds a named task with a 5-field cron spec.
// Panics if the spec is invalid or name is empty. Must be called before Start.
func (s *Scheduler) Register(name string, spec string, fn func()) {
	if name == "" {
		panic("cron: task name must not be empty")
	}
	if fn == nil {
		panic("cron: task function must not be nil")
	}
	for _, e := range s.entries {
		if e.name == name {
			panic("cron: duplicate task name: " + name)
		}
	}
	expr, err := parse(spec)
	if err != nil {
		panic(fmt.Sprintf("cron: invalid spec %q for task %q: %v", spec, name, err))
	}
	s.entries = append(s.entries, entry{name: name, spec: spec, expr: expr, fn: fn})
}

// Start evaluates registered schedules every minute. Blocks until ctx is
// cancelled or Stop is called.
func (s *Scheduler) Start(ctx context.Context) {
	s.log.Info("scheduler.started", map[string]interface{}{
		"tasks": len(s.entries),
	})

	if s.store != nil {
		regs := make([]scheduler.RegistrationEntry, len(s.entries))
		for i, e := range s.entries {
			regs[i] = scheduler.RegistrationEntry{Name: e.name, Spec: e.spec}
		}
		// Best-effort: a registration-persistence hiccup must not block
		// this process's actual scheduling — admin introspection just
		// shows stale data until the next successful Start. Bounded (and
		// derived from ctx, so an early shutdown cancels it immediately
		// too) so a hung Postgres call can't delay Start indefinitely.
		upsertCtx, cancel := context.WithTimeout(ctx, startRegistrationUpsertTimeout)
		err := s.store.UpsertRegistrations(upsertCtx, regs)
		cancel()
		if err != nil {
			s.log.Error("scheduler.registrations.upsert_failed", err, nil)
		}
	}

	// Align to the start of the next minute.
	now := time.Now()
	next := now.Truncate(time.Minute).Add(time.Minute)
	alignTimer := time.NewTimer(time.Until(next))
	defer alignTimer.Stop()

	select {
	case <-ctx.Done():
		s.log.Info("scheduler.stopped", map[string]interface{}{"reason": "context"})
		return
	case <-s.stop:
		s.log.Info("scheduler.stopped", map[string]interface{}{"reason": "stop"})
		return
	case <-alignTimer.C:
	}

	s.tick(time.Now())

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.log.Info("scheduler.stopped", map[string]interface{}{"reason": "context"})
			return
		case <-s.stop:
			s.log.Info("scheduler.stopped", map[string]interface{}{"reason": "stop"})
			return
		case t := <-ticker.C:
			s.tick(t)
		}
	}
}

// Stop signals the scheduler to shut down and waits for in-flight tasks.
// Safe to call multiple times.
func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() {
		// Mark stopped before closing stop/waiting: tryAdd checks this
		// under the same mutex, so any wg.Add that hasn't already
		// happened by the time this Lock is acquired will never happen —
		// closing the window where Add could race with the Wait below
		// (sync.WaitGroup panics on "Add called concurrently with Wait").
		s.stopMu.Lock()
		s.stopped = true
		s.stopMu.Unlock()
		close(s.stop)
	})
	s.wg.Wait()
}

// tryAdd increments wg only if the scheduler hasn't been stopped yet. See
// the stopMu field comment for why this is required to avoid a
// wg.Add/wg.Wait race between a task firing (tick or TriggerLocal) and
// Stop().
func (s *Scheduler) tryAdd() bool {
	s.stopMu.RLock()
	defer s.stopMu.RUnlock()
	if s.stopped {
		return false
	}
	s.wg.Add(1)
	return true
}

func (s *Scheduler) tick(t time.Time) {
	var matched []entry
	for _, e := range s.entries {
		if e.expr.matches(t) {
			matched = append(matched, e)
		}
	}
	if len(matched) == 0 {
		return
	}

	enabled := map[string]bool{}
	if s.store != nil {
		names := make([]string, len(matched))
		for i, e := range matched {
			names[i] = e.name
		}
		ctx, cancel := context.WithTimeout(context.Background(), isEnabledTimeout)
		result, err := s.store.IsEnabledBatch(ctx, names)
		cancel()
		if err != nil {
			// Fail open: a transient override-check failure (including a
			// timeout) must not silently stop scheduled work (e.g.
			// reservation-expiry cleanup) — that failure mode is worse
			// than occasionally firing a task an admin meant to keep
			// disabled. Leaving `enabled` empty means every matched task
			// is treated as enabled below.
			s.log.Error("scheduler.override.check_failed", err, map[string]interface{}{
				"tasks": names,
			})
		} else {
			enabled = result
		}
	}

	for _, e := range matched {
		if v, ok := enabled[e.name]; ok && !v {
			s.log.Info("scheduler.task.skipped_disabled", map[string]interface{}{
				"task": e.name,
			})
			continue
		}
		if !s.tryAdd() {
			s.log.Info("scheduler.task.skipped_stopping", map[string]interface{}{
				"task": e.name,
			})
			continue
		}
		s.log.Info("scheduler.task.fire", map[string]interface{}{
			"task": e.name,
		})
		go func(e entry) {
			defer s.wg.Done()
			s.run(e)
		}(e)
	}
}

// TriggerLocal invokes a registered task's fn immediately, out-of-band from
// the normal tick — the same function, so a manual trigger and a real tick
// firing are indistinguishable to the task itself. Fires regardless of a
// disabled override (see domain/scheduler.Catalog.Trigger's doc comment).
func (s *Scheduler) TriggerLocal(name string) error {
	for _, e := range s.entries {
		if e.name == name {
			if !s.tryAdd() {
				return apperror.Conflict("scheduler is shutting down; task not triggered")
			}
			go func(e entry) {
				defer s.wg.Done()
				s.run(e)
			}(e)
			return nil
		}
	}
	return apperror.NotFound(fmt.Sprintf("no scheduled task named %q", name))
}

func (s *Scheduler) run(e entry) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("scheduler.task.panic", fmt.Errorf("%v", r), map[string]interface{}{
				"task": e.name,
			})
		}
	}()
	e.fn()
}

// --- Cron expression parsing ---

// cronExpr holds the parsed sets for each of the 5 cron fields.
type cronExpr struct {
	minute     set
	hour       set
	dayOfMonth set
	month      set
	dayOfWeek  set
	domWild    bool // true when day-of-month was "*"
	dowWild    bool // true when day-of-week was "*"
}

// matches returns true if the time t satisfies the cron expression.
// DOM and DOW follow Vixie cron semantics: when both are restricted (non-*),
// the day matches if either DOM or DOW matches.
func (c *cronExpr) matches(t time.Time) bool {
	if !c.minute.contains(t.Minute()) || !c.hour.contains(t.Hour()) || !c.month.contains(int(t.Month())) {
		return false
	}
	domMatch := c.dayOfMonth.contains(t.Day())
	dowMatch := c.dayOfWeek.contains(int(t.Weekday()))
	if !c.domWild && !c.dowWild {
		// Both restricted: OR (Vixie cron).
		return domMatch || dowMatch
	}
	return domMatch && dowMatch
}

// next returns the earliest minute-resolution time strictly after `after`
// at which the expression matches — the same resolution the scheduler's
// own tick loop uses. Bounded to avoid an infinite loop for a
// syntactically valid but practically impossible spec; returns the zero
// Time if no match is found within the bound.
func (c *cronExpr) next(after time.Time) time.Time {
	t := after.Truncate(time.Minute).Add(time.Minute)
	const maxLookahead = 4 * 366 * 24 * 60 // ~4 years of minutes
	for i := 0; i < maxLookahead; i++ {
		if c.matches(t) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}
}

// NextRun returns the earliest minute-resolution time strictly after
// `after` at which spec matches, or the zero Time if none is found within
// a bounded lookahead. Exported for admin catalog display (see
// domain/scheduler.Catalog) — spec is parsed fresh on every call,
// independent of any running Scheduler instance.
func NextRun(spec string, after time.Time) (time.Time, error) {
	expr, err := parse(spec)
	if err != nil {
		return time.Time{}, err
	}
	return expr.next(after), nil
}

// set is a sorted slice of allowed values for a cron field.
type set []int

func (s set) contains(v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// parse parses a standard 5-field cron expression.
// Supports: * (wildcard), */n (step), n-m (range), n,m (list), n-m/s (range with step).
func parse(spec string) (*cronExpr, error) {
	fields := strings.Fields(spec)
	if len(fields) != 5 {
		return nil, fmt.Errorf("expected 5 fields, got %d", len(fields))
	}

	minute, err := parseField(fields[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("minute: %w", err)
	}
	hour, err := parseField(fields[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("hour: %w", err)
	}
	dom, err := parseField(fields[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("day-of-month: %w", err)
	}
	month, err := parseField(fields[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("month: %w", err)
	}
	dow, err := parseField(fields[4], 0, 6)
	if err != nil {
		return nil, fmt.Errorf("day-of-week: %w", err)
	}

	return &cronExpr{
		minute:     minute,
		hour:       hour,
		dayOfMonth: dom,
		month:      month,
		dayOfWeek:  dow,
		domWild:    fields[2] == "*",
		dowWild:    fields[4] == "*",
	}, nil
}

// parseField parses a single cron field into a set of allowed values.
func parseField(field string, min, max int) (set, error) {
	var result set

	for _, part := range strings.Split(field, ",") {
		vals, err := parsePart(part, min, max)
		if err != nil {
			return nil, err
		}
		result = append(result, vals...)
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("empty field")
	}
	return result, nil
}

func parsePart(part string, min, max int) (set, error) {
	// Handle step: */n or n-m/s
	var step int
	if idx := strings.Index(part, "/"); idx != -1 {
		s, err := strconv.Atoi(part[idx+1:])
		if err != nil || s <= 0 {
			return nil, fmt.Errorf("invalid step in %q", part)
		}
		step = s
		part = part[:idx]
	}

	var lo, hi int

	switch {
	case part == "*":
		lo, hi = min, max
	case strings.Contains(part, "-"):
		rangeParts := strings.SplitN(part, "-", 2)
		var err error
		lo, err = strconv.Atoi(rangeParts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid range start in %q", part)
		}
		hi, err = strconv.Atoi(rangeParts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid range end in %q", part)
		}
	default:
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid value %q", part)
		}
		if n < min || n > max {
			return nil, fmt.Errorf("value %d out of range [%d, %d]", n, min, max)
		}
		if step > 0 {
			// Single value with step doesn't make sense; treat as starting point through max
			lo, hi = n, max
		} else {
			return set{n}, nil
		}
	}

	if lo < min || hi > max || lo > hi {
		return nil, fmt.Errorf("range %d-%d out of bounds [%d, %d]", lo, hi, min, max)
	}

	if step == 0 {
		step = 1
	}

	var result set
	for v := lo; v <= hi; v += step {
		result = append(result, v)
	}
	return result, nil
}
