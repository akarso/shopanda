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

// ImportRowContext carries mutable CSV row data across import row hook handlers.
type ImportRowContext struct {
	Entity   string
	Row      map[string]string
	RowIndex int
	Meta     map[string]interface{}
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
