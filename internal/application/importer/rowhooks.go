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

// RowHookOutcome is the post-chain result for one CSV row.
type RowHookOutcome struct {
	Row        map[string]string
	Skip       bool
	Errors     []importctx.ImportError
	HandlerErr error
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
func (r *RowHookRunner) Invoke(ctx context.Context, entity string, lineNum int, row map[string]string) RowHookOutcome {
	if !r.Enabled() {
		return RowHookOutcome{Row: row}
	}
	rowCtx, err := importctx.NewRowContext(entity, lineNum, row)
	if err != nil {
		return RowHookOutcome{HandlerErr: err}
	}
	if err := r.registry.Invoke(ctx, rowCtx); err != nil {
		return RowHookOutcome{HandlerErr: err}
	}
	if rowCtx.Skip {
		return RowHookOutcome{Skip: true}
	}
	if len(rowCtx.Errors) > 0 {
		return RowHookOutcome{Errors: append([]importctx.ImportError(nil), rowCtx.Errors...)}
	}
	return RowHookOutcome{Row: rowCtx.Row}
}

// FormatImportError renders a structured import error for CLI logs.
func FormatImportError(err importctx.ImportError) string {
	if err.Code != "" {
		return fmt.Sprintf("line %d: %s: %s", err.RowIndex, err.Code, err.Message)
	}
	return fmt.Sprintf("line %d: %s", err.RowIndex, err.Message)
}

// HandleRowHookOutcome applies hook chain results to import counters.
// Returns the row map and whether the importer should continue processing the row.
func HandleRowHookOutcome(lineNum int, outcome RowHookOutcome, skipped *int, strErrs *[]string, rowErrs *[]importctx.ImportError) (map[string]string, bool) {
	if outcome.HandlerErr != nil {
		*skipped++
		msg := RowHookError(lineNum, outcome.HandlerErr)
		*strErrs = append(*strErrs, msg)
		return nil, false
	}
	if outcome.Skip {
		*skipped++
		return nil, false
	}
	if len(outcome.Errors) > 0 {
		*skipped++
		for _, e := range outcome.Errors {
			*rowErrs = append(*rowErrs, e)
			*strErrs = append(*strErrs, FormatImportError(e))
		}
		return nil, false
	}
	return outcome.Row, true
}

// RowHookError formats a row-level hook failure for import result errors.
func RowHookError(lineNum int, err error) string {
	return fmt.Sprintf("line %d: import row hook: %v", lineNum, err)
}
