package extapi

// ImportEntity names a supported CSV import target.
type ImportEntity string

const (
	ImportEntityProduct   ImportEntity = "product"
	ImportEntityPrice     ImportEntity = "price"
	ImportEntityStock     ImportEntity = "stock"
	ImportEntityCategory  ImportEntity = "category"
	ImportEntityCustomer  ImportEntity = "customer"
	ImportEntityAttribute ImportEntity = "attribute"
)

var importEntities = []ImportEntity{
	ImportEntityProduct,
	ImportEntityPrice,
	ImportEntityStock,
	ImportEntityCategory,
	ImportEntityCustomer,
	ImportEntityAttribute,
}

// ImportEntities returns documented stable import entity names.
func ImportEntities() []string {
	out := make([]string, len(importEntities))
	for i, entity := range importEntities {
		out[i] = string(entity)
	}
	return out
}

// ImportRowHandler mutates import row context during chain execution.
type ImportRowHandler func(ctx *ImportRowContext) error

// ImportError is a structured row-level import failure.
type ImportError struct {
	RowIndex int
	Code     string
	Message  string
}

// ImportRowContext carries mutable CSV row data across import row hook handlers.
type ImportRowContext struct {
	Entity   string
	Row      map[string]string
	RowIndex int
	Meta     map[string]interface{}
	Skip     bool
	Errors   []ImportError
}

// GetMeta returns a meta value.
func (c *ImportRowContext) GetMeta(key string) (interface{}, bool) {
	if c == nil || c.Meta == nil {
		return nil, false
	}
	v, ok := c.Meta[key]
	return v, ok
}

// SetMeta stores a meta value.
func (c *ImportRowContext) SetMeta(key string, value interface{}) {
	if c == nil {
		return
	}
	if c.Meta == nil {
		c.Meta = make(map[string]interface{})
	}
	c.Meta[key] = value
}

// AppendError records a structured error on the row context.
func (c *ImportRowContext) AppendError(code, message string) {
	if c == nil {
		return
	}
	c.Errors = append(c.Errors, ImportError{
		RowIndex: c.RowIndex,
		Code:     code,
		Message:  message,
	})
}

// SkipRow marks the row to be skipped without error after the hook chain completes.
func (c *ImportRowContext) SkipRow() {
	if c == nil {
		return
	}
	c.Skip = true
}

// RowHookPoint returns the hook point name for entity (e.g. import.product.row).
func RowHookPoint(entity ImportEntity) string {
	return "import." + string(entity) + ".row"
}

// ImportRowHookCatalog returns all documented import row hook point names.
func ImportRowHookCatalog() []string {
	out := make([]string, len(importEntities))
	for i, entity := range importEntities {
		out[i] = RowHookPoint(entity)
	}
	return out
}
