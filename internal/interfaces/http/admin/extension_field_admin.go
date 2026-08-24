package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	"github.com/akarso/shopanda/internal/application/admin"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// ExtensionFieldAdminHandler serves extension field definition admin endpoints.
type ExtensionFieldAdminHandler struct {
	service *extensionapp.FieldService
	auditor *admin.Auditor
}

// NewExtensionFieldAdminHandler creates an ExtensionFieldAdminHandler with a default auditor.
func NewExtensionFieldAdminHandler(service *extensionapp.FieldService) *ExtensionFieldAdminHandler {
	return NewExtensionFieldAdminHandlerWithAuditor(service, admin.NewAuditor(logger.New("info")))
}

// NewExtensionFieldAdminHandlerWithAuditor creates an ExtensionFieldAdminHandler with a custom auditor.
func NewExtensionFieldAdminHandlerWithAuditor(service *extensionapp.FieldService, auditor *admin.Auditor) *ExtensionFieldAdminHandler {
	if service == nil {
		panic("ExtensionFieldAdminHandler: field service must not be nil")
	}
	if auditor == nil {
		panic("ExtensionFieldAdminHandler: auditor must not be nil")
	}
	return &ExtensionFieldAdminHandler{service: service, auditor: auditor}
}

type extensionFieldAccessRequest struct {
	ReadRoles       []string `json:"read_roles,omitempty"`
	WriteRoles      []string `json:"write_roles,omitempty"`
	PluginOnlyWrite bool     `json:"plugin_only_write,omitempty"`
}

type extensionFieldValidationRequest struct {
	Required bool     `json:"required,omitempty"`
	Min      *int64   `json:"min,omitempty"`
	Max      *int64   `json:"max,omitempty"`
	Regex    string   `json:"regex,omitempty"`
	Options  []string `json:"options,omitempty"`
}

type extensionFieldRequestBody struct {
	Label       string                          `json:"label"`
	Description string                          `json:"description,omitempty"`
	Type        string                          `json:"type"`
	Scope       string                          `json:"scope"`
	TargetType  string                          `json:"target_type,omitempty"`
	StorageMode string                          `json:"storage_mode,omitempty"`
	Visibility  string                          `json:"visibility,omitempty"`
	Access      extensionFieldAccessRequest     `json:"access"`
	Validation  extensionFieldValidationRequest `json:"validation"`
}

type createExtensionFieldRequest struct {
	Code string `json:"code"`
	extensionFieldRequestBody
}

type updateExtensionFieldRequest struct {
	extensionFieldRequestBody
}

type extensionFieldResponse struct {
	Code        string                          `json:"code"`
	Label       string                          `json:"label"`
	Description string                          `json:"description,omitempty"`
	Type        string                          `json:"type"`
	Scope       string                          `json:"scope"`
	StorageMode string                          `json:"storage_mode"`
	Visibility  string                          `json:"visibility"`
	Access      extensionFieldAccessRequest     `json:"access"`
	Validation  extensionFieldValidationRequest `json:"validation"`
}

func (h *ExtensionFieldAdminHandler) auditField(r *http.Request, action admin.AuditAction, resourceID string, details map[string]interface{}, err error) {
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
		ResourceType: "extension_field",
		ResourceID:   resourceID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}

func toExtensionFieldResponse(field domainext.ExtensionField) extensionFieldResponse {
	return extensionFieldResponse{
		Code:        field.Code,
		Label:       field.Label,
		Description: field.Description,
		Type:        string(field.Type),
		Scope:       string(field.Scope),
		StorageMode: string(field.StorageMode),
		Visibility:  string(field.Visibility),
		Access: extensionFieldAccessRequest{
			ReadRoles:       field.Access.ReadRoles,
			WriteRoles:      field.Access.WriteRoles,
			PluginOnlyWrite: field.Access.PluginOnlyWrite,
		},
		Validation: extensionFieldValidationRequest{
			Required: field.Validation.Required,
			Min:      field.Validation.Min,
			Max:      field.Validation.Max,
			Regex:    field.Validation.Regex,
			Options:  field.Validation.Options,
		},
	}
}

func fieldDefFromRequestBody(code string, body extensionFieldRequestBody) domainext.FieldDef {
	scope := resolveScopeFromRequest(body.Scope, body.TargetType)
	return domainext.FieldDef{
		Code:        strings.TrimSpace(code),
		Label:       strings.TrimSpace(body.Label),
		Description: strings.TrimSpace(body.Description),
		Type:        domainext.FieldType(strings.TrimSpace(body.Type)),
		Scope:       scope,
		StorageMode: domainext.StorageMode(strings.TrimSpace(body.StorageMode)),
		Visibility:  domainext.Visibility(strings.TrimSpace(body.Visibility)),
		Access: domainext.Access{
			ReadRoles:       body.Access.ReadRoles,
			WriteRoles:      body.Access.WriteRoles,
			PluginOnlyWrite: body.Access.PluginOnlyWrite,
		},
		Validation: domainext.Validation{
			Required: body.Validation.Required,
			Min:      body.Validation.Min,
			Max:      body.Validation.Max,
			Regex:    strings.TrimSpace(body.Validation.Regex),
			Options:  body.Validation.Options,
		},
	}
}

func fieldDefFromCreateRequest(req createExtensionFieldRequest) domainext.FieldDef {
	return fieldDefFromRequestBody(req.Code, req.extensionFieldRequestBody)
}

