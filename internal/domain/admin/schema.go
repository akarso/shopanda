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

// Form describes an admin create/edit form.
type Form struct {
	Name   string
	Fields []Field
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
