package http

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/akarso/shopanda/internal/application/admin"
	mediaApp "github.com/akarso/shopanda/internal/application/media"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
)

const maxUploadSize = config.DefaultHTTPMediaMaxBodyBytes // default; overridable via SetMaxUploadBytes

// MediaHandler handles media endpoints.
type MediaHandler struct {
	svc            *mediaApp.Service
	auditor        *admin.Auditor
	maxUploadBytes int64
}

// NewMediaHandler creates a MediaHandler with a default auditor.
func NewMediaHandler(svc *mediaApp.Service) *MediaHandler {
	return NewMediaHandlerWithAuditor(svc, admin.NewAuditor(logger.New("info")))
}

// NewMediaHandlerWithAuditor creates a MediaHandler with a custom auditor.
func NewMediaHandlerWithAuditor(svc *mediaApp.Service, auditor *admin.Auditor) *MediaHandler {
	if auditor == nil {
		panic("MediaHandler: auditor must not be nil")
	}
	return &MediaHandler{svc: svc, auditor: auditor, maxUploadBytes: maxUploadSize}
}

// SetMaxUploadBytes overrides the multipart upload cap (bytes). Non-positive keeps the default.
func (h *MediaHandler) SetMaxUploadBytes(n int64) {
	if n > 0 {
		h.maxUploadBytes = n
	}
}

func (h *MediaHandler) audit(r *http.Request, action admin.AuditAction, resourceID string, details map[string]interface{}, err error) {
	merged := mergeAuditDetails(details, fullAdminScopeDetailsFromRequest(r))
	result := "success"
	errMsg := ""
	if err != nil {
		result = "error"
		errMsg = err.Error()
	}
	h.auditor.LogAction(r.Context(), admin.AuditEntry{
		AdminID:      adminIDFromRequest(r),
		Action:       action,
		ResourceType: "media",
		ResourceID:   resourceID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
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
		limit := h.maxUploadBytes
		if limit <= 0 {
			limit = maxUploadSize
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)

		file, header, err := r.FormFile("file")
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				msg := fmt.Sprintf("file exceeds maximum upload size of %d bytes", limit)
				verr := apperror.Validation(msg)
				h.audit(r, admin.AuditMediaUpload, "", nil, verr)
				JSONError(w, verr)
				return
			}
			verr := apperror.Validation("file is required")
			h.audit(r, admin.AuditMediaUpload, "", nil, verr)
			JSONError(w, verr)
			return
		}
		defer file.Close()

		filename := filepath.Base(header.Filename)
		if filename == "." || filename == "/" {
			verr := apperror.Validation("invalid filename")
			h.audit(r, admin.AuditMediaUpload, "", nil, verr)
			JSONError(w, verr)
			return
		}

		result, err := h.svc.Upload(r.Context(), mediaApp.UploadInput{
			Filename: filename,
			File:     file,
		})
		if err != nil {
			h.audit(r, admin.AuditMediaUpload, "", map[string]interface{}{"filename": filename}, err)
			JSONError(w, err)
			return
		}

		h.audit(r, admin.AuditMediaUpload, result.Asset.ID, map[string]interface{}{"filename": filename}, nil)

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
			verr := apperror.Validation("asset id is required")
			h.audit(r, admin.AuditMediaDelete, "", nil, verr)
			JSONError(w, verr)
			return
		}
		if err := h.svc.Delete(r.Context(), assetID); err != nil {
			h.audit(r, admin.AuditMediaDelete, assetID, nil, err)
			JSONError(w, err)
			return
		}
		h.audit(r, admin.AuditMediaDelete, assetID, nil, nil)
		JSON(w, http.StatusOK, map[string]interface{}{"deleted": true, "asset_id": assetID})
	}
}
