package admin

// Option represents a selectable value for a Field of type "select".
type Option struct {
	Label string
	Value string
}

// Field describes a single form input.
type Field struct {
	Name     string
	Type     string // text, number, select, checkbox
	Label    string
	Required bool
	Default  interface{}
	Options  []Option // populated for "select" fields
	Meta     map[string]interface{}
}

// FormSection is a named block on a form whose visibility is driven by a
// form-specific selector (for product.form: the product Type field, PR-1053).
//
// Types holds opaque visibility tokens compared to the selector value. Empty
// Types means always visible. For product.form, tokens must be catalog product
// types — prefer application/admin.RegisterProductFormSection so invalid types
// are rejected at registration. Types is not validated inside the generic
// Registry (keeps admin domain free of catalog imports).
//
// Section bodies render outside the main <form> (same pattern as categories /
// variants). They must use their own save APIs; fields inside a section body
// are not collected by the core product form payload.
type FormSection struct {
	ID    string
	Title string
	Types []string // visibility tokens; empty = always visible
	Meta  map[string]interface{}
}

// Form describes an admin create/edit form.
type Form struct {
	Name     string
	Fields   []Field
	Sections []FormSection
}

// Column describes a single grid column.
type Column struct {
	Name  string
	Label string
	Value func(row interface{}) interface{}
	Meta  map[string]interface{}
}

// Action describes a bulk action available on a grid.
type Action struct {
	Name    string
	Label   string
	Execute func(ids []string) error
}

// Grid describes an admin list view.
type Grid struct {
	Name    string
	Columns []Column
	Actions []Action
}
