package exporter

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"

	exportctx "github.com/akarso/shopanda/internal/application/exportctx"
	"github.com/akarso/shopanda/internal/domain/customer"
)

// CustomerResult holds the summary of a customer export run.
type CustomerResult struct {
	Entries   int
	Skipped   int
	Errors    []string
	RowErrors []exportctx.ExportError
}

// CustomerExporter writes customers to CSV.
type CustomerExporter struct {
	customers customer.CustomerRepository
	rowHooks  *RowHookRunner
}

// NewCustomerExporter creates a CustomerExporter.
func NewCustomerExporter(customers customer.CustomerRepository) *CustomerExporter {
	return &CustomerExporter{customers: customers}
}

// WithRowHooks wires export row hooks invoked before CSV write.
func (exp *CustomerExporter) WithRowHooks(registry *exportctx.Registry) *CustomerExporter {
	exp.rowHooks = NewRowHookRunner(registry)
	return exp
}

// Export writes all customers to w in CSV format.
//
// CSV columns: email, first_name, last_name, role, status.
func (exp *CustomerExporter) Export(ctx context.Context, w io.Writer) (*CustomerResult, error) {
	writer := csv.NewWriter(w)

	if err := writer.Write([]string{"email", "first_name", "last_name", "role", "status"}); err != nil {
		return nil, fmt.Errorf("customer export: write header: %w", err)
	}

	result := &CustomerResult{}
	rowIndex := 0
	header := []string{"email", "first_name", "last_name", "role", "status"}
	offset := 0
	for {
		customers, err := exp.customers.ListCustomers(ctx, offset, pageSize)
		if err != nil {
			return nil, fmt.Errorf("customer export: list customers: %w", err)
		}
		if len(customers) == 0 {
			break
		}
		for _, c := range customers {
			rowIndex++
			rowMap := map[string]string{
				"email":      c.Email,
				"first_name": c.FirstName,
				"last_name":  c.LastName,
				"role":       string(c.Role),
				"status":     string(c.Status),
			}
			if exp.rowHooks != nil && exp.rowHooks.Enabled() {
				var cont bool
				rowMap, cont = HandleRowHookOutcome(rowIndex, exp.rowHooks.Invoke(ctx, exportctx.EntityCustomer, rowIndex, rowMap), &result.Skipped, &result.Errors, &result.RowErrors)
				if !cont {
					continue
				}
			}
			for k, v := range rowMap {
				rowMap[k] = sanitizeCSVCell(v)
			}
			if err := writer.Write(RowToRecord(header, rowMap)); err != nil {
				return nil, fmt.Errorf("customer export: write row: %w", err)
			}
			result.Entries++
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return nil, fmt.Errorf("customer export: flush csv: %w", err)
		}
		if len(customers) < pageSize {
			break
		}
		offset += len(customers)
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("customer export: flush csv: %w", err)
	}

	return result, nil
}
