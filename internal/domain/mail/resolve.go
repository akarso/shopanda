package mail

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"strings"
)

// overrideLocales returns the DB locale lookup chain for a render request.
// Exact non-empty locale first, then empty (store default language row).
func overrideLocales(locale string) []string {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		return []string{""}
	}
	return []string{locale, ""}
}

// RenderWithOverrides resolves DB → in-memory Templates (file/builtin).
// On a DB hit, subject/body are parsed and executed without mutating t.
// A parse/execute error from DB content is returned (no silent fallthrough).
// Nil src, empty storeID, or ErrTemplateNotFound for every locale falls back to t.Render.
func RenderWithOverrides(
	ctx context.Context,
	t *Templates,
	src OverrideSource,
	storeID, name, locale, to string,
	data interface{},
) (Message, error) {
	if t == nil {
		return Message{}, fmt.Errorf("mail: nil templates registry")
	}
	storeID = strings.TrimSpace(storeID)
	name = strings.TrimSpace(name)
	if name == "" {
		return Message{}, fmt.Errorf("mail: empty template name")
	}

	if src != nil && storeID != "" {
		for _, loc := range overrideLocales(locale) {
			subject, body, err := src.Get(ctx, storeID, name, loc)
			if errors.Is(err, ErrTemplateNotFound) {
				continue
			}
			if err != nil {
				return Message{}, fmt.Errorf("mail: override %q locale %q: %w", name, loc, err)
			}
			msg, err := renderParsed(name, to, subject, body, data)
			if err != nil {
				return Message{}, err
			}
			return msg, nil
		}
	}

	return t.Render(name, to, data)
}

func renderParsed(name, to, subject, body string, data interface{}) (Message, error) {
	tmpl, err := ParseNamed(name, subject, body)
	if err != nil {
		return Message{}, err
	}
	return executeNamed(tmpl, name, to, data)
}

func executeNamed(tmpl *template.Template, name, to string, data interface{}) (Message, error) {
	var subBuf bytes.Buffer
	subTmpl := tmpl.Lookup(name + ".subject")
	if subTmpl == nil {
		return Message{}, fmt.Errorf("mail: subject template missing for %q", name)
	}
	if err := subTmpl.Execute(&subBuf, data); err != nil {
		return Message{}, fmt.Errorf("mail: render subject %q: %w", name, err)
	}
	var bodyBuf bytes.Buffer
	if err := tmpl.Execute(&bodyBuf, data); err != nil {
		return Message{}, fmt.Errorf("mail: render body %q: %w", name, err)
	}
	return Message{
		To:      to,
		Subject: subBuf.String(),
		Body:    bodyBuf.String(),
	}, nil
}
