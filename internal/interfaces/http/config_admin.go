package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/akarso/shopanda/internal/application/admin"
	domainCfg "github.com/akarso/shopanda/internal/domain/config"
	"github.com/akarso/shopanda/internal/platform/apperror"
	appconfig "github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// SMTPTestConfig carries SMTP settings for a test email request.
type SMTPTestConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	From     string
}

// SMTPTestFunc sends a test email using the provided SMTP settings.
type SMTPTestFunc func(ctx context.Context, cfg SMTPTestConfig, to string) error

// ConfigAdminHandler serves grouped admin config endpoints.
type ConfigAdminHandler struct {
	repo          domainCfg.Repository
	cfg           *appconfig.Config
	testEmailFunc SMTPTestFunc
	auditor       *admin.Auditor
}

const redactedSecretValue = "***"

const (
	configScopeGlobal       = "global"
	configScopeStore        = "store"
	configScopeTranslatable = "translatable"
)

var configGroupKeys = map[string][]string{
	"store": {
		"store.address",
		"store.logo",
	},
	"email": {
		"mail.smtp.host",
		"mail.smtp.port",
		"mail.smtp.user",
		"mail.smtp.password",
		"mail.smtp.from",
	},
	"media": {
		"media.storage",
		"media.local.base_path",
		"media.local.base_url",
		"media.s3.endpoint",
		"media.s3.bucket",
		"media.s3.region",
		"media.s3.base_url",
		"media.s3.public_acl",
	},
	"currency": {
		"default_currency",
		"currency.display_format",
	},
	"tax": {
		"tax.default_class",
		"tax.included",
	},
}

var secretConfigKeys = map[string]struct{}{
	"mail.smtp.password": {},
}

var configKeyScopes = map[string]string{
	"store.address":           configScopeStore,
	"store.logo":              configScopeStore,
	"default_currency":        configScopeStore,
	"currency.display_format": configScopeStore,
	"tax.default_class":       configScopeStore,
	"tax.included":            configScopeStore,
}

// NewConfigAdminHandler creates a ConfigAdminHandler.
func NewConfigAdminHandler(repo domainCfg.Repository, cfg *appconfig.Config, testEmailFunc SMTPTestFunc, log logger.Logger) *ConfigAdminHandler {
	if repo == nil {
		panic("http: config repository must not be nil")
	}
	if cfg == nil {
		panic("http: config config must not be nil")
	}
	if testEmailFunc == nil {
		panic("http: config test email function must not be nil")
	}
	if log == nil {
		panic("http: config logger must not be nil")
	}
	return &ConfigAdminHandler{repo: repo, cfg: cfg, testEmailFunc: testEmailFunc, auditor: admin.NewAuditor(log)}
}

type updateConfigRequest struct {
	Entries map[string]interface{} `json:"entries"`
}

type testEmailRequest struct {
	To       string  `json:"to"`
	Host     string  `json:"host"`
	Port     *int    `json:"port"`
	User     *string `json:"user"`
	Password *string `json:"password"`
	From     string  `json:"from"`
}

// Get handles GET /api/v1/admin/config.
func (h *ConfigAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		group := strings.TrimSpace(r.URL.Query().Get("group"))
		keys, err := keysForGroup(group)
		if err != nil {
			h.auditConfigError(r, admin.AuditSettingsRead, "config_group", "", map[string]interface{}{"group": group}, err)
			JSONError(w, apperror.Validation(err.Error()))
			return
		}
		storeID := resolveStoreScopeID(r)

		values := h.defaultEntries(keys)
		for _, key := range keys {
			value, err := h.resolveScopedValue(r.Context(), key, storeID)
			if err != nil {
				h.auditConfigError(r, admin.AuditSettingsRead, "config_group", "", map[string]interface{}{"group": group, "key": key}, err)
				JSONError(w, err)
				return
			}
			if value != nil {
				values[key] = value
			}
		}
		h.auditConfigSuccess(r, admin.AuditSettingsRead, "config_group", "", map[string]interface{}{"group": group, "keys_count": len(keys)})

		JSON(w, http.StatusOK, map[string]interface{}{
			"group":        group,
			"entries":      h.redactEntries(values),
			"scope":        scopePayloadFromRequest(r),
			"field_scopes": fieldScopesForKeys(keys),
		})
	}
}

