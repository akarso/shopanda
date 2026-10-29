package media

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	domainDownload "github.com/akarso/shopanda/internal/domain/download"
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

// assetAttachmentCounter reports how many downloadable products reference an
// asset (PR-1074). Delete fails closed when nil (same as cart). Count is
// defense-in-depth; the assets row is deleted before the blob so RESTRICT
// wins if a join still exists.
type assetAttachmentCounter interface {
	CountByAssetID(ctx context.Context, assetID string) (int, error)
}

// assetGrantCounter reports how many DownloadGrant rows reference an asset
// (PR-1075). Distinct from product attachments so operators see which gate
// blocked the delete.
type assetGrantCounter interface {
	CountByAssetID(ctx context.Context, assetID string) (int, error)
}

// TxStarter begins the transaction that wraps grant purge + live count +
// asset row delete (PR-1075). *sql.DB satisfies it.
type TxStarter interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// grantTXBinder binds grant purge/count into the same TX as assets.Delete.
// *download.Service satisfies it when the postgres grant repo supports WithTx.
type grantTXBinder interface {
	CountByAssetID(ctx context.Context, assetID string) (int, error)
	DeleteExpiredOrRevokedByAssetID(ctx context.Context, assetID string) (int, error)
	BindsTx() bool
	WithTxRepository(tx *sql.Tx) domainDownload.Repository
}

