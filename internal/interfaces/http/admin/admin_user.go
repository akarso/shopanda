package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	"github.com/akarso/shopanda/internal/application/admin"
	adminuserApp "github.com/akarso/shopanda/internal/application/adminuser"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// AdminUserHandler serves admin user management endpoints.
type AdminUserHandler struct {
	users   *adminuserApp.Service
	auditor *admin.Auditor
}

// NewAdminUserHandler creates an AdminUserHandler.
func NewAdminUserHandler(users *adminuserApp.Service, auditor *admin.Auditor) *AdminUserHandler {
	if users == nil {
		panic("http: admin user service must not be nil")
	}
	if auditor == nil {
		panic("http: auditor must not be nil")
	}
	return &AdminUserHandler{users: users, auditor: auditor}
}

type adminUserResponse struct {
	ID              string  `json:"id"`
	Email           string  `json:"email"`
	FirstName       string  `json:"first_name"`
	LastName        string  `json:"last_name"`
	Role            string  `json:"role"`
	Status          string  `json:"status"`
	EmailVerifiedAt *string `json:"email_verified_at,omitempty"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

// List handles GET /api/v1/admin/users.
func (h *AdminUserHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminID := adminIDFromRequest(r)
		offset, limit, err := httpshared.ParsePagination(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}

		users, err := h.users.List(r.Context(), offset, limit)
		if err != nil {
			h.audit(r.Context(), adminID, admin.AuditAdminUserList, "", "error", err.Error(), nil)
			httpshared.JSONError(w, err)
			return
		}

		out := make([]adminUserResponse, 0, len(users))
		for i := range users {
			out = append(out, toAdminUserResponse(&users[i]))
		}
		h.audit(r.Context(), adminID, admin.AuditAdminUserList, "", "success", "", map[string]interface{}{
			"offset": offset,
			"limit":  limit,
			"count":  len(out),
		})
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"users":  out,
			"offset": offset,
			"limit":  limit,
		})
	}
}

// Get handles GET /api/v1/admin/users/{userId}.
func (h *AdminUserHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.PathValue("userId")
		adminID := adminIDFromRequest(r)

		user, err := h.users.Get(r.Context(), userID)
		if err != nil {
			h.audit(r.Context(), adminID, admin.AuditAdminUserRead, userID, "error", err.Error(), nil)
			httpshared.JSONError(w, err)
			return
		}
		h.audit(r.Context(), adminID, admin.AuditAdminUserRead, userID, "success", "", nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"user": toAdminUserResponse(user),
		})
	}
}

type createAdminUserBody struct {
	Email     string `json:"email"`
	Password  string `json:"password"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Role      string `json:"role"`
}

// Create handles POST /api/v1/admin/users.
func (h *AdminUserHandler) Create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminID := adminIDFromRequest(r)
		var req createAdminUserBody
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		user, err := h.users.Create(r.Context(), adminuserApp.CreateInput{
			Email:     req.Email,
			Password:  req.Password,
			FirstName: req.FirstName,
			LastName:  req.LastName,
			Role:      customer.Role(req.Role),
		})
		if err != nil {
			h.audit(r.Context(), adminID, admin.AuditAdminUserCreate, "", "error", err.Error(), map[string]interface{}{
				"email": req.Email,
				"role":  req.Role,
			})
			httpshared.JSONError(w, err)
			return
		}

		h.audit(r.Context(), adminID, admin.AuditAdminUserCreate, user.ID, "success", "", map[string]interface{}{
			"role": string(user.Role),
		})
		httpshared.JSON(w, http.StatusCreated, map[string]interface{}{
			"user": toAdminUserResponse(user),
		})
	}
}

type updateAdminUserBody struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Role      string `json:"role"`
	Status    string `json:"status"`
}

// Update handles PUT /api/v1/admin/users/{userId}.
func (h *AdminUserHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.PathValue("userId")
		adminID := adminIDFromRequest(r)

		var req updateAdminUserBody
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		user, err := h.users.Update(r.Context(), adminID, userID, adminuserApp.UpdateInput{
			FirstName: req.FirstName,
			LastName:  req.LastName,
			Role:      customer.Role(req.Role),
			Status:    customer.Status(req.Status),
		})
		if err != nil {
			h.audit(r.Context(), adminID, admin.AuditAdminUserUpdate, userID, "error", err.Error(), nil)
			httpshared.JSONError(w, err)
			return
		}

		h.audit(r.Context(), adminID, admin.AuditAdminUserUpdate, userID, "success", "", map[string]interface{}{
			"role":   string(user.Role),
			"status": string(user.Status),
		})
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"user": toAdminUserResponse(user),
		})
	}
}

type resetAdminPasswordBody struct {
	Password string `json:"password"`
}

// ResetPassword handles POST /api/v1/admin/users/{userId}/reset-password.
func (h *AdminUserHandler) ResetPassword() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.PathValue("userId")
		adminID := adminIDFromRequest(r)

		var req resetAdminPasswordBody
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		if err := h.users.ResetPassword(r.Context(), adminID, userID, req.Password); err != nil {
			h.audit(r.Context(), adminID, admin.AuditAdminUserResetPassword, userID, "error", err.Error(), nil)
			httpshared.JSONError(w, err)
			return
		}

		h.audit(r.Context(), adminID, admin.AuditAdminUserResetPassword, userID, "success", "", nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"user_id": userID,
			"status":  "password_reset",
		})
	}
}

func (h *AdminUserHandler) audit(ctx context.Context, adminID string, action admin.AuditAction, resourceID, result, errMsg string, details map[string]interface{}) {
	entry := admin.AuditEntry{
		AdminID:      adminID,
		Action:       action,
		ResourceType: "admin_user",
		ResourceID:   resourceID,
		Result:       result,
		Details:      details,
	}
	if errMsg != "" {
		entry.Error = errMsg
	}
	h.auditor.LogAction(ctx, entry)
}

func toAdminUserResponse(c *customer.Customer) adminUserResponse {
	resp := adminUserResponse{
		ID:        c.ID,
		Email:     c.Email,
		FirstName: c.FirstName,
		LastName:  c.LastName,
		Role:      string(c.Role),
		Status:    string(c.Status),
		CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: c.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if c.EmailVerifiedAt != nil {
		s := c.EmailVerifiedAt.UTC().Format(time.RFC3339)
		resp.EmailVerifiedAt = &s
	}
	return resp
}
