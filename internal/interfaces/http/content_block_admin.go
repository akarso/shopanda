package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/cms"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

// ContentBlockAdminHandler serves content block admin endpoints.
type ContentBlockAdminHandler struct {
	blocks  cms.ContentBlockRepository
	auditor *admin.Auditor
}

// NewContentBlockAdminHandler creates a ContentBlockAdminHandler.
func NewContentBlockAdminHandler(blocks cms.ContentBlockRepository, auditor *admin.Auditor) *ContentBlockAdminHandler {
	if blocks == nil {
		panic("ContentBlockAdminHandler: blocks repository must not be nil")
	}
	if auditor == nil {
		panic("ContentBlockAdminHandler: auditor must not be nil")
	}
	return &ContentBlockAdminHandler{blocks: blocks, auditor: auditor}
}

type adminContentBlockResponse struct {
	ID        string                 `json:"id"`
	Title     string                 `json:"title"`
	BlockType string                 `json:"block_type"`
	Config    map[string]interface{} `json:"config"`
	IsActive  bool                   `json:"is_active"`
	CreatedAt string                 `json:"created_at"`
	UpdatedAt string                 `json:"updated_at"`
}

type createContentBlockRequest struct {
	Title     string                 `json:"title"`
	BlockType string                 `json:"block_type"`
	Config    map[string]interface{} `json:"config"`
	IsActive  *bool                  `json:"is_active"`
}

type updateContentBlockRequest struct {
	Title    *string                `json:"title"`
	Config   map[string]interface{} `json:"config"`
	IsActive *bool                  `json:"is_active"`
}

type updateContentBlockTargetRequest struct {
	BlockIDs []string `json:"block_ids"`
}

func toAdminContentBlockResponse(block *cms.ContentBlock) adminContentBlockResponse {
	return adminContentBlockResponse{
		ID:        block.ID(),
		Title:     block.Title(),
		BlockType: string(block.BlockType()),
		Config:    block.Config(),
		IsActive:  block.IsActive(),
		CreatedAt: block.CreatedAt().UTC().Format(time.RFC3339),
		UpdatedAt: block.UpdatedAt().UTC().Format(time.RFC3339),
	}
}

func (h *ContentBlockAdminHandler) audit(r *http.Request, action admin.AuditAction, resourceID string, details map[string]interface{}, err error) {
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
		ResourceType: "content_block",
		ResourceID:   resourceID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}

// List handles GET /api/v1/admin/content-blocks.
func (h *ContentBlockAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := ParsePagination(r)
		if err != nil {
			JSONError(w, err)
			return
		}
		blocks, err := h.blocks.List(r.Context(), offset, limit)
		if err != nil {
			h.audit(r, admin.AuditContentBlockRead, "", nil, err)
			JSONError(w, err)
			return
		}
		result := make([]adminContentBlockResponse, 0, len(blocks))
		for _, block := range blocks {
			result = append(result, toAdminContentBlockResponse(block))
		}
		h.audit(r, admin.AuditContentBlockRead, "", map[string]interface{}{"count": len(result)}, nil)
		JSON(w, http.StatusOK, result)
	}
}

// Get handles GET /api/v1/admin/content-blocks/{id}.
func (h *ContentBlockAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		blockID := r.PathValue("id")
		if blockID == "" {
			JSONError(w, apperror.Validation("content block id is required"))
			return
		}
		block, err := h.blocks.FindByID(r.Context(), blockID)
		if err != nil {
			h.audit(r, admin.AuditContentBlockRead, blockID, nil, err)
			JSONError(w, err)
			return
		}
		if block == nil {
			h.audit(r, admin.AuditContentBlockRead, blockID, nil, apperror.NotFound("content block not found"))
			JSONError(w, apperror.NotFound("content block not found"))
			return
		}
		h.audit(r, admin.AuditContentBlockRead, blockID, map[string]interface{}{"block_type": block.BlockType()}, nil)
		JSON(w, http.StatusOK, toAdminContentBlockResponse(block))
	}
}

// Create handles POST /api/v1/admin/content-blocks.
func (h *ContentBlockAdminHandler) Create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createContentBlockRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid JSON body"))
			return
		}
		block, err := cms.NewContentBlock(id.New(), req.Title, cms.BlockType(req.BlockType), req.Config)
		if err != nil {
			h.audit(r, admin.AuditContentBlockCreate, "", nil, err)
			JSONError(w, apperror.Validation(err.Error()))
			return
		}
		if req.IsActive != nil {
			block.SetActive(*req.IsActive)
		}
		if err := h.blocks.Create(r.Context(), block); err != nil {
			h.audit(r, admin.AuditContentBlockCreate, block.ID(), map[string]interface{}{"block_type": block.BlockType()}, err)
			JSONError(w, err)
			return
		}
		h.audit(r, admin.AuditContentBlockCreate, block.ID(), map[string]interface{}{"block_type": block.BlockType()}, nil)
		JSON(w, http.StatusCreated, toAdminContentBlockResponse(block))
	}
}

