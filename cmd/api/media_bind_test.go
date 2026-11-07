package main

import (
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

type bindTestMedia struct{}

func (bindTestMedia) Name() string                 { return "bind-test" }
func (bindTestMedia) Save(string, io.Reader) error { return nil }
func (bindTestMedia) Delete(string) error          { return nil }
func (bindTestMedia) URL(string) string            { return "" }

var _ media.Storage = bindTestMedia{}

func TestResolveAndBindMediaStorage_UsesRegistered(t *testing.T) {
	app := &plugin.App{}
	registered := bindTestMedia{}
	app.RegisterMediaStorage(registered)
	got, err := resolveAndBindMediaStorage(app, &config.Config{
		Media: config.MediaConfig{Storage: "local"},
	})
	if err != nil {
		t.Fatalf("resolveAndBindMediaStorage: %v", err)
	}
	if got != registered {
		t.Fatal("must return plugin-registered storage")
	}
	bound, ok := app.BoundMediaStorage()
	if !ok || bound != registered {
		t.Fatalf("BoundMediaStorage = (%v, %v)", bound, ok)
	}
}
