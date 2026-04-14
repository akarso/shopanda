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
// It marshals the body first so the status header is only committed on success.
func JSON(w http.ResponseWriter, status int, data interface{}) {
	body, err := json.Marshal(Response{Data: data})
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"data":null,"error":{"code":"internal","message":"response encoding failed"}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// JSONError writes a JSON error response derived from err.
// If err is an *apperror.Error, the code and message are taken from it.
// Otherwise a generic 500 response is returned.
func JSONError(w http.ResponseWriter, err error) {
	code := string(apperror.CodeInternal)
	msg := "internal server error"
	status := http.StatusInternalServerError

	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		status = StatusFromCode(appErr.Code)
		code = string(appErr.Code)
		msg = appErr.Message
		if status == http.StatusInternalServerError {
			code = string(apperror.CodeInternal)
			msg = "internal server error"
		}
	}

	body, marshalErr := json.Marshal(Response{
		Error: &ErrorBody{
			Code:    code,
			Message: msg,
		},
	})
	if marshalErr != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"data":null,"error":{"code":"internal","message":"internal server error"}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
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
	case apperror.CodeRateLimited:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}