func fieldDefFromUpdateRequest(req updateExtensionFieldRequest, code string) domainext.FieldDef {
	return fieldDefFromRequestBody(code, req.extensionFieldRequestBody)
}

func resolveScopeFromRequest(scope, targetType string) domainext.TargetType {
	if s := strings.TrimSpace(targetType); s != "" {
		return domainext.TargetType(s)
	}
	return domainext.TargetType(strings.TrimSpace(scope))
}

func resolveScopeQuery(r *http.Request) domainext.TargetType {
	if s := strings.TrimSpace(r.URL.Query().Get("target_type")); s != "" {
		return domainext.TargetType(s)
	}
	return domainext.TargetType(strings.TrimSpace(r.URL.Query().Get("scope")))
}

func includePrivateExtensionFields(r *http.Request) bool {
	if strings.TrimSpace(r.URL.Query().Get("include_private")) != "true" {
		return false
	}
	ac, err := admin.FromContext(r.Context())
	if err != nil || ac == nil {
		return false
	}
	return ac.HasPermission(string(rbac.ExtensionsPrivateRead))
}

func extensionFieldAPIError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, extensionapp.ErrFieldAlreadyExists) {
		return apperror.Conflict(err.Error())
	}
	if errors.Is(err, extensionapp.ErrFieldNotFound) {
		return apperror.NotFound(err.Error())
	}
	if apperror.Is(err, apperror.CodeNotFound) {
		return err
	}
	if domainext.IsValidationError(err) {
		return apperror.Validation(err.Error())
	}
	return apperror.Internal("extension field operation failed")
}

// ListFields handles GET /api/v1/admin/extensions/fields.
func (h *ExtensionFieldAdminHandler) ListFields() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		includePrivate := includePrivateExtensionFields(r)
		filter := extensionapp.ListFilter{
			Scope:          resolveScopeQuery(r),
			IncludePrivate: includePrivate,
		}
		if v := strings.TrimSpace(r.URL.Query().Get("visibility")); v != "" {
			filter.Visibility = domainext.Visibility(v)
		}

		fields := h.service.List(filter)
		resp := make([]extensionFieldResponse, 0, len(fields))
		for _, field := range fields {
			resp = append(resp, toExtensionFieldResponse(field))
		}
		h.auditField(r, admin.AuditExtensionFieldRead, "", map[string]interface{}{
			"scope":           string(filter.Scope),
			"visibility":      string(filter.Visibility),
			"include_private": includePrivate,
			"count":           len(resp),
		}, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"fields": resp})
	}
}

// GetField handles GET /api/v1/admin/extensions/fields/{code}.
func (h *ExtensionFieldAdminHandler) GetField() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimSpace(r.PathValue("code"))
		includePrivate := includePrivateExtensionFields(r)
		field, err := h.service.Get(code, includePrivate)
		if err != nil {
			apiErr := extensionFieldAPIError(err)
			h.auditField(r, admin.AuditExtensionFieldRead, code, nil, apiErr)
			httpshared.JSONError(w, apiErr)
			return
		}
		h.auditField(r, admin.AuditExtensionFieldRead, code, nil, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"field": toExtensionFieldResponse(field)})
	}
}

// CreateField handles POST /api/v1/admin/extensions/fields.
func (h *ExtensionFieldAdminHandler) CreateField() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createExtensionFieldRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apiErr := apperror.Validation("invalid request body")
			h.auditField(r, admin.AuditExtensionFieldCreate, "", nil, apiErr)
			httpshared.JSONError(w, apiErr)
			return
		}

		field, err := h.service.Create(r.Context(), fieldDefFromCreateRequest(req))
		if err != nil {
			apiErr := extensionFieldAPIError(err)
			h.auditField(r, admin.AuditExtensionFieldCreate, strings.TrimSpace(req.Code), nil, apiErr)
			httpshared.JSONError(w, apiErr)
			return
		}
		h.auditField(r, admin.AuditExtensionFieldCreate, field.Code, nil, nil)
		httpshared.JSON(w, http.StatusCreated, map[string]interface{}{"field": toExtensionFieldResponse(field)})
	}
}

// UpdateField handles PUT /api/v1/admin/extensions/fields/{code}.
func (h *ExtensionFieldAdminHandler) UpdateField() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimSpace(r.PathValue("code"))
		var req updateExtensionFieldRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apiErr := apperror.Validation("invalid request body")
			h.auditField(r, admin.AuditExtensionFieldUpdate, code, nil, apiErr)
			httpshared.JSONError(w, apiErr)
			return
		}

		field, err := h.service.Update(r.Context(), code, fieldDefFromUpdateRequest(req, code))
		if err != nil {
			apiErr := extensionFieldAPIError(err)
			h.auditField(r, admin.AuditExtensionFieldUpdate, code, nil, apiErr)
			httpshared.JSONError(w, apiErr)
			return
		}
		h.auditField(r, admin.AuditExtensionFieldUpdate, code, nil, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"field": toExtensionFieldResponse(field)})
	}
}

// DeleteField handles DELETE /api/v1/admin/extensions/fields/{code}.
func (h *ExtensionFieldAdminHandler) DeleteField() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimSpace(r.PathValue("code"))
		if err := h.service.Delete(r.Context(), code); err != nil {
			apiErr := extensionFieldAPIError(err)
			h.auditField(r, admin.AuditExtensionFieldDelete, code, nil, apiErr)
			httpshared.JSONError(w, apiErr)
			return
		}
		h.auditField(r, admin.AuditExtensionFieldDelete, code, nil, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"deleted": true})
	}
}
