package store_test

import (
	"net/mail"
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
		" Sales@Example.com ",
		" Support@Example.com ",
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
	if id.EmailSales != "sales@example.com" {
		t.Errorf("EmailSales = %q", id.EmailSales)
	}
	if id.EmailSupport != "support@example.com" {
		t.Errorf("EmailSupport = %q", id.EmailSupport)
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
		t.Run(tt.name+"/general", func(t *testing.T) {
			id, err := store.NewIdentity("s-1", "Acme", "", "", tt.input, "", "")
			if err != nil {
				t.Fatalf("NewIdentity: %v", err)
			}
			if id.EmailGeneral != tt.want {
				t.Fatalf("EmailGeneral = %q, want %q", id.EmailGeneral, tt.want)
			}
		})
		t.Run(tt.name+"/sales", func(t *testing.T) {
			id, err := store.NewIdentity("s-1", "Acme", "", "", "", tt.input, "")
			if err != nil {
				t.Fatalf("NewIdentity: %v", err)
			}
			if id.EmailSales != tt.want {
				t.Fatalf("EmailSales = %q, want %q", id.EmailSales, tt.want)
			}
		})
		t.Run(tt.name+"/support", func(t *testing.T) {
			id, err := store.NewIdentity("s-1", "Acme", "", "", "", "", tt.input)
			if err != nil {
				t.Fatalf("NewIdentity: %v", err)
			}
			if id.EmailSupport != tt.want {
				t.Fatalf("EmailSupport = %q, want %q", id.EmailSupport, tt.want)
			}
		})
	}
}

// mustMailbox builds an n-octet addr that mail.ParseAddress accepts
// (64-char local + dotted domain labels ≤ 63).
func mustMailbox(t *testing.T, n int) string {
	t.Helper()
	const localLen = 64
	if n < localLen+4 {
		t.Fatalf("n=%d too small", n)
	}
	local := strings.Repeat("a", localLen)
	need := n - localLen - 1
	var labels []string
	for need > 0 {
		chunk := 63
		if need < chunk {
			chunk = need
		}
		labels = append(labels, strings.Repeat("b", chunk))
		need -= chunk
		if need > 0 {
			need-- // account for joining dot
		}
	}
	out := local + "@" + strings.Join(labels, ".")
	if len(out) != n {
		t.Fatalf("built len=%d want %d (%q)", len(out), n, out)
	}
	if _, err := mail.ParseAddress(out); err != nil {
		t.Fatalf("ParseAddress(%q): %v", out, err)
	}
	return out
}

func TestNewIdentity_EmailLengthCap(t *testing.T) {
	ok254 := mustMailbox(t, store.MaxEmailLen)
	id, err := store.NewIdentity("s-1", "Acme", "", "", ok254, "", "")
	if err != nil {
		t.Fatalf("254-octet mailbox: %v", err)
	}
	if id.EmailGeneral != ok254 {
		t.Fatalf("stored = %q", id.EmailGeneral)
	}

	bad255 := mustMailbox(t, store.MaxEmailLen+1)
	_, err = store.NewIdentity("s-1", "Acme", "", "", bad255, "", "")
	if err == nil {
		t.Fatal("expected reject for 255-octet mailbox")
	}
	if !strings.Contains(err.Error(), "general email") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestNewIdentity_HugeDisplayNameRejected(t *testing.T) {
	raw := strings.Repeat("X", store.MaxEmailInputLen+1) + " <ok@shop.test>"
	if _, err := store.NewIdentity("s-1", "Acme", "", "", raw, "", ""); err == nil {
		t.Fatal("expected reject for oversized raw email input")
	}
}

func TestNewIdentity_Validation(t *testing.T) {
	long := strings.Repeat("a", store.MaxLegalNameLen+1)
	tests := []struct {
		name  string
		sid   string
		legal string
		gen   string
		sale  string
		sup   string
		want  string
	}{
		{name: "empty store", sid: " ", want: "store id"},
		{name: "newline store", sid: "s\n1", want: "store id"},
		{name: "nul store", sid: "s\x001", want: "store id"},
		{name: "long store", sid: strings.Repeat("s", store.MaxStoreIDLen+1), want: "store id"},
		{name: "bad general", sid: "s-1", gen: "not-an-email", want: "general email"},
		{name: "bad sales", sid: "s-1", sale: "not-an-email", want: "sales email"},
		{name: "bad support", sid: "s-1", sup: "not-an-email", want: "support email"},
		{name: "newline legal", sid: "s-1", legal: "Acme\nLtd", want: "line breaks or control"},
		{name: "ls legal", sid: "s-1", legal: "Acme\u2028Ltd", want: "line breaks or control"},
		{name: "vt legal", sid: "s-1", legal: "Acme\vLtd", want: "line breaks or control"},
		{name: "nul legal", sid: "s-1", legal: "Acme\x00Ltd", want: "line breaks or control"},
		{name: "long legal", sid: "s-1", legal: long, want: "max length"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.NewIdentity(tt.sid, tt.legal, "", "", tt.gen, tt.sale, tt.sup)
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

func TestExportedLengthConstants(t *testing.T) {
	if store.MaxEmailLen != 254 || store.MaxLegalNameLen != 255 || store.MaxPhoneLen != 64 {
		t.Fatalf("unexpected caps: email=%d legal=%d phone=%d", store.MaxEmailLen, store.MaxLegalNameLen, store.MaxPhoneLen)
	}
}
