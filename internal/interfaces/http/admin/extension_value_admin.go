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

const maxExtensionValueBodyBytes int64 = 1 << 20 // 1 MiB

// ExtensionValueAdminHandler serves extension value admin endpoints.
type ExtensionValueAdminHandler struct {
	values  *extensionapp.ValueService
	auditor *admin.Auditor
}

// NewExtensionValueAdminHandler creates an ExtensionValueAdminHandler with a default auditor.
func NewExtensionValueAdminHandler(values *extensionapp.ValueService) *ExtensionValueAdminHandler {
	return NewExtensionValueAdminHandlerWithAuditor(values, admin.NewAuditor(logger.New("info")))
}

// NewExtensionValueAdminHandlerWithAuditor creates an ExtensionValueAdminHandler with a custom auditor.
func NewExtensionValueAdminHandlerWithAuditor(values *extensionapp.ValueService, auditor *admin.Auditor) *ExtensionValueAdminHandler {
	if values == nil {
		panic("ExtensionValueAdminHandler: value service must not be nil")
	}
	if auditor == nil {
		panic("ExtensionValueAdminHandler: auditor must not be nil")
	}
	return &ExtensionValueAdminHandler{values: values, auditor: auditor}
}

type extensionValueWriteRequest struct {
	Values []extensionValueWriteItem `json:"values"`
}

type extensionValueWriteItem struct {
	FieldCode string      `json:"field_code"`
	Value     interface{} `json:"value"`
}

type extensionValueResponse struct {
	FieldCode string      `json:"field_code"`
	Type      string      `json:"type"`
	Value     interface{} `json:"value"`
}

func (h *ExtensionValueAdminHandler) auditValue(r *http.Request, action admin.AuditAction, target domainext.Target, fieldCode string, private bool, err error) {
	details := map[string]interface{}{
		"target_type": string(target.Type),
		"target_id":   target.ID,
	}
	if fieldCode != "" {
		details["field_code"] = fieldCode
	}
	if private {
		details["private_field"] = true
	}
	result := "success"
	errMsg := ""
	if err != nil {
		result = "error"
		errMsg = err.Error()
	}
	h.auditor.LogAction(r.Context(), admin.AuditEntry{
		AdminID:      adminIDFromRequest(r),
		Action:       action,
		ResourceType: "extension_value",
		ResourceID:   target.ID,
		Details:      mergeAuditDetails(details, fullAdminScopeDetailsFromRequest(r)),
		Result:       result,
		Error:        errMsg,
	})
}

func canAccessPrivateExtensionFields(r *http.Request) bool {
	ac, err := admin.FromContext(r.Context())
	if err != nil || ac == nil {
		return false
	}
	return ac.HasPermission(string(rbac.ExtensionsPrivateRead))
}

func includePrivateExtensionValues(r *http.Request) bool {
	if strings.TrimSpace(r.URL.Query().Get("include_private")) != "true" {
		return false
	}
	return canAccessPrivateExtensionFields(r)
}

func extensionValueAPIError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, domainext.ErrUnknownFieldCode) {
		return apperror.UnknownFieldCode(err.Error())
	}
	if errors.Is(err, domainext.ErrForbiddenPrivateField) {
		return apperror.ForbiddenPrivateField(err.Error())
	}
	if domainext.IsValidationError(err) {
		return apperror.FieldValidationFailed(err.Error())
	}
	if apperror.Is(err, apperror.CodeNotFound) {
		return err
	}
	return apperror.Internal("extension value operation failed")
}

func toExtensionValueResponses(values []domainext.Value, registry *extensionapp.Registry) ([]extensionValueResponse, error) {
	resp := make([]extensionValueResponse, 0, len(values))
	for _, value := range values {
		field, ok := registry.Get(value.FieldCode)
		if !ok {
			continue
		}
		apiValue, err := domainext.APIValue(field, value.Payload)
		if err != nil {
			return nil, err
		}
		resp = append(resp, extensionValueResponse{
			FieldCode: value.FieldCode,
			Type:      string(field.Type),
			Value:     apiValue,
		})
	}
	return resp, nil
}

func targetFromRequest(targetType, targetID string) domainext.Target {
	return domainext.Target{
		Type: domainext.TargetType(strings.TrimSpace(targetType)),
		ID:   strings.TrimSpace(targetID),
	}
}

func valueInputsFromRequest(req extensionValueWriteRequest) []domainext.ValueInput {
	inputs := make([]domainext.ValueInput, 0, len(req.Values))
	for _, item := range req.Values {
		inputs = append(inputs, domainext.ValueInput{
			FieldCode: item.FieldCode,
			Value:     item.Value,
		})
	}
	return inputs
}

func isPrivateField(registry *extensionapp.Registry, fieldCode string) bool {
	field, ok := registry.Get(fieldCode)
	return ok && field.Visibility == domainext.VisibilityPrivate
}