// Update handles PUT /api/v1/admin/config.
func (h *ConfigAdminHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req updateConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		if len(req.Entries) == 0 {
			JSONError(w, apperror.Validation("entries are required"))
			return
		}

		entries, err := h.normalizeUpdateEntries(r.Context(), req.Entries)
		if err != nil {
			h.auditConfigError(r, admin.AuditSettingsChange, "config_group", "", map[string]interface{}{"entries_count": len(req.Entries)}, err)
			JSONError(w, err)
			return
		}
		storeID := resolveStoreScopeID(r)
		persisted := make(map[string]interface{}, len(entries))
		for key, value := range entries {
			targetKey := key
			if scopeForConfigKey(key) == configScopeStore && storeID != "" {
				targetKey = scopedConfigKey(storeID, key)
			}
			persisted[targetKey] = value
		}

		if err := h.repo.SetMany(r.Context(), persisted); err != nil {
			h.auditConfigError(r, admin.AuditSettingsChange, "config_group", "", map[string]interface{}{"entries_count": len(entries), "store_id": storeID}, err)
			JSONError(w, err)
			return
		}
		h.auditConfigSuccess(r, admin.AuditSettingsChange, "config_group", "", map[string]interface{}{"entries_count": len(entries), "store_id": storeID})

		JSON(w, http.StatusOK, map[string]interface{}{
			"entries": h.redactEntries(req.Entries),
			"scope":   scopePayloadFromRequest(r),
		})
	}
}

func (h *ConfigAdminHandler) auditConfigSuccess(r *http.Request, action admin.AuditAction, resourceType, resourceID string, details map[string]interface{}) {
	adminID, scopeDetails := adminAuditInfoFromRequest(r)
	merged := mergeAuditDetails(details, scopeDetails)
	h.auditor.LogAction(r.Context(), admin.AuditEntry{
		AdminID:      adminID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Details:      merged,
		Result:       "success",
	})
}

func (h *ConfigAdminHandler) auditConfigError(r *http.Request, action admin.AuditAction, resourceType, resourceID string, details map[string]interface{}, err error) {
	adminID, scopeDetails := adminAuditInfoFromRequest(r)
	merged := mergeAuditDetails(details, scopeDetails)
	h.auditor.LogAction(r.Context(), admin.AuditEntry{
		AdminID:      adminID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Details:      merged,
		Result:       "error",
		Error:        err.Error(),
	})
}

func (h *ConfigAdminHandler) resolveScopedValue(ctx context.Context, key, storeID string) (interface{}, error) {
	if scopeForConfigKey(key) == configScopeStore && storeID != "" {
		overrideValue, err := h.repo.Get(ctx, scopedConfigKey(storeID, key))
		if err != nil {
			return nil, err
		}
		if overrideValue != nil {
			return overrideValue, nil
		}
	}
	return h.repo.Get(ctx, key)
}

func resolveStoreScopeID(r *http.Request) string {
	explicit := strings.TrimSpace(r.URL.Query().Get("store_id"))
	ac, err := admin.FromContext(r.Context())
	if err == nil && ac != nil {
		contextStoreID := strings.TrimSpace(ac.StoreID)
		if contextStoreID != "" {
			// Keep tenant boundary deterministic: context scope wins if query conflicts.
			if explicit != "" && explicit != contextStoreID {
				return contextStoreID
			}
			return contextStoreID
		}
	}
	return explicit
}

func scopePayloadFromRequest(r *http.Request) map[string]interface{} {
	payload := map[string]interface{}{
		"store_id": strings.TrimSpace(r.URL.Query().Get("store_id")),
		"language": "",
		"currency": "",
	}
	ac, err := admin.FromContext(r.Context())
	if err != nil || ac == nil {
		return payload
	}
	payload["store_id"] = strings.TrimSpace(ac.StoreID)
	payload["language"] = strings.TrimSpace(ac.Language)
	payload["currency"] = strings.TrimSpace(ac.Currency)
	return payload
}

