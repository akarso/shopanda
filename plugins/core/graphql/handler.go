package graphql

import (
	"encoding/json"
	"io"
	"net/http"

	gql "github.com/graphql-go/graphql"
)

// Handler serves POST /api/v1/graphql.
type Handler struct {
	schema gql.Schema
}

// NewHandler creates a GraphQL HTTP handler.
func NewHandler(schema gql.Schema) *Handler {
	return &Handler{schema: schema}
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
	enc := json.NewEncoder(w)
	if len(result.Errors) > 0 {
		w.WriteHeader(http.StatusBadRequest)
	}
	_ = enc.Encode(result)
}