func writeIncludesPrivateField(registry *extensionapp.Registry, inputs []domainext.ValueInput) bool {
	for _, input := range inputs {
		if isPrivateField(registry, strings.TrimSpace(input.FieldCode)) {
			return true
		}
	}
	return false
}

func (h *ExtensionValueAdminHandler) listValues(w http.ResponseWriter, r *http.Request, target domainext.Target) {
	includePrivate := includePrivateExtensionValues(r)
	values, err := h.values.List(r.Context(), target, includePrivate)
	if err != nil {
		apiErr := extensionValueAPIError(err)
		h.auditValue(r, admin.AuditExtensionValueRead, target, "", includePrivate, apiErr)
		httpshared.JSONError(w, apiErr)
		return
	}
	resp, err := toExtensionValueResponses(values, h.values.Registry())
	if err != nil {
		apiErr := extensionValueAPIError(err)
		h.auditValue(r, admin.AuditExtensionValueRead, target, "", includePrivate, apiErr)
		httpshared.JSONError(w, apiErr)
		return
	}
	h.auditValue(r, admin.AuditExtensionValueRead, target, "", includePrivate, nil)
	httpshared.JSON(w, http.StatusOK, map[string]interface{}{"values": resp})
}

func (h *ExtensionValueAdminHandler) putValues(w http.ResponseWriter, r *http.Request, target domainext.Target) {
	var req extensionValueWriteRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxExtensionValueBodyBytes))
	if err := dec.Decode(&req); err != nil {
		apiErr := apperror.Validation("invalid request body")
		h.auditValue(r, admin.AuditExtensionValueUpdate, target, "", false, apiErr)
		httpshared.JSONError(w, apiErr)
		return
	}
	inputs := valueInputsFromRequest(req)
	private := writeIncludesPrivateField(h.values.Registry(), inputs)
	canAccessPrivate := canAccessPrivateExtensionFields(r)
	values, err := h.values.UpsertBatch(r.Context(), target, inputs, adminIDFromRequest(r), canAccessPrivate)
	if err != nil {
		apiErr := extensionValueAPIError(err)
		h.auditValue(r, admin.AuditExtensionValueUpdate, target, "", private, apiErr)
		httpshared.JSONError(w, apiErr)
		return
	}
	resp, err := toExtensionValueResponses(values, h.values.Registry())
	if err != nil {
		apiErr := extensionValueAPIError(err)
		h.auditValue(r, admin.AuditExtensionValueUpdate, target, "", private, apiErr)
		httpshared.JSONError(w, apiErr)
		return
	}
	h.auditValue(r, admin.AuditExtensionValueUpdate, target, "", private, nil)
	httpshared.JSON(w, http.StatusOK, map[string]interface{}{"values": resp})
}

// ListValues handles GET /api/v1/admin/extensions/values/{targetType}/{targetID}.
func (h *ExtensionValueAdminHandler) ListValues() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := targetFromRequest(r.PathValue("targetType"), r.PathValue("targetID"))
		h.listValues(w, r, target)
	}
}

// PutValues handles PUT /api/v1/admin/extensions/values/{targetType}/{targetID}.
func (h *ExtensionValueAdminHandler) PutValues() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := targetFromRequest(r.PathValue("targetType"), r.PathValue("targetID"))
		h.putValues(w, r, target)
	}
}

// DeleteValue handles DELETE /api/v1/admin/extensions/values/{targetType}/{targetID}/{fieldCode}.
func (h *ExtensionValueAdminHandler) DeleteValue() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := targetFromRequest(r.PathValue("targetType"), r.PathValue("targetID"))
		fieldCode := strings.TrimSpace(r.PathValue("fieldCode"))
		private := isPrivateField(h.values.Registry(), fieldCode)
		canAccessPrivate := canAccessPrivateExtensionFields(r)
		err := h.values.Delete(r.Context(), target, fieldCode, canAccessPrivate)
		if err != nil {
			apiErr := extensionValueAPIError(err)
			h.auditValue(r, admin.AuditExtensionValueDelete, target, fieldCode, private, apiErr)
			httpshared.JSONError(w, apiErr)
			return
		}
		h.auditValue(r, admin.AuditExtensionValueDelete, target, fieldCode, private, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"deleted": true})
	}
}

// ListProductExtensions handles GET /api/v1/admin/products/{id}/extensions.
func (h *ExtensionValueAdminHandler) ListProductExtensions() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := domainext.Target{
			Type: domainext.TargetProduct,
			ID:   strings.TrimSpace(r.PathValue("id")),
		}
		h.listValues(w, r, target)
	}
}

// PutProductExtensions handles PUT /api/v1/admin/products/{id}/extensions.
func (h *ExtensionValueAdminHandler) PutProductExtensions() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := domainext.Target{
			Type: domainext.TargetProduct,
			ID:   strings.TrimSpace(r.PathValue("id")),
		}
		h.putValues(w, r, target)
	}
}
