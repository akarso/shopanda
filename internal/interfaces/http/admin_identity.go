package http

import (
	"net/http"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
)

func adminIDFromRequest(r *http.Request) string {
	ac, err := adminapp.FromContext(r.Context())
	if err != nil || ac == nil || ac.AdminID == "" {
		return "system"
	}
	return ac.AdminID
}
