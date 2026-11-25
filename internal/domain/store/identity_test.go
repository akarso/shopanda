package store_test

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
	if id.EmailGeneral != "general@example.com" {
		t.Errorf("EmailGeneral = %q", id.EmailGeneral)
	}
	if id.UpdatedAt.Before(before) || id.UpdatedAt.After(after) {
		t.Errorf("UpdatedAt = %v", id.UpdatedAt)
	}
}

func TestNewIdentity_EmptyLegalNameAndEmailsOK(t *testing.T) {
	id, err := store.NewIdentity("s-1", "", "", "", "", "", "")
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	if id.LegalName != "" || id.EmailGeneral != "" {
		t.Fatalf("want empty draft fields, got %+v", id)
	}
}

func TestNewIdentity_EmailMailboxOnly(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "display name", input: "Alice <Sales@Shop.test>", want: "sales@shop.test"},
		{name: "angle addr", input: "<sales@shop.test>", want: "sales@shop.test"},
		{name: "quoted local", input: `"quoted"@example.com`, want: "quoted@example.com"},
		{name: "comment", input: "sales@shop.test (Sales desk)", want: "sales@shop.test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := store.NewIdentity("s-1", "Acme", "", "", tt.input, "", "")
			if err != nil {
				t.Fatalf("NewIdentity: %v", err)
			}
			if id.EmailGeneral != tt.want {
				t.Fatalf("EmailGeneral = %q, want %q", id.EmailGeneral, tt.want)
			}
		})
	}
}

func TestNewIdentity_Validation(t *testing.T) {
	long := strings.Repeat("a", 256)
	tests := []struct {
		name  string
		sid   string
		legal string
		gen   string
		want  string
	}{
		{name: "empty store", sid: " ", want: "store id"},
		{name: "bad general", sid: "s-1", gen: "not-an-email", want: "general email"},
		{name: "newline legal", sid: "s-1", legal: "Acme\nLtd", want: "newlines"},
		{name: "long legal", sid: "s-1", legal: long, want: "max length"},
		{name: "long email", sid: "s-1", gen: strings.Repeat("a", 250) + "@x.co", want: "general email"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.NewIdentity(tt.sid, tt.legal, "", "", tt.gen, "", "")
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

func TestNewIdentityFromDB_UnvalidatedRoundTrip(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	badEmail := "Alice <Not A Mailbox>"
	id := store.NewIdentityFromDB("s-1", "Acme\nLtd", "1", "9-5", badEmail, "s@b.c", "u@b.c", ts)
	if id == nil {
		t.Fatal("nil Identity")
	}
	if id.EmailGeneral != badEmail {
		t.Fatalf("FromDB must keep raw email %q, got %q", badEmail, id.EmailGeneral)
	}
	if id.LegalName != "Acme\nLtd" || id.UpdatedAt != ts {
		t.Fatalf("unexpected %+v", id)
	}
}

func TestNewIdentity_HoursAllowsNewline(t *testing.T) {
	id, err := store.NewIdentity("s-1", "Acme", "", "Mon–Fri\n9–17", "", "", "")
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	if !strings.Contains(id.Hours, "\n") {
		t.Fatalf("hours = %q", id.Hours)
	}
	if utf8.RuneCountInString(id.Hours) == 0 {
		t.Fatal("empty hours")
	}
}
