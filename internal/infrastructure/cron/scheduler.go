package cron

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/akarso/shopanda/internal/domain/scheduler"
)

// Compile-time check.
var _ scheduler.Scheduler = (*Scheduler)(nil)

// Logger is the logging interface used by the scheduler.
type Logger interface {
	Info(msg string, fields map[string]interface{})
	Error(msg string, err error, fields map[string]interface{})
}

// entry is a registered scheduled task.
type entry struct {
	name string
	expr *cronExpr
	fn   func()
}

// Scheduler is an in-process cron scheduler that evaluates tasks every minute.
type Scheduler struct {
	entries  []entry
	log      Logger
	stop     chan struct{}
	stopOnce sync.Once
}

// New creates a Scheduler.
func New(log Logger) *Scheduler {
	return &Scheduler{
		log:  log,
		stop: make(chan struct{}),
	}
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
	expr, err := parse(spec)
	if err != nil {
		panic(fmt.Sprintf("cron: invalid spec %q for task %q: %v", spec, name, err))
	}
	s.entries = append(s.entries, entry{name: name, expr: expr, fn: fn})
}

// Start evaluates registered schedules every minute. Blocks until ctx is
// cancelled or Stop is called.
func (s *Scheduler) Start(ctx context.Context) {
	s.log.Info("scheduler.started", map[string]interface{}{
		"tasks": len(s.entries),
	})

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

// Stop signals the scheduler to shut down. Safe to call multiple times.
func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() { close(s.stop) })
}

func (s *Scheduler) tick(t time.Time) {
	for _, e := range s.entries {
		if e.expr.matches(t) {
			s.log.Info("scheduler.task.fire", map[string]interface{}{
				"task": e.name,
			})
			s.run(e)
		}
	}
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
}

// matches returns true if the time t satisfies all five fields.
func (c *cronExpr) matches(t time.Time) bool {
	return c.minute.contains(t.Minute()) &&
		c.hour.contains(t.Hour()) &&
		c.dayOfMonth.contains(t.Day()) &&
		c.month.contains(int(t.Month())) &&
		c.dayOfWeek.contains(int(t.Weekday()))
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
