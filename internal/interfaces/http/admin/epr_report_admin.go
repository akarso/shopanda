package admin

import (
	"bytes"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
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
		storeID := ResolveStoreScopeID(r)

		var buf bytes.Buffer
		if _, err := h.exporter.Export(r.Context(), &buf, exporter.EprExportOptions{
			StoreID:      storeID,
			IncludeEmpty: includeEmpty,
		}); err != nil {
			httpshared.JSONError(w, apperror.Wrap(apperror.CodeInternal, "epr export failed", err))
			return
		}

		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="epr-packaging.csv"`)
		_, _ = w.Write(buf.Bytes())
	}
}
