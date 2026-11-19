package pluginsdk_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

type stubMediaStorage struct {
	name string
}

func (s stubMediaStorage) Name() string               { return s.name }
func (stubMediaStorage) Save(string, io.Reader) error { return nil }
func (stubMediaStorage) Delete(string) error          { return nil }
func (stubMediaStorage) URL(path string) string       { return "https://example/" + path }

type stubReadableMedia struct {
	stubMediaStorage
	body []byte
}

func (s stubReadableMedia) Read(context.Context, string) ([]byte, error) {
	return s.body, nil
}

type stubSignedMedia struct {
	stubMediaStorage
}

func (stubSignedMedia) Head(context.Context, string) error { return nil }
func (stubSignedMedia) SignedURL(context.Context, string, media.SignURLOptions) (string, error) {
	return "https://signed.example/x", nil
}

func TestAsHostFor_Media_GatedByCap(t *testing.T) {
	app := &plugin.App{}
	if err := app.BindMediaStorage(stubReadableMedia{
		stubMediaStorage: stubMediaStorage{name: "local"},
		body:             []byte("pdf"),
	}); err != nil {
		t.Fatal(err)
	}

	denied := pluginsdk.AsHostFor(app, "acme/media")
	if _, ok := denied.Media(); ok {
		t.Fatal("expected Media() denied without Require(CapMedia)")
	}

	h := pluginsdk.AsHostFor(app, "acme/media")
	h.Require(pluginsdk.CapMedia)
	mp, ok := h.Media()
	if !ok || mp == nil {
		t.Fatal("expected Media() after Require(CapMedia)")
	}
	if mp.Name() != "local" {
		t.Fatalf("Name = %q", mp.Name())
	}
	if err := mp.Save("a.bin", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatal(err)
	}
	readable, ok := mp.AsReadableStorage()
	if !ok {
		t.Fatal("expected AsReadableStorage")
	}
	got, err := readable.Read(context.Background(), "a.bin")
	if err != nil || string(got) != "pdf" {
		t.Fatalf("Read = (%q, %v)", got, err)
	}
	if _, ok := mp.AsSignedURLStorage(); ok {
		t.Fatal("plain readable must not report SignedURL")
	}
}

func TestMediaPort_SignedURL(t *testing.T) {
	app := &plugin.App{}
	if err := app.BindMediaStorage(stubSignedMedia{stubMediaStorage: stubMediaStorage{name: "s3"}}); err != nil {
		t.Fatal(err)
	}
	h := pluginsdk.AsHostFor(app, "acme/media")
	h.Require(pluginsdk.CapMedia)
	mp, ok := h.Media()
	if !ok {
		t.Fatal("Media()")
	}
	signed, ok := mp.AsSignedURLStorage()
	if !ok {
		t.Fatal("AsSignedURLStorage")
	}
	url, err := signed.SignedURL(context.Background(), "p", pluginsdk.SignURLOptions{
		Disposition: pluginsdk.MediaDispositionInline,
	})
	if err != nil || url == "" {
		t.Fatalf("SignedURL = (%q, %v)", url, err)
	}
}

func TestBindMediaStorage_SatisfiesCapMedia(t *testing.T) {
	app := &plugin.App{}
	app.RecordNeeds(plugin.CapMedia)
	if err := app.AssertNeedsMet(); err == nil {
		t.Fatal("expected unbound CapMedia error")
	}
	if err := app.BindMediaStorage(stubMediaStorage{name: "local"}); err != nil {
		t.Fatal(err)
	}
	if err := app.AssertNeedsMet(); err != nil {
		t.Fatalf("AssertNeedsMet after BindMediaStorage: %v", err)
	}
}
