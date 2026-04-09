package exporter

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"

	"github.com/akarso/shopanda/internal/domain/customer"
)

// CustomerResult holds the summary of a customer export run.
type CustomerResult struct {
	Entries int
}

// CustomerExporter writes customers to CSV.
type CustomerExporter struct {
	customers customer.CustomerRepository
}

// NewCustomerExporter creates a CustomerExporter.
func NewCustomerExporter(customers customer.CustomerRepository) *CustomerExporter {
	return &CustomerExporter{customers: customers}
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
			row := []string{
				sanitizeCSVCell(c.Email),
				sanitizeCSVCell(c.FirstName),
				sanitizeCSVCell(c.LastName),
				string(c.Role),
				string(c.Status),
			}
			if err := writer.Write(row); err != nil {
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
