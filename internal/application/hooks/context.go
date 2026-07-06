package hooks

import (
	"fmt"
	"regexp"
	"strings"
)

// Handler mutates hook context during synchronous chain execution.
type Handler func(ctx *Context) error

// Context carries mutable hook payload across handlers in a chain.
type Context struct {
	Name    string
	Payload map[string]interface{}
}

// NewContext creates a hook context for name.
func NewContext(name string) *Context {
	return &Context{
		Name:    strings.TrimSpace(name),
		Payload: make(map[string]interface{}),
	}
}

// Get returns a payload value.
func (c *Context) Get(key string) (interface{}, bool) {
	if c == nil || c.Payload == nil {
		return nil, false
	}
	v, ok := c.Payload[key]
	return v, ok
}

// Set stores a payload value.
func (c *Context) Set(key string, value interface{}) {
	if c == nil {
		return
	}
	if c.Payload == nil {
		c.Payload = make(map[string]interface{})
	}
	c.Payload[key] = value
}

const (
	// HookCartAddItemAfter runs after a cart line is persisted.
	HookCartAddItemAfter = "cart.add_item.after"
)

var hookNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// ValidateHookName checks hook naming conventions.
func ValidateHookName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("hook name must not be empty")
	}
	if !hookNamePattern.MatchString(name) {
		return fmt.Errorf("hook name %q must be dot-separated lowercase segments (e.g. cart.add_item.after)", name)
	}
	return nil
}
