package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/httpx"
)

// Response is the standard API response envelope.
type Response = httpx.Response

// ErrorBody represents the error portion of an API response.
type ErrorBody = httpx.ErrorBody

// JSON writes a JSON response with the given status code and data.
func JSON(w http.ResponseWriter, status int, data interface{}) {
	httpx.JSON(w, status, data)
}

// JSONWithError writes a JSON response with data and an error envelope.
func JSONWithError(w http.ResponseWriter, status int, data interface{}, err error) {
	code := string(apperror.CodeInternal)
	msg := "internal server error"
	if status == 0 {
		status = http.StatusInternalServerError
	}

	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		if status == http.StatusInternalServerError {
			status = StatusFromCode(appErr.Code)
		}
		code = string(appErr.Code)
		msg = appErr.Message
		if status == http.StatusInternalServerError {
			code = string(apperror.CodeInternal)
			msg = "internal server error"
		}
	}

	body, marshalErr := json.Marshal(Response{
		Data: data,
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

// JSONError writes a JSON error response derived from err.
func JSONError(w http.ResponseWriter, err error) {
	httpx.JSONError(w, err)
}

// StatusFromCode maps an apperror.Code to an HTTP status code.
func StatusFromCode(code apperror.Code) int {
	return httpx.StatusFromCode(code)
}
