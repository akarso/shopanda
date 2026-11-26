package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/invoice"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/jackc/pgx/v5/pgconn"
)

// Compile-time check that InvoiceRepo implements invoice.InvoiceRepository.
var _ invoice.InvoiceRepository = (*InvoiceRepo)(nil)

// InvoiceRepo implements invoice.InvoiceRepository using PostgreSQL.
type InvoiceRepo struct {
	db *sql.DB
}

// NewInvoiceRepo returns a new InvoiceRepo backed by db.
func NewInvoiceRepo(db *sql.DB) (*InvoiceRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewInvoiceRepo: nil *sql.DB")
	}
	return &InvoiceRepo{db: db}, nil
}

// invoiceScanner abstracts *sql.Row and *sql.Rows for shared hydration.
type invoiceScanner interface {
	Scan(dest ...interface{}) error
}

// hydrateInvoice reads an invoice header from any scanner.
func hydrateInvoice(s invoiceScanner) (*invoice.Invoice, error) {
	var id, orderID, customerID, status, currency string
	var invoiceNumber, subtotalAmount, taxAmount, totalAmount int64
	var createdAt time.Time
	var sellerRaw []byte
	err := s.Scan(
		&id, &invoiceNumber, &orderID, &customerID,
		&status, &currency,
		&subtotalAmount, &taxAmount, &totalAmount,
		&createdAt, &sellerRaw,
	)
	if err != nil {
		return nil, err
	}
	sub, err := shared.NewMoney(subtotalAmount, currency)
	if err != nil {
		return nil, fmt.Errorf("invoice_repo: subtotal money: %w", err)
	}
	tax, err := shared.NewMoney(taxAmount, currency)
	if err != nil {
		return nil, fmt.Errorf("invoice_repo: tax money: %w", err)
	}
	tot, err := shared.NewMoney(totalAmount, currency)
	if err != nil {
		return nil, fmt.Errorf("invoice_repo: total money: %w", err)
	}
	inv := invoice.NewInvoiceFromDB(id, invoiceNumber, orderID, customerID, currency,
		sub, tax, tot, createdAt)
	if err := inv.SetStatusFromDB(status); err != nil {
		return nil, err
	}
	seller, err := decodeSellerSnapshot(sellerRaw)
	if err != nil {
		return nil, err
	}
	inv.SetSellerSnapshotFromDB(seller)
	return &inv, nil
}

func decodeSellerSnapshot(raw []byte) (invoice.SellerBlock, error) {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "{}" {
		return invoice.SellerBlock{}, nil
	}
	var seller invoice.SellerBlock
	if err := json.Unmarshal(raw, &seller); err != nil {
		return invoice.SellerBlock{}, fmt.Errorf("invoice_repo: seller_snapshot: %w", err)
	}
	return seller, nil
}

const invoiceCols = `id, invoice_number, order_id, customer_id, status, currency,
	subtotal_amount, tax_amount, total_amount, created_at, seller_snapshot`

// FindByID returns an invoice with its items by ID.
// Returns (nil, nil) when not found.
func (r *InvoiceRepo) FindByID(ctx context.Context, id string) (*invoice.Invoice, error) {
	if id == "" {
		return nil, fmt.Errorf("invoice_repo: find: empty id")
	}
	q := `SELECT ` + invoiceCols + ` FROM invoices WHERE id = $1`
	inv, err := hydrateInvoice(r.db.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("invoice_repo: find by id: %w", err)
	}
	items, err := r.loadItems(ctx, inv.ID())
	if err != nil {
		return nil, err
	}
	if err := inv.SetItemsFromDB(items); err != nil {
		return nil, fmt.Errorf("invoice_repo: set items: %w", err)
	}
	return inv, nil
}

