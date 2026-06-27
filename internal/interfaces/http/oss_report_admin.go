package http

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/application/exporter"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// OssReportHandler serves OSS/IOSS tax CSV exports.
type OssReportHandler struct {
	exporter *exporter.OssExporter
}

// NewOssReportHandler creates an OssReportHandler.
func NewOssReportHandler(exp *exporter.OssExporter) *OssReportHandler {
	if exp == nil {
		panic("http: oss exporter must not be nil")
	}
	return &OssReportHandler{exporter: exp}
}

// Export handles GET /api/v1/admin/reports/oss.
func (h *OssReportHandler) Export() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		from, to, err := parseOssReportRange(r)
		if err != nil {
			JSONError(w, apperror.Validation(err.Error()))
			return
		}
		summary := strings.EqualFold(r.URL.Query().Get("summary"), "true")

		var buf bytes.Buffer
		if _, err := h.exporter.Export(r.Context(), &buf, exporter.OssExportOptions{
			From:    from,
			To:      to,
			Summary: summary,
		}); err != nil {
			JSONError(w, apperror.Wrap(apperror.CodeInternal, "oss export failed", err))
			return
		}

		filename := "oss-tax-detail.csv"
		if summary {
			filename = "oss-tax-summary.csv"
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
		_, _ = w.Write(buf.Bytes())
	}
}

func parseOssReportRange(r *http.Request) (from, to time.Time, err error) {
	fromRaw := strings.TrimSpace(r.URL.Query().Get("from"))
	toRaw := strings.TrimSpace(r.URL.Query().Get("to"))

	fromDate, err := exporter.ParseReportDate(fromRaw)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	toDate, err := exporter.ParseReportDate(toRaw)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	if fromDate.IsZero() && toDate.IsZero() {
		now := time.Now().UTC()
		year, month, _ := now.Date()
		quarterStartMonth := time.Month(((int(month)-1)/3)*3 + 1)
		fromDate = time.Date(year, quarterStartMonth, 1, 0, 0, 0, 0, time.UTC)
		toDate = now
	} else if fromDate.IsZero() || toDate.IsZero() {
		return time.Time{}, time.Time{}, errors.New("from and to are required unless both are omitted (defaults to current calendar quarter)")
	}

	toExclusive := exporter.ReportDateRangeEnd(toDate)
	if !toExclusive.After(fromDate) {
		return time.Time{}, time.Time{}, errors.New("to must be on or after from")
	}
	return fromDate, toExclusive, nil
}