// Update handles PUT /api/v1/admin/content-blocks/{id}.
func (h *ContentBlockAdminHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		blockID := r.PathValue("id")
		if blockID == "" {
			JSONError(w, apperror.Validation("content block id is required"))
			return
		}
		var req updateContentBlockRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid JSON body"))
			return
		}
		block, err := h.blocks.FindByID(r.Context(), blockID)
		if err != nil {
			h.audit(r, admin.AuditContentBlockUpdate, blockID, nil, err)
			JSONError(w, err)
			return
		}
		if block == nil {
			h.audit(r, admin.AuditContentBlockUpdate, blockID, nil, apperror.NotFound("content block not found"))
			JSONError(w, apperror.NotFound("content block not found"))
			return
		}
		if req.Title != nil {
			if err := block.SetTitle(*req.Title); err != nil {
				h.audit(r, admin.AuditContentBlockUpdate, blockID, nil, err)
				JSONError(w, apperror.Validation(err.Error()))
				return
			}
		}
		if req.Config != nil {
			if err := block.SetConfig(req.Config); err != nil {
				h.audit(r, admin.AuditContentBlockUpdate, blockID, nil, err)
				JSONError(w, apperror.Validation(err.Error()))
				return
			}
		}
		if req.IsActive != nil {
			block.SetActive(*req.IsActive)
		}
		if err := h.blocks.Update(r.Context(), block); err != nil {
			h.audit(r, admin.AuditContentBlockUpdate, blockID, map[string]interface{}{"block_type": block.BlockType()}, err)
			JSONError(w, err)
			return
		}
		h.audit(r, admin.AuditContentBlockUpdate, blockID, map[string]interface{}{"block_type": block.BlockType()}, nil)
		JSON(w, http.StatusOK, toAdminContentBlockResponse(block))
	}
}

// Delete handles DELETE /api/v1/admin/content-blocks/{id}.
func (h *ContentBlockAdminHandler) Delete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		blockID := r.PathValue("id")
		if blockID == "" {
			JSONError(w, apperror.Validation("content block id is required"))
			return
		}
		if err := h.blocks.Delete(r.Context(), blockID); err != nil {
			h.audit(r, admin.AuditContentBlockDelete, blockID, nil, err)
			JSONError(w, err)
			return
		}
		h.audit(r, admin.AuditContentBlockDelete, blockID, nil, nil)
		JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// GetTarget handles GET /api/v1/admin/content-block-targets/{targetType}/{targetKey}.
func (h *ContentBlockAdminHandler) GetTarget() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetType, targetKey, err := parseContentBlockTarget(r)
		if err != nil {
			JSONError(w, err)
			return
		}
		blocks, err := h.blocks.FindBlocksByTarget(r.Context(), targetType, targetKey)
		if err != nil {
			h.audit(r, admin.AuditContentBlockRead, "", map[string]interface{}{
				"target_type": targetType,
				"target_key":  targetKey,
			}, err)
			JSONError(w, err)
			return
		}
		result := make([]adminContentBlockResponse, 0, len(blocks))
		for _, block := range blocks {
			result = append(result, toAdminContentBlockResponse(block))
		}
		h.audit(r, admin.AuditContentBlockRead, "", map[string]interface{}{
			"target_type": targetType,
			"target_key":  targetKey,
			"count":       len(result),
		}, nil)
		JSON(w, http.StatusOK, map[string]interface{}{
			"target_type": string(targetType),
			"target_key":  targetKey,
			"blocks":      result,
		})
	}
}

// UpdateTarget handles PUT /api/v1/admin/content-block-targets/{targetType}/{targetKey}.
func (h *ContentBlockAdminHandler) UpdateTarget() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetType, targetKey, err := parseContentBlockTarget(r)
		if err != nil {
			JSONError(w, err)
			return
		}
		var req updateContentBlockTargetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid JSON body"))
			return
		}
		if err := h.blocks.SaveTargetPlacements(r.Context(), targetType, targetKey, req.BlockIDs); err != nil {
			h.audit(r, admin.AuditContentBlockUpdate, "", map[string]interface{}{
				"target_type": targetType,
				"target_key":  targetKey,
			}, err)
			JSONError(w, err)
			return
		}
		blocks, err := h.blocks.FindBlocksByTarget(r.Context(), targetType, targetKey)
		if err != nil {
			h.audit(r, admin.AuditContentBlockUpdate, "", map[string]interface{}{
				"target_type": targetType,
				"target_key":  targetKey,
			}, err)
			JSONError(w, err)
			return
		}
		result := make([]adminContentBlockResponse, 0, len(blocks))
		for _, block := range blocks {
			result = append(result, toAdminContentBlockResponse(block))
		}
		h.audit(r, admin.AuditContentBlockUpdate, "", map[string]interface{}{
			"target_type": targetType,
			"target_key":  targetKey,
			"count":       len(result),
		}, nil)
		JSON(w, http.StatusOK, map[string]interface{}{
			"target_type": string(targetType),
			"target_key":  targetKey,
			"blocks":      result,
		})
	}
}

func parseContentBlockTarget(r *http.Request) (cms.TargetType, string, error) {
	targetType := cms.TargetType(r.PathValue("targetType"))
	targetKey := cms.NormalizeTargetKey(r.PathValue("targetKey"))
	if targetKey == "" {
		return "", "", apperror.Validation("target key is required")
	}
	if !cms.ValidTargetType(targetType) {
		return "", "", apperror.Validation("invalid target type")
	}
	if targetType == cms.TargetTypeLayout && !cms.ValidLayoutTarget(targetKey) {
		return "", "", apperror.Validation("invalid layout target")
	}
	return targetType, targetKey, nil
}
