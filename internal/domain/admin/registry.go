package admin

import (
	"fmt"
	"strings"
	"sync"

	"github.com/akarso/shopanda/internal/domain/rbac"
)

// Registry holds all registered admin form and grid schemas.
type Registry struct {
	mu              sync.RWMutex
	forms           map[string]*Form
	grids           map[string]*Grid
	formPermissions map[string]rbac.Permission
	gridPermissions map[string]rbac.Permission
}

// NewRegistry creates an empty schema registry.
func NewRegistry() *Registry {
	return &Registry{
		forms:           make(map[string]*Form),
		grids:           make(map[string]*Grid),
		formPermissions: make(map[string]rbac.Permission),
		gridPermissions: make(map[string]rbac.Permission),
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

// RegisterFormSection appends a visibility-scoped section to an existing form.
// Returns an error if the form has not been registered, ID is empty, ID is a
// duplicate on that form, or any Types entry is blank.
// For product.form catalog-type validation, use application/admin.RegisterProductFormSection.
func (r *Registry) RegisterFormSection(formName string, section FormSection) error {
	id := strings.TrimSpace(section.ID)
	if id == "" {
		return fmt.Errorf("admin: form section id must not be empty")
	}
	section.ID = id
	if len(section.Types) > 0 {
		types := make([]string, len(section.Types))
		for i, t := range section.Types {
			trimmed := strings.TrimSpace(t)
			if trimmed == "" {
				return fmt.Errorf("admin: form section %q types[%d] must not be empty", id, i)
			}
			types[i] = trimmed
		}
		section.Types = types
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.forms[formName]
	if !ok {
		return fmt.Errorf("admin: form %q not registered", formName)
	}
	for _, existing := range f.Sections {
		if existing.ID == id {
			return fmt.Errorf("admin: form %q already has section %q", formName, id)
		}
	}
	f.Sections = append(f.Sections, cloneFormSection(section))
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

// SetFormPermission associates a permission with a registered form.
// Returns an error if the form has not been registered or if perm is empty.
func (r *Registry) SetFormPermission(formName string, perm rbac.Permission) error {
	if perm == "" {
		return fmt.Errorf("admin: empty permission not allowed for form %q", formName)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.forms[formName]; !ok {
		return fmt.Errorf("admin: form %q not registered", formName)
	}
	r.formPermissions[formName] = perm
	return nil
}

// FormPermission returns the permission required for a form and true,
// or an empty Permission and false if none is set.
func (r *Registry) FormPermission(formName string) (rbac.Permission, bool) {
	r.mu.RLock()
	p, ok := r.formPermissions[formName]
	r.mu.RUnlock()
	return p, ok
}

// SetGridPermission associates a permission with a registered grid.
// Returns an error if the grid has not been registered or if perm is empty.
func (r *Registry) SetGridPermission(gridName string, perm rbac.Permission) error {
	if perm == "" {
		return fmt.Errorf("admin: empty permission not allowed for grid %q", gridName)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.grids[gridName]; !ok {
		return fmt.Errorf("admin: grid %q not registered", gridName)
	}
	r.gridPermissions[gridName] = perm
	return nil
}

// GridPermission returns the permission required for a grid and true,
// or an empty Permission and false if none is set.
func (r *Registry) GridPermission(gridName string) (rbac.Permission, bool) {
	r.mu.RLock()
	p, ok := r.gridPermissions[gridName]
	r.mu.RUnlock()
	return p, ok
}

// --- deep-copy helpers ---

func cloneMetaValue(val interface{}) interface{} {
	switch v := val.(type) {
	case map[string]interface{}:
		m := make(map[string]interface{}, len(v))
		for k, mv := range v {
			m[k] = cloneMetaValue(mv)
		}
		return m
	case []interface{}:
		s := make([]interface{}, len(v))
		for i, sv := range v {
			s[i] = cloneMetaValue(sv)
		}
		return s
	default:
		return val
	}
}

func cloneMeta(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	m := make(map[string]interface{}, len(src))
	for k, v := range src {
		m[k] = cloneMetaValue(v)
	}
	return m
}

func cloneField(f Field) Field {
	if f.Options != nil {
		opts := make([]Option, len(f.Options))
		copy(opts, f.Options)
		f.Options = opts
	}
	f.Meta = cloneMeta(f.Meta)
	return f
}

func cloneColumn(c Column) Column {
	c.Meta = cloneMeta(c.Meta)
	return c
}

func cloneFormSection(s FormSection) FormSection {
	if s.Types != nil {
		types := make([]string, len(s.Types))
		copy(types, s.Types)
		s.Types = types
	}
	s.Meta = cloneMeta(s.Meta)
	return s
}

func cloneForm(f Form) Form {
	if f.Fields != nil {
		fields := make([]Field, len(f.Fields))
		for i, fld := range f.Fields {
			fields[i] = cloneField(fld)
		}
		f.Fields = fields
	}
	if f.Sections != nil {
		sections := make([]FormSection, len(f.Sections))
		for i, s := range f.Sections {
			sections[i] = cloneFormSection(s)
		}
		f.Sections = sections
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
