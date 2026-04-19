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

		JSON(w, http.StatusCreated, assetResponse{
			ID:         result.Asset.ID,
			Path:       result.Asset.Path,
			Filename:   result.Asset.Filename,
			MimeType:   result.Asset.MimeType,
			Size:       result.Asset.Size,
			URL:        result.URL,
			Thumbnails: result.Thumbnails,
			CreatedAt:  result.Asset.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
}
