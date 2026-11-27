package mail

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// overrideLocales returns the DB locale lookup chain for a render request.
// Exact non-empty (canonical) locale first, then empty (store default language row).
func overrideLocales(locale string) []string {
	locale = CanonicalLocale(locale)
	if locale == "" {
		return []string{""}
	}
	return []string{locale, ""}
}

// RenderResult is a rendered message plus whether a DB override supplied it.
type RenderResult struct {
	Message    Message
	FromOverride bool
}

// RenderWithOverrides resolves DB → in-memory Templates (file/builtin).
// On a DB hit, subject/body are parsed and executed without mutating t.
// A parse/execute error from DB content is returned (no silent fallthrough).
// Non-overridable names, nil src, empty storeID, or ErrTemplateNotFound for
// every locale falls back to t.Render (FromOverride=false).
func RenderWithOverrides(
	ctx context.Context,
	t *Templates,
	src OverrideSource,
	storeID, name, locale, to string,
	data interface{},
) (RenderResult, error) {
	if t == nil {
		return RenderResult{}, fmt.Errorf("mail: nil templates registry")
	}
	storeID = strings.TrimSpace(storeID)
	name = strings.TrimSpace(name)
	if name == "" {
		return RenderResult{}, fmt.Errorf("mail: empty template name")
	}

	if src != nil && storeID != "" && IsOverridableName(name) {
		for _, loc := range overrideLocales(locale) {
			subject, body, err := src.Get(ctx, storeID, name, loc)
			if errors.Is(err, ErrTemplateNotFound) {
				continue
			}
			if err != nil {
				return RenderResult{}, fmt.Errorf("mail: override %q locale %q: %w", name, loc, err)
			}
			parts, err := ParseNamed(name, subject, body)
			if err != nil {
				return RenderResult{}, err
			}
			msg, err := executeNamed(parts, name, to, data)
			if err != nil {
				return RenderResult{}, err
			}
			return RenderResult{Message: msg, FromOverride: true}, nil
		}
	}

	msg, err := t.Render(name, to, data)
	if err != nil {
		return RenderResult{}, err
	}
	return RenderResult{Message: msg, FromOverride: false}, nil
}
