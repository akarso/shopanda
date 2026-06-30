package cli

import (
	"fmt"
	"sort"
	"strings"
)

// Registry holds plugin-registered CLI commands.
type Registry struct {
	commands map[string]Command
}

// NewRegistry creates an empty command registry.
func NewRegistry() *Registry {
	return &Registry{commands: make(map[string]Command)}
}

// Register adds a command. Panics on invalid or duplicate names.
func (r *Registry) Register(cmd Command) {
	if r == nil {
		panic("cli: registry must not be nil")
	}
	name := strings.TrimSpace(cmd.Name)
	if name == "" {
		panic("cli: command name must not be empty")
	}
	if strings.TrimSpace(cmd.Description) == "" {
		panic(fmt.Sprintf("cli: command %q description must not be empty", name))
	}
	if cmd.Run == nil {
		panic(fmt.Sprintf("cli: command %q run must not be nil", name))
	}
	if _, exists := r.commands[name]; exists {
		panic(fmt.Sprintf("cli: duplicate command name: %q", name))
	}
	cmd.Name = name
	cmd.Description = strings.TrimSpace(cmd.Description)
	r.commands[name] = cmd
}

// Get returns a registered command by name.
func (r *Registry) Get(name string) (Command, bool) {
	if r == nil {
		return Command{}, false
	}
	cmd, ok := r.commands[name]
	return cmd, ok
}

// List returns registered commands sorted by name.
func (r *Registry) List() []Command {
	if r == nil || len(r.commands) == 0 {
		return nil
	}
	out := make([]Command, 0, len(r.commands))
	for _, cmd := range r.commands {
		out = append(out, cmd)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// Run executes a registered command.
func (r *Registry) Run(name string, ctx Context, args []string) error {
	cmd, ok := r.Get(name)
	if !ok {
		return fmt.Errorf("cli: unknown command %q", name)
	}
	return cmd.Run(ctx, args)
}
