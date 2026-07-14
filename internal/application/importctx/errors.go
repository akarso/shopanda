package importctx

import "strings"

// ImportError is a structured row-level import failure.
type ImportError struct {
	RowIndex int
	Code     string
	Message  string
}

// AppendError records a structured error on the row context.
func (c *RowContext) AppendError(code, message string) {
	if c == nil {
		return
	}
	c.Errors = append(c.Errors, ImportError{
		RowIndex: c.RowIndex,
		Code:     strings.TrimSpace(code),
		Message:  message,
	})
}

// SkipRow marks the row to be skipped without error after the hook chain completes.
func (c *RowContext) SkipRow() {
	if c == nil {
		return
	}
	c.Skip = true
}
