package graphql

import (
	"encoding/json"
	"io"
	"net/http"

	gql "github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/gqlerrors"

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

	// GraphQL execution that succeeds returns HTTP 200. When the query fails
	// validation or a resolver returns an error, result.Errors is non-empty and
	// this endpoint responds with HTTP 400. Resolver/DB error text is never
	// returned to clients: such errors are logged server-side and replaced with
	// a generic message (see sanitizeErrors). Validation/syntax errors, which
	// describe the client's own query, are passed through unchanged.
	if len(result.Errors) > 0 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data":   result.Data,
			"errors": h.sanitizeErrors(result.Errors),
		})
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

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
		if h.log != nil {
			h.log.Error("graphql.resolver_error", inner, map[string]interface{}{
				"path": e.Path,
			})
		}
		out[i] = gqlerrors.FormattedError{
			Message:   "internal server error",
			Locations: e.Locations,
			Path:      e.Path,
		}
	}
	return out
}