// Service orchestrates media use cases.
type Service struct {
	storage          domainMedia.Storage
	assets           domainMedia.AssetRepository
	assetAttachments assetAttachmentCounter
	assetGrants      assetGrantCounter
	txStarter        TxStarter
	bus              *event.Bus
	log              logger.Logger
	processor        domainMedia.ImageProcessor
	presets          []domainMedia.ThumbnailPreset
	webpEnabled      bool
	webpQuality      int
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

// SetWebPConfig enables or disables automatic WebP variant generation.
func (s *Service) SetWebPConfig(enabled bool, quality int) {
	s.webpEnabled = enabled
	if quality <= 0 {
		quality = 80
	} else if quality > 100 {
		quality = 100
	}
	s.webpQuality = quality
}

// WithAssetAttachments wires the attachment counter required by Delete
// (PR-1074). Nil fails closed — Delete will not remove the blob. Production
// wiring always sets the downloadable file repo. AssetRepo.Delete still maps
// 23503 when a concurrent join appears after the count check.
func (s *Service) WithAssetAttachments(c assetAttachmentCounter) *Service {
	s.assetAttachments = c
	return s
}

// WithAssetGrants wires the DownloadGrant counter for Delete (PR-1075).
// Nil fails closed — Delete will not remove the blob (same as attachments).
// Production wiring always sets the grant repo. AssetRepo.Delete maps
// download_grants FK 23503 distinctly from attachments.
func (s *Service) WithAssetGrants(c assetGrantCounter) *Service {
	s.assetGrants = c
	return s
}

// WithTxStarter wires the transaction used by Delete for grant purge +
// live count + asset row delete. Without it (or without TX-capable repos),
// Delete counts live grants first so a rejected delete does not erase
// expired/revoked history.
func (s *Service) WithTxStarter(starter TxStarter) *Service {
	if s == nil {
		return nil
	}
	s.txStarter = starter
	return s
}

// UploadInput holds the parameters for an upload.
type UploadInput struct {
	Filename string
	File     io.Reader
	// PrivateDownload stores the object under media.PrivateDownloadsPrefix
	// (private/downloads/…) so S3 Save applies private, no-store Cache-Control
	// and skips public ACL. Prefer this for downloadable grant targets (PR-1077).
	PrivateDownload bool
}

// UploadResult holds the result of an upload.
type UploadResult struct {
	Asset      domainMedia.Asset
	URL        string
	Thumbnails map[string]string // preset name → public URL
}

// AssetView represents an asset plus resolved public URLs.
type AssetView struct {
	Asset      domainMedia.Asset
	URL        string
	Thumbnails map[string]string
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
	if input.PrivateDownload {
		dir = domainMedia.PrivateDownloadsPrefix + assetID
	}
	path := dir + "/" + safeFilename

	if err := s.storage.Save(path, bytes.NewReader(fileBytes)); err != nil {
		return nil, fmt.Errorf("media: save file: %w", err)
	}

	// Generate thumbnails when a processor is configured.
	thumbPaths := make(map[string]string)
	if s.processor != nil && len(s.presets) > 0 {
		webpConvert := s.webpEnabled && (detected == "image/jpeg" || detected == "image/png")
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

			// Buffer resized bytes so the reader can be consumed by storage.Save.
			var resizedBuf bytes.Buffer
			if _, copyErr := io.Copy(&resizedBuf, resized); copyErr != nil {
				s.log.Warn("media: thumbnail buffer failed", map[string]interface{}{
					"preset": preset.Name,
					"error":  copyErr.Error(),
				})
				continue
			}
			resizedBytes := resizedBuf.Bytes()

			if saveErr := s.storage.Save(thumbPath, bytes.NewReader(resizedBytes)); saveErr != nil {
				s.log.Warn("media: thumbnail save failed", map[string]interface{}{
					"preset": preset.Name,
					"error":  saveErr.Error(),
				})
				continue
			}
			thumbPaths[preset.Name] = thumbPath

			// Generate WebP variant alongside the original-format thumbnail.
			if webpConvert {
				webpName := webpFilename(safeFilename)
				if webpName == safeFilename {
					// Skip — source already has .webp extension; paths would collide.
					continue
				}
				webpPath := dir + "/" + preset.Name + "/" + webpName
				webpData, webpErr := s.processor.Resize(bytes.NewReader(fileBytes), domainMedia.ResizeOpts{
					Width:    preset.Width,
					Height:   preset.Height,
					Fit:      preset.Fit,
					Quality:  s.webpQuality,
					MimeType: "image/webp",
				})
				if webpErr != nil {
					s.log.Warn("media: webp conversion failed", map[string]interface{}{
						"preset": preset.Name,
						"error":  webpErr.Error(),
					})
				} else if saveErr := s.storage.Save(webpPath, webpData); saveErr != nil {
					s.log.Warn("media: webp save failed", map[string]interface{}{
						"preset": preset.Name,
						"error":  saveErr.Error(),
					})
				} else {
					thumbPaths[preset.Name+"_webp"] = webpPath
				}
			}
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

// List returns a page of assets with resolved public URLs.
func (s *Service) List(ctx context.Context, offset, limit int) ([]AssetView, error) {
	assets, err := s.assets.List(ctx, offset, limit)
	if err != nil {
		return nil, err
	}
	views := make([]AssetView, 0, len(assets))
	for i := range assets {
		views = append(views, s.assetView(assets[i]))
	}
	return views, nil
}

// Delete removes an asset row first, then its stored files.
//
// Order is intentional (PR-1074 review): delete the assets row before the
// blob. ON DELETE RESTRICT on downloadable_files rejects the row delete when
// still attached; an orphan blob after a failed storage delete is recoverable.
// A missing join after a successful blob-first delete is not.
func (s *Service) Delete(ctx context.Context, assetID string) error {
	asset, err := s.assets.FindByID(ctx, assetID)
	if err != nil {
		return err
	}
	if asset == nil {
		return apperror.NotFound("asset not found")
	}

	if s.assetAttachments == nil {
		return apperror.Internal("media: asset attachment counter is not configured")
	}
	n, err := s.assetAttachments.CountByAssetID(ctx, asset.ID)
	if err != nil {
		return fmt.Errorf("media: count asset attachments: %w", err)
	}
	if n > 0 {
		return apperror.Validation("asset is still attached to a downloadable product")
	}
	if s.assetGrants == nil {
		return apperror.Internal("media: asset grant counter is not configured")
	}
	// Purge + live count + asset row delete: one TX when possible so a
	// live-grant rejection rolls back the expired/revoked purge.
	if err := s.deleteAssetRowPurgingGrants(ctx, asset.ID); err != nil {
		return err
	}

	// Unlisting is complete once the assets row is gone. Blob/thumbnail
	// storage errors are logged; do not 500 a successful unlist (orphan
	// blobs are recoverable). Always publish EventAssetDeleted after the
	// row delete so listeners see the asset as gone.
	if err := s.storage.Delete(asset.Path); err != nil {
		s.log.Warn("media: blob delete failed after asset row removed", map[string]interface{}{
			"asset_id": asset.ID,
			"path":     asset.Path,
			"error":    err.Error(),
		})
	}
	for name, path := range asset.Thumbnails {
		if err := s.storage.Delete(path); err != nil {
			s.log.Warn("media: thumbnail delete failed", map[string]interface{}{
				"asset_id": asset.ID,
				"preset":   name,
				"path":     path,
				"error":    err.Error(),
			})
		}
	}

	s.log.Info("media.asset.deleted", map[string]interface{}{
		"asset_id": asset.ID,
		"path":     asset.Path,
		"filename": asset.Filename,
	})

	if pubErr := s.bus.Publish(ctx, event.New(domainMedia.EventAssetDeleted, "media.service", domainMedia.AssetEventData{
		AssetID:  asset.ID,
		Path:     asset.Path,
		Filename: asset.Filename,
		MimeType: asset.MimeType,
	})); pubErr != nil {
		s.log.Warn("media: publish event failed", map[string]interface{}{
			"event":    domainMedia.EventAssetDeleted,
			"asset_id": asset.ID,
			"error":    pubErr.Error(),
		})
	}

	return nil
}

// deleteAssetRowPurgingGrants removes expired/revoked grant rows then the
// assets row. Preferred path: one TX (purge → live count → delete) so a
// live-grant rejection rolls the purge back. Fallback (no TX wiring): count
// live first so rejected deletes never erase grant history.
func (s *Service) deleteAssetRowPurgingGrants(ctx context.Context, assetID string) error {
	binder, canBind := s.assetGrants.(grantTXBinder)
	assetsTX, assetsBind := s.assets.(interface {
		WithTx(tx *sql.Tx) domainMedia.AssetRepository
	})
	if s.txStarter != nil && canBind && binder.BindsTx() && assetsBind {
		tx, err := s.txStarter.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("media: begin tx: %w", err)
		}
		defer tx.Rollback()

		grants := binder.WithTxRepository(tx)
		if grants == nil {
			return apperror.Internal("media: grant repository missing after WithTx")
		}
		if _, err := grants.DeleteExpiredOrRevokedByAssetID(ctx, assetID); err != nil {
			return fmt.Errorf("media: purge expired/revoked grants: %w", err)
		}
		gn, err := grants.CountByAssetID(ctx, assetID)
		if err != nil {
			return fmt.Errorf("media: count asset grants: %w", err)
		}
		if gn > 0 {
			return apperror.Validation("asset is referenced by a download grant")
		}
		if err := assetsTX.WithTx(tx).Delete(ctx, assetID); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("media: commit asset delete: %w", err)
		}
		return nil
	}

	// Fallback: refuse before purge when live grants exist.
	gn, err := s.assetGrants.CountByAssetID(ctx, assetID)
	if err != nil {
		return fmt.Errorf("media: count asset grants: %w", err)
	}
	if gn > 0 {
		return apperror.Validation("asset is referenced by a download grant")
	}
	if purger, ok := s.assetGrants.(interface {
		DeleteExpiredOrRevokedByAssetID(ctx context.Context, assetID string) (int, error)
	}); ok {
		if _, err := purger.DeleteExpiredOrRevokedByAssetID(ctx, assetID); err != nil {
			return fmt.Errorf("media: purge expired/revoked grants: %w", err)
		}
	}
	return s.assets.Delete(ctx, assetID)
}

func (s *Service) assetView(asset domainMedia.Asset) AssetView {
	thumbURLs := make(map[string]string, len(asset.Thumbnails))
	for name, path := range asset.Thumbnails {
		thumbURLs[name] = s.storage.URL(path)
	}
	return AssetView{
		Asset:      asset,
		URL:        s.storage.URL(asset.Path),
		Thumbnails: thumbURLs,
	}
}

// webpFilename replaces the file extension with .webp.
func webpFilename(filename string) string {
	ext := filepath.Ext(filename)
	if ext == "" {
		return filename + ".webp"
	}
	return filename[:len(filename)-len(ext)] + ".webp"
}
