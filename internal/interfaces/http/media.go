package http

import (
	"errors"
	"net/http"
	"path/filepath"

	mediaApp "github.com/akarso/shopanda/internal/application/media"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

const maxUploadSize = 10 << 20 // 10 MB

// MediaHandler handles media endpoints.
type MediaHandler struct {
	svc *mediaApp.Service
}

// NewMediaHandler creates a MediaHandler.
func NewMediaHandler(svc *mediaApp.Service) *MediaHandler {
	return &MediaHandler{svc: svc}
}

type assetResponse struct {
	ID         string            `json:"id"`
	Path       string            `json:"path"`
	Filename   string            `json:"filename"`
	MimeType   string            `json:"mime_type"`
	Size       int64             `json:"size"`
	URL        string            `json:"url"`
	Thumbnails map[string]string `json:"thumbnails,omitempty"`
	CreatedAt  string            `json:"created_at"`
}

func toAssetResponse(view mediaApp.AssetView) assetResponse {
	return assetResponse{
		ID:         view.Asset.ID,
		Path:       view.Asset.Path,
		Filename:   view.Asset.Filename,
		MimeType:   view.Asset.MimeType,
		Size:       view.Asset.Size,
		URL:        view.URL,
		Thumbnails: view.Thumbnails,
		CreatedAt:  view.Asset.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// List returns a handler for GET /api/v1/admin/media.
func (h *MediaHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := parsePagination(r)
		if err != nil {
			JSONError(w, err)
			return
		}

		assets, err := h.svc.List(r.Context(), offset, limit)
		if err != nil {
			JSONError(w, err)
			return
		}

		out := make([]assetResponse, 0, len(assets))
		for i := range assets {
			out = append(out, toAssetResponse(assets[i]))
		}

		JSON(w, http.StatusOK, map[string]interface{}{"assets": out})
	}
}

// Upload returns a handler for POST /api/v1/admin/media/upload.
func (h *MediaHandler) Upload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

		file, header, err := r.FormFile("file")
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				JSONError(w, apperror.Validation("file exceeds maximum upload size of 10MB"))
				return
			}
			JSONError(w, apperror.Validation("file is required"))
			return
		}
		defer file.Close()

		filename := filepath.Base(header.Filename)
		if filename == "." || filename == "/" {
			JSONError(w, apperror.Validation("invalid filename"))
			return
		}

		result, err := h.svc.Upload(r.Context(), mediaApp.UploadInput{
			Filename: filename,
			File:     file,
		})
		if err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusCreated, toAssetResponse(mediaApp.AssetView{
			Asset:      result.Asset,
			URL:        result.URL,
			Thumbnails: result.Thumbnails,
		}))
	}
}

// Delete returns a handler for DELETE /api/v1/admin/media/{assetId}.
func (h *MediaHandler) Delete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		assetID := r.PathValue("assetId")
		if assetID == "" {
			JSONError(w, apperror.Validation("asset id is required"))
			return
		}
		if err := h.svc.Delete(r.Context(), assetID); err != nil {
			JSONError(w, err)
			return
		}
		JSON(w, http.StatusOK, map[string]interface{}{"deleted": true, "asset_id": assetID})
	}
}