// FindByOrderID returns the invoice for an order.
// Returns (nil, nil) when not found.
func (r *InvoiceRepo) FindByOrderID(ctx context.Context, orderID string) (*invoice.Invoice, error) {
	if orderID == "" {
		return nil, fmt.Errorf("invoice_repo: find by order: empty order id")
	}
	q := `SELECT ` + invoiceCols + ` FROM invoices WHERE order_id = $1`
	inv, err := hydrateInvoice(r.db.QueryRowContext(ctx, q, orderID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("invoice_repo: find by order id: %w", err)
	}
	items, err := r.loadItems(ctx, inv.ID())
	if err != nil {
		return nil, err
	}
	if err := inv.SetItemsFromDB(items); err != nil {
		return nil, fmt.Errorf("invoice_repo: set items: %w", err)
	}
	return inv, nil
}

// Save persists an invoice and its items (insert-only).
// Assigns InvoiceNumber from the DB sequence.
func (r *InvoiceRepo) Save(ctx context.Context, inv *invoice.Invoice) error {
	if inv == nil {
		return fmt.Errorf("invoice_repo: save: invoice must not be nil")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("invoice_repo: begin tx: %w", err)
	}
	defer tx.Rollback()

	sellerJSON, err := json.Marshal(inv.Seller())
	if err != nil {
		return fmt.Errorf("invoice_repo: marshal seller_snapshot: %w", err)
	}
	const insertInvoice = `INSERT INTO invoices
		(id, order_id, customer_id, status, currency,
		 subtotal_amount, tax_amount, total_amount, created_at, seller_snapshot)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING invoice_number`
	var invoiceNumber int64
	err = tx.QueryRowContext(ctx, insertInvoice,
		inv.ID(), inv.OrderID(), inv.CustomerID(),
		string(inv.Status()), inv.Currency(),
		inv.SubtotalAmount().Amount(), inv.TaxAmount().Amount(), inv.TotalAmount().Amount(),
		inv.CreatedAt(), sellerJSON,
	).Scan(&invoiceNumber)
	if err != nil {
		if mapped := mapInvoiceInsertError(err); mapped != nil {
			return mapped
		}
		return fmt.Errorf("invoice_repo: insert invoice: %w", err)
	}

	items := inv.Items()
	if len(items) > 0 {
		const insertItem = `INSERT INTO invoice_items
			(invoice_id, variant_id, sku, name, quantity, unit_price, currency)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`
		stmt, err := tx.PrepareContext(ctx, insertItem)
		if err != nil {
			return fmt.Errorf("invoice_repo: prepare item insert: %w", err)
		}
		defer stmt.Close()
		for i := range items {
			it := &items[i]
			_, err = stmt.ExecContext(ctx,
				inv.ID(), it.VariantID, it.SKU, it.Name, it.Quantity,
				it.UnitPrice.Amount(), it.UnitPrice.Currency(),
			)
			if err != nil {
				return fmt.Errorf("invoice_repo: insert item %q: %w", it.VariantID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("invoice_repo: commit: %w", err)
	}
	inv.SetInvoiceNumberFromDB(invoiceNumber)
	return nil
}

// invoicesOrderIDUniq is the UNIQUE constraint on invoices.order_id.
const invoicesOrderIDUniq = "invoices_order_id_key"

// mapInvoiceInsertError maps order_id unique violations to Conflict so
// concurrent order.paid can repair. Other 23505s (id, invoice_number) are
// left unmapped so callers do not FindByOrderID and get a false empty row.
func mapInvoiceInsertError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return nil
	}
	if pgErr.ConstraintName == invoicesOrderIDUniq {
		return apperror.Conflict("invoice already exists for this order")
	}
	return nil
}

// Delete removes an invoice and its items so a failed PDF finalize can retry.
func (r *InvoiceRepo) Delete(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("invoice_repo: delete: empty id")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("invoice_repo: delete begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM invoice_items WHERE invoice_id = $1`, id); err != nil {
		return fmt.Errorf("invoice_repo: delete items: %w", err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM invoices WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("invoice_repo: delete invoice: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("invoice_repo: delete rows: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("invoice_repo: delete: invoice %s not found", id)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("invoice_repo: delete commit: %w", err)
	}
	return nil
}

// loadItems fetches all items for an invoice.
func (r *InvoiceRepo) loadItems(ctx context.Context, invoiceID string) ([]invoice.Item, error) {
	const q = `SELECT variant_id, sku, name, quantity, unit_price, currency
		FROM invoice_items WHERE invoice_id = $1`
	rows, err := r.db.QueryContext(ctx, q, invoiceID)
	if err != nil {
		return nil, fmt.Errorf("invoice_repo: load items: %w", err)
	}
	defer rows.Close()
	return scanInvoiceItems(rows)
}

// scanInvoiceItems reads item rows into a slice.
func scanInvoiceItems(rows *sql.Rows) ([]invoice.Item, error) {
	var items []invoice.Item
	for rows.Next() {
		var variantID, sku, name string
		var quantity int
		var priceAmount int64
		var priceCurrency string
		if err := rows.Scan(&variantID, &sku, &name, &quantity, &priceAmount, &priceCurrency); err != nil {
			return nil, fmt.Errorf("invoice_repo: scan item: %w", err)
		}
		unitPrice, err := shared.NewMoney(priceAmount, priceCurrency)
		if err != nil {
			return nil, fmt.Errorf("invoice_repo: item money: %w", err)
		}
		items = append(items, invoice.Item{
			VariantID: variantID,
			SKU:       sku,
			Name:      name,
			Quantity:  quantity,
			UnitPrice: unitPrice,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("invoice_repo: item rows: %w", err)
	}
	return items, nil
}
