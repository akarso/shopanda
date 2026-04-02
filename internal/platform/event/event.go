package event

import (
	"time"

	"github.com/akarso/shopanda/internal/platform/id"
)

// Event is the standard envelope for all domain events.
type Event struct {
	ID        string
	Name      string
	Version   int
	Timestamp time.Time
	Source    string
	Data      interface{}
	Meta      map[string]string
}

// New creates an Event with a generated ID, version 1 and current UTC timestamp.
func New(name, source string, data interface{}) Event {
	return Event{
		ID:        id.New(),
		Name:      name,
		Version:   1,
		Timestamp: time.Now().UTC(),
		Source:    source,
		Data:      data,
		Meta:      nil,
	}
}

// WithMeta returns a copy of the event with the given metadata merged in.
func (e Event) WithMeta(key, value string) Event {
	if e.Meta == nil {
		e.Meta = make(map[string]string)
	}
	e.Meta[key] = value
	return e
}

// WithVersion returns a copy of the event with the given version.
func (e Event) WithVersion(v int) Event {
	e.Version = v
	return e
}
