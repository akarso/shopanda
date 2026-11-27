package mail_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/mail"
)

type overrideStub struct {
	// key: storeID|name|locale → subject, body
	rows map[string][2]string
	err  error
}

func (s *overrideStub) Get(_ context.Context, storeID, name, locale string) (string, string, error) {
	if s.err != nil {
		return "", "", s.err
	}
	key := storeID + "|" + name + "|" + locale
	row, ok := s.rows[key]
	if !ok {
		return "", "", mail.ErrTemplateNotFound
	}
	return row[0], row[1], nil
}

func TestRenderWithOverrides_DBWins(t *testing.T) {
	reg := mail.NewTemplates()
	reg.Register("order_confirmed", "File subject", "<p>file</p>")
	src := &overrideStub{rows: map[string][2]string{
		"s1|order_confirmed|": {"DB {{.Data.X}}", "<p>db-{{.Data.X}}</p>"},
	}}
	msg, err := mail.RenderWithOverrides(context.Background(), reg, src, "s1", "order_confirmed", "en", "a@b.com", mail.EmailData{
		Data: map[string]interface{}{"X": "ok"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "DB ok" || !strings.Contains(msg.Body, "db-ok") {
		t.Fatalf("got subject=%q body=%q", msg.Subject, msg.Body)
	}
}

func TestRenderWithOverrides_ExactBeatsEmpty(t *testing.T) {
	reg := mail.NewTemplates()
	reg.Register("order_confirmed", "File", "<p>file</p>")
	src := &overrideStub{rows: map[string][2]string{
		"s1|order_confirmed|":   {"Default", "<p>default</p>"},
		"s1|order_confirmed|de": {"German", "<p>de</p>"},
	}}
	msg, err := mail.RenderWithOverrides(context.Background(), reg, src, "s1", "order_confirmed", "de", "a@b.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "German" {
		t.Fatalf("subject = %q, want German", msg.Subject)
	}
}

func TestRenderWithOverrides_FallsBackToEmptyLocale(t *testing.T) {
	reg := mail.NewTemplates()
	reg.Register("order_confirmed", "File", "<p>file</p>")
	src := &overrideStub{rows: map[string][2]string{
		"s1|order_confirmed|": {"Default", "<p>default</p>"},
	}}
	msg, err := mail.RenderWithOverrides(context.Background(), reg, src, "s1", "order_confirmed", "fr", "a@b.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "Default" {
		t.Fatalf("subject = %q, want Default", msg.Subject)
	}
}

func TestRenderWithOverrides_DeleteReturnsToFile(t *testing.T) {
	reg := mail.NewTemplates()
	reg.Register("order_confirmed", "File subject", "<p>file-body</p>")
	src := &overrideStub{rows: map[string][2]string{}}
	msg, err := mail.RenderWithOverrides(context.Background(), reg, src, "s1", "order_confirmed", "en", "a@b.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "File subject" || !strings.Contains(msg.Body, "file-body") {
		t.Fatalf("got subject=%q body=%q", msg.Subject, msg.Body)
	}
}

func TestRenderWithOverrides_UnknownName(t *testing.T) {
	reg := mail.NewTemplates()
	src := &overrideStub{rows: map[string][2]string{}}
	_, err := mail.RenderWithOverrides(context.Background(), reg, src, "s1", "missing", "", "a@b.com", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRenderWithOverrides_BadDBBody(t *testing.T) {
	reg := mail.NewTemplates()
	reg.Register("order_confirmed", "File", "<p>file</p>")
	src := &overrideStub{rows: map[string][2]string{
		"s1|order_confirmed|": {"Subj", "{{.Broken"},
	}}
	_, err := mail.RenderWithOverrides(context.Background(), reg, src, "s1", "order_confirmed", "", "a@b.com", nil)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Fatalf("err = %v", err)
	}
}

func TestRenderWithOverrides_NilSrcUsesRegistry(t *testing.T) {
	reg := mail.NewTemplates()
	reg.Register("order_confirmed", "File", "<p>file</p>")
	msg, err := mail.RenderWithOverrides(context.Background(), reg, nil, "s1", "order_confirmed", "en", "a@b.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "File" {
		t.Fatalf("subject = %q", msg.Subject)
	}
}

func TestRenderWithOverrides_SourceError(t *testing.T) {
	reg := mail.NewTemplates()
	reg.Register("order_confirmed", "File", "<p>file</p>")
	boom := errors.New("db down")
	src := &overrideStub{err: boom}
	_, err := mail.RenderWithOverrides(context.Background(), reg, src, "s1", "order_confirmed", "", "a@b.com", nil)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestEmailTemplate_Validate(t *testing.T) {
	ok := &mail.EmailTemplate{StoreID: "s1", Name: "order_confirmed", Subject: "Hi", Body: "<p>x</p>"}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := &mail.EmailTemplate{Name: "order_confirmed"}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected store_id error")
	}
}
