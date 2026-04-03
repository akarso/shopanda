package http

import (
	"encoding/json"
	"net/http"

	appAuth "github.com/akarso/shopanda/internal/application/auth"
	"github.com/akarso/shopanda/internal/platform/apperror"
	platformAuth "github.com/akarso/shopanda/internal/platform/auth"
)

// AuthHandler handles authentication endpoints.
type AuthHandler struct {
	svc *appAuth.Service
}

// NewAuthHandler creates an AuthHandler.
func NewAuthHandler(svc *appAuth.Service) *AuthHandler {
	return &AuthHandler{svc: svc}
}

type registerRequest struct {
	Email     string `json:"email"`
	Password  string `json:"password"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type authTokenResponse struct {
	CustomerID string `json:"customer_id"`
	Token      string `json:"token"`
	ExpiresAt  string `json:"expires_at"`
}

// Register returns a handler for POST /auth/register.
func (h *AuthHandler) Register() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req registerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		out, err := h.svc.Register(r.Context(), appAuth.RegisterInput{
			Email:     req.Email,
			Password:  req.Password,
			FirstName: req.FirstName,
			LastName:  req.LastName,
		})
		if err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusCreated, authTokenResponse{
			CustomerID: out.CustomerID,
			Token:      out.Token,
			ExpiresAt:  out.ExpiresAt.Format("2006-01-02T15:04:05Z"),
		})
	}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Login returns a handler for POST /auth/login.
func (h *AuthHandler) Login() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		out, err := h.svc.Login(r.Context(), appAuth.LoginInput{
			Email:    req.Email,
			Password: req.Password,
		})
		if err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, authTokenResponse{
			CustomerID: out.CustomerID,
			Token:      out.Token,
			ExpiresAt:  out.ExpiresAt.Format("2006-01-02T15:04:05Z"),
		})
	}
}

type meResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Status    string `json:"status"`
}

// Me returns a handler for GET /auth/me.
func (h *AuthHandler) Me() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := platformAuth.IdentityFrom(r.Context())
		if id.IsGuest() {
			JSONError(w, apperror.Unauthorized("authentication required"))
			return
		}

		c, err := h.svc.Me(r.Context(), id.UserID)
		if err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, meResponse{
			ID:        c.ID,
			Email:     c.Email,
			FirstName: c.FirstName,
			LastName:  c.LastName,
			Status:    string(c.Status),
		})
	}
}

// Logout returns a handler for POST /auth/logout.
func (h *AuthHandler) Logout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := platformAuth.IdentityFrom(r.Context())
		if id.IsGuest() {
			JSONError(w, apperror.Unauthorized("authentication required"))
			return
		}

		if err := h.svc.Logout(r.Context(), id.UserID); err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, map[string]string{"message": "logged out"})
	}
}

type passwordResetRequestBody struct {
	Email string `json:"email"`
}

// RequestPasswordReset returns a handler for POST /auth/password-reset/request.
func (h *AuthHandler) RequestPasswordReset() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req passwordResetRequestBody
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		if err := h.svc.RequestPasswordReset(r.Context(), req.Email); err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, map[string]string{
			"message": "if the email exists, a reset link has been sent",
		})
	}
}

type passwordResetConfirmBody struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// ConfirmPasswordReset returns a handler for POST /auth/password-reset/confirm.
func (h *AuthHandler) ConfirmPasswordReset() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req passwordResetConfirmBody
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		err := h.svc.ConfirmPasswordReset(r.Context(), appAuth.ConfirmPasswordResetInput{
			Token:       req.Token,
			NewPassword: req.NewPassword,
		})
		if err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, map[string]string{"message": "password has been reset"})
	}
}
