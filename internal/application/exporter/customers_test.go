package exporter_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/exporter"
	"github.com/akarso/shopanda/internal/domain/customer"
)

// --- customer test mocks ---

type mockCustomerRepoForExport struct {
	customers []customer.Customer
	listErr   error
}

func (m *mockCustomerRepoForExport) FindByID(_ context.Context, _ string) (*customer.Customer, error) {
	return nil, nil
}
func (m *mockCustomerRepoForExport) FindByEmail(_ context.Context, _ string) (*customer.Customer, error) {
	return nil, nil
}
func (m *mockCustomerRepoForExport) Create(_ context.Context, _ *customer.Customer) error {
	return nil
}
func (m *mockCustomerRepoForExport) Update(_ context.Context, _ *customer.Customer) error {
	return nil
}
func (m *mockCustomerRepoForExport) ListCustomers(_ context.Context, offset, limit int) ([]customer.Customer, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if offset >= len(m.customers) {
		return nil, nil
	}
	end := offset + limit
	if end > len(m.customers) {
		end = len(m.customers)
	}
	return m.customers[offset:end], nil
}
func (m *mockCustomerRepoForExport) BumpTokenGeneration(_ context.Context, _ string) error {
	return nil
}
func (m *mockCustomerRepoForExport) Delete(_ context.Context, _ string) error     { return nil }
func (m *mockCustomerRepoForExport) WithTx(_ *sql.Tx) customer.CustomerRepository { return m }

// --- tests ---

func TestCustomerExport_Basic(t *testing.T) {
	repo := &mockCustomerRepoForExport{
		customers: []customer.Customer{
			{ID: "c1", Email: "alice@example.com", FirstName: "Alice", LastName: "Smith", Role: customer.RoleCustomer, Status: customer.StatusActive},
			{ID: "c2", Email: "bob@example.com", FirstName: "Bob", LastName: "Jones", Role: customer.RoleAdmin, Status: customer.StatusDisabled},
		},
	}

	exp := exporter.NewCustomerExporter(repo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Entries != 2 {
		t.Errorf("Entries = %d, want 2", result.Entries)
	}

	records := parseCSV(t, &buf)
	if len(records) != 3 {
		t.Fatalf("rows = %d, want 3", len(records))
	}
	if strings.Join(records[0], ",") != "email,first_name,last_name,role,status" {
		t.Errorf("header = %v", records[0])
	}
	if records[1][0] != "alice@example.com" || records[1][1] != "Alice" || records[1][3] != "customer" {
		t.Errorf("row1 = %v", records[1])
	}
	if records[2][0] != "bob@example.com" || records[2][3] != "admin" || records[2][4] != "disabled" {
		t.Errorf("row2 = %v", records[2])
	}
}

func TestCustomerExport_Empty(t *testing.T) {
	repo := &mockCustomerRepoForExport{}

	exp := exporter.NewCustomerExporter(repo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Entries != 0 {
		t.Errorf("Entries = %d, want 0", result.Entries)
	}
	records := parseCSV(t, &buf)
	if len(records) != 1 {
		t.Errorf("rows = %d, want 1", len(records))
	}
}

func TestCustomerExport_ListError(t *testing.T) {
	repo := &mockCustomerRepoForExport{listErr: fmt.Errorf("db unavailable")}

	exp := exporter.NewCustomerExporter(repo)
	var buf bytes.Buffer
	_, err := exp.Export(context.Background(), &buf)
	if err == nil {
		t.Fatal("expected error for list failure")
	}
	if !strings.Contains(err.Error(), "db unavailable") {
		t.Errorf("error = %q, want containing 'db unavailable'", err.Error())
	}
}

func TestCustomerExport_Pagination(t *testing.T) {
	custs := make([]customer.Customer, 150)
	for i := range custs {
		custs[i] = customer.Customer{
			ID:     fmt.Sprintf("c%d", i+1),
			Email:  fmt.Sprintf("user%03d@example.com", i+1),
			Role:   customer.RoleCustomer,
			Status: customer.StatusActive,
		}
	}
	repo := &mockCustomerRepoForExport{customers: custs}

	exp := exporter.NewCustomerExporter(repo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Entries != 150 {
		t.Errorf("Entries = %d, want 150", result.Entries)
	}
	records := parseCSV(t, &buf)
	if len(records) != 151 {
		t.Fatalf("rows = %d, want 151", len(records))
	}
}

func TestCustomerExport_NoPasswordInOutput(t *testing.T) {
	repo := &mockCustomerRepoForExport{
		customers: []customer.Customer{
			{ID: "c1", Email: "alice@example.com", PasswordHash: "secrethashvalue", Role: customer.RoleCustomer, Status: customer.StatusActive},
		},
	}

	exp := exporter.NewCustomerExporter(repo)
	var buf bytes.Buffer
	_, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	output := buf.String()
	if strings.Contains(output, "secrethashvalue") {
		t.Error("CSV output contains password hash")
	}
	if strings.Contains(output, "password") {
		t.Error("CSV header contains password column")
	}
}

func TestCustomerExport_FormulaInjectionSanitized(t *testing.T) {
	repo := &mockCustomerRepoForExport{
		customers: []customer.Customer{
			{ID: "c1", Email: "=cmd@example.com", FirstName: "+Alice", LastName: "-Smith", Role: customer.RoleCustomer, Status: customer.StatusActive},
			{ID: "c2", Email: "@evil.com", FirstName: "Bob", LastName: "Jones", Role: customer.RoleAdmin, Status: customer.StatusActive},
		},
	}

	exp := exporter.NewCustomerExporter(repo)
	var buf bytes.Buffer
	_, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	records := parseCSV(t, &buf)
	// row 1: email starts with '=', first_name with '+', last_name with '-'
	if !strings.HasPrefix(records[1][0], "'") {
		t.Errorf("email %q not sanitized (expected leading ')", records[1][0])
	}
	if !strings.HasPrefix(records[1][1], "'") {
		t.Errorf("first_name %q not sanitized (expected leading ')", records[1][1])
	}
	if !strings.HasPrefix(records[1][2], "'") {
		t.Errorf("last_name %q not sanitized (expected leading ')", records[1][2])
	}
	// row 2: email starts with '@'
	if !strings.HasPrefix(records[2][0], "'") {
		t.Errorf("email %q not sanitized (expected leading ')", records[2][0])
	}
}
