package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	catalogApp "github.com/akarso/shopanda/internal/application/catalog"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	"github.com/akarso/shopanda/internal/application/admin"
	downloadApp "github.com/akarso/shopanda/internal/application/download"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domainDownload "github.com/akarso/shopanda/internal/domain/download"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// DownloadableFileAdminHandler lists and edits downloadable product → asset
// attachments and per-product grant settings (PR-1078).
type DownloadableFileAdminHandler struct {
	files     *catalogApp.DownloadableFileService
	downloads *downloadApp.Service
	assets    media.AssetRepository
	auditor   *admin.Auditor
}

// NewDownloadableFileAdminHandler creates a handler over the attachment and
// grant-settings services. assets enrich list rows; writes go through services.
func NewDownloadableFileAdminHandler(
	files *catalogApp.DownloadableFileService,
	downloads *downloadApp.Service,
	assets media.AssetRepository,
	auditor *admin.Auditor,
) *DownloadableFileAdminHandler {
	if files == nil {
		panic("http: downloadable file handler: file service must not be nil")
	}
	if downloads == nil {
		panic("http: downloadable file handler: download service must not be nil")
	}
	if assets == nil {
		panic("http: downloadable file handler: asset repository must not be nil")
	}
	if auditor == nil {
		panic("http: downloadable file handler: auditor must not be nil")
	}
	return &DownloadableFileAdminHandler{
		files:     files,
		downloads: downloads,
		assets:    assets,
		auditor:   auditor,
	}
}

type downloadableFileDTO struct {
	ProductID string    `json:"product_id"`
	AssetID   string    `json:"asset_id"`
	Position  int       `json:"position"`
	Label     string    `json:"label"`
	Filename  string    `json:"filename"`
	MimeType  string    `json:"mime_type"`
	CreatedAt time.Time `json:"created_at,omitzero"`
}

type attachDownloadableFileRequest struct {
	AssetID  string `json:"asset_id"`
	Position *int   `json:"position"`
	Label    string `json:"label"`
}

type replaceDownloadableFileRequest struct {
	OldAssetID     string  `json:"old_asset_id"`
	AssetID        string  `json:"asset_id"`
	Position       *int    `json:"position"`
	Label          *string `json:"label"`
	GlobalRetarget bool    `json:"global_retarget"`
}

type downloadableGrantSettingsDTO struct {
	ProductID     string `json:"product_id"`
	ExpiryDays    *int   `json:"expiry_days"`
	DownloadLimit *int   `json:"download_limit"`
}

type putDownloadableGrantSettingsRequest struct {
	ExpiryDays    *int `json:"expiry_days"`
	DownloadLimit *int `json:"download_limit"`
}

// ListFiles handles GET /api/v1/admin/products/{id}/downloadable-files.
// Successful GETs are not audited (writes only, like assignments).
func (h *DownloadableFileAdminHandler) ListFiles() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		productID := strings.TrimSpace(r.PathValue("id"))
		rows, err := h.files.List(r.Context(), productID)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		out := h.fileDTOsFrom(r.Context(), rows)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"files": out})
	}
}

// AttachFile handles POST /api/v1/admin/products/{id}/downloadable-files.
// Upserts position/label for the asset. Asset must be under private/downloads/.
func (h *DownloadableFileAdminHandler) AttachFile() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		productID := strings.TrimSpace(r.PathValue("id"))
		var req attachDownloadableFileRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			err = apperror.Validation("invalid request body")
			h.auditFile(r, admin.AuditDownloadableFileAttach, productID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		f, err := catalog.NewDownloadableFile(productID, req.AssetID)
		if err != nil {
			err = apperror.Validation(err.Error())
			h.auditFile(r, admin.AuditDownloadableFileAttach, productID, map[string]interface{}{
				"asset_id": strings.TrimSpace(req.AssetID),
			}, err)
			httpshared.JSONError(w, err)
			return
		}
		if req.Position != nil {
			f.Position = *req.Position
		}
		f.Label = req.Label
		if err := h.files.Attach(r.Context(), f); err != nil {
			h.auditFile(r, admin.AuditDownloadableFileAttach, productID, map[string]interface{}{
				"asset_id": f.AssetID,
			}, err)
			httpshared.JSONError(w, err)
			return
		}
		dto := h.fileDTOFrom(r.Context(), *f)
		h.auditFile(r, admin.AuditDownloadableFileAttach, productID, map[string]interface{}{
			"asset_id": dto.AssetID,
			"position": dto.Position,
		}, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"file": dto})
	}
}

// ReplaceFile handles PUT /api/v1/admin/products/{id}/downloadable-files.
// Swaps old_asset_id for asset_id in one service transaction and retargets
// grants by asset id (not product-scoped). Shared attachments refuse with
// 422 unless global_retarget=true (then every grant on the blob moves).
// Omitted position/label preserve the old join values.
func (h *DownloadableFileAdminHandler) ReplaceFile() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		productID := strings.TrimSpace(r.PathValue("id"))
		var req replaceDownloadableFileRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			err = apperror.Validation("invalid request body")
			h.auditFile(r, admin.AuditDownloadableFileReplace, productID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		f, err := catalog.NewDownloadableFile(productID, req.AssetID)
		if err != nil {
			err = apperror.Validation(err.Error())
			h.auditFile(r, admin.AuditDownloadableFileReplace, productID, map[string]interface{}{
				"old_asset_id": strings.TrimSpace(req.OldAssetID),
				"asset_id":     strings.TrimSpace(req.AssetID),
			}, err)
			httpshared.JSONError(w, err)
			return
		}
		meta := catalogApp.ReplaceMeta{
			Position:       req.Position,
			Label:          req.Label,
			GlobalRetarget: req.GlobalRetarget,
		}
		details := map[string]interface{}{
			"old_asset_id":    strings.TrimSpace(req.OldAssetID),
			"asset_id":        f.AssetID,
			"global_retarget": req.GlobalRetarget,
		}
		result, err := h.files.Replace(r.Context(), productID, req.OldAssetID, f, meta)
		if err != nil {
			h.auditFile(r, admin.AuditDownloadableFileReplace, productID, details, err)
			httpshared.JSONError(w, err)
			return
		}
		dto := h.fileDTOFrom(r.Context(), *f)
		details["grants_moved"] = result.GrantsMoved
		details["grants_revoked"] = result.GrantsRevoked
		h.auditFile(r, admin.AuditDownloadableFileReplace, productID, details, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"file":           dto,
			"grants_moved":   result.GrantsMoved,
			"grants_revoked": result.GrantsRevoked,
		})
	}
}

