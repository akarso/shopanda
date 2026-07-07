package graphql

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	gql "github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/gqlerrors"

	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// Handler serves POST /api/v1/graphql.
type Handler struct {
	schema gql.Schema
	log    logger.Logger
}

// NewHandler creates a GraphQL HTTP handler. log may be nil.
func NewHandler(schema gql.Schema, log logger.Logger) *Handler {
	return &Handler{schema: schema, log: log}
}

type graphQLRequest struct {
	Query         string                 `json:"query"`
	OperationName string                 `json:"operationName"`
	Variables     map[string]interface{} `json:"variables"`
}

// ServeHTTP executes a GraphQL query from the JSON request body.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	var req graphQLRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
	}
	if req.Query == "" {
		http.Error(w, "query is required", http.StatusBadRequest)
		return
	}

	result := gql.Do(gql.Params{
		Schema:         h.schema,
		RequestString:  req.Query,
		OperationName:  req.OperationName,
		VariableValues: req.Variables,
		Context:        r.Context(),
	})

	w.Header().Set("Content-Type", "application/json")

	// HTTP status: 200 on success; 400 for client query/validation errors; 500
	// when any resolver/DB failure was sanitized (see sanitizeErrors).
	if len(result.Errors) > 0 {
		sanitized := h.sanitizeErrors(result.Errors)
		status := http.StatusBadRequest
		for _, e := range sanitized {
			if e.Message == internalErrorMessage {
				status = http.StatusInternalServerError
				break
			}
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data":   result.Data,
			"errors": sanitized,
		})
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

const internalErrorMessage = "internal server error"

// sanitizeErrors strips internal resolver/DB error text from GraphQL errors
// before they reach the client. Errors originating from a resolver (i.e. with
// an underlying Go error) are logged in full and replaced with a generic
// message; validation and syntax errors are returned as-is because they only
// describe the caller's query.
func (h *Handler) sanitizeErrors(errs []gqlerrors.FormattedError) []gqlerrors.FormattedError {
	out := make([]gqlerrors.FormattedError, len(errs))
	for i, e := range errs {
		var inner error
		if gerr, ok := e.OriginalError().(*gqlerrors.Error); ok {
			inner = gerr.OriginalError
		}
		if inner == nil {
			out[i] = e
			continue
		}
		var appErr *apperror.Error
		if errors.As(inner, &appErr) && appErr.Code != apperror.CodeInternal {
			out[i] = gqlerrors.FormattedError{
				Message:   appErr.Message,
				Locations: e.Locations,
				Path:      e.Path,
			}
			continue
		}
		if h.log != nil {
			h.log.Error("graphql.resolver_error", inner, map[string]interface{}{
				"path": e.Path,
			})
		}
		out[i] = gqlerrors.FormattedError{
			Message:   internalErrorMessage,
			Locations: e.Locations,
			Path:      e.Path,
		}
	}
	return out
}
