package media

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	domainMedia "github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// AllowedMimeTypes lists the file types accepted for upload.
var AllowedMimeTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// Service orchestrates media use cases.
type Service struct {
	storage domainMedia.Storage
	assets  domainMedia.AssetRepository
	bus     *event.Bus
	log     logger.Logger
}

// NewService creates a media application service.
func NewService(
	storage domainMedia.Storage,
	assets domainMedia.AssetRepository,
	bus *event.Bus,
	log logger.Logger,
) *Service {
	if storage == nil {
		panic("media.NewService: nil storage")
	}
	if assets == nil {
		panic("media.NewService: nil assets")
	}
	if bus == nil {
		panic("media.NewService: nil bus")
	}
	if log == nil {
		panic("media.NewService: nil log")
	}
	return &Service{storage: storage, assets: assets, bus: bus, log: log}
}

// UploadInput holds the parameters for an upload.
type UploadInput struct {
	Filename string
	File     io.Reader
}

// UploadResult holds the result of an upload.
type UploadResult struct {
	Asset domainMedia.Asset
	URL   string
}

// sniffSize is the number of bytes read for MIME detection.
const sniffSize = 512

// countingReader wraps an io.Reader and counts bytes read.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// Upload stores a file and creates an asset record.
func (s *Service) Upload(ctx context.Context, input UploadInput) (*UploadResult, error) {
	// Sniff actual MIME type from file content instead of trusting client header.
	buf := make([]byte, sniffSize)
	n, err := io.ReadAtLeast(input.File, buf, 1)
	if err != nil {
		return nil, apperror.Validation("unable to read file content")
	}
	detected := http.DetectContentType(buf[:n])
	if !AllowedMimeTypes[detected] {
		return nil, apperror.Validation(fmt.Sprintf("unsupported file type: %s", detected))
	}
	// Reassemble the stream so storage.Save receives the full content.
	cr := &countingReader{r: io.MultiReader(bytes.NewReader(buf[:n]), input.File)}

	assetID := id.New()
	path := "uploads/" + assetID + "/" + input.Filename

	if err := s.storage.Save(path, cr); err != nil {
		return nil, fmt.Errorf("media: save file: %w", err)
	}

	asset, err := domainMedia.NewAsset(assetID, path, input.Filename, detected, cr.n)
	if err != nil {
		if delErr := s.storage.Delete(path); delErr != nil {
			s.log.Error("media: rollback delete failed", delErr, map[string]interface{}{"path": path})
		}
		return nil, fmt.Errorf("media: create asset: %w", err)
	}

	if err := s.assets.Save(ctx, &asset); err != nil {
		if delErr := s.storage.Delete(path); delErr != nil {
			s.log.Error("media: rollback delete failed", delErr, map[string]interface{}{"path": path})
		}
		return nil, fmt.Errorf("media: persist asset: %w", err)
	}

	s.log.Info("media.asset.uploaded", map[string]interface{}{
		"asset_id": asset.ID,
		"path":     asset.Path,
		"filename": asset.Filename,
		"size":     asset.Size,
	})

	if pubErr := s.bus.Publish(ctx, event.New(domainMedia.EventAssetUploaded, "media.service", domainMedia.AssetEventData{
		AssetID:  asset.ID,
		Path:     asset.Path,
		Filename: asset.Filename,
		MimeType: asset.MimeType,
	})); pubErr != nil {
		s.log.Warn("media: publish event failed", map[string]interface{}{
			"event":    domainMedia.EventAssetUploaded,
			"asset_id": asset.ID,
			"error":    pubErr.Error(),
		})
	}

	return &UploadResult{
		Asset: asset,
		URL:   s.storage.URL(asset.Path),
	}, nil
}
