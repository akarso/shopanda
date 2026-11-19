package pluginsdk

import (
	"context"
	"io"

	"github.com/akarso/shopanda/internal/domain/media"
)

// MediaDisposition selects Content-Disposition on a signed GET.
type MediaDisposition = media.Disposition

const (
	MediaDispositionAttachment = media.DispositionAttachment
	MediaDispositionInline     = media.DispositionInline
)

// SignURLOptions configures a short-lived download or preview URL.
type SignURLOptions = media.SignURLOptions

// MediaIsNotFound reports whether err indicates a missing storage object.
func MediaIsNotFound(err error) bool {
	return media.IsNotFound(err)
}

// NormalizeMediaDisposition returns attachment when d is empty or unknown.
func NormalizeMediaDisposition(d MediaDisposition) MediaDisposition {
	return media.NormalizeDisposition(d)
}

// ReadableMediaStorage is the optional Read surface on a MediaPort backend.
type ReadableMediaStorage interface {
	Read(ctx context.Context, path string) ([]byte, error)
}

// SignedURLMediaStorage is the optional Head/SignedURL surface on a MediaPort.
type SignedURLMediaStorage interface {
	Head(ctx context.Context, path string) error
	SignedURL(ctx context.Context, path string, opts SignURLOptions) (string, error)
}

// MediaPort is the Host-visible media storage surface for CapMedia plugins.
// Optional Read / signed-URL capabilities are exposed via As* helpers so
// plugins do not import internal/domain/media.
type MediaPort interface {
	Name() string
	Save(path string, file io.Reader) error
	Delete(path string) error
	URL(path string) string
	AsReadableStorage() (ReadableMediaStorage, bool)
	AsSignedURLStorage() (SignedURLMediaStorage, bool)
}

type mediaPort struct {
	storage media.Storage
}

func newMediaPort(storage media.Storage) MediaPort {
	return &mediaPort{storage: storage}
}

func (p *mediaPort) Name() string { return p.storage.Name() }

func (p *mediaPort) Save(path string, file io.Reader) error {
	return p.storage.Save(path, file)
}

func (p *mediaPort) Delete(path string) error {
	return p.storage.Delete(path)
}

func (p *mediaPort) URL(path string) string {
	return p.storage.URL(path)
}

func (p *mediaPort) AsReadableStorage() (ReadableMediaStorage, bool) {
	r, ok := p.storage.(media.ReadableStorage)
	if !ok {
		return nil, false
	}
	return r, true
}

func (p *mediaPort) AsSignedURLStorage() (SignedURLMediaStorage, bool) {
	s, ok := media.AsSignedURLStorage(p.storage)
	if !ok {
		return nil, false
	}
	return s, true
}
