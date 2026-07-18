package exporter

import (
	"context"
	"fmt"
	"sort"

	exportctx "github.com/akarso/shopanda/internal/application/exportctx"
)

// RowHookRunner invokes export row hooks for CSV rows.
type RowHookRunner struct {
	registry *exportctx.Registry
}

// RowHookOutcome is the post-chain result for one export row.
type RowHookOutcome struct {
	Row        map[string]string
	Skip       bool
	Errors     []exportctx.ExportError
	HandlerErr error
}

// NewRowHookRunner creates a runner for the given registry. Nil registry is a no-op.
func NewRowHookRunner(registry *exportctx.Registry) *RowHookRunner {
	return &RowHookRunner{registry: registry}
}

// Enabled reports whether row hooks will run.
func (r *RowHookRunner) Enabled() bool {
	return r != nil && r.registry != nil
}

// RowToRecord builds a CSV record from header column order and row map values.
func RowToRecord(header []string, row map[string]string) []string {
	record := make([]string, len(header))
	for i, col := range header {
		record[i] = row[col]
	}
	return record
}

// Invoke runs the row hook chain for entity at rowIndex (1-based data row).
func (r *RowHookRunner) Invoke(ctx context.Context, entity string, rowIndex int, row map[string]string) RowHookOutcome {
	if !r.Enabled() {
		return RowHookOutcome{Row: row}
	}
	rowCtx, err := exportctx.NewRowContext(entity, rowIndex, row)
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
		return RowHookOutcome{Errors: append([]exportctx.ExportError(nil), rowCtx.Errors...)}
	}
	return RowHookOutcome{Row: rowCtx.Row}
}

// FormatExportError renders a structured export error for CLI logs.
func FormatExportError(err exportctx.ExportError) string {
	if err.Code != "" {
		return fmt.Sprintf("row %d: %s: %s", err.RowIndex, err.Code, err.Message)
	}
	return fmt.Sprintf("row %d: %s", err.RowIndex, err.Message)
}

// HandleRowHookOutcome applies hook chain results to export counters.
// Returns the row map and whether the exporter should write the row.
func HandleRowHookOutcome(rowIndex int, outcome RowHookOutcome, skipped *int, strErrs *[]string, rowErrs *[]exportctx.ExportError) (map[string]string, bool) {
	if outcome.HandlerErr != nil {
		*skipped++
		msg := RowHookError(rowIndex, outcome.HandlerErr)
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
			*strErrs = append(*strErrs, FormatExportError(e))
		}
		return nil, false
	}
	return outcome.Row, true
}

// RowHookError formats a row-level hook failure for export result errors.
func RowHookError(rowIndex int, err error) string {
	return fmt.Sprintf("row %d: export row hook: %v", rowIndex, err)
}

// MergeExtraColumns appends sorted keys from row maps that are not in baseHeader.
func MergeExtraColumns(baseHeader []string, rows []map[string]string) []string {
	known := make(map[string]struct{}, len(baseHeader))
	for _, col := range baseHeader {
		known[col] = struct{}{}
	}
	extra := make(map[string]struct{})
	for _, row := range rows {
		for k := range row {
			if _, ok := known[k]; !ok {
				extra[k] = struct{}{}
			}
		}
	}
	if len(extra) == 0 {
		return baseHeader
	}
	names := make([]string, 0, len(extra))
	for k := range extra {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]string, len(baseHeader), len(baseHeader)+len(names))
	copy(out, baseHeader)
	return append(out, names...)
}
