package importer

import (
	"context"
	"fmt"
	"strings"

	importctx "github.com/akarso/shopanda/internal/application/importctx"
)

// RowHookRunner invokes import row hooks for CSV rows.
type RowHookRunner struct {
	registry *importctx.Registry
}

// NewRowHookRunner creates a runner for the given registry. Nil registry is a no-op.
func NewRowHookRunner(registry *importctx.Registry) *RowHookRunner {
	return &RowHookRunner{registry: registry}
}

// Enabled reports whether row hooks will run.
func (r *RowHookRunner) Enabled() bool {
	return r != nil && r.registry != nil
}

// RecordToRow builds a lowercase column map from a CSV record.
func RecordToRow(record []string, colIndex map[string]int) map[string]string {
	row := make(map[string]string, len(colIndex))
	for col, idx := range colIndex {
		if idx >= 0 && idx < len(record) {
			row[col] = strings.TrimSpace(record[idx])
		}
	}
	return row
}

// colValRow returns a trimmed column value from a row map.
func colValRow(row map[string]string, col string) string {
	return strings.TrimSpace(row[col])
}

// Invoke runs the row hook chain for entity at lineNum (1-based data row).
// Returns the possibly mutated row map.
func (r *RowHookRunner) Invoke(ctx context.Context, entity string, lineNum int, row map[string]string) (map[string]string, error) {
	if !r.Enabled() {
		return row, nil
	}
	rowCtx, err := importctx.NewRowContext(entity, lineNum, row)
	if err != nil {
		return nil, err
	}
	if err := r.registry.Invoke(ctx, rowCtx); err != nil {
		return nil, err
	}
	return rowCtx.Row, nil
}

// RowHookError formats a row-level hook failure for import result errors.
func RowHookError(lineNum int, err error) string {
	return fmt.Sprintf("line %d: import row hook: %v", lineNum, err)
}
