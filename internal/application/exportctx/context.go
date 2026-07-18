package exportctx

import "fmt"

// RowHandler mutates export row context during synchronous chain execution.
type RowHandler func(ctx *RowContext) error

// RowContext carries mutable CSV row data across export row hook handlers.
type RowContext struct {
	Entity   string
	Row      map[string]string
	RowIndex int
	Meta     map[string]interface{}
	Skip     bool
	Errors   []ExportError
}

// NewRowContext creates a row context for entity at rowIndex (1-based data row).
func NewRowContext(entity string, rowIndex int, row map[string]string) (*RowContext, error) {
	if err := ValidateEntity(entity); err != nil {
		return nil, err
	}
	if rowIndex <= 0 {
		return nil, fmt.Errorf("export row index must be greater than zero")
	}
	copied := make(map[string]string, len(row))
	for k, v := range row {
		copied[k] = v
	}
	return &RowContext{
		Entity:   entity,
		Row:      copied,
		RowIndex: rowIndex,
		Meta:     make(map[string]interface{}),
	}, nil
}

// GetMeta returns a meta value.
func (c *RowContext) GetMeta(key string) (interface{}, bool) {
	if c == nil || c.Meta == nil {
		return nil, false
	}
	v, ok := c.Meta[key]
	return v, ok
}

// SetMeta stores a meta value.
func (c *RowContext) SetMeta(key string, value interface{}) {
	if c == nil {
		return
	}
	if c.Meta == nil {
		c.Meta = make(map[string]interface{})
	}
	c.Meta[key] = value
}