func adminAuditInfoFromRequest(r *http.Request) (string, map[string]interface{}) {
	ac, err := admin.FromContext(r.Context())
	if err != nil || ac == nil {
		return "system", map[string]interface{}{}
	}
	adminID := strings.TrimSpace(ac.AdminID)
	if adminID == "" {
		adminID = "system"
	}
	return adminID, map[string]interface{}{
		"store_id": strings.TrimSpace(ac.StoreID),
		"language": strings.TrimSpace(ac.Language),
		"currency": strings.TrimSpace(ac.Currency),
	}
}

func mergeAuditDetails(a, b map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func scopedConfigKey(storeID, key string) string {
	return "store::" + storeID + "::" + key
}

func fieldScopesForKeys(keys []string) map[string]string {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		out[key] = scopeForConfigKey(key)
	}
	return out
}

func scopeForConfigKey(key string) string {
	if scope, ok := configKeyScopes[key]; ok {
		return scope
	}
	return configScopeGlobal
}

// TestEmail handles POST /api/v1/admin/config/test-email.
func (h *ConfigAdminHandler) TestEmail() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req testEmailRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		if strings.TrimSpace(req.To) == "" {
			JSONError(w, apperror.Validation("to is required"))
			return
		}

		settings, err := h.resolveSMTPSettings(r.Context(), req)
		if err != nil {
			JSONError(w, err)
			return
		}

		if err := h.testEmailFunc(r.Context(), settings, strings.TrimSpace(req.To)); err != nil {
			JSONError(w, apperror.Internal("failed to send test email: "+err.Error()))
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"sent": true,
			"to":   strings.TrimSpace(req.To),
		})
	}
}

func (h *ConfigAdminHandler) defaultEntries(keys []string) map[string]interface{} {
	values := make(map[string]interface{}, len(keys))
	for _, key := range keys {
		values[key] = h.defaultValue(key)
	}
	return values
}

func (h *ConfigAdminHandler) defaultValue(key string) interface{} {
	switch key {
	case "store.address", "store.logo", "currency.display_format", "tax.default_class":
		return ""
	case "tax.included":
		return false
	case "mail.smtp.host":
		return h.cfg.Mail.SMTP.Host
	case "mail.smtp.port":
		return h.cfg.Mail.SMTP.Port
	case "mail.smtp.user":
		return h.cfg.Mail.SMTP.User
	case "mail.smtp.password":
		return h.cfg.Mail.SMTP.Password
	case "mail.smtp.from":
		return h.cfg.Mail.SMTP.From
	case "media.storage":
		return h.cfg.Media.Storage
	case "media.local.base_path":
		return h.cfg.Media.Local.BasePath
	case "media.local.base_url":
		return h.cfg.Media.Local.BaseURL
	case "media.s3.endpoint":
		return h.cfg.Media.S3.Endpoint
	case "media.s3.bucket":
		return h.cfg.Media.S3.Bucket
	case "media.s3.region":
		return h.cfg.Media.S3.Region
	case "media.s3.base_url":
		return h.cfg.Media.S3.BaseURL
	case "media.s3.public_acl":
		return h.cfg.Media.S3.PublicACL
	case "default_currency":
		return "EUR"
	default:
		return ""
	}
}

