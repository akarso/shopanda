package store_test

import (
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/store"
)

func TestNewIdentity(t *testing.T) {
	before := time.Now().UTC()
	id, err := store.NewIdentity(
		"s-1",
		"  Acme Sp. z o.o. ",
		" +48 111 ",
		" Mon–Fri 9–17 ",
		" General@Example.com ",
		"sales@example.com",
		"support@example.com",
	)
	after := time.Now().UTC()
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	if id.StoreID != "s-1" {
		t.Errorf("StoreID = %q", id.StoreID)
	}
	if id.LegalName != "Acme Sp. z o.o." {
		t.Errorf("LegalName = %q", id.LegalName)
	}
	if id.Phone != "+48 111" {
		t.Errorf("Phone = %q", id.Phone)
	}
	if id.Hours != "Mon–Fri 9–17" {
		t.Errorf("Hours = %q", id.Hours)
	}
	if id.EmailGeneral != "general@example.com" {
		t.Errorf("EmailGeneral = %q", id.EmailGeneral)
	}
	if id.EmailSales != "sales@example.com" || id.EmailSupport != "support@example.com" {
		t.Errorf("sales/support = %q / %q", id.EmailSales, id.EmailSupport)
	}
	if id.UpdatedAt.Before(before) || id.UpdatedAt.After(after) {
		t.Errorf("UpdatedAt = %v", id.UpdatedAt)
	}
}

func TestNewIdentity_EmptyEmailsOK(t *testing.T) {
	id, err := store.NewIdentity("s-1", "Acme", "", "", "", "", "")
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	if id.EmailGeneral != "" || id.EmailSales != "" || id.EmailSupport != "" {
		t.Fatalf("want empty emails, got %+v", id)
	}
}

func TestNewIdentity_Validation(t *testing.T) {
	tests := []struct {
		name string
		sid  string
		gen  string
		sale string
		sup  string
		want string
	}{
		{name: "empty store", sid: " ", want: "store id"},
		{name: "bad general", sid: "s-1", gen: "not-an-email", want: "general email"},
		{name: "bad sales", sid: "s-1", sale: "x@", want: "sales email"},
		{name: "bad support", sid: "s-1", sup: "@@", want: "support email"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.NewIdentity(tt.sid, "Acme", "", "", tt.gen, tt.sale, tt.sup)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

func TestNewIdentityFromDB(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	id := store.NewIdentityFromDB("s-1", "Acme", "1", "9-5", "a@b.c", "s@b.c", "u@b.c", ts)
	if id.StoreID != "s-1" || id.UpdatedAt != ts || id.EmailGeneral != "a@b.c" {
		t.Fatalf("unexpected %+v", id)
	}
}
