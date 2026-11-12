// Package pluginwrite carries the in-process "trusted plugin write" marker.
// It lives in platform so the event bus can strip it by default.
// Not an entry point for plugins: use App.WithPluginWrite (plugins) or
// extensionapp.WithPluginWrite (core tests/services).
package pluginwrite

import (
	"context"
	"strings"
)

type key struct{}

type grant struct {
	registrant string
	codes      map[string]struct{}
}

// Grant marks ctx as a trusted write by registrant for the named field codes.
// Panics on a blank registrant, no codes or a blank code.
func Grant(ctx context.Context, registrant string, fieldCodes ...string) context.Context {
	registrant = strings.TrimSpace(registrant)
	if registrant == "" {
		panic("pluginwrite: plugin write registrant must not be empty")
	}
	if len(fieldCodes) == 0 {
		panic("pluginwrite: plugin write requires at least one field code")
	}
	codes := make(map[string]struct{}, len(fieldCodes))
	for _, code := range fieldCodes {
		code = strings.TrimSpace(code)
		if code == "" {
			panic("pluginwrite: plugin write field code must not be empty")
		}
		codes[code] = struct{}{}
	}
	return context.WithValue(ctx, key{}, grant{registrant: registrant, codes: codes})
}

// For returns the registrant trusted to write code, if any.
func For(ctx context.Context, code string) (string, bool) {
	g, ok := ctx.Value(key{}).(grant)
	if !ok {
		return "", false
	}
	if _, ok := g.codes[code]; !ok {
		return "", false
	}
	return g.registrant, true
}

// Strip returns ctx without any grant. Context values cannot be deleted, so it
// shadows the key with nil; For treats that as untrusted.
func Strip(ctx context.Context) context.Context {
	if _, ok := ctx.Value(key{}).(grant); !ok {
		return ctx
	}
	return context.WithValue(ctx, key{}, nil)
}