// RemoveFile handles DELETE /api/v1/admin/products/{id}/downloadable-files/{assetId}.
func (h *DownloadableFileAdminHandler) RemoveFile() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		productID := strings.TrimSpace(r.PathValue("id"))
		assetID := strings.TrimSpace(r.PathValue("assetId"))
		details := map[string]interface{}{"asset_id": assetID}
		if err := h.files.Remove(r.Context(), productID, assetID); err != nil {
			h.auditFile(r, admin.AuditDownloadableFileRemove, productID, details, err)
			httpshared.JSONError(w, err)
			return
		}
		h.auditFile(r, admin.AuditDownloadableFileRemove, productID, details, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"removed": true})
	}
}

// GetSettings handles GET /api/v1/admin/products/{id}/downloadable-grant-settings.
// Missing override returns null expiry/limit (global defaults apply).
// 404 when the product does not exist. Successful GETs are not audited.
func (h *DownloadableFileAdminHandler) GetSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		productID := strings.TrimSpace(r.PathValue("id"))
		row, err := h.downloads.GetSettings(r.Context(), productID)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"settings": settingsDTOFrom(productID, row),
		})
	}
}

// PutSettings handles PUT /api/v1/admin/products/{id}/downloadable-grant-settings.
func (h *DownloadableFileAdminHandler) PutSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		productID := strings.TrimSpace(r.PathValue("id"))
		var req putDownloadableGrantSettingsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			err = apperror.Validation("invalid request body")
			h.auditFile(r, admin.AuditDownloadableSettingsPut, productID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		settings := &domainDownload.GrantSettings{
			ProductID:     productID,
			ExpiryDays:    req.ExpiryDays,
			DownloadLimit: req.DownloadLimit,
		}
		if err := h.downloads.UpsertSettings(r.Context(), settings); err != nil {
			h.auditFile(r, admin.AuditDownloadableSettingsPut, productID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		h.auditFile(r, admin.AuditDownloadableSettingsPut, productID, map[string]interface{}{
			"expiry_days":    req.ExpiryDays,
			"download_limit": req.DownloadLimit,
		}, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"settings": settingsDTOFrom(productID, settings),
		})
	}
}

// DeleteSettings handles DELETE /api/v1/admin/products/{id}/downloadable-grant-settings.
func (h *DownloadableFileAdminHandler) DeleteSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		productID := strings.TrimSpace(r.PathValue("id"))
		if err := h.downloads.ClearSettings(r.Context(), productID); err != nil {
			h.auditFile(r, admin.AuditDownloadableSettingsDel, productID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		h.auditFile(r, admin.AuditDownloadableSettingsDel, productID, nil, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"removed": true})
	}
}

func settingsDTOFrom(productID string, row *domainDownload.GrantSettings) *downloadableGrantSettingsDTO {
	if row == nil {
		return &downloadableGrantSettingsDTO{ProductID: productID}
	}
	return &downloadableGrantSettingsDTO{
		ProductID:     row.ProductID,
		ExpiryDays:    row.ExpiryDays,
		DownloadLimit: row.DownloadLimit,
	}
}

func (h *DownloadableFileAdminHandler) fileDTOsFrom(ctx context.Context, rows []catalog.DownloadableFile) []downloadableFileDTO {
	out := make([]downloadableFileDTO, 0, len(rows))
	if len(rows) == 0 {
		return out
	}
	cache := make(map[string]*media.Asset, len(rows))
	for _, row := range rows {
		if _, ok := cache[row.AssetID]; ok {
			continue
		}
		asset, err := h.assets.FindByID(ctx, row.AssetID)
		if err != nil || asset == nil {
			cache[row.AssetID] = nil
			continue
		}
		cache[row.AssetID] = asset
	}
	for _, row := range rows {
		dto := downloadableFileDTO{
			ProductID: row.ProductID,
			AssetID:   row.AssetID,
			Position:  row.Position,
			Label:     row.Label,
			CreatedAt: row.CreatedAt,
		}
		if asset := cache[row.AssetID]; asset != nil {
			dto.Filename = asset.Filename
			dto.MimeType = asset.MimeType
		}
		out = append(out, dto)
	}
	return out
}

func (h *DownloadableFileAdminHandler) fileDTOFrom(ctx context.Context, row catalog.DownloadableFile) downloadableFileDTO {
	dtos := h.fileDTOsFrom(ctx, []catalog.DownloadableFile{row})
	if len(dtos) == 0 {
		return downloadableFileDTO{}
	}
	return dtos[0]
}

func (h *DownloadableFileAdminHandler) auditFile(r *http.Request, action admin.AuditAction, productID string, details map[string]interface{}, err error) {
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
		ResourceType: "product",
		ResourceID:   productID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}
