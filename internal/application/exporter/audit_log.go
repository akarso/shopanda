package exporter

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	domainadmin "github.com/akarso/shopanda/internal/domain/admin"
)

const auditExportBatchSize = 500

// AuditLogExportFormat selects CSV or JSON output.
type AuditLogExportFormat string

const (
	AuditLogFormatCSV  AuditLogExportFormat = "csv"
	AuditLogFormatJSON AuditLogExportFormat = "json"
)

// AuditLogExportOptions filters exported audit rows.
type AuditLogExportOptions struct {
	Format       AuditLogExportFormat
	Action       string
	ResourceType string
	ResourceID   string
	From         *time.Time
	To           *time.Time
}

// AuditLogExportResult summarizes an export run.
type AuditLogExportResult struct {
	Entries int
}

// AuditLogExporter writes admin audit log rows to CSV or JSON.
type AuditLogExporter struct {
	repo domainadmin.AuditLogRepository
}

// NewAuditLogExporter creates an AuditLogExporter.
func NewAuditLogExporter(repo domainadmin.AuditLogRepository) *AuditLogExporter {
	if repo == nil {
		panic("exporter: audit log repository must not be nil")
	}
	return &AuditLogExporter{repo: repo}
}

// Export writes matching audit rows to w.
func (exp *AuditLogExporter) Export(ctx context.Context, w io.Writer, opts AuditLogExportOptions) (*AuditLogExportResult, error) {
	format := opts.Format
	if format == "" {
		format = AuditLogFormatCSV
	}
	switch format {
	case AuditLogFormatCSV:
		return exp.exportCSV(ctx, w, opts)
	case AuditLogFormatJSON:
		return exp.exportJSON(ctx, w, opts)
	default:
		return nil, fmt.Errorf("audit export: unsupported format %q", format)
	}
}

func (exp *AuditLogExporter) exportCSV(ctx context.Context, w io.Writer, opts AuditLogExportOptions) (*AuditLogExportResult, error) {
	writer := csv.NewWriter(w)
	header := []string{
		"id", "created_at", "admin_id", "action", "resource_type", "resource_id",
		"result", "error", "store_id", "language", "currency", "metadata",
	}
	if err := writer.Write(header); err != nil {
		return nil, fmt.Errorf("audit export: write header: %w", err)
	}

	result := &AuditLogExportResult{}
	offset := 0
	for {
		entries, err := exp.repo.List(ctx, exp.listFilter(opts, offset))
		if err != nil {
			return nil, fmt.Errorf("audit export: list: %w", err)
		}
		if len(entries) == 0 {
			break
		}
		for _, entry := range entries {
			meta := ""
			if len(entry.Metadata) > 0 {
				raw, err := json.Marshal(entry.Metadata)
				if err != nil {
					return nil, fmt.Errorf("audit export: marshal metadata: %w", err)
				}
				meta = string(raw)
			}
			row := []string{
				sanitizeCSVCell(entry.ID),
				sanitizeCSVCell(entry.CreatedAt.UTC().Format(time.RFC3339)),
				sanitizeCSVCell(entry.AdminID),
				sanitizeCSVCell(entry.Action),
				sanitizeCSVCell(entry.ResourceType),
				sanitizeCSVCell(entry.ResourceID),
				sanitizeCSVCell(entry.Result),
				sanitizeCSVCell(entry.ErrorMessage),
				sanitizeCSVCell(entry.StoreID),
				sanitizeCSVCell(entry.Language),
				sanitizeCSVCell(entry.Currency),
				sanitizeCSVCell(meta),
			}
			if err := writer.Write(row); err != nil {
				return nil, fmt.Errorf("audit export: write row: %w", err)
			}
			result.Entries++
		}
		if len(entries) < auditExportBatchSize {
			break
		}
		offset += len(entries)
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("audit export: flush csv: %w", err)
	}
	return result, nil
}

func (exp *AuditLogExporter) exportJSON(ctx context.Context, w io.Writer, opts AuditLogExportOptions) (*AuditLogExportResult, error) {
	all := make([]auditLogExportItem, 0)
	offset := 0
	for {
		entries, err := exp.repo.List(ctx, exp.listFilter(opts, offset))
		if err != nil {
			return nil, fmt.Errorf("audit export: list: %w", err)
		}
		if len(entries) == 0 {
			break
		}
		for _, entry := range entries {
			all = append(all, auditLogExportItem{
				ID:           entry.ID,
				CreatedAt:    entry.CreatedAt.UTC(),
				AdminID:      entry.AdminID,
				Action:       entry.Action,
				ResourceType: entry.ResourceType,
				ResourceID:   entry.ResourceID,
				Result:       entry.Result,
				Error:        entry.ErrorMessage,
				StoreID:      entry.StoreID,
				Language:     entry.Language,
				Currency:     entry.Currency,
				Metadata:     entry.Metadata,
			})
		}
		if len(entries) < auditExportBatchSize {
			break
		}
		offset += len(entries)
	}

	raw, err := json.Marshal(all)
	if err != nil {
		return nil, fmt.Errorf("audit export: marshal json: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		return nil, fmt.Errorf("audit export: write json: %w", err)
	}
	return &AuditLogExportResult{Entries: len(all)}, nil
}

func (exp *AuditLogExporter) listFilter(opts AuditLogExportOptions, offset int) domainadmin.AuditLogFilter {
	return domainadmin.AuditLogFilter{
		Action:       strings.TrimSpace(opts.Action),
		ResourceType: strings.TrimSpace(opts.ResourceType),
		ResourceID:   strings.TrimSpace(opts.ResourceID),
		From:         opts.From,
		To:           opts.To,
		Offset:       offset,
		Limit:        auditExportBatchSize,
	}
}

type auditLogExportItem struct {
	ID           string                 `json:"id"`
	CreatedAt    time.Time              `json:"created_at"`
	AdminID      string                 `json:"admin_id"`
	Action       string                 `json:"action"`
	ResourceType string                 `json:"resource_type"`
	ResourceID   string                 `json:"resource_id"`
	Result       string                 `json:"result"`
	Error        string                 `json:"error,omitempty"`
	StoreID      string                 `json:"store_id,omitempty"`
	Language     string                 `json:"language,omitempty"`
	Currency     string                 `json:"currency,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// ParseAuditExportFormat normalizes the format query parameter.
func ParseAuditExportFormat(raw string) (AuditLogExportFormat, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "csv":
		return AuditLogFormatCSV, nil
	case "json":
		return AuditLogFormatJSON, nil
	default:
		return "", fmt.Errorf("format must be csv or json")
	}
}
