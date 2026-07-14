package importer

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	importctx "github.com/akarso/shopanda/internal/application/importctx"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/password"
)

// CustomerResult holds the summary of a customer import run.
type CustomerResult struct {
	Created int
	Skipped int
	Errors  []string
}

// CustomerImporter imports customers from CSV.
type CustomerImporter struct {
	customers customer.CustomerRepository
	rowHooks  *RowHookRunner
}

// NewCustomerImporter creates a CustomerImporter.
func NewCustomerImporter(customers customer.CustomerRepository) *CustomerImporter {
	return &CustomerImporter{customers: customers}
}

// WithRowHooks wires import row hooks invoked after header validation and before persist.
func (imp *CustomerImporter) WithRowHooks(registry *importctx.Registry) *CustomerImporter {
	imp.rowHooks = NewRowHookRunner(registry)
	return imp
}

// Import reads CSV rows from r and creates customer records.
//
// Required columns: email.
// Optional columns: first_name, last_name, role, status, password.
func (imp *CustomerImporter) Import(ctx context.Context, r io.Reader) (*CustomerResult, error) {
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("customer import: read header: %w", err)
	}

	colIdx := make(map[string]int, len(header))
	for i, h := range header {
		colIdx[strings.TrimSpace(strings.ToLower(h))] = i
	}

	_, hasEmail := colIdx["email"]
	if !hasEmail {
		return nil, fmt.Errorf("customer import: CSV must have 'email' column")
	}

	_, hasFirstName := colIdx["first_name"]
	_, hasLastName := colIdx["last_name"]
	_, hasRole := colIdx["role"]
	_, hasStatus := colIdx["status"]
	_, hasPassword := colIdx["password"]

	result := &CustomerResult{}
	lineNum := 1 // header is line 1

	for {
		lineNum++
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %v", lineNum, err))
			result.Skipped++
			continue
		}

		rowMap := RecordToRow(record, colIdx)
		if imp.rowHooks != nil {
			var hookErr error
			rowMap, hookErr = imp.rowHooks.Invoke(ctx, importctx.EntityCustomer, lineNum, rowMap)
			if hookErr != nil {
				result.Errors = append(result.Errors, RowHookError(lineNum, hookErr))
				result.Skipped++
				continue
			}
		}

		email := colValRow(rowMap, "email")
		if email == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: empty email", lineNum))
			result.Skipped++
			continue
		}

		c, err := customer.NewCustomer(id.New(), email)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %v", lineNum, err))
			result.Skipped++
			continue
		}

		if hasFirstName {
			c.FirstName = colValRow(rowMap, "first_name")
		}
		if hasLastName {
			c.LastName = colValRow(rowMap, "last_name")
		}

		if hasRole {
			r := customer.Role(strings.ToLower(colValRow(rowMap, "role")))
			if r != "" && !r.IsValid() {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: invalid role %q", lineNum, rowMap["role"]))
				result.Skipped++
				continue
			}
			if r != "" {
				c.Role = r
			}
		}

		if hasStatus {
			s := customer.Status(strings.ToLower(colValRow(rowMap, "status")))
			if s != "" && !s.IsValid() {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: invalid status %q", lineNum, rowMap["status"]))
				result.Skipped++
				continue
			}
			if s != "" {
				c.Status = s
			}
		}

		if hasPassword {
			plain := colValRow(rowMap, "password")
			if plain != "" {
				hash, err := password.Hash(plain)
				if err != nil {
					return nil, fmt.Errorf("customer import: hash password line %d: %w", lineNum, err)
				}
				c.PasswordHash = hash
			}
		}

		if err := imp.customers.Create(ctx, &c); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %v", lineNum, err))
			result.Skipped++
			continue
		}
		result.Created++
	}

	return result, nil
}
