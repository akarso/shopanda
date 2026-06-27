package exporter

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/legal"
	"github.com/akarso/shopanda/internal/domain/order"
)

// OssResult holds the summary of an OSS export run.
type OssResult struct {
	Rows int
}

// OssExportOptions controls OSS CSV export behavior.
type OssExportOptions struct {
	StoreID string
	From    time.Time
	To      time.Time
	Summary bool
}

// OssExporter writes OSS/IOSS tax breakdown CSV for merchant accounting tools.
type OssExporter struct {
	orders order.OrderRepository
	config legal.ConfigGetter
}

// NewOssExporter creates an OssExporter.
func NewOssExporter(orders order.OrderRepository, config legal.ConfigGetter) *OssExporter {
	return &OssExporter{orders: orders, config: config}
}

var ossDetailHeader = []string{
	"order_id",
	"order_date",
	"destination_country",
	"currency",
	"subtotal",
	"tax",
	"total",
}

var ossSummaryHeader = []string{
	"destination_country",
	"currency",
	"order_count",
	"subtotal",
	"tax",
	"total",
}

// Export writes OSS tax rows to w in CSV format.
func (exp *OssExporter) Export(ctx context.Context, w io.Writer, opts OssExportOptions) (*OssResult, error) {
	if exp.orders == nil {
		return nil, fmt.Errorf("oss export: orders repository must not be nil")
	}
	if !opts.To.After(opts.From) {
		return nil, fmt.Errorf("oss export: to must be after from")
	}
	if err := legal.EnsureOssExportEnabled(ctx, exp.config, opts.StoreID); err != nil {
		return nil, fmt.Errorf("oss export: %w", err)
	}

	rows, err := exp.orders.ListPaidTaxSnapshots(ctx, opts.From, opts.To)
	if err != nil {
		return nil, fmt.Errorf("oss export: list orders: %w", err)
	}

	writer := csv.NewWriter(w)
	header := ossDetailHeader
	if opts.Summary {
		header = ossSummaryHeader
	}
	if err := writer.Write(header); err != nil {
		return nil, fmt.Errorf("oss export: write header: %w", err)
	}

	result := &OssResult{}
	if opts.Summary {
		result.Rows, err = exp.writeSummaryRows(writer, rows)
	} else {
		result.Rows, err = exp.writeDetailRows(writer, rows)
	}
	if err != nil {
		return nil, err
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("oss export: flush csv: %w", err)
	}
	return result, nil
}

func (exp *OssExporter) writeDetailRows(writer *csv.Writer, rows []order.TaxSnapshotRow) (int, error) {
	count := 0
	for _, row := range rows {
		total := row.SubtotalAmount + row.TaxAmount
		record := []string{
			row.OrderID,
			row.CreatedAt.UTC().Format(time.RFC3339),
			row.DestinationCountry,
			row.Currency,
			formatMoneyMinor(row.SubtotalAmount),
			formatMoneyMinor(row.TaxAmount),
			formatMoneyMinor(total),
		}
		if err := writer.Write(record); err != nil {
			return count, fmt.Errorf("oss export: write row: %w", err)
		}
		count++
	}
	return count, nil
}

type ossSummaryKey struct {
	country  string
	currency string
}

type ossSummaryAgg struct {
	country  string
	currency string
	orders   int
	subtotal int64
	tax      int64
}

func (exp *OssExporter) writeSummaryRows(writer *csv.Writer, rows []order.TaxSnapshotRow) (int, error) {
	agg := make(map[ossSummaryKey]*ossSummaryAgg)
	for _, row := range rows {
		key := ossSummaryKey{country: row.DestinationCountry, currency: row.Currency}
		entry, ok := agg[key]
		if !ok {
			entry = &ossSummaryAgg{country: row.DestinationCountry, currency: row.Currency}
			agg[key] = entry
		}
		entry.orders++
		entry.subtotal += row.SubtotalAmount
		entry.tax += row.TaxAmount
	}

	keys := make([]ossSummaryKey, 0, len(agg))
	for key := range agg {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].country != keys[j].country {
			return keys[i].country < keys[j].country
		}
		return keys[i].currency < keys[j].currency
	})

	count := 0
	for _, key := range keys {
		entry := agg[key]
		total := entry.subtotal + entry.tax
		record := []string{
			entry.country,
			entry.currency,
			strconv.Itoa(entry.orders),
			formatMoneyMinor(entry.subtotal),
			formatMoneyMinor(entry.tax),
			formatMoneyMinor(total),
		}
		if err := writer.Write(record); err != nil {
			return count, fmt.Errorf("oss export: write summary row: %w", err)
		}
		count++
	}
	return count, nil
}

func formatMoneyMinor(amount int64) string {
	sign := ""
	if amount < 0 {
		sign = "-"
		amount = -amount
	}
	major := amount / 100
	minor := amount % 100
	return sign + strconv.FormatInt(major, 10) + "." + fmt.Sprintf("%02d", minor)
}

// ParseReportDate parses YYYY-MM-DD in UTC. Empty input returns zero time.
func ParseReportDate(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q: use YYYY-MM-DD", raw)
	}
	return t.UTC(), nil
}

// ReportDateRangeEnd returns the exclusive upper bound for a report ending on date.
func ReportDateRangeEnd(date time.Time) time.Time {
	if date.IsZero() {
		return time.Time{}
	}
	return date.Add(24 * time.Hour)
}
