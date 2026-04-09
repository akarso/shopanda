package importer

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"

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
}

// NewCustomerImporter creates a CustomerImporter.
func NewCustomerImporter(customers customer.CustomerRepository) *CustomerImporter {
	return &CustomerImporter{customers: customers}
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

	emailIdx, hasEmail := colIdx["email"]
	if !hasEmail {
		return nil, fmt.Errorf("customer import: CSV must have 'email' column")
	}

	firstNameIdx, hasFirstName := colIdx["first_name"]
	lastNameIdx, hasLastName := colIdx["last_name"]
	roleIdx, hasRole := colIdx["role"]
	statusIdx, hasStatus := colIdx["status"]
	passwordIdx, hasPassword := colIdx["password"]

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

		email := strings.TrimSpace(record[emailIdx])
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

		if hasFirstName && firstNameIdx < len(record) {
			c.FirstName = strings.TrimSpace(record[firstNameIdx])
		}
		if hasLastName && lastNameIdx < len(record) {
			c.LastName = strings.TrimSpace(record[lastNameIdx])
		}

		if hasRole && roleIdx < len(record) {
			r := customer.Role(strings.TrimSpace(strings.ToLower(record[roleIdx])))
			if r != "" && !r.IsValid() {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: invalid role %q", lineNum, record[roleIdx]))
				result.Skipped++
				continue
			}
			if r != "" {
				c.Role = r
			}
		}

		if hasStatus && statusIdx < len(record) {
			s := customer.Status(strings.TrimSpace(strings.ToLower(record[statusIdx])))
			if s != "" && !s.IsValid() {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: invalid status %q", lineNum, record[statusIdx]))
				result.Skipped++
				continue
			}
			if s != "" {
				c.Status = s
			}
		}

		if hasPassword && passwordIdx < len(record) {
			plain := strings.TrimSpace(record[passwordIdx])
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
