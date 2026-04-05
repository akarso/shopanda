package plugin

import (
	"fmt"

	"github.com/akarso/shopanda/internal/platform/logger"
)

// State represents the lifecycle state of a plugin.
type State string

const (
	StateLoaded      State = "loaded"
	StateInitialized State = "initialized"
	StateActive      State = "active"
	StateFailed      State = "failed"
)

// Entry tracks a registered plugin and its lifecycle state.
type Entry struct {
	Plugin Plugin
	State  State
	Err    error // non-nil when State == StateFailed
}

// Registry manages plugin loading and initialization.
type Registry struct {
	entries []Entry
	log     logger.Logger
}

// NewRegistry creates a Registry.
func NewRegistry(log logger.Logger) *Registry {
	if log == nil {
		panic("plugin: logger must not be nil")
	}
	return &Registry{log: log}
}

// Register adds a plugin to the registry in the loaded state.
// Panics if the plugin is nil or has an empty name.
// Panics if a plugin with the same name is already registered.
func (r *Registry) Register(p Plugin) {
	if p == nil {
		panic("plugin: plugin must not be nil")
	}
	name := p.Name()
	if name == "" {
		panic("plugin: plugin name must not be empty")
	}
	for _, e := range r.entries {
		if e.Plugin.Name() == name {
			panic(fmt.Sprintf("plugin: duplicate plugin name: %q", name))
		}
	}
	r.entries = append(r.entries, Entry{
		Plugin: p,
		State:  StateLoaded,
	})
	r.log.Info("plugin.registered", map[string]interface{}{
		"plugin": name,
	})
}

// InitAll initializes all loaded plugins by calling Init(app).
// Plugins that fail initialization are marked as failed and skipped.
// Returns the count of successfully initialized plugins.
func (r *Registry) InitAll(app *App) int {
	if app == nil {
		panic("plugin: app must not be nil")
	}
	initialized := 0
	for i := range r.entries {
		e := &r.entries[i]
		if e.State != StateLoaded {
			continue
		}
		name := e.Plugin.Name()
		r.log.Info("plugin.init.start", map[string]interface{}{
			"plugin": name,
		})
		if err := e.Plugin.Init(app); err != nil {
			e.State = StateFailed
			e.Err = err
			r.log.Error("plugin.init.failed", err, map[string]interface{}{
				"plugin": name,
			})
			continue
		}
		e.State = StateActive
		initialized++
		r.log.Info("plugin.init.complete", map[string]interface{}{
			"plugin": name,
		})
	}
	return initialized
}

// Entries returns a copy of all plugin entries.
func (r *Registry) Entries() []Entry {
	cp := make([]Entry, len(r.entries))
	copy(cp, r.entries)
	return cp
}

// Len returns the number of registered plugins.
func (r *Registry) Len() int {
	return len(r.entries)
}
