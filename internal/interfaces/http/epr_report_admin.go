package http

import (
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/application/exporter"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type EprReportHandler struct {
	exporter *exporter.EprExporter
}

// NewEprReportHandler creates an EprReportHandler.
func NewEprReportHandler(exp *exporter.EprExporter) *EprReportHandler {
	if exp == nil {
		panic("http: epr exporter must not be nil")
	}
	return &EprReportHandler{exporter: exp}
}

// Export handles GET /api/v1/admin/reports/epr.
func (h *EprReportHandler) Export() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		includeEmpty := strings.EqualFold(r.URL.Query().Get("include_empty"), "true")
		storeID := resolveStoreScopeID(r)

		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="epr-packaging.csv"`)

		if _, err := h.exporter.Export(r.Context(), w, exporter.EprExportOptions{
			StoreID:      storeID,
			IncludeEmpty: includeEmpty,
		}); err != nil {
			JSONError(w, apperror.Wrap(apperror.CodeInternal, "epr export failed", err))
			return
		}
	}
}
