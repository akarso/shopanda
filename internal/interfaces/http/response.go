package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/akarso/shopanda/internal/platform/apperror"
)

// Response is the standard API response envelope.
type Response struct {
	Data  interface{} `json:"data"`
	Error *ErrorBody  `json:"error"`
}

// ErrorBody represents the error portion of an API response.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// JSON writes a JSON response with the given status code and data.
func JSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(Response{Data: data})
}

// JSONError writes a JSON error response derived from err.
// If err is an *apperror.Error, the code and message are taken from it.
// Otherwise a generic 500 response is returned.
func JSONError(w http.ResponseWriter, err error) {
	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		status := StatusFromCode(appErr.Code)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(Response{
			Error: &ErrorBody{
				Code:    string(appErr.Code),
				Message: appErr.Message,
			},
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(Response{
		Error: &ErrorBody{
			Code:    string(apperror.CodeInternal),
			Message: "internal server error",
		},
	})
}

// StatusFromCode maps an apperror.Code to an HTTP status code.
func StatusFromCode(code apperror.Code) int {
	switch code {
	case apperror.CodeNotFound:
		return http.StatusNotFound
	case apperror.CodeConflict:
		return http.StatusConflict
	case apperror.CodeValidation:
		return http.StatusUnprocessableEntity
	case apperror.CodeUnauthorized:
		return http.StatusUnauthorized
	case apperror.CodeForbidden:
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}
