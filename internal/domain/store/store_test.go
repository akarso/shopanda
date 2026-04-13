package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/store"
)

func TestNewStore(t *testing.T) {
	before := time.Now().UTC()
	s, err := store.NewStore("s-1", "default", "Default Store", "EUR", "DE", "shop.example.com")
	after := time.Now().UTC()

	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if s.ID != "s-1" {
		t.Errorf("ID = %q, want s-1", s.ID)
	}
	if s.Code != "default" {
		t.Errorf("Code = %q, want default", s.Code)
	}
	if s.Name != "Default Store" {
		t.Errorf("Name = %q, want Default Store", s.Name)
	}
	if s.Currency != "EUR" {
		t.Errorf("Currency = %q, want EUR", s.Currency)
	}
	if s.Country != "DE" {
		t.Errorf("Country = %q, want DE", s.Country)
	}
	if s.Domain != "shop.example.com" {
		t.Errorf("Domain = %q, want shop.example.com", s.Domain)
	}
	if s.IsDefault {
		t.Error("IsDefault should be false by default")
	}
	if s.CreatedAt.Before(before) || s.CreatedAt.After(after) {
		t.Errorf("CreatedAt = %v, want between %v and %v", s.CreatedAt, before, after)
	}
	if s.UpdatedAt.Before(before) || s.UpdatedAt.After(after) {
		t.Errorf("UpdatedAt = %v, want between %v and %v", s.UpdatedAt, before, after)
	}
}

func TestNewStore_NormalizesCase(t *testing.T) {
	s, err := store.NewStore("s-1", "us", "US Store", "usd", "us", "")
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if s.Currency != "USD" {
		t.Errorf("Currency = %q, want USD (uppercased)", s.Currency)
	}
	if s.Country != "US" {
		t.Errorf("Country = %q, want US (uppercased)", s.Country)
	}
}

func TestNewStore_Validation(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		code     string
		sName    string
		currency string
		country  string
		domain   string
	}{
		{"empty id", "", "code", "Name", "EUR", "DE", ""},
		{"empty code", "id", "", "Name", "EUR", "DE", ""},
		{"empty name", "id", "code", "", "EUR", "DE", ""},
		{"empty currency", "id", "code", "Name", "", "DE", ""},
		{"invalid currency length", "id", "code", "Name", "EU", "DE", ""},
		{"empty country", "id", "code", "Name", "EUR", "", ""},
		{"invalid country length", "id", "code", "Name", "EUR", "DEU", ""},
		{"whitespace id", "  ", "code", "Name", "EUR", "DE", ""},
		{"whitespace code", "id", "  ", "Name", "EUR", "DE", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := store.NewStore(tc.id, tc.code, tc.sName, tc.currency, tc.country, tc.domain)
			if err == nil {
				t.Error("NewStore() expected error")
			}
		})
	}
}

func TestNewStoreFromDB(t *testing.T) {
	now := time.Now().UTC()
	s := store.NewStoreFromDB("s-1", "default", "Default", "EUR", "DE", "shop.com", true, now, now)

	if s == nil {
		t.Fatal("NewStoreFromDB() returned nil")
	}
	if s.ID != "s-1" {
		t.Errorf("ID = %q, want s-1", s.ID)
	}
	if !s.IsDefault {
		t.Error("IsDefault should be true")
	}
}

func TestStoreContext(t *testing.T) {
	s := store.NewStoreFromDB("s-1", "default", "Default", "EUR", "DE", "", true, time.Now(), time.Now())

	ctx := store.WithStore(context.Background(), s)
	got := store.FromContext(ctx)
	if got == nil {
		t.Fatal("FromContext() returned nil")
	}
	if got.ID != "s-1" {
		t.Errorf("FromContext().ID = %q, want s-1", got.ID)
	}
}

func TestStoreContext_Missing(t *testing.T) {
	got := store.FromContext(context.Background())
	if got != nil {
		t.Errorf("FromContext() = %v, want nil", got)
	}
}

func TestNormalizeCurrency(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"valid uppercase", "EUR", "EUR", false},
		{"valid lowercase", "usd", "USD", false},
		{"trims whitespace", " eur ", "EUR", false},
		{"empty", "", "", true},
		{"whitespace only", "  ", "", true},
		{"too short", "EU", "", true},
		{"too long", "EURO", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := store.NormalizeCurrency(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("NormalizeCurrency(%q) error = %v, wantErr %v", tc.input, err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("NormalizeCurrency(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeCountry(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"valid uppercase", "DE", "DE", false},
		{"valid lowercase", "us", "US", false},
		{"trims whitespace", " de ", "DE", false},
		{"empty", "", "", true},
		{"whitespace only", "  ", "", true},
		{"too short", "D", "", true},
		{"too long", "DEU", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := store.NormalizeCountry(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("NormalizeCountry(%q) error = %v, wantErr %v", tc.input, err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("NormalizeCountry(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
