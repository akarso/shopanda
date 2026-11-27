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

// RenderResult is a rendered message plus which layer supplied it.
type RenderResult struct {
	Message        Message
	FromOverride   bool
	ResolvedLocale string // DB locale key when FromOverride; empty for registry
	Source         string // "db", OriginFile, or OriginBuiltin
	Fallback       bool   // true when a non-empty request fell through to empty locale or registry
}

// RenderOption configures RenderWithOverrides.
type RenderOption func(*renderOptions)

type renderOptions struct {
	exactLocale bool
}

// WithExactLocale skips the empty-locale fallback (exact key or registry only).
func WithExactLocale() RenderOption {
	return func(o *renderOptions) { o.exactLocale = true }
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
	opts ...RenderOption,
) (RenderResult, error) {
	var o renderOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	if t == nil {
		return RenderResult{}, fmt.Errorf("mail: nil templates registry")
	}
	storeID = strings.TrimSpace(storeID)
	name = strings.TrimSpace(name)
	if name == "" {
		return RenderResult{}, fmt.Errorf("mail: empty template name")
	}
	requested := CanonicalLocale(locale)

	if src != nil && storeID != "" && IsOverridableName(name) {
		chain := overrideLocales(locale)
		if o.exactLocale {
			chain = []string{requested}
		}
		for _, loc := range chain {
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
			return RenderResult{
				Message:        msg,
				FromOverride:   true,
				ResolvedLocale: loc,
				Source:         "db",
				Fallback:       requested != "" && loc != requested,
			}, nil
		}
	}

	msg, err := t.Render(name, to, data)
	if err != nil {
		return RenderResult{}, err
	}
	origin := OriginBuiltin
	if srcInfo, ok := t.Source(name); ok && srcInfo.Origin != "" {
		origin = srcInfo.Origin
	}
	return RenderResult{
		Message:      msg,
		FromOverride: false,
		Source:       origin,
		// Requested a locale, tried DB for an overridable name, landed on registry.
		Fallback: requested != "" && src != nil && storeID != "" && IsOverridableName(name),
	}, nil
}

// ValidateRenderable parses subject/body and executes them with data.
// Rejects executed subjects that contain CR/LF (SMTP header injection).
func ValidateRenderable(name, subject, body string, data interface{}) error {
	parts, err := ParseNamed(name, subject, body)
	if err != nil {
		return err
	}
	_, err = executeNamed(parts, name, "validate@example.com", data)
	return err
}
