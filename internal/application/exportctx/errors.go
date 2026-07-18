package exportctx

import "strings"

// ExportError is a structured row-level export failure.
type ExportError struct {
	RowIndex int
	Code     string
	Message  string
}

// AppendError records a structured error on the row context.
func (c *RowContext) AppendError(code, message string) {
	if c == nil {
		return
	}
	c.Errors = append(c.Errors, ExportError{
		RowIndex: c.RowIndex,
		Code:     strings.TrimSpace(code),
		Message:  message,
	})
}

// SkipRow marks the row to be omitted from export output without error.
func (c *RowContext) SkipRow() {
	if c == nil {
		return
	}
	c.Skip = true
}
