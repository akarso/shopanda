package admin

import "fmt"

// Registry holds all registered admin form and grid schemas.
type Registry struct {
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
	r.forms[name] = &form
}

// RegisterFormField appends a field to an existing form.
// Returns an error if the form has not been registered.
func (r *Registry) RegisterFormField(formName string, field Field) error {
	f, ok := r.forms[formName]
	if !ok {
		return fmt.Errorf("admin: form %q not registered", formName)
	}
	f.Fields = append(f.Fields, field)
	return nil
}

// RegisterGrid registers a grid schema under the given name.
// If a grid with the same name already exists it is replaced.
func (r *Registry) RegisterGrid(name string, grid Grid) {
	grid.Name = name
	r.grids[name] = &grid
}

// RegisterGridColumn appends a column to an existing grid.
// Returns an error if the grid has not been registered.
func (r *Registry) RegisterGridColumn(gridName string, column Column) error {
	g, ok := r.grids[gridName]
	if !ok {
		return fmt.Errorf("admin: grid %q not registered", gridName)
	}
	g.Columns = append(g.Columns, column)
	return nil
}

// RegisterAction appends an action to an existing grid.
// Returns an error if the grid has not been registered.
func (r *Registry) RegisterAction(gridName string, action Action) error {
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
	f, ok := r.forms[name]
	if !ok {
		return Form{}, false
	}
	return *f, true
}

// Grid returns the grid registered under name and true,
// or a zero Grid and false if not found.
func (r *Registry) Grid(name string) (Grid, bool) {
	g, ok := r.grids[name]
	if !ok {
		return Grid{}, false
	}
	return *g, true
}