func (h *ConfigAdminHandler) resolveSMTPSettings(ctx context.Context, req testEmailRequest) (SMTPTestConfig, error) {
	storedHost, err := h.lookupConfigString(ctx, "mail.smtp.host")
	if err != nil {
		return SMTPTestConfig{}, err
	}
	storedUser, err := h.lookupConfigString(ctx, "mail.smtp.user")
	if err != nil {
		return SMTPTestConfig{}, err
	}
	storedPassword, err := h.lookupConfigString(ctx, "mail.smtp.password")
	if err != nil {
		return SMTPTestConfig{}, err
	}
	storedFrom, err := h.lookupConfigString(ctx, "mail.smtp.from")
	if err != nil {
		return SMTPTestConfig{}, err
	}
	host := firstNonEmpty(req.Host, storedHost, h.cfg.Mail.SMTP.Host)
	from := firstNonEmpty(req.From, storedFrom, h.cfg.Mail.SMTP.From)
	user := firstNonEmpty(storedUser, h.cfg.Mail.SMTP.User)
	if req.User != nil {
		user = strings.TrimSpace(*req.User)
	}
	password := firstNonEmpty(storedPassword, h.cfg.Mail.SMTP.Password)
	if req.Password != nil {
		password = *req.Password
	}
	port := h.cfg.Mail.SMTP.Port
	if storedPort, ok, err := h.lookupConfigInt(ctx, "mail.smtp.port"); err != nil {
		return SMTPTestConfig{}, err
	} else if ok {
		port = storedPort
	}
	if req.Port != nil {
		port = *req.Port
	}

	if strings.TrimSpace(host) == "" {
		return SMTPTestConfig{}, apperror.Validation("mail.smtp.host is required")
	}
	if port <= 0 {
		return SMTPTestConfig{}, apperror.Validation("mail.smtp.port must be positive")
	}
	if strings.TrimSpace(from) == "" {
		return SMTPTestConfig{}, apperror.Validation("mail.smtp.from is required")
	}

	return SMTPTestConfig{
		Host:     strings.TrimSpace(host),
		Port:     port,
		User:     strings.TrimSpace(user),
		Password: password,
		From:     strings.TrimSpace(from),
	}, nil
}

func (h *ConfigAdminHandler) normalizeUpdateEntries(ctx context.Context, entries map[string]interface{}) (map[string]interface{}, error) {
	normalized := make(map[string]interface{}, len(entries))
	for key, value := range entries {
		if !isAllowedConfigKey(key) {
			return nil, apperror.Validation("invalid config key: " + key)
		}
		if value == nil {
			return nil, apperror.Validation("config value must not be null: " + key)
		}
		if isSecretConfigKey(key) {
			secretValue, ok := value.(string)
			if !ok {
				return nil, apperror.Validation("config value must be string: " + key)
			}
			if secretValue == redactedSecretValue || strings.TrimSpace(secretValue) == "" {
				continue
			}
		}
		normalized[key] = value
	}
	return normalized, nil
}

func (h *ConfigAdminHandler) redactEntries(entries map[string]interface{}) map[string]interface{} {
	redacted := make(map[string]interface{}, len(entries))
	for key, value := range entries {
		if isSecretConfigKey(key) {
			redacted[key] = redactSecretValue(value)
			continue
		}
		redacted[key] = value
	}
	return redacted
}

func (h *ConfigAdminHandler) lookupConfigString(ctx context.Context, key string) (string, error) {
	val, err := h.repo.Get(ctx, key)
	if err != nil {
		return "", err
	}
	if val == nil {
		return "", nil
	}
	switch typed := val.(type) {
	case string:
		return typed, nil
	case fmt.Stringer:
		return typed.String(), nil
	default:
		return fmt.Sprintf("%v", typed), nil
	}
}

func (h *ConfigAdminHandler) lookupConfigInt(ctx context.Context, key string) (int, bool, error) {
	val, err := h.repo.Get(ctx, key)
	if err != nil {
		return 0, false, err
	}
	if val == nil {
		return 0, false, nil
	}
	switch typed := val.(type) {
	case float64:
		return int(typed), true, nil
	case int:
		return typed, true, nil
	default:
		return 0, false, nil
	}
}

func isSecretConfigKey(key string) bool {
	_, ok := secretConfigKeys[key]
	return ok
}

func redactSecretValue(value interface{}) interface{} {
	if value == nil {
		return ""
	}
	if str, ok := value.(string); ok && strings.TrimSpace(str) == "" {
		return ""
	}
	return redactedSecretValue
}

func keysForGroup(group string) ([]string, error) {
	if group == "" {
		keys := make([]string, 0)
		for _, groupKeys := range configGroupKeys {
			keys = append(keys, groupKeys...)
		}
		sort.Strings(keys)
		return keys, nil
	}
	keys, ok := configGroupKeys[group]
	if !ok {
		return nil, fmt.Errorf("unknown config group")
	}
	copyKeys := make([]string, len(keys))
	copy(copyKeys, keys)
	return copyKeys, nil
}

func isAllowedConfigKey(key string) bool {
	for _, keys := range configGroupKeys {
		if containsKey(keys, key) {
			return true
		}
	}
	return false
}

func containsKey(keys []string, key string) bool {
	for _, item := range keys {
		if item == key {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
