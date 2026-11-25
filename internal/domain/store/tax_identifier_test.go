package store_test

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/store"
)

func TestNewTaxIdentifier(t *testing.T) {
	ti, err := store.NewTaxIdentifier("s-1", " PL_NIP ", " 123-456-78-90 ")
	if err != nil {
		t.Fatalf("NewTaxIdentifier: %v", err)
	}
	if ti.Type != "pl_nip" {
		t.Errorf("Type = %q, want pl_nip", ti.Type)
	}
	if ti.Value != "123-456-78-90" {
		t.Errorf("Value = %q (must not fold punctuation)", ti.Value)
	}
}

func TestNewTaxIdentifier_Validation(t *testing.T) {
	tests := []struct {
		name  string
		sid   string
		typ   string
		value string
		want  string
	}{
		{name: "empty store", sid: " ", typ: "pl_nip", value: "1", want: "store id"},
		{name: "empty type", sid: "s-1", typ: "  ", value: "1", want: "type"},
		{name: "empty value", sid: "s-1", typ: "pl_nip", value: " ", want: "value"},
		{name: "space in type", sid: "s-1", typ: "pl nip", value: "1", want: "snake token"},
		{name: "slash in type", sid: "s-1", typ: "a/b", value: "1", want: "snake token"},
		{name: "control in type", sid: "s-1", typ: "pl\tnip", value: "1", want: "snake token"},
		{name: "leading digit", sid: "s-1", typ: "1nip", value: "1", want: "snake token"},
		{name: "long type", sid: "s-1", typ: "a" + strings.Repeat("b", 64), value: "1", want: "max length"},
		{name: "long value", sid: "s-1", typ: "pl_nip", value: strings.Repeat("1", 65), want: "max length"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.NewTaxIdentifier(tt.sid, tt.typ, tt.value)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

func TestNewTaxIdentifierFromDB_UnvalidatedRoundTrip(t *testing.T) {
	ti := store.NewTaxIdentifierFromDB("s-1", "pl nip", "x")
	if ti == nil {
		t.Fatal("nil TaxIdentifier")
	}
	if ti.Type != "pl nip" {
		t.Fatalf("FromDB must keep raw type %q", ti.Type)
	}
}
