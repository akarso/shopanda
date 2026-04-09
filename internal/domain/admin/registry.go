package admin

import (
	"fmt"
	"sync"
)

// Registry holds all registered admin form and grid schemas.
type Registry struct {
	mu    sync.RWMutex
	forms map[string]*Form
	grids map[string]*Grid
}

// NewRegistry creates an empty schema registry.
func NewRegistry() *Registry {
	return &Registry{
		forms: make(map[string]*Form),
		grids: make(map[string]*Grid),
	}
}

// RegisterForm registers a form schema under the given name.
// If a form with the same name already exists it is replaced.
func (r *Registry) RegisterForm(name string, form Form) {
	form.Name = name
	cp := cloneForm(form)
	r.mu.Lock()
	r.forms[name] = &cp
	r.mu.Unlock()
}

// RegisterFormField appends a field to an existing form.
// Returns an error if the form has not been registered.
func (r *Registry) RegisterFormField(formName string, field Field) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.forms[formName]
	if !ok {
		return fmt.Errorf("admin: form %q not registered", formName)
	}
	f.Fields = append(f.Fields, cloneField(field))
	return nil
}

// RegisterGrid registers a grid schema under the given name.
// If a grid with the same name already exists it is replaced.
func (r *Registry) RegisterGrid(name string, grid Grid) {
	grid.Name = name
	cp := cloneGrid(grid)
	r.mu.Lock()
	r.grids[name] = &cp
	r.mu.Unlock()
}

// RegisterGridColumn appends a column to an existing grid.
// Returns an error if the grid has not been registered.
func (r *Registry) RegisterGridColumn(gridName string, column Column) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, ok := r.grids[gridName]
	if !ok {
		return fmt.Errorf("admin: grid %q not registered", gridName)
	}
	g.Columns = append(g.Columns, cloneColumn(column))
	return nil
}

// RegisterAction appends an action to an existing grid.
// Returns an error if the grid has not been registered.
func (r *Registry) RegisterAction(gridName string, action Action) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, ok := r.grids[gridName]
	if !ok {
		return fmt.Errorf("admin: grid %q not registered", gridName)
	}
	g.Actions = append(g.Actions, action)
	return nil
}

// Form returns the form registered under name and true,
// or a zero Form and false if not found.
func (r *Registry) Form(name string) (Form, bool) {
	r.mu.RLock()
	f, ok := r.forms[name]
	if !ok {
		r.mu.RUnlock()
		return Form{}, false
	}
	cp := cloneForm(*f)
	r.mu.RUnlock()
	return cp, true
}

// Grid returns the grid registered under name and true,
// or a zero Grid and false if not found.
func (r *Registry) Grid(name string) (Grid, bool) {
	r.mu.RLock()
	g, ok := r.grids[name]
	if !ok {
		r.mu.RUnlock()
		return Grid{}, false
	}
	cp := cloneGrid(*g)
	r.mu.RUnlock()
	return cp, true
}

// --- deep-copy helpers ---

func cloneField(f Field) Field {
	if f.Options != nil {
		opts := make([]Option, len(f.Options))
		copy(opts, f.Options)
		f.Options = opts
	}
	if f.Meta != nil {
		m := make(map[string]interface{}, len(f.Meta))
		for k, v := range f.Meta {
			m[k] = v
		}
		f.Meta = m
	}
	return f
}

func cloneColumn(c Column) Column {
	if c.Meta != nil {
		m := make(map[string]interface{}, len(c.Meta))
		for k, v := range c.Meta {
			m[k] = v
		}
		c.Meta = m
	}
	return c
}

func cloneForm(f Form) Form {
	if f.Fields != nil {
		fields := make([]Field, len(f.Fields))
		for i, fld := range f.Fields {
			fields[i] = cloneField(fld)
		}
		f.Fields = fields
	}
	return f
}

func cloneGrid(g Grid) Grid {
	if g.Columns != nil {
		cols := make([]Column, len(g.Columns))
		for i, c := range g.Columns {
			cols[i] = cloneColumn(c)
		}
		g.Columns = cols
	}
	if g.Actions != nil {
		acts := make([]Action, len(g.Actions))
		copy(acts, g.Actions)
		g.Actions = acts
	}
	return g
}
