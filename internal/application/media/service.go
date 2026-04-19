package media

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

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
	storage   domainMedia.Storage
	assets    domainMedia.AssetRepository
	bus       *event.Bus
	log       logger.Logger
	processor domainMedia.ImageProcessor
	presets   []domainMedia.ThumbnailPreset
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

// SetImageProcessor configures optional thumbnail generation.
// When set, Upload will generate a resized thumbnail for each preset.
func (s *Service) SetImageProcessor(p domainMedia.ImageProcessor, presets []domainMedia.ThumbnailPreset) {
	s.processor = p
	s.presets = presets
}

// UploadInput holds the parameters for an upload.
type UploadInput struct {
	Filename string
	File     io.Reader
}

// UploadResult holds the result of an upload.
type UploadResult struct {
	Asset      domainMedia.Asset
	URL        string
	Thumbnails map[string]string // preset name → public URL
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
// When an ImageProcessor is configured, thumbnails are generated for each preset.
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

	// Buffer entire file: needed for both storage save and thumbnail generation.
	var fileBuf bytes.Buffer
	fileBuf.Write(buf[:n])
	if _, err := io.Copy(&fileBuf, input.File); err != nil {
		return nil, apperror.Validation("unable to read file content")
	}
	fileBytes := fileBuf.Bytes()
	fileSize := int64(len(fileBytes))

	// Sanitize filename: strip directory components, reject traversal attempts.
	safeFilename := filepath.Base(input.Filename)
	if safeFilename == "." || safeFilename == ".." || strings.ContainsAny(safeFilename, "/\\") {
		return nil, apperror.Validation("invalid filename")
	}

	assetID := id.New()
	dir := "uploads/" + assetID
	path := dir + "/" + safeFilename

	if err := s.storage.Save(path, bytes.NewReader(fileBytes)); err != nil {
		return nil, fmt.Errorf("media: save file: %w", err)
	}

	// Generate thumbnails when a processor is configured.
	thumbPaths := make(map[string]string)
	if s.processor != nil && len(s.presets) > 0 {
		for _, preset := range s.presets {
			thumbPath := dir + "/" + preset.Name + "/" + safeFilename
			resized, resizeErr := s.processor.Resize(bytes.NewReader(fileBytes), domainMedia.ResizeOpts{
				Width:    preset.Width,
				Height:   preset.Height,
				Fit:      preset.Fit,
				MimeType: detected,
			})
			if resizeErr != nil {
				s.log.Warn("media: thumbnail generation failed", map[string]interface{}{
					"preset": preset.Name,
					"error":  resizeErr.Error(),
				})
				continue
			}
			if saveErr := s.storage.Save(thumbPath, resized); saveErr != nil {
				s.log.Warn("media: thumbnail save failed", map[string]interface{}{
					"preset": preset.Name,
					"error":  saveErr.Error(),
				})
				continue
			}
			thumbPaths[preset.Name] = thumbPath
		}
	}

	asset, err := domainMedia.NewAsset(assetID, path, safeFilename, detected, fileSize)
	if err != nil {
		if delErr := s.storage.Delete(path); delErr != nil {
			s.log.Error("media: rollback delete failed", delErr, map[string]interface{}{"path": path})
		}
		for tName, tPath := range thumbPaths {
			if delErr := s.storage.Delete(tPath); delErr != nil {
				s.log.Error("media: rollback thumbnail delete failed", delErr, map[string]interface{}{"preset": tName, "path": tPath})
			}
		}
		return nil, fmt.Errorf("media: create asset: %w", err)
	}
	asset.Thumbnails = thumbPaths

	if err := s.assets.Save(ctx, &asset); err != nil {
		if delErr := s.storage.Delete(path); delErr != nil {
			s.log.Error("media: rollback delete failed", delErr, map[string]interface{}{"path": path})
		}
		for tName, tPath := range thumbPaths {
			if delErr := s.storage.Delete(tPath); delErr != nil {
				s.log.Error("media: rollback thumbnail delete failed", delErr, map[string]interface{}{"preset": tName, "path": tPath})
			}
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

	// Build thumbnail URLs from stored paths.
	thumbURLs := make(map[string]string, len(thumbPaths))
	for name, tp := range thumbPaths {
		thumbURLs[name] = s.storage.URL(tp)
	}

	return &UploadResult{
		Asset:      asset,
		URL:        s.storage.URL(asset.Path),
		Thumbnails: thumbURLs,
	}, nil
}
