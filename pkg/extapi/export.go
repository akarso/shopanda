package extapi

// ExportEntity names a supported CSV export target.
type ExportEntity string

const (
	ExportEntityProduct   ExportEntity = "product"
	ExportEntityPrice     ExportEntity = "price"
	ExportEntityStock     ExportEntity = "stock"
	ExportEntityCategory  ExportEntity = "category"
	ExportEntityCustomer  ExportEntity = "customer"
	ExportEntityAttribute ExportEntity = "attribute"
)

var exportEntities = []ExportEntity{
	ExportEntityProduct,
	ExportEntityPrice,
	ExportEntityStock,
	ExportEntityCategory,
	ExportEntityCustomer,
	ExportEntityAttribute,
}

// ExportEntities returns documented stable export entity names.
func ExportEntities() []string {
	out := make([]string, len(exportEntities))
	for i, entity := range exportEntities {
		out[i] = string(entity)
	}
	return out
}

// ExportRowHandler mutates export row context during chain execution.
type ExportRowHandler func(ctx *ExportRowContext) error

// ExportError is a structured row-level export failure.
type ExportError struct {
	RowIndex int
	Code     string
	Message  string
}

// ExportRowContext carries mutable CSV row data across export row hook handlers.
type ExportRowContext struct {
	Entity   string
	Row      map[string]string
	RowIndex int
	Meta     map[string]interface{}
	Skip     bool
	Errors   []ExportError
}

// GetMeta returns a meta value.
func (c *ExportRowContext) GetMeta(key string) (interface{}, bool) {
	if c == nil || c.Meta == nil {
		return nil, false
	}
	v, ok := c.Meta[key]
	return v, ok
}

// SetMeta stores a meta value.
func (c *ExportRowContext) SetMeta(key string, value interface{}) {
	if c == nil {
		return
	}
	if c.Meta == nil {
		c.Meta = make(map[string]interface{})
	}
	c.Meta[key] = value
}

// AppendError records a structured error on the row context.
func (c *ExportRowContext) AppendError(code, message string) {
	if c == nil {
		return
	}
	c.Errors = append(c.Errors, ExportError{
		RowIndex: c.RowIndex,
		Code:     code,
		Message:  message,
	})
}

// SkipRow marks the row to be omitted from export output without error.
func (c *ExportRowContext) SkipRow() {
	if c == nil {
		return
	}
	c.Skip = true
}

// ExportRowHookPoint returns the hook point name for entity (e.g. export.product.row).
func ExportRowHookPoint(entity ExportEntity) string {
	return "export." + string(entity) + ".row"
}

// ExportRowHookCatalog returns all documented export row hook point names.
func ExportRowHookCatalog() []string {
	out := make([]string, len(exportEntities))
	for i, entity := range exportEntities {
		out[i] = ExportRowHookPoint(entity)
	}
	return out
}
