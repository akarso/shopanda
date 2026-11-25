package store_test

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/store"
)

func TestNewTaxIdentifier(t *testing.T) {
	ti, err := store.NewTaxIdentifier("s-1", " PL_NIP ", " 1234567890 ")
	if err != nil {
		t.Fatalf("NewTaxIdentifier: %v", err)
	}
	if ti.Type != "pl_nip" {
		t.Errorf("Type = %q, want pl_nip", ti.Type)
	}
	if ti.Value != "1234567890" {
		t.Errorf("Value = %q", ti.Value)
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
